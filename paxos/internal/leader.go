// Package internal
package internal

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
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
			CurrentHighestLeaderProposalId: s.leaderState.AcceptedProposedID,
			CurrentLeaderAddress:           s.leaderState.AcceptedLeaderAddr,
			HighestDecidedSlotId:           s.highestSlotID,
		}, nil

	}
	// recusa; informa por que e retorna o maior promised
	errMsg := fmt.Sprintf(
		"proposal_id %d ≤ promised %d",
		req.ProposalId, s.leaderState.HighestPromisedID,
	)
	return &pb.ProposeLeaderResponse{
		Success:                        false,
		ErrorMessage:                   errMsg,
		CurrentHighestLeaderProposalId: s.leaderState.HighestPromisedID,
		CurrentLeaderAddress:           s.leaderState.AcceptedLeaderAddr,
		HighestDecidedSlotId:           s.highestSlotID,
	}, fmt.Errorf(errMsg)
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
	learnedMaxSlot := maxHighestSlotID
	log.Printf("[LeaderElection] %s foi eleito LÍDER com proposal ID %d! O log mais avançado está em %d. Iniciando sincronização...", s.nodeName, candidateProposalID, learnedMaxSlot)
	s.mu.Unlock()

	s.SynchronizeLog(learnedMaxSlot) // Sincroniza o log com o líder recém-eleito

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

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Timeout menor para heartbeats
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

	// Se o heartbeat é de um líder com um ID de proposta *maior* do que eu já aceitei
	// ou do que eu mesmo propus (se eu era líder ou candidato), eu devo aceitá-lo.
	// Esta lógica é crucial para a transição de liderança.
	if req.CurrentProposalId < s.leaderState.HighestPromisedID {
		errMsg := fmt.Sprintf("Recebido heartbeat com proposal ID %d menor que o já prometido %d", req.CurrentProposalId, s.leaderState.HighestPromisedID)
		log.Printf("[Acceptor-Heartbeat] %s Rejeitando: %s\n", s.nodeName, errMsg)
		return &pb.LeaderHeartbeatResponse{
			Success:            false,
			ErrorMessage:       errMsg,
			KnownHighestSlotId: s.highestSlotID,
		}, nil
	}

	// Se eu sou o líder, só vou abdicr se o proposal ID do outro for maior que o meu.
	// Se for menor ou igual, é um líder antigo ou inválido.
	if s.isLeader && req.CurrentProposalId <= s.leaderProposalID {
		errMsg := fmt.Sprintf("Rejeitando heartbeat de líder com proposta (%d) não superior à minha (%d)", req.CurrentProposalId, s.leaderProposalID)
		log.Println("[Acceptor-Heartbeat]", errMsg)
		return &pb.LeaderHeartbeatResponse{Success: false, ErrorMessage: errMsg}, nil
	}

	// Se chegou aqui, é porque o heartbeat é válido e deve ser aceito.
	s.currentLeader = req.LeaderAddress
	s.lastHeartbeat = time.Now()                             // Reseta o timer de timeout do líder
	s.leaderState.HighestPromisedID = req.CurrentProposalId  // Atualiza o maior ID prometido
	s.leaderState.AcceptedProposedID = req.CurrentProposalId // Atualiza o ID da proposta aceita

	wasLeader := s.isLeader                           // Verifica se era líder antes de atualizar
	s.isLeader = (s.nodeAddress == req.LeaderAddress) // Atualiza se este nó é o líder

	if wasLeader && !s.isLeader {
		log.Printf("[Acceptor-Heartbeat] %s Abri mão da liderança para '%s' com Proposta ID %d (minha %d). Meu highestSlotID: %d\n",
			s.nodeName, req.LeaderAddress, req.CurrentProposalId, s.leaderProposalID, s.highestSlotID)
	}

	// Sincroniza o log se o líder tem um log mais avançado.
	// Esta parte é crucial para nós novos ou que voltaram online.
	if req.HighestDecidedSlotId > s.highestSlotID {
		log.Printf("[Acceptor-Heartbeat] %s: Líder '%s' tem um log mais avançado (%d > %d). Iniciando sincronização...\n", s.nodeName, req.LeaderAddress, req.HighestDecidedSlotId, s.highestSlotID)
		// Liberar o lock *antes* de chamar SynchronizeLog para evitar deadlock,
		// já que SynchronizeLog vai adquirir o lock internamente.
		s.mu.Unlock()
		s.SynchronizeLog(req.HighestDecidedSlotId)
		s.mu.Lock() // Adquirir o lock novamente antes de retornar
	} else {
		log.Printf("[Acceptor-Heartbeat] %s: Meu log já está atualizado (%d >= %d). Não preciso sincronizar.\n", s.nodeName, s.highestSlotID, req.HighestDecidedSlotId)
	}

	return &pb.LeaderHeartbeatResponse{
		Success:            true,
		KnownHighestSlotId: s.highestSlotID, // Informa ao líder o seu maior slot conhecido
	}, nil
}

func (s *PaxosServer) Start() error {
	s.mu.Lock()
	s.lastHeartbeat = time.Now() // Inicializa o último heartbeat
	s.mu.Unlock()

	go s.StartLeaderMonitor()    // Inicia o monitoramento do líder
	go s.StartLeaderHeartbeats() // Inicia os heartbeats do líder
	return nil
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

			go func() {
				// Gera um atraso aleatório para evitar que todos os nós iniciem a eleição ao mesmo tempo

				delay := time.Duration(rand.Intn(201)+50) * time.Millisecond

				log.Printf("[LeaderMonitor] Atrasando eleição de líder por %v para evitar colisões...\n", delay)

				time.Sleep(delay)

				s.mu.RLock()
				isStillNoLeader := (s.currentLeader == "" || time.Since(s.lastHeartbeat) > s.leaderTimeout)
				s.mu.RUnlock()

				if isStillNoLeader {
					log.Printf("[LeaderMonitor] Iniciando eleição de líder após atraso de %v...\n", delay)
					s.LeaderElection() // Tentar se tornar líder
				} else {
					log.Printf("[LeaderMonitor] Líder '%s' voltou ou já foi eleito. Ignorando eleição.\n", s.currentLeader)
				}
			}()
		} else {
			log.Printf("[LeaderMonitor] Líder '%s' ativo. Último heartbeat há %v.\n", currentLeader, time.Since(lastHB))
		}
	}
}

func (s *PaxosServer) SynchronizeLog(targetSlotID int64) {
	s.mu.Lock()
	startSlot := s.highestSlotID + 1
	nodeAddress := s.nodeAddress
	registryClient := s.registryClient
	s.mu.Unlock()

	if startSlot > targetSlotID {
		log.Printf("[SyncrhoinzeLog] O slot %d já está atualizado. Nada a sincronizar.\n", targetSlotID)
		log.Printf("debug: startSlot=%d, targetSlotID=%d, nodeAddress=%s", startSlot, targetSlotID, nodeAddress)
		return
	}

	log.Printf("[Sync] Iniciando sincronização do log do slot %d até %d", startSlot, targetSlotID)

	// Pega a lista de outros nós para perguntar
	registryResp, err := registryClient.ListAll(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.Printf("[Sync] Erro ao listar serviços para sincronização: %v", err)
		return

	}

	log.Printf("[Sync] Encontrados %d serviços registrados para sincronização", len(registryResp.Services))

	for slotToLearn := startSlot; slotToLearn <= targetSlotID; slotToLearn++ {
		learned := false

		for _, service := range registryResp.Services {
			if service.Address == nodeAddress {
				continue
			}

			conn, err := grpc.NewClient(service.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[Sync] Não conseguiu conectar a %s: %v", service.Address, err)
				continue
			}

			client := pb.NewPaxosClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			resp, err := client.Learn(ctx, &pb.LearnRequest{SlotId: slotToLearn})
			cancel()     // Cancela o contexto após a chamada
			conn.Close() // Fecha a conexão após a chamada

			if err == nil && resp.Decided {
				log.Printf("[Sync] Aprendi o valor para o slot %d do nó %s", slotToLearn, service.Address)

				s.mu.Lock()

				if s.slots == nil {
					s.slots = make(map[int64]*PaxosState) // Inicializa o mapa de slots se for nil
				}
				s.slots[slotToLearn] = &PaxosState{
					HighestPromisedID:  resp.Command.GetProposalId(),
					AcceptedProposedID: resp.Command.GetProposalId(),
					AcceptedCommand:    resp.Command,
				}
				s.ApplyCommand(resp.Command)
				if slotToLearn > s.highestSlotID {
					s.highestSlotID = slotToLearn // Atualiza o maior slot conhecido
					log.Printf("[Sync] Nó %s: Atualizado highestSlotID para %d após aprender slot %d\n", s.nodeName, s.highestSlotID, slotToLearn)

				}
				s.mu.Unlock()
				learned = true
				break // Sai do loop interno, vai para o próximo slot
			} else if err != nil {
				log.Printf("[Sync] Erro ao tentar aprender slot %d de %s: %v\n", slotToLearn, service.Address, err)
			} else if !resp.Decided {
				log.Printf("[Sync] Nó %s não tem valor decidido para slot %d ainda.\n", service.Address, slotToLearn)
			}
		}

		if !learned {
			log.Printf("[Sync] AVISO: Não foi possível aprender o valor para o slot %d de nenhum nó.", slotToLearn)
			// Aqui você pode decidir parar a sincronização ou tentar novamente.
			// Parar é mais seguro para evitar um log inconsistente.
			break
		}
	}

	log.Printf("[Sync] Sincronização do log concluída até o slot %d", targetSlotID)
}
