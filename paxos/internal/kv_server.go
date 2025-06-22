package internal

import (
	"context"
	"sort"

	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/kvstore"
	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/kvstore"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
)

type KVStoreServer struct {
	pb.UnimplementedKVStoreServer
	paxosNode *PaxosServer
}

func NewKVStoreServer(paxosNode *PaxosServer) *KVStoreServer {
	return &KVStoreServer{
		paxosNode: paxosNode,
	}
}

func (s *KVStoreServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	kvstore := s.paxosNode.GetKVStore()

	value, found := kvstore[req.GetKey()]
	if found {
		return &pb.GetResponse{
			Found: true,
			Value: string(value),
		}, nil
	}

	return &pb.GetResponse{
		Found:        false,
		ErrorMessage: "Key not found",
	}, nil
}

func (s *KVStoreServer) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	s.paxosNode.mu.Lock()
	isLeader := s.paxosNode.isLeader
	s.paxosNode.mu.Unlock()

	if !isLeader {
		return &pb.SetResponse{
			Success:      false,
			ErrorMessage: "Não sou o líder. Não posso processar a requisição.",
		}, nil
	}

	cmd := &paxos.Command{Type: paxos.CommandType_SET, Key: req.GetKey(), Value: []byte(req.GetValue())}

	success := s.paxosNode.ProposeCommand(cmd)

	if !success {
		return &pb.SetResponse{
			Success:      false,
			ErrorMessage: "Falha ao propor comando.",
		}, nil
	}
	return &pb.SetResponse{
		Success: true,
	}, nil
}

func (s *KVStoreServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	s.paxosNode.mu.Lock()
	isLeader := s.paxosNode.isLeader
	s.paxosNode.mu.Unlock()

	if !isLeader {
		return &pb.DeleteResponse{
			Success:      false,
			ErrorMessage: "Não sou o líder. Não posso processar a requisição.",
		}, nil
	}

	cmd := &paxos.Command{Type: paxos.CommandType_DELETE, Key: req.GetKey()}

	success := s.paxosNode.ProposeCommand(cmd)

	if !success {
		return &pb.DeleteResponse{
			Success:      false,
			ErrorMessage: "Falha ao propor comando.",
		}, nil
	}
	return &pb.DeleteResponse{
		Success: true,
	}, nil
}

func (s *KVStoreServer) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	s.paxosNode.mu.RLock()
	defer s.paxosNode.mu.RUnlock()

	kvstore := s.paxosNode.GetKVStore()

	pairs := make([]*pb.KeyValuePair, 0, len(kvstore))
	for key, value := range kvstore {
		pairs = append(pairs, &pb.KeyValuePair{
			Key:   key,
			Value: string(value),
		})
	}

	if len(pairs) == 0 {
		return &pb.ListResponse{
			Pairs:        nil,
			ErrorMessage: "Nenhum par chave-valor encontrado.",
		}, nil
	}

	response := &pb.ListResponse{
		Pairs: pairs,
	}

	return response, nil
}

func (s *KVStoreServer) ListLog(ctx context.Context, req *pb.ListRequest) (*pb.ListLogResponse, error) {
	s.paxosNode.mu.RLock()
	defer s.paxosNode.mu.RUnlock()

	logs := s.paxosNode.GetAllSlots()

	if len(logs) == 0 {
		return &pb.ListLogResponse{
			Entries:      nil,
			ErrorMessage: "Nenhum log encontrado.",
		}, nil
	}

	response := &pb.ListLogResponse{
		Entries: make([]*pb.LogEntry, 0, len(logs)),
	}

	for id, log := range logs {
		response.Entries = append(response.Entries, &pb.LogEntry{
			SlotId: id,
			Command: &kvstore.Command{
				Type:       log.AcceptedCommand.Type,
				Key:        log.AcceptedCommand.Key,
				Value:      string(log.AcceptedCommand.Value),
				ProposalId: log.AcceptedProposedID,
			},
		})
	}

	// sort the entries by SlotId
	if len(response.Entries) > 0 {
		sort.Slice(response.Entries, func(i, j int) bool {
			return response.Entries[i].SlotId < response.Entries[j].SlotId
		})
	}

	return response, nil
}

func (s *KVStoreServer) TryElectSelf(ctx context.Context, req *pb.TryElectRequest) (*pb.TryElectResponse, error) {
	s.paxosNode.mu.Lock()
	if s.paxosNode.isLeader {
		return &pb.TryElectResponse{
			Success:      false,
			ErrorMessage: "Já sou o líder.",
		}, nil
	}
	s.paxosNode.mu.Unlock()

	success := s.paxosNode.LeaderElection()

	if !success {
		return &pb.TryElectResponse{
			Success:      false,
			ErrorMessage: "Falha ao tentar se eleger como líder.",
		}, nil
	}

	return &pb.TryElectResponse{
		Success: true,
	}, nil
}
