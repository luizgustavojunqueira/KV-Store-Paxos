/*
Package internal provides the core implementation of the Paxos distributed consensus algorithm.
It includes the roles of Proposer, Acceptor, and the state management for a single instance of Paxos.
This package is designed to be extended for Multi-Paxos to support a replicated log.
*/
package internal

import (
	"context"
	"fmt"
	"log"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
)

func (s *PaxosServer) Prepare(ctx context.Context, req *pb.PrepareRequest) (*pb.PrepareResponse, error) {
	state := s.GetSlotState(req.SlotId)

	log.Printf("[Acceptor] Slot %d: Recebido Prepare com proposal_id %d. Estado atual: promised=%d, accepted_n=%d\n",
		req.SlotId, req.ProposalId, state.HighestPromisedID, state.AcceptedProposedID)

	if req.ProposalId > state.HighestPromisedID {
		state.HighestPromisedID = req.ProposalId // Promessa feita

		return &pb.PrepareResponse{
			Success:            true,
			AcceptedProposalId: state.AcceptedProposedID,
			AcceptedCommand:    state.AcceptedCommand,
			CurrentProposalId:  state.HighestPromisedID,
		}, nil
	} else {
		return &pb.PrepareResponse{
			Success: false,
			ErrorMessage: fmt.Sprintf("Já prometi para uma proposta %d maior ou igual a %d para o slot %d",
				state.HighestPromisedID, req.ProposalId, req.SlotId),
			CurrentProposalId: state.HighestPromisedID,
		}, nil
	}
}

func (s *PaxosServer) Accept(ctx context.Context, req *pb.AcceptRequest) (*pb.AcceptResponse, error) {
	state := s.GetSlotState(req.SlotId) // Obter estado para o slot específico

	log.Printf("[Acceptor] Slot %d: Recebido Accept com proposal_id %d e comando %+v. Estado atual: promised=%d, accepted_n=%d\n",
		req.SlotId, req.ProposalId, req.Command, state.HighestPromisedID, state.AcceptedProposedID)

	if req.ProposalId >= state.HighestPromisedID {
		state.HighestPromisedID = req.ProposalId
		state.AcceptedProposedID = req.ProposalId
		state.AcceptedCommand = req.Command

		log.Printf("[Acceptor] Slot %d: Aceito ProposalId %d com comando %+v\n",
			req.SlotId, req.ProposalId, req.Command)

		s.mu.Lock()
		s.ApplyCommand(req.Command) // Aplicar o comando no KV Store
		s.mu.Unlock()

		if req.SlotId > s.highestSlotID {
			s.mu.Lock()
			s.highestSlotID = req.SlotId
			s.mu.Unlock()
			log.Printf("[Acceptor] Atualizado highestSlotID para %d\n", s.highestSlotID)
		}

		return &pb.AcceptResponse{
			Success:           true,
			CurrentProposalId: state.HighestPromisedID,
		}, nil
	} else {
		return &pb.AcceptResponse{
			Success: false,
			ErrorMessage: fmt.Sprintf("Já prometi para uma proposta %d maior ou igual a %d para o slot %d",
				state.HighestPromisedID, req.ProposalId, req.SlotId),
			CurrentProposalId: state.HighestPromisedID,
		}, nil
	}
}

func (s *PaxosServer) Learn(ctx context.Context, req *pb.LearnRequest) (*pb.LearnResponse, error) {
	slotID := req.SlotId

	state := s.GetSlotState(slotID)

	if state == nil || state.AcceptedCommand == nil {
		log.Printf("[Learner] Nó %s requisitou o valor do slot %d, mas não há valor decidido aqui.", "remoto", slotID)
		return &pb.LearnResponse{Decided: false}, nil
	}

	log.Printf("[Learner] Nó %s requisitou o valor do slot %d. Enviando comando: %v", "remoto", slotID, state.AcceptedCommand)
	return &pb.LearnResponse{
		Decided: true,
		Command: state.AcceptedCommand,
	}, nil
}
