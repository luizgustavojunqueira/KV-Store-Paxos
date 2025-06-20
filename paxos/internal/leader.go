// Leader election and management for Paxos protocol
package internal

import (
	"context"
	"fmt"
	"log"
	"sync" // Certifique-se de importar sync
	"time"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *PaxosServer) ProposeLeader(ctx context.Context, req *pb.ProposeLeaderRequest) (*pb.ProposeLeaderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[Acceptor-Leader] Recebido ProposeLeader para candidato '%s' com proposal_id %d. Estado: promised=%d, accepted_n=%d, accepted_leader=%s\n",
		req.CandidateAddress, req.ProposalId, s.leaderState.HighestPromisedID, s.leaderState.AcceptedProposedID, s.leaderState.AcceptedLeaderAddr)

	if req.ProposalId > s.leaderState.HighestPromisedID {
		s.leaderState.HighestPromisedID = req.ProposalId

		return &pb.ProposeLeaderResponse{
			Success:                        true,
			CurrentHighestLeaderProposalId: s.leaderState.HighestPromisedID,
			CurrentLeaderAddress:           s.leaderState.AcceptedLeaderAddr,
			HighestDecidedSlotId:           s.highestSlotID,
		}, nil
	} else {
		return &pb.ProposeLeaderResponse{
			Success:                        false,
			CurrentHighestLeaderProposalId: s.leaderState.HighestPromisedID,
			CurrentLeaderAddress:           s.leaderState.AcceptedLeaderAddr,
			HighestDecidedSlotId:           s.highestSlotID,
		}, fmt.Errorf("proposal_id %d é menor ou igual ao mais alto prometido %d", req.ProposalId, s.leaderState.HighestPromisedID)
	}
}

func (s *PaxosServer) LeaderElection() {
	s.mu.Lock()
	isAlreadyLeader := s.isLeader
	s.mu.Unlock()

	if isAlreadyLeader {
		log.Println("[LeaderElection] Já sou o líder, não preciso iniciar uma nova eleição.")
		return
	}

	log.Printf("[LeaderElection] Tentando se tornar líder para %s...\n", s.nodeName)

	// Aumentar o ID da proposta de liderança
	s.mu.Lock()
	s.leaderProposalID = time.Now().UnixNano()
	candidateProposalID := s.leaderProposalID
	s.mu.Unlock()

	registryResp, err := s.registryClient.ListAll(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.Printf("[LeaderElection] Erro ao listar serviços do registry: %v", err)
		return
	}

	quorumSize := (len(registryResp.Services) / 2) + 1
	if quorumSize == 0 {
		quorumSize = 1
	}
	log.Printf("[LeaderElection] Total de %d serviços, quorum para liderança: %d\n", len(registryResp.Services), quorumSize)

	var (
		promisesReceived int
		// highestAcceptedLeaderID string // O ID do líder aceito com o maior N_a
		highestAcceptedLeaderN int64 // O N_a do líder aceito mais alto
		maxHighestSlotID       int64 // O maior slot_id decidido entre todos os acceptors
		mu                     sync.Mutex
		wg                     sync.WaitGroup
	)

	// FASE 1 da Eleição: ProposeLeader (Prepare)
	for _, service := range registryResp.Services {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[LeaderElection] Não conectou a %s: %v\n", addr, err)
				return
			}
			defer conn.Close()
			client := pb.NewPaxosClient(conn)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req := &pb.ProposeLeaderRequest{
				ProposalId:       candidateProposalID,
				CandidateAddress: s.nodeAddress,
			}

			resp, err := client.ProposeLeader(ctx, req)
			if err != nil {
				log.Printf("[LeaderElection] Erro ao enviar ProposeLeader para %s: %v", addr, err)
				return
			}

			if resp.Success {
				mu.Lock()
				promisesReceived++
				// Lógica para aprender o líder e o log mais atualizados
				if resp.CurrentHighestLeaderProposalId > highestAcceptedLeaderN {
					highestAcceptedLeaderN = resp.CurrentHighestLeaderProposalId
					// highestAcceptedLeaderID = resp.CurrentLeaderAddress
				}
				if resp.HighestDecidedSlotId > maxHighestSlotID {
					maxHighestSlotID = resp.HighestDecidedSlotId
				}
				mu.Unlock()
				log.Printf("[LeaderElection] Recebeu Promise de %s. N_p=%d, N_a=%d, V_a='%s', MaxSlot=%d\n",
					addr, resp.CurrentHighestLeaderProposalId, resp.CurrentHighestLeaderProposalId, resp.CurrentLeaderAddress, resp.HighestDecidedSlotId)
			} else {
				log.Printf("[LeaderElection] Acceptor %s recusou ProposeLeader: %s\n", addr, resp.ErrorMessage)
				// Se recusou porque já prometeu para um ID maior, o candidato deve abortar ou tentar novamente com um ID maior.
				// Para esta versão, se recusar, consideramos falha temporária.
			}
		}(service.Address)
	}
	wg.Wait()

	if promisesReceived < quorumSize {
		log.Printf("[LeaderElection] Não obteve quorum de Promises (%d/%d). Falha na eleição.\n", promisesReceived, quorumSize)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	s.isLeader = true
	s.currentLeader = s.nodeName
	s.highestSlotID = maxHighestSlotID // O líder aprende o log mais atualizado
	log.Printf("[LeaderElection] %s foi eleito LÍDER com proposal ID %d! Highest Decided Slot: %d\n", s.nodeName, candidateProposalID, s.highestSlotID)
	s.mu.Unlock()

	go s.StartLeaderHeartbeats()
}

// StartLeaderHeartbeats é uma goroutine que o líder executa para manter sua liderança
func (s *PaxosServer) StartLeaderHeartbeats() {
	ticker := time.NewTicker(s.leaderTimeout / 3) // Frequência dos heartbeats (ex: 1/3 do timeout)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.RLock()
		if !s.isLeader { // Se não é mais líder (pode ter sido destituído)
			s.mu.RUnlock()
			return // Sai da goroutine de heartbeats
		}
		leaderID := s.currentLeader
		currentPropID := s.leaderProposalID
		highestSlot := s.highestSlotID
		s.mu.RUnlock()

		registryResp, err := s.registryClient.ListAll(context.Background(), &emptypb.Empty{})
		if err != nil {
			log.Printf("[LeaderHeartbeat] Erro ao listar serviços do registry: %v", err)
			continue
		}

		// Envia heartbeat para todos os peers (exceto ele mesmo)
		for _, service := range registryResp.Services {
			if service.Address == s.nodeAddress {
				continue // Não envia heartbeat para si mesmo
			}
			go func(addr string) {
				conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					log.Printf("[LeaderHeartbeat] Não conectou a %s: %v\n", addr, err)
					return
				}
				defer conn.Close()
				client := pb.NewPaxosClient(conn)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // Timeout menor para heartbeats
				defer cancel()

				req := &pb.LeaderHeartbeat{
					LeaderAddress:        leaderID,
					CurrentProposalId:    currentPropID,
					HighestDecidedSlotId: highestSlot,
				}

				resp, err := client.SendHeartbeat(ctx, req)
				if err != nil {
					log.Printf("[LeaderHeartbeat] Erro ao enviar heartbeat para %s: %v", addr, err)
					// Se o heartbeat falhar, pode significar que o peer caiu ou que a rede falhou.
					// O líder deve lidar com isso (ex: remover peer da lista ativa, ou tentar de novo).
					return
				}
				if !resp.Success {
					log.Printf("[LeaderHeartbeat] Peer %s recusou heartbeat: %s", addr, resp.ErrorMessage)
					// Se o peer recusou, pode ser que ele tenha visto um líder mais novo ou que esteja em processo de eleição.
					// O líder pode optar por iniciar uma nova eleição ou simplesmente ignorar.
				}
				// log.Printf("[LeaderHeartbeat] Heartbeat enviado para %s. Peer Highest Slot: %d\n", addr, resp.KnownHighestSlotId)
				// O líder pode usar resp.KnownHighestSlotId para iniciar a sincronização do log com peers atrasados.
			}(service.Address)
		}
	}
}

// SendHeartbeat (Método RPC no Acceptor) - Aceita heartbeats do líder
func (s *PaxosServer) SendHeartbeat(ctx context.Context, req *pb.LeaderHeartbeat) (*pb.LeaderHeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[Acceptor-Heartbeat] Recebido heartbeat do líder '%s' (PropID: %d, MaxSlot: %d)\n", req.LeaderAddress, req.CurrentProposalId, req.HighestDecidedSlotId)

	// 1. Validar o líder (se o PropID é o maior que já vimos para liderança)
	if req.CurrentProposalId < s.leaderState.AcceptedProposedID {
		return &pb.LeaderHeartbeatResponse{
			Success:            false,
			ErrorMessage:       fmt.Sprintf("Recebido heartbeat com proposal ID %d menor que o aceito %d", req.CurrentProposalId, s.leaderState.AcceptedProposedID),
			KnownHighestSlotId: s.highestSlotID,
		}, nil
	}

	// 2. Aceitar o líder e atualizar estado
	s.currentLeader = req.LeaderAddress
	s.leaderState.AcceptedProposedID = req.CurrentProposalId // Atualiza que este é o líder aceito
	s.lastHeartbeat = time.Now()                             // Reseta o timer de timeout do líder
	s.isLeader = (s.nodeName == req.LeaderAddress)           // Atualiza seu próprio estado de liderança

	// 3. Atualizar highestSlotID local se o líder tem um log mais avançado
	if req.HighestDecidedSlotId > s.highestSlotID {
		log.Printf("[Acceptor-Heartbeat] Líder tem um log mais avançado (%d > %d). Preciso sincronizar!\n", req.HighestDecidedSlotId, s.highestSlotID)
		// Aqui, um LEARNER real iniciaria um processo de sincronização de log
		// (pediria os comandos dos slots faltantes ao líder ou outros peers)
		s.highestSlotID = req.HighestDecidedSlotId // Atualiza provisoriamente
	}

	return &pb.LeaderHeartbeatResponse{
		Success:            true,
		KnownHighestSlotId: s.highestSlotID, // Informa ao líder o seu maior slot conhecido
	}, nil
}

// StartLeaderMonitor é uma goroutine que todos os nós (exceto o próprio líder) executam
// para monitorar o líder e iniciar uma eleição se ele falhar.
func (s *PaxosServer) StartLeaderMonitor() {
	ticker := time.NewTicker(s.leaderTimeout / 2) // Checa com o dobro da frequência do timeout
	defer ticker.Stop()

	for range ticker.C {
		s.mu.RLock()
		isCurrentLeader := s.isLeader // Verificar se já sou o líder (para não me monitorar)
		currentLeader := s.currentLeader
		lastHB := s.lastHeartbeat
		timeout := s.leaderTimeout
		s.mu.RUnlock()

		if isCurrentLeader {
			continue // O líder não precisa monitorar a si mesmo
		}

		// Se nunca houve um líder ou se o último heartbeat excedeu o timeout
		if currentLeader == "" || time.Since(lastHB) > timeout {
			log.Printf("[LeaderMonitor] Líder '%s' não responde ou não definido. Último heartbeat há %v. Iniciando eleição...\n", currentLeader, time.Since(lastHB))
			s.LeaderElection() // Tentar se tornar líder
		} else {
			// log.Printf("[LeaderMonitor] Líder '%s' ativo. Último heartbeat há %v.\n", currentLeader, time.Since(lastHB))
		}
	}
}
