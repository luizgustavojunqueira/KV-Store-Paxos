/*
Package internal implements the Paxos logic for all the roles in the Paxos algorithm.
*/
package internal

import (
	"context"
	"fmt"
	"log"

	paxosPB "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
)

func (s *PaxosServer) Prepare(ctx context.Context, req *paxosPB.PrepareRequest) (*paxosPB.PrepareResponse, error) {
	state := s.GetSlotState(req.SlotId)

	log.Printf("[Acceptor] Slot %d: Recebido Prepare com proposal_id %d. Estado atual: promised=%d, accepted_n=%d\n",
		req.SlotId, req.ProposalId, state.HighestPromisedID, state.AcceptedProposedID)

	// Se a proposta é maior que a maior promessa já feita, aceitamos
	if req.ProposalId > state.HighestPromisedID {
		state.HighestPromisedID = req.ProposalId // Promessa feita

		return &paxosPB.PrepareResponse{
			Success:            true,
			AcceptedProposalId: state.AcceptedProposedID,
			AcceptedCommand:    state.AcceptedCommand,
			CurrentProposalId:  state.HighestPromisedID,
		}, nil
	} else {
		// Se a proposta é menor ou igual, rejeitamos
		return &paxosPB.PrepareResponse{
			Success: false,
			ErrorMessage: fmt.Sprintf("Já prometi para uma proposta %d maior ou igual a %d para o slot %d",
				state.HighestPromisedID, req.ProposalId, req.SlotId),
			CurrentProposalId: state.HighestPromisedID,
		}, nil
	}
}

func (s *PaxosServer) Accept(ctx context.Context, req *paxosPB.AcceptRequest) (*paxosPB.AcceptResponse, error) {
	state := s.GetSlotState(req.SlotId) // Obter estado para o slot específico

	log.Printf("[Acceptor] Slot %d: Recebido Accept com proposal_id %d e comando %+v. Estado atual: promised=%d, accepted_n=%d\n",
		req.SlotId, req.ProposalId, req.Command, state.HighestPromisedID, state.AcceptedProposedID)

	// Verifica se a proposta é maior ou igual à maior promessa já feita
	if req.ProposalId >= state.HighestPromisedID {
		state.HighestPromisedID = req.ProposalId
		state.AcceptedProposedID = req.ProposalId
		state.AcceptedCommand = req.Command

		log.Printf("[Acceptor] Slot %d: Aceito ProposalId %d com comando %+v\n",
			req.SlotId, req.ProposalId, req.Command)

		s.mu.Lock()
		s.ApplyCommand(req.Command) // Aplicar o comando no KV Store
		s.mu.Unlock()

		// Atualiza o highestSlotID se necessário
		if req.SlotId > s.highestSlotID {
			s.mu.Lock()
			s.highestSlotID = req.SlotId
			s.mu.Unlock()
			log.Printf("[Acceptor] Atualizado highestSlotID para %d\n", s.highestSlotID)
		}

		return &paxosPB.AcceptResponse{
			Success:           true,
			CurrentProposalId: state.HighestPromisedID,
		}, nil
	} else {
		// Se a proposta é menor que a maior promessa já feita, rejeitamos
		return &paxosPB.AcceptResponse{
			Success: false,
			ErrorMessage: fmt.Sprintf("Já prometi para uma proposta %d maior ou igual a %d para o slot %d",
				state.HighestPromisedID, req.ProposalId, req.SlotId),
			CurrentProposalId: state.HighestPromisedID,
		}, nil
	}
}

// Learn é chamado por um nó aprendiz para obter o valor decidido de um slot específico.
func (s *PaxosServer) Learn(ctx context.Context, req *paxosPB.LearnRequest) (*paxosPB.LearnResponse, error) {
	slotID := req.SlotId

	state := s.GetSlotState(slotID)

	if state == nil || state.AcceptedCommand == nil {
		log.Printf("[Learner] Nó %s requisitou o valor do slot %d, mas não há valor decidido aqui.", "remoto", slotID)
		return &paxosPB.LearnResponse{Decided: false}, nil
	}

	log.Printf("[Learner] Nó %s requisitou o valor do slot %d. Enviando comando: %v", "remoto", slotID, state.AcceptedCommand)
	// Envia o comando aceito para o aprendiz
	return &paxosPB.LearnResponse{
		Decided: true,
		Command: &paxosPB.Command{
			Type:       state.AcceptedCommand.Type,
			Key:        state.AcceptedCommand.Key,
			Value:      state.AcceptedCommand.Value,
			ProposalId: state.AcceptedProposedID,
		},
	}, nil
}
