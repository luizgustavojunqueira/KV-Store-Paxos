package internal

import (
	"context"
	"log"
	"sync"
	"time"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *PaxosServer) SimulateProposer(key string, value []byte, proposalID int64) {
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
