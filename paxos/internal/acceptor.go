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
