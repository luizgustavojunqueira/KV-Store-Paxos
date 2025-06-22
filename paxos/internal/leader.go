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
	"golang.org/x/sync/semaphore" // Importe este pacote
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
	s.leaderProposalID = time.Now().UnixNano()
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
		promisesReceived       int
		highestAcceptedLeaderN int64
		maxHighestSlotID       int64
		mu                     sync.Mutex
		wg                     sync.WaitGroup
		// Novo: use um contexto com cancelamento para abortar todas as RPCs em andamento
		electionCtx, electionCancel = context.WithTimeout(context.Background(), 15*time.Second) // Aumentado para 15s
	)
	defer electionCancel() // Garante que o contexto seja cancelado no final da função

	// Este canal será fechado para sinalizar o aborto da eleição.
	// É mais robusto do que uma variável booleana, pois pode ser checado por `select`.
	abortSignaler := make(chan struct{})

	const maxConcurrentProposeRPCs = 500 // Limita o número de goroutines concorrentes para evitar flood de RPCs
	sem := semaphore.NewWeighted(maxConcurrentProposeRPCs)

	for _, service := range registryResp.Services {
		if service.Address == s.nodeAddress {
			continue
		}
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			// Isso pode bloquear a goroutine se o número máximo de RPCs já estiver em andamento.
			// Se o contexto da eleição for cancelado/expirar enquanto espera, a aquisição falhará.
			if err := sem.Acquire(electionCtx, 1); err != nil {
				log.Printf("[%s][LeaderElection] Falha ao adquirir semáforo para %s: %v\n", s.nodeName, addr, err)
				return // A goroutine não prossegue se não conseguir a permissão
			}
			defer sem.Release(1) // Libera a permissão do semáforo quando a goroutine termina

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
			client := pb.NewPaxosClient(conn)

			req := &pb.ProposeLeaderRequest{
				ProposalId:       candidateProposalID,
				CandidateAddress: s.nodeAddress,
			}

			resp, err := client.ProposeLeader(electionCtx, req) // Use electionCtx aqui
			if err != nil {
				log.Printf("[LeaderElection] Erro ao enviar ProposeLeader para %s: %v", addr, err)
				// Se a RPC falhou devido a timeout, isso pode ser um problema de rede,
				// não necessariamente uma proposta maior. Não aborta por isso.
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
				// SE A RECUSA FOI POR UMA PROPOSTA MAIOR, CANCELA A ELEIÇÃO INTEIRA.
				if resp.CurrentHighestLeaderProposalId > candidateProposalID {
					log.Printf("[LeaderElection] *** Detecção de Proposta MAIOR (ID: %d) de %s. ABORTANDO eleição para %s! ***\n",
						resp.CurrentHighestLeaderProposalId, addr, s.nodeName)
					select {
					case <-abortSignaler: // Já está fechado
					default:
						close(abortSignaler) // Fecha o canal para sinalizar o aborto a todas as outras goroutines
						electionCancel()     // Cancela o contexto principal da eleição também, para forçar o fim das RPCs em andamento
					}
					// Não precisa retornar aqui, a goroutine continuará até o `select` pegar o sinal de aborto.
				}
			}
		}(service.Address)
	}
	wg.Wait() // Espera todas as goroutines de RPC terminarem

	// Após o Wait(), verifica se a eleição foi abortada
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

	// O stopHeartbeats() e StartLeaderHeartbeats() são executados DENTRO do lock/unlock
	// para garantir que a transição de liderança seja atômica em relação ao estado do servidor.
	// Primeiro, parar qualquer goroutine de heartbeat antiga (se houver)
	s.stopHeartbeats()           // Esta função já usa um mutex interno, então é segura.
	go s.StartLeaderHeartbeats() // Inicia a nova goroutine de heartbeats

	// A sincronização de log deve ser non-blocking (em goroutine separada) se for demorada.
	// É importante que o líder comece a enviar heartbeats rapidamente.
	go s.SynchronizeLog(learnedMaxSlot)

	return true
}

// StartLeaderHeartbeats é uma goroutine que o líder executa para manter sua liderança
func (s *PaxosServer) StartLeaderHeartbeats() {
	// A frequência dos heartbeats. Deve ser significativamente menor que o leaderTimeout mínimo.
	// Por exemplo, se o leaderTimeout mínimo é 5 segundos, 2 segundos é razoável.
	heartbeatInterval := 2 * time.Second
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// Obtém o canal de parada específico para esta instância da goroutine de heartbeats.
	// Isso é crucial para que 'stopHeartbeats' possa sinalizar especificamente esta goroutine.
	s.mu.RLock()
	currentHeartbeatStopChan := s.heartBeatStopChan
	s.mu.RUnlock()

	log.Printf("[%s] Iniciando goroutine de heartbeats com intervalo de %v.\n", s.nodeName, heartbeatInterval)

	const maxConcurrentHeartbeatRPCs = 500 // Ajuste este valor (pode ser maior que o de ProposeLeader)
	sem := semaphore.NewWeighted(maxConcurrentHeartbeatRPCs)

	for {
		select {
		case <-ticker.C:
			// O ticker disparou, é hora de enviar heartbeats
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

		// Use um WaitGroup para esperar que todos os heartbeats sejam enviados
		var wg sync.WaitGroup
		// Use um canal para coletar resultados de forma não bloqueante ou para limitar goroutines
		// Não é estritamente necessário para heartbeats se o número de peers é limitado.

		// Envia heartbeat para todos os peers (exceto ele mesmo)
		for _, service := range registryResp.Services {
			if service.Address == s.nodeAddress {
				continue // Não envia heartbeat para si mesmo
			}
			wg.Add(1)
			go func(addr string) {
				defer wg.Done()

				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second) // Aumentado para 8s ou mais
				defer cancel()

				if err := sem.Acquire(ctx, 1); err != nil {
					log.Printf("[%s][LeaderHeartbeat] Falha ao adquirir semáforo para %s: %v\n", s.nodeName, addr, err)
					return
				}
				defer sem.Release(1) // Libera a permissão

				conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					log.Printf("[%s][LeaderHeartbeat] Não conectou a %s: %v\n", s.nodeName, addr, err)
					return
				}
				defer conn.Close()
				client := pb.NewPaxosClient(conn)

				req := &pb.LeaderHeartbeat{
					LeaderAddress:        leaderID,
					CurrentProposalId:    currentPropID,
					HighestDecidedSlotId: highestSlot,
				}

				resp, err := client.SendHeartbeat(ctx, req)
				if err != nil {
					log.Printf("[%s][LeaderHeartbeat] Erro ao enviar heartbeat para %s: %v", s.nodeName, addr, err)
					// Se o heartbeat falhar para um peer, isso não necessariamente significa que o líder perdeu a liderança.
					// O líder continua a enviar, e o monitor do peer eventualmente decidirá se o líder falhou.
					return
				}
				if !resp.Success {
					log.Printf("[%s][LeaderHeartbeat] Peer %s recusou heartbeat: %s", s.nodeName, addr, resp.ErrorMessage)
					// Se o peer recusou e a mensagem indica que ele viu uma proposta maior,
					// o líder *pode* optar por se destituir.
					// Sua lógica atual em SendHeartbeat no acceptor já trata disso ao atualizar
					// o currentLeader e isLeader, então aqui o líder pode apenas logar.
				}
				// log.Printf("[%s][LeaderHeartbeat] Heartbeat enviado para %s. Peer Highest Slot: %d\n", s.nodeName, addr, resp.KnownHighestSlotId)
				// O líder pode usar resp.KnownHighestSlotId para iniciar a sincronização do log com peers atrasados
				// em uma goroutine separada, se ainda não estiver fazendo isso via SynchronizeLog inicial.
			}(service.Address)
		}
		wg.Wait() // Espera todos os heartbeats serem enviados para o tick atual
	}
}

// --- SendHeartbeat (RPC no Acceptor) ---
func (s *PaxosServer) SendHeartbeat(ctx context.Context, req *pb.LeaderHeartbeat) (*pb.LeaderHeartbeatResponse, error) {
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
		return &pb.LeaderHeartbeatResponse{
			Success:            false,
			ErrorMessage:       errMsg,
			KnownHighestSlotId: s.highestSlotID,
		}, nil
	} else { // req.CurrentProposalId == s.leaderState.HighestPromisedID
		// É o mesmo leaderProposalID que eu já conheço/prometi. Aceitar para atualizar heartbeat.
		// Isso é crucial para manter a liderança atual ativa.
		log.Printf("[Acceptor-Heartbeat] %s: Heartbeat do líder atual '%s' (PropID: %d). Atualizando tempo.\n", s.nodeName, req.LeaderAddress, req.CurrentProposalId)
		s.currentLeader = req.LeaderAddress
		s.lastHeartbeat = time.Now()
		s.isLeader = (s.nodeAddress == req.LeaderAddress)

		if s.isLeader && (req.LeaderAddress != s.nodeAddress) {
			// Este cenário indica que o nó se considera líder, mas recebeu um heartbeat
			// de OUTRO nó que tem o MESMO proposal ID. Isso é um split-brain perigoso
			// e deve levar este nó a abdicar.
			log.Printf("[Acceptor-Heartbeat] ALERTA: %s se considera líder, mas recebeu heartbeat do líder %s com o mesmo PropID %d. Abdicando.", s.nodeName, req.LeaderAddress, req.CurrentProposalId)
			s.isLeader = false
			s.stopHeartbeats()
		}
	}

	// Sincroniza o log se o líder tem um log mais avançado.
	if req.HighestDecidedSlotId > s.highestSlotID {
		log.Printf("[Acceptor-Heartbeat] %s: Líder '%s' tem um log mais avançado (%d > %d). Iniciando sincronização em goroutine separada...\n", s.nodeName, req.LeaderAddress, req.HighestDecidedSlotId, s.highestSlotID)
		// Desbloquear antes de iniciar a goroutine de sincronização
		s.mu.Unlock()
		go s.SynchronizeLog(req.HighestDecidedSlotId) // Chame como goroutine
		s.mu.Lock()                                   // Re-adquirir o lock antes de retornar
	} else {
		log.Printf("[Acceptor-Heartbeat] %s: Meu log já está atualizado (%d >= %d). Não preciso sincronizar.\n", s.nodeName, s.highestSlotID, req.HighestDecidedSlotId)
	}

	return &pb.LeaderHeartbeatResponse{
		Success:            true,
		KnownHighestSlotId: s.highestSlotID,
	}, nil
}

func (s *PaxosServer) Start() error {
	s.mu.Lock()
	s.lastHeartbeat = time.Now() // Inicializa o último heartbeat
	s.mu.Unlock()

	go s.StartLeaderMonitor() // Inicia o monitoramento do líder
	return nil
}

// stopHeartbeats é chamada para sinalizar que a goroutine de heartbeats do líder deve parar.
func (s *PaxosServer) stopHeartbeats() {
	// Para garantir que o canal de stop correto seja fechado/sinalizado,
	// precisamos acessar o canal atual sob proteção de mutex.
	s.mu.Lock()
	// Fazemos uma cópia do canal atual para não fechar um canal que já foi recriado.
	currentStopChan := s.heartBeatStopChan
	// Recria o canal para futuras instâncias de StartLeaderHeartbeats.
	// Isso evita que um stop antigo afete um novo líder.
	s.heartBeatStopChan = make(chan struct{})
	s.mu.Unlock()

	// Envia o sinal para parar a goroutine antiga.
	// Usar select para não bloquear se a goroutine já tiver terminado de escutar.
	select {
	case currentStopChan <- struct{}{}:
		log.Println("[stopHeartbeats] Sinal de parada enviado.")
	default:
		log.Println("[stopHeartbeats] Canal de parada já sem receptor ou nil. Ignorando.")
	}
}

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

			// Tenta adquirir o slot do electionSignal. Se não conseguir, outra eleição já está em andamento.
			select {
			case s.electionSignal <- struct{}{}: // Tenta enviar um valor para o canal (adquirir o "token")
				go func() {
					defer func() { <-s.electionSignal }() // Garante que o token seja liberado ao final desta goroutine

					// Atraso aleatório antes de INICIAR a eleição
					delay := time.Duration(rand.Intn(40001)+10000) * time.Millisecond // 100ms a 500ms
					log.Printf("[%s][LeaderMonitor] Atrasando eleição de líder por %v para evitar colisões...\n", s.nodeName, delay)
					time.Sleep(delay)

					s.mu.RLock()
					// Re-verifica se ainda é necessário iniciar a eleição APÓS o atraso
					// CRUCIAL: Verificar se um líder já foi eleito *neste meio tempo* (pelo atraso).
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
