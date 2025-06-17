package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
	rpb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

type PaxosState struct {
	highestPromisedID  int64
	acceptedProposedID int64
	acceptedValue      []byte
}

type PaxosServer struct {
	pb.UnimplementedPaxosServer
	mu             sync.RWMutex
	state          map[string]*PaxosState
	registryClient rpb.RegistryClient
	nodeAddress    string
}

func NewPaxosServer(registryClient rpb.RegistryClient, nodeAddress string) *PaxosServer {
	return &PaxosServer{
		state:          make(map[string]*PaxosState),
		registryClient: registryClient,
		nodeAddress:    nodeAddress,
	}
}

func (s *PaxosServer) GetState(key string) *PaxosState {
	if _, ok := s.state[key]; !ok {
		s.state[key] = &PaxosState{}
	}
	return s.state[key]
}

func (s *PaxosServer) Prepare(ctx context.Context, req *pb.PrepareRequest) (*pb.PrepareResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.GetState(req.Key)

	log.Printf("[Acceptor] Recebido Prepare para chave '%s' com proposal_id %d. Estado atual: promised=%d, accepted_n=%d\n", req.Key, req.ProposalId, state.highestPromisedID, state.acceptedProposedID)

	if req.ProposalId > state.highestPromisedID {
		state.highestPromisedID = req.ProposalId

		return &pb.PrepareResponse{
			Success:            true,
			AcceptedProposalId: state.acceptedProposedID,
			AcceptedValue:      state.acceptedValue,
		}, nil
	} else {
		return &pb.PrepareResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Já prometi para uma proposta %d maior ou igual a %d", state.highestPromisedID, req.ProposalId),
		}, nil
	}
}

func (s *PaxosServer) Accept(ctx context.Context, req *pb.AcceptRequest) (*pb.AcceptResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.GetState(req.Key)

	log.Printf("[Acceptor] Recebido Accept para chave '%s' com proposal_id %d e valor '%s'. Estado atual: promised=%d, accepted_n=%d\n", req.Key, req.ProposalId, string(req.Value), state.highestPromisedID, state.acceptedProposedID)

	if req.ProposalId >= state.highestPromisedID {
		state.highestPromisedID = req.ProposalId
		state.acceptedProposedID = req.ProposalId
		state.acceptedValue = req.Value

		log.Printf("[Acceptor] Aceito ProposalId %d para chave '%s' com valor '%s'\n",
			req.ProposalId, req.Key, string(req.Value))

		return &pb.AcceptResponse{
			Success:           true,
			CurrentProposalId: state.highestPromisedID,
		}, nil
	} else {
		return &pb.AcceptResponse{
			Success:           false,
			ErrorMessage:      fmt.Sprintf("Já aceitei uma proposta %d maior ou igual a %d", state.acceptedProposedID, req.ProposalId),
			CurrentProposalId: state.highestPromisedID,
		}, nil
	}
}

func (s *PaxosServer) simulateProposer(key string, value []byte, proposalID int64) {
	log.Printf("[Proposer] Iniciando proposta para chave '%s' com valor '%s' e proposal_id %d\n", key, string(value), proposalID)

	registryResp, err := s.registryClient.ListAll(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.Printf("[Proposer] Erro ao listar serviços do registry: %v", err)
		return
	}

	quorumSize := (len(registryResp.Services) / 2) + 1

	if quorumSize == 0 {
		quorumSize = 1
	}

	log.Printf("[Proposer] Total de %d serviços registrados, quorum necessário: %d\n", len(registryResp.Services), quorumSize)

	var (
		promisesReceived   int
		highestAcceptedN   int64
		highestAcceptedVal []byte
		mu                 sync.Mutex
		wg                 sync.WaitGroup
	)

	for _, service := range registryResp.Services {
		if service.Address == s.nodeAddress {
			log.Printf("[Proposer] (Self) Simulating Prepare response from self for key '%s' with proposal_id %d\n", key, proposalID)

			resp, _ := s.Prepare(context.Background(), &pb.PrepareRequest{
				Key:        key,
				ProposalId: proposalID,
			})

			if resp.Success {
				mu.Lock()
				promisesReceived++
				if resp.AcceptedProposalId > highestAcceptedN {
					highestAcceptedN = resp.AcceptedProposalId
					highestAcceptedVal = resp.AcceptedValue
				}
				mu.Unlock()
			} else {
				log.Printf("[Proposer] (Self) Prepare falhou: %s\n", resp.ErrorMessage)
			}
			continue
		}
		wg.Add(1)

		go func(addr string) {
			defer wg.Done()

			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Fatalf("did not connect: %v", &err)
			}

			defer conn.Close()

			client := pb.NewPaxosClient(conn)

			log.Printf("[Proposer] Simulando Prepare para chave '%s' com proposal_id %d no serviço %s\n", key, proposalID, addr)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := client.Prepare(ctx, &pb.PrepareRequest{
				Key:        key,
				ProposalId: proposalID,
			})
			if err != nil {
				log.Printf("[Proposer] Erro ao enviar Prepare para %s: %v", addr, err)
				return
			}

			if resp.Success {
				mu.Lock()
				promisesReceived++
				if resp.AcceptedProposalId > highestAcceptedN {
					highestAcceptedN = resp.AcceptedProposalId
					highestAcceptedVal = resp.AcceptedValue
				}
				mu.Unlock()
				log.Printf("[Proposer] Recebida Promise de %s para key '%s'. Accepted ID: %d, Value: '%s'\n", addr, key, resp.AcceptedProposalId, string(resp.AcceptedValue))
			} else {
				log.Printf("[Proposer] Acceptor %s recusou Prepare para key '%s': %s\n", addr, key, resp.ErrorMessage)
			}
		}(service.Address)
	}

	wg.Wait()

	if promisesReceived < quorumSize {
		log.Printf("[Proposer] Não recebeu promessas suficientes (%d) para atingir o quorum de %d. Abortando proposta.\n", promisesReceived, quorumSize)
		return
	}

	finalValue := value

	if highestAcceptedN > 0 {
		log.Printf("[Proposer] Um valor previamente aceito (%s) foi encontrado para chave '%s'. Usando este valor.\n", string(highestAcceptedVal), key)
		finalValue = highestAcceptedVal
	} else {
		log.Printf("[Proposer] Nenhum valor pré-existente para chave '%s'. Usando valor original '%s'.\n", key, string(finalValue))
	}

	var (
		acceptsReceived int
		wgAccepts       sync.WaitGroup
	)

	for _, service := range registryResp.Services {
		if service.Address == s.nodeAddress {
			log.Printf("[Proposer] (Self) Simulating Accept response from self for key '%s'\n", key)
			resp, _ := s.Accept(context.Background(), &pb.AcceptRequest{
				ProposalId: proposalID,
				Key:        key,
				Value:      finalValue,
			})
			if resp.Success {
				mu.Lock()
				acceptsReceived++
				mu.Unlock()
			} else {
				log.Printf("[Proposer] (Self) Acceptor recusou Accept para key '%s': %s\n", key, resp.ErrorMessage)
			}
			continue
		}

		wgAccepts.Add(1)

		go func(addr string) {
			defer wgAccepts.Done()

			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[Proposer] Não foi possível conectar ao Acceptor %s: %v\n", addr, err)
				return
			}
			defer conn.Close()

			client := pb.NewPaxosClient(conn)
			log.Printf("[Proposer] Simulando Accept para chave '%s' com proposal_id %d no serviço %s\n", key, proposalID, addr)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := client.Accept(ctx, &pb.AcceptRequest{
				ProposalId: proposalID,
				Key:        key,
				Value:      finalValue,
			})
			if err != nil {
				log.Printf("[Proposer] Erro ao enviar Accept para %s: %v", addr, err)
				return
			}

			if resp.Success {
				mu.Lock()
				acceptsReceived++
				mu.Unlock()
				log.Printf("[Proposer] Recebida Aceitação de %s para key '%s'. Proposal ID: %d\n", addr, key, resp.CurrentProposalId)
			} else {
				log.Printf("[Proposer] Acceptor %s recusou Accept para key '%s': %s\n", addr, key, resp.ErrorMessage)
			}
		}(service.Address)
	}

	wgAccepts.Wait()

	if acceptsReceived < quorumSize {
		log.Printf("[Proposer] Não obteve Quorum de Accepted (%d/%d). Proposta para chave '%s' NÃO DECIDIDA.\n", acceptsReceived, quorumSize, key)
		return
	}
	log.Printf("[Proposer] Proposta para chave '%s' DECIDIDA com sucesso! Valor final: '%s'\n", key, string(finalValue))
	s.GetState(key).acceptedValue = finalValue
	s.GetState(key).acceptedProposedID = proposalID
	s.GetState(key).highestPromisedID = proposalID
}

var (
	registryAddr = flag.String("registryAddr", "localhost:50051", "Registry Server Address")
	name         = flag.String("nodeName", "node-1", "the name of the node")
	nodeAddress  = flag.String("nodeAddr", "localhost:8080", "the address of the node (e.g., localhost:8080)")
	isProposer   = flag.Bool("isProposer", false, "Set to true if this node should act as a proposer for a test key")
	key          = flag.String("key", "test_key", "the key to be used for the proposal (only relevant if isProposer is true)")
	value        = flag.String("value", "test_value", "the value to be used for the proposal (only relevant if isProposer is true)")
	proposalID   = flag.Int64("proposalID", 1, "the proposal ID to be used for the proposal (only relevant if isProposer is true)")
)

func main() {
	flag.Parse()

	log.Printf("Iniciando Paxos Node '%s' no endereço '%s'\n", *name, *nodeAddress)

	conn, err := grpc.NewClient(*registryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao Registry: %v", err)
	}
	defer conn.Close()

	// Connect to registry server
	registryClient := rpb.NewRegistryClient(conn)

	log.Printf("Conectado ao Registry em %s", *registryAddr)

	// Register the node with the registry

	_, err = registryClient.Register(context.Background(), &rpb.RegisterRequest{
		Name:    *name,
		Address: *nodeAddress,
	})
	if err != nil {
		log.Fatalf("Não foi possível registrar o nó: %v", err)
	}
	log.Printf("Nó '%s' registrado com sucesso no Registry", *name)

	// Start the Paxos server
	lis, err := net.Listen("tcp", *nodeAddress)
	if err != nil {
		log.Fatalf("Falha ao ouvir na porta %s: %v", *nodeAddress, err)
	}
	s := grpc.NewServer()
	paxosNode := NewPaxosServer(registryClient, *nodeAddress) // Passar o cliente do registry e o próprio endereço
	pb.RegisterPaxosServer(s, paxosNode)
	log.Printf("Paxos Server ouvindo em %v\n", lis.Addr())

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Falha ao iniciar o servidor: %v", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second) // A cada 5 segundos
		defer ticker.Stop()
		for range ticker.C {
			_, err := registryClient.Heartbeat(context.Background(), &rpb.HeartbeatRequest{
				Name:    *name,
				Address: *nodeAddress,
			})
			if err != nil {
				log.Printf("Erro no heartbeat para %s: %v. Tentando re-registrar...\n", *name, err)
				_, err = registryClient.Register(context.Background(), &rpb.RegisterRequest{
					Name:    *name,
					Address: *nodeAddress,
				})
				if err != nil {
					log.Printf("Falha ao re-registrar nó %s: %v\n", *name, err)
				} else {
					log.Printf("Nó %s re-registrado com sucesso.\n", *name)
				}
			} else {
				// log.Printf("Heartbeat enviado por %s.\n", nodeName)
			}
		}
	}()

	log.Printf("Paxos Node '%s' iniciado com sucesso no endereço '%s'.\n", *name, *nodeAddress)

	// If this node is a proposer, simulate a proposal
	if *isProposer {
		log.Printf("Este nó está configurado como Proposer. Iniciando simulação de proposta...\n")
		time.Sleep(3 * time.Second)
		// Simulate a proposer for testing
		paxosNode.simulateProposer(*key, []byte(*value), *proposalID)
		log.Printf("Proposta simulada para chave '%s' com valor '%s'.\n", *key, *value)
	} else {
		log.Printf("Este nó está configurado como Acceptor. Aguardando propostas...\n")
	}
	select {} // Keep the server running indefinitely
}
