// Package internal
package internal

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	paxosPB "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *PaxosServer) ProposeLeader(ctx context.Context, req *paxosPB.ProposeLeaderRequest) (*paxosPB.ProposeLeaderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[Acceptor-Leader] Recebido ProposeLeader para candidato '%s' com proposal_id %d. Estado: promised=%d, accepted_n=%d, accepted_leader=%s\n",
		req.CandidateAddress, req.ProposalId, s.leaderState.HighestPromisedID, s.leaderState.AcceptedProposedID, s.leaderState.AcceptedLeaderAddr)

	if req.ProposalId > s.leaderState.HighestPromisedID {
		s.leaderState.HighestPromisedID = req.ProposalId

		return &paxosPB.ProposeLeaderResponse{
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
	return &paxosPB.ProposeLeaderResponse{
		Success:                        false,
		ErrorMessage:                   errMsg,
		CurrentHighestLeaderProposalId: s.leaderState.HighestPromisedID,
		CurrentLeaderAddress:           s.leaderState.AcceptedLeaderAddr,
		HighestDecidedSlotId:           s.highestSlotID,
	}, fmt.Errorf(errMsg)
}

func (s *PaxosServer) LeaderElection() bool {
	s.mu.Lock()
	isAlreadyLeader := s.isLeader
	s.mu.Unlock()

	if isAlreadyLeader {
		log.Println("[LeaderElection] Já sou o líder, não preciso iniciar uma nova eleição.")
		return true
	}

	log.Printf("[LeaderElection] Tentando se tornar líder para %s...\n", s.nodeName)

	s.mu.Lock()
	s.leaderProposalID = time.Now().UnixNano()<<8 | int64(s.nodeAddress[len(s.nodeAddress)-1]) // Garante que seja único e crescente
	candidateProposalID := s.leaderProposalID
	s.mu.Unlock()

	registryResp, err := s.registryClient.ListAll(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.Printf("[LeaderElection] Erro ao listar serviços do registry: %v", err)
		return false
	}

	quorumSize := (len(registryResp.Services) / 2) + 1
	if quorumSize == 0 {
		quorumSize = 1
	}
	log.Printf("[LeaderElection] Total de %d serviços, quorum para liderança: %d\n", len(registryResp.Services), quorumSize)

	var (
		promisesReceived            int
		highestAcceptedLeaderN      int64
		maxHighestSlotID            int64
		mu                          sync.Mutex
		wg                          sync.WaitGroup
		electionCtx, electionCancel = context.WithTimeout(context.Background(), 15*time.Second) // Aumentado para 15s
	)
	defer electionCancel()

	abortSignaler := make(chan struct{})

	const maxConcurrentProposeRPCs = 1000 // Limita o número de goroutines concorrentes para evitar flood de RPCs
	sem := semaphore.NewWeighted(maxConcurrentProposeRPCs)

	for _, service := range registryResp.Services {
		if service.Address == s.nodeAddress {
			continue
		}
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			if err := sem.Acquire(electionCtx, 1); err != nil {
				log.Printf("[%s][LeaderElection] Falha ao adquirir semáforo para %s: %v\n", s.nodeName, addr, err)
				return // A goroutine não prossegue se não conseguir a permissão
			}
			defer sem.Release(1)

			select {
			case <-electionCtx.Done(): // Se o contexto principal da eleição expirou ou foi cancelado
				log.Printf("[LeaderElection] Goroutine para %s abortada via contexto: %v\n", addr, electionCtx.Err())
				return
			case <-abortSignaler: // Se a eleição foi explicitamente abortada por uma proposta maior
				log.Printf("[LeaderElection] Goroutine para %s abortada via sinalizador: já há proposta maior.\n", addr)
				return
			default:
				// Continua
			}

			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[LeaderElection] Não conectou a %s: %v\n", addr, err)
				return
			}
			defer conn.Close()
			client := paxosPB.NewPaxosClient(conn)

			req := &paxosPB.ProposeLeaderRequest{
				ProposalId:       candidateProposalID,
				CandidateAddress: s.nodeAddress,
			}

			resp, err := client.ProposeLeader(electionCtx, req)
			if err != nil {
				log.Printf("[LeaderElection] Erro ao enviar ProposeLeader para %s: %v", addr, err)
				return
			}

			mu.Lock()
			defer mu.Unlock()

			if resp.Success {
				promisesReceived++
				if resp.CurrentHighestLeaderProposalId > highestAcceptedLeaderN {
					highestAcceptedLeaderN = resp.CurrentHighestLeaderProposalId
				}
				if resp.HighestDecidedSlotId > maxHighestSlotID {
					maxHighestSlotID = resp.HighestDecidedSlotId
				}
				log.Printf("[LeaderElection] Recebeu Promise de %s. N_p=%d, N_a=%d, V_a='%s', MaxSlot=%d\n",
					addr, resp.CurrentHighestLeaderProposalId, resp.CurrentHighestLeaderProposalId, resp.CurrentLeaderAddress, resp.HighestDecidedSlotId)
			} else {
				log.Printf("[LeaderElection] Acceptor %s recusou ProposeLeader: %s. Highest ID conhecido: %d\n", addr, resp.ErrorMessage, resp.CurrentHighestLeaderProposalId)
				if resp.CurrentHighestLeaderProposalId > candidateProposalID {
					log.Printf("[LeaderElection] *** Detecção de Proposta MAIOR (ID: %d) de %s. ABORTANDO eleição para %s! ***\n",
						resp.CurrentHighestLeaderProposalId, addr, s.nodeName)
					select {
					case <-abortSignaler:
					default:
						close(abortSignaler)
						electionCancel()
					}
				}
			}
		}(service.Address)
	}
	wg.Wait() // Espera todas as goroutines de RPC terminarem

	select {
	case <-abortSignaler: // Se o canal foi fechado, significa que foi abortado
		log.Printf("[LeaderElection] %s: Eleição abortada por uma proposta de liderança maior. Não se tornou líder.\n", s.nodeName)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
		return false
	default:
		// A eleição não foi abortada explicitamente por uma proposta maior
	}

	if promisesReceived < quorumSize {
		log.Printf("[LeaderElection] %s: Não obteve quorum de Promises (%d/%d). Falha na eleição.\n", s.nodeName, promisesReceived, quorumSize)
		s.mu.Lock()
		s.isLeader = false
		s.mu.Unlock()
		return false
	}

	// Se chegou aqui, conseguiu quorum e não foi abortado
	s.mu.Lock()
	s.isLeader = true
	s.currentLeader = s.nodeAddress
	s.lastHeartbeat = time.Now()
	s.leaderState.HighestPromisedID = candidateProposalID  // O líder eleito deve "prometer" a si mesmo
	s.leaderState.AcceptedProposedID = candidateProposalID // E aceitar sua própria proposta
	s.leaderState.AcceptedLeaderAddr = s.nodeAddress       // E aceitar a si mesmo como líder
	learnedMaxSlot := maxHighestSlotID
	log.Printf("[LeaderElection] %s foi eleito LÍDER com proposal ID %d! O log mais avançado está em %d. Iniciando sincronização e heartbeats...", s.nodeName, candidateProposalID, learnedMaxSlot)
	s.mu.Unlock()

	s.stopHeartbeats()
	go s.StartLeaderHeartbeats()

	go s.SynchronizeLog(learnedMaxSlot)

	return true
}

// StartLeaderHeartbeats é uma goroutine que o líder executa para manter sua liderança
func (s *PaxosServer) StartLeaderHeartbeats() {
	heartbeatInterval := 2 * time.Second
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	s.mu.RLock()
	currentHeartbeatStopChan := s.heartBeatStopChan
	s.mu.RUnlock()

	log.Printf("[%s] Iniciando goroutine de heartbeats com intervalo de %v.\n", s.nodeName, heartbeatInterval)

	const maxConcurrentHeartbeatRPCs = 1000
	sem := semaphore.NewWeighted(maxConcurrentHeartbeatRPCs)

	for {
		select {
		case <-ticker.C:
		case <-currentHeartbeatStopChan: // Recebeu um sinal para parar esta goroutine
			log.Printf("[%s] Recebido sinal para parar heartbeats. Encerrando goroutine.", s.nodeName)
			return
		}

		s.mu.RLock()
		if !s.isLeader { // Verifica se ainda é o líder (pode ter sido destituído por outro nó)
			s.mu.RUnlock()
			log.Printf("[%s] Não sou mais o líder. Encerrando goroutine de heartbeats.", s.nodeName)
			return // Sai da goroutine de heartbeats
		}
		leaderID := s.nodeAddress // O líder se identifica com seu próprio endereço
		currentPropID := s.leaderProposalID
		highestSlot := s.highestSlotID
		s.mu.RUnlock()

		registryResp, err := s.registryClient.ListAll(context.Background(), &emptypb.Empty{})
		if err != nil {
			log.Printf("[%s][LeaderHeartbeat] Erro ao listar serviços do registry: %v", s.nodeName, err)
			continue // Tenta novamente no próximo tick
		}

		var wg sync.WaitGroup

		// Envia heartbeat para todos os peers (exceto ele mesmo)
		for _, service := range registryResp.Services {
			if service.Address == s.nodeAddress {
				continue // Não envia heartbeat para si mesmo
			}
			wg.Add(1)
			go func(addr string) {
				defer wg.Done()

				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()

				if err := sem.Acquire(ctx, 1); err != nil {
					log.Printf("[%s][LeaderHeartbeat] Falha ao adquirir semáforo para %s: %v\n", s.nodeName, addr, err)
					return
				}
				defer sem.Release(1)

				conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					log.Printf("[%s][LeaderHeartbeat] Não conectou a %s: %v\n", s.nodeName, addr, err)
					return
				}
				defer conn.Close()
				client := paxosPB.NewPaxosClient(conn)

				req := &paxosPB.LeaderHeartbeat{
					LeaderAddress:        leaderID,
					CurrentProposalId:    currentPropID,
					HighestDecidedSlotId: highestSlot,
				}

				resp, err := client.SendHeartbeat(ctx, req)
				if err != nil {
					log.Printf("[%s][LeaderHeartbeat] Erro ao enviar heartbeat para %s: %v", s.nodeName, addr, err)
					return
				}
				if !resp.Success {
					log.Printf("[%s][LeaderHeartbeat] Peer %s recusou heartbeat: %s", s.nodeName, addr, resp.ErrorMessage)
					if strings.Contains(resp.ErrorMessage, "proposal ID menor que o já prometido") {
						log.Printf("[%s][LeaderHeartbeat] Detectei que fui suplantado por um novo líder. Abdicando.", s.nodeName)
						s.mu.Lock()
						s.isLeader = false // Rebaixa-se
						s.mu.Unlock()
					}
				}
			}(service.Address)
		}
		wg.Wait() // Espera todos os heartbeats serem enviados para o tick atual
	}
}

// SendHeartbeat é chamado pelo líder para enviar um heartbeat para os acceptors
func (s *PaxosServer) SendHeartbeat(ctx context.Context, req *paxosPB.LeaderHeartbeat) (*paxosPB.LeaderHeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[Acceptor-Heartbeat] Recebido heartbeat do líder '%s' (PropID: %d, MaxSlot: %d) de %s\n", req.LeaderAddress, req.CurrentProposalId, req.HighestDecidedSlotId, s.nodeName)

	// Lógica para abdicar da liderança ou aceitar um novo líder com ID maior
	if req.CurrentProposalId > s.leaderState.HighestPromisedID {
		log.Printf("[Acceptor-Heartbeat] %s: Aceitando novo líder '%s' com proposta ID %d > meu HighestPromisedID %d\n", s.nodeName, req.LeaderAddress, req.CurrentProposalId, s.leaderState.HighestPromisedID)
		s.currentLeader = req.LeaderAddress
		s.lastHeartbeat = time.Now()
		s.leaderState.HighestPromisedID = req.CurrentProposalId
		s.leaderState.AcceptedProposedID = req.CurrentProposalId // Também aceita esta proposta

		wasLeader := s.isLeader // Verifica se era líder antes de mudar o status
		s.isLeader = (s.nodeAddress == req.LeaderAddress)

		if wasLeader && !s.isLeader { // Se era líder e deixou de ser
			log.Printf("[Acceptor-Heartbeat] %s: Abri mão da liderança para '%s' (Proposta %d).", s.nodeName, req.LeaderAddress, req.CurrentProposalId)
			s.stopHeartbeats() // O antigo líder para de enviar heartbeats
		}
	} else if req.CurrentProposalId < s.leaderState.HighestPromisedID {
		// Recebeu um heartbeat com ID de proposta menor. Recusar.
		errMsg := fmt.Sprintf("Recebido heartbeat com proposal ID %d menor que o já prometido %d", req.CurrentProposalId, s.leaderState.HighestPromisedID)
		log.Printf("[Acceptor-Heartbeat] %s: Rejeitando heartbeat de %s. %s\n", s.nodeName, req.LeaderAddress, errMsg)
		return &paxosPB.LeaderHeartbeatResponse{
			Success:            false,
			ErrorMessage:       errMsg,
			KnownHighestSlotId: s.highestSlotID,
		}, nil
	} else {
		log.Printf("[Acceptor-Heartbeat] %s: Heartbeat do líder atual '%s' (PropID: %d). Atualizando tempo.\n", s.nodeName, req.LeaderAddress, req.CurrentProposalId)
		s.currentLeader = req.LeaderAddress
		s.lastHeartbeat = time.Now()
		s.isLeader = (s.nodeAddress == req.LeaderAddress)

		if s.isLeader && (req.LeaderAddress != s.nodeAddress) {
			log.Printf("[Acceptor-Heartbeat] ALERTA: %s se considera líder, mas recebeu heartbeat do líder %s com o mesmo PropID %d. Abdicando.", s.nodeName, req.LeaderAddress, req.CurrentProposalId)
			s.isLeader = false
			s.stopHeartbeats()
		}
	}

	// Sincroniza o log se o líder tem um log mais avançado.
	if req.HighestDecidedSlotId > s.highestSlotID {
		log.Printf("[Acceptor-Heartbeat] %s: Líder '%s' tem um log mais avançado (%d > %d). Iniciando sincronização em goroutine separada...\n", s.nodeName, req.LeaderAddress, req.HighestDecidedSlotId, s.highestSlotID)
		s.mu.Unlock()
		go s.SynchronizeLog(req.HighestDecidedSlotId)
		s.mu.Lock()
	} else {
		log.Printf("[Acceptor-Heartbeat] %s: Meu log já está atualizado (%d >= %d). Não preciso sincronizar.\n", s.nodeName, s.highestSlotID, req.HighestDecidedSlotId)
	}

	return &paxosPB.LeaderHeartbeatResponse{
		Success:            true,
		KnownHighestSlotId: s.highestSlotID,
	}, nil
}

func (s *PaxosServer) Start() error {
	s.mu.Lock()
	s.lastHeartbeat = time.Now()
	s.mu.Unlock()

	go s.StartLeaderMonitor() // Inicia o monitoramento do líder
	return nil
}

// stopHeartbeats fecha o canal de heartbeats atual e recria um novo canal para futuras instâncias de StartLeaderHeartbeats.
func (s *PaxosServer) stopHeartbeats() {
	s.mu.Lock()
	currentStopChan := s.heartBeatStopChan
	s.heartBeatStopChan = make(chan struct{})
	s.mu.Unlock()

	select {
	case currentStopChan <- struct{}{}:
		log.Println("[stopHeartbeats] Sinal de parada enviado.")
	default:
		log.Println("[stopHeartbeats] Canal de parada já sem receptor ou nil. Ignorando.")
	}
}

// StartLeaderMonitor inicia uma goroutine que monitora o líder atual e inicia uma eleição se necessário.
func (s *PaxosServer) StartLeaderMonitor() {
	ticker := time.NewTicker(s.leaderTimeout / 2)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.RLock()
		isCurrentNodeLeader := s.isLeader
		currentLeader := s.currentLeader
		lastHB := s.lastHeartbeat
		timeout := s.leaderTimeout
		s.mu.RUnlock()

		if isCurrentNodeLeader {
			continue
		}

		if currentLeader == "" || time.Since(lastHB) > timeout {
			log.Printf("[%s][LeaderMonitor] Líder '%s' não responde ou não definido. Último heartbeat há %v (timeout %v). Tentando iniciar eleição...\n", s.nodeName, currentLeader, time.Since(lastHB), timeout)

			select {
			case s.electionSignal <- struct{}{}:
				go func() {
					defer func() { <-s.electionSignal }()

					// Atraso aleatório antes de INICIAR a eleição
					delay := time.Duration(rand.Intn(40001)+10000) * time.Millisecond // 100ms a 500ms
					log.Printf("[%s][LeaderMonitor] Atrasando eleição de líder por %v para evitar colisões...\n", s.nodeName, delay)
					time.Sleep(delay)

					s.mu.RLock()
					isStillNoLeader := (s.currentLeader == "" || time.Since(s.lastHeartbeat) > s.leaderTimeout)
					s.mu.RUnlock()

					if isStillNoLeader {
						log.Printf("[%s][LeaderMonitor] Iniciando eleição de líder após atraso de %v...\n", s.nodeName, delay)
						success := s.LeaderElection() // Tentar se tornar líder
						if !success {
							log.Printf("[%s][LeaderElection] Eleição falhou, aguardando próximo ciclo do monitor.", s.nodeName)
						}
					} else {
						log.Printf("[%s][LeaderMonitor] Líder '%s' voltou ou já foi eleito (%v). Ignorando eleição.\n", s.nodeName, s.currentLeader, time.Since(s.lastHeartbeat))
					}
				}()
			default:
				log.Printf("[%s][LeaderMonitor] Eleição já está em andamento. Pulando tentativa de iniciar uma nova.\n", s.nodeName)
			}
		} else {
			log.Printf("[%s][LeaderMonitor] Líder '%s' ativo. Último heartbeat há %v.\n", s.nodeName, currentLeader, time.Since(lastHB))
		}
	}
}

// SynchronizeLog sincroniza o log do nó atual com o log de outro nó até o slot especificado.
func (s *PaxosServer) SynchronizeLog(targetSlotID int64) {
	s.mu.RLock()
	startSlot := s.highestSlotID + 1
	nodeAddress := s.nodeAddress
	registryClient := s.registryClient
	s.mu.RUnlock()

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

			client := paxosPB.NewPaxosClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			resp, err := client.Learn(ctx, &paxosPB.LearnRequest{SlotId: slotToLearn})
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
			break
		}
	}

	log.Printf("[Sync] Sincronização do log concluída até o slot %d", targetSlotID)
}
