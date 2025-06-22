package internal

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	paxosPB "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *PaxosServer) ProposeCommand(cmd *paxosPB.Command) bool {
	s.mu.Lock()
	// Se não for o líder, não podemos propor comandos
	if !s.isLeader {
		s.mu.Unlock()
		log.Printf("[Proposer] Não sou o líder. Não posso propor comando %+v\n", cmd)
		return false
	}
	slotID := s.highestSlotID + 1 // Tentar propor no próximo slot livre
	proposalID := s.leaderProposalID
	currentNeedsPreparePhase := s.needsPreparePhase
	s.needsPreparePhase = false
	s.mu.Unlock()

	log.Printf("[Proposer] Tentando propor comando %+v para o slot %d com ProposalID %d\n", cmd, slotID, proposalID)

	// Pega todos os serviços registrados no registry
	registryResp, err := s.registryClient.ListAll(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.Printf("[Proposer] Erro ao listar serviços do registry: %v", err)
		return false
	}

	quorumSize := (len(registryResp.Services) / 2) + 1
	if quorumSize == 0 {
		quorumSize = 1
	}

	log.Printf("[Proposer] Total de %d serviços registrados, quorum necessário: %d\n", len(registryResp.Services), quorumSize)

	var (
		promisesReceived   int
		highestAcceptedID  int64
		highestAcceptedCmd *paxosPB.Command
		mu                 sync.Mutex
		wg                 sync.WaitGroup
	)

	mu.Lock()
	promisesReceived++
	mu.Unlock()

	if currentNeedsPreparePhase {
		log.Printf("[Proposer] Executando Fase 1 (Prepare/Promise) para slot %d.\n", slotID)

		// Envia Prepare para todos os Acceptors
		for _, service := range registryResp.Services {
			if service.Address == s.nodeAddress {
				continue // Pular o próprio nó
			}
			wg.Add(1)
			go func(addr string) {
				defer wg.Done()

				conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					log.Printf("[Proposer] Não foi possível conectar ao Acceptor %s: %v\n", addr, err)
					return
				}
				defer conn.Close()
				client := paxosPB.NewPaxosClient(conn)

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				req := &paxosPB.PrepareRequest{
					SlotId:     slotID,
					ProposalId: proposalID,
				}

				log.Printf("[Proposer] Enviando Prepare para %s: %+v\n", addr, req)

				resp, err := client.Prepare(ctx, req)
				if err != nil {
					log.Printf("[Proposer] Erro ao enviar Prepare para %s: %v", addr, err)
					return
				}
				if resp.Success {
					mu.Lock()
					promisesReceived++
					if resp.AcceptedProposalId > highestAcceptedID {
						highestAcceptedID = resp.AcceptedProposalId
						highestAcceptedCmd = resp.AcceptedCommand
					}
					mu.Unlock()
					log.Printf("[Proposer] Recebida Promise de %s. Accepted ID: %d, Command: %+v\n", addr, resp.AcceptedProposalId, resp.AcceptedCommand)
				} else {
					log.Printf("[Proposer] Acceptor %s recusou Prepare: %s\n", addr, resp.ErrorMessage)
				}
			}(service.Address)
		}

		wg.Wait()

		if promisesReceived < quorumSize {
			log.Printf("[Proposer] Não recebeu promessas suficientes (%d) para atingir o quorum de %d para o slot %d. Abortando proposta.\n", promisesReceived, quorumSize, slotID)
			return false
		}
	} else {
		log.Printf("[Proposer] Pulando Fase 1 (Prepare/Promise) para slot %d. Líder estável.\n", slotID)
	}

	finalCommand := cmd
	if highestAcceptedID > 0 && highestAcceptedCmd != nil {
		log.Printf("[Proposer] Um comando previamente aceito (%+v) foi encontrado para slot %d. Usando este comando.\n", highestAcceptedCmd, slotID)
		finalCommand = highestAcceptedCmd
	} else {
		log.Printf("[Proposer] Nenhum comando pré-existente para slot %d. Usando comando original %+v.\n", slotID, finalCommand)
	}

	var (
		acceptsReceived int
		wgAccepts       sync.WaitGroup
	)

	mu.Lock()
	acceptsReceived++ // Contamos o próprio nó como um Accept
	mu.Unlock()

	stateForLeaderSlot := s.GetSlotState(slotID)
	stateForLeaderSlot.HighestPromisedID = proposalID  // Atualiza o ID da proposta mais alta prometida
	stateForLeaderSlot.AcceptedProposedID = proposalID // Atualiza o ID da proposta aceita
	stateForLeaderSlot.AcceptedCommand = finalCommand  // Define o comando aceito

	s.mu.Lock()
	s.ApplyCommand(finalCommand)
	if slotID > s.highestSlotID {
		s.highestSlotID = slotID // Atualiza o highestSlotID se necessário
		log.Printf("[Proposer] Líder (%s) atualizou highestSlotID para %d após aceitar seu próprio comando.\n", s.nodeName, s.highestSlotID)
	}
	s.mu.Unlock()

	for _, service := range registryResp.Services {
		if service.Address == s.nodeAddress {
			continue // Pular o próprio nó
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
			client := paxosPB.NewPaxosClient(conn)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req := &paxosPB.AcceptRequest{
				SlotId:     slotID,
				ProposalId: proposalID,
				Command:    finalCommand,
			}

			log.Printf("[Proposer] Enviando Accept para %s: %+v\n", addr, req)

			resp, err := client.Accept(ctx, req)
			if err != nil {
				log.Printf("[Proposer] Erro ao enviar Accept para %s: %v", addr, err)
				return
			}

			if resp.Success {
				mu.Lock()
				acceptsReceived++
				mu.Unlock()
				log.Printf("[Proposer] Recebida Aceitação de %s para slot %d. Proposal ID: %d\n", addr, slotID, resp.CurrentProposalId)
			} else {
				log.Printf("[Proposer] Acceptor %s recusou Accept para slot %d: %s\n", addr, slotID, resp.ErrorMessage)

				if strings.Contains(resp.ErrorMessage, "proposal_id") && strings.Contains(resp.ErrorMessage, "promised") {
					s.mu.Lock()
					s.needsPreparePhase = true // Precisará de prepare na próxima
					s.mu.Unlock()
				}
			}
		}(service.Address)
	}

	wgAccepts.Wait()

	if acceptsReceived < quorumSize {
		log.Printf("[Proposer] Não obteve Quorum de Accepted (%d/%d) para slot %d. Comando NÃO DECIDIDO.\n", acceptsReceived, quorumSize, slotID)
		s.mu.Lock()
		s.needsPreparePhase = true // Precisará de prepare na próxima
		s.mu.Unlock()
		return false
	}

	log.Printf("[Proposer] Comando para slot %d DECIDIDO com sucesso! Comando final: %+v\n", slotID, finalCommand)

	return true
}
