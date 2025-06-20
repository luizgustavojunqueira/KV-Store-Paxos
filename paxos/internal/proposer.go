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

func (s *PaxosServer) ProposeCommand(cmd *pb.Command) bool {
	s.mu.Lock() // Proteger o acesso a nextSlotID e isLeader
	if !s.isLeader {
		s.mu.Unlock()
		log.Printf("[Proposer] Não sou o líder. Não posso propor comando %+v\n", cmd)
		return false // A lógica real de Paxos teria que encontrar o líder ou tentar se tornar um
	}
	slotID := s.highestSlotID + 1 // Tentar propor no próximo slot livre
	s.mu.Unlock()

	log.Printf("[Proposer] Tentando propor comando %+v para o slot %d\n", cmd, slotID)

	proposalID := time.Now().UnixNano() // Gerar um ID de proposta único baseado no tempo

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
		highestAcceptedCmd *pb.Command
		mu                 sync.Mutex
		wg                 sync.WaitGroup
	)

	for _, service := range registryResp.Services {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[Proposer] Não foi possível conectar ao Acceptor %s: %v\n", addr, err)
				return
			}
			defer conn.Close()
			client := pb.NewPaxosClient(conn)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req := &pb.PrepareRequest{
				SlotId:     slotID,
				ProposalId: proposalID,
			}

			if addr == s.nodeAddress {
				resp, err := s.Prepare(context.Background(), req) // Call local method
				if err != nil {
					log.Printf("[Proposer] Erro ao enviar Prepare para self: %v", err)
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

					log.Printf("[Proposer] (Self) Recebida Promise. Accepted ID: %d, Command: %+v\n", resp.AcceptedProposalId, resp.AcceptedCommand)
				} else {
					log.Printf("[Proposer] (Self) Prepare falhou: %s\n", resp.ErrorMessage)
				}
				return
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

	for _, service := range registryResp.Services {
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

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req := &pb.AcceptRequest{
				SlotId:     slotID,
				ProposalId: proposalID,
				Command:    finalCommand,
			}

			if addr == s.nodeAddress {
				resp, err := s.Accept(context.Background(), req) // Call local method
				if err != nil {
					log.Printf("[Proposer] Erro ao enviar Accept para self: %v", err)
					return
				}
				if resp.Success {
					mu.Lock()
					acceptsReceived++
					mu.Unlock()
					log.Printf("[Proposer] (Self) Aceito Accept. Current Proposal ID: %d\n", resp.CurrentProposalId)
				} else {
					log.Printf("[Proposer] (Self) Acceptor recusou Accept: %s\n", resp.ErrorMessage)
				}
				return
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
			}
		}(service.Address)
	}

	wgAccepts.Wait()

	if acceptsReceived < quorumSize {
		log.Printf("[Proposer] Não obteve Quorum de Accepted (%d/%d) para slot %d. Comando NÃO DECIDIDO.\n", acceptsReceived, quorumSize, slotID)
		return false
	}

	log.Printf("[Proposer] Comando para slot %d DECIDIDO com sucesso! Comando final: %+v\n", slotID, finalCommand)

	s.mu.Lock()
	if slotID > s.highestSlotID {
		s.highestSlotID = slotID
	}
	s.mu.Unlock()
	return true
}

func (s *PaxosServer) TryBecomeLeader() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isLeader = true // Para simulação, apenas o nó com a flag se torna líder
	log.Printf("[Leader Election] %s se declarou LÍDER.\n", s.nodeAddress)
	return true
}
