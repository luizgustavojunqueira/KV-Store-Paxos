/*
Package internal implements the Paxos logic for all the roles in the Paxos algorithm.
*/
package internal

import (
	"context"
	"sort"

	kvstorePB "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/kvstore"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
)

// KVStoreServer implementa o serviço gRPC para o KV Store usando o Paxos.
type KVStoreServer struct {
	kvstorePB.UnimplementedKVStoreServer
	paxosNode *PaxosServer
}

// NewKVStoreServer cria uma nova instância do KVStoreServer com o PaxosNode fornecido.
func NewKVStoreServer(paxosNode *PaxosServer) *KVStoreServer {
	return &KVStoreServer{
		paxosNode: paxosNode,
	}
}

// Get implementa o método Get do KVStoreServer, que busca um valor associado a uma chave.
func (s *KVStoreServer) Get(ctx context.Context, req *kvstorePB.GetRequest) (*kvstorePB.GetResponse, error) {
	kvstore := s.paxosNode.GetKVStore()

	value, found := kvstore[req.GetKey()]
	if found {
		return &kvstorePB.GetResponse{
			Found: true,
			Value: string(value),
		}, nil
	}

	return &kvstorePB.GetResponse{
		Found:        false,
		ErrorMessage: "Key not found",
	}, nil
}

// Set implementa o método Set do KVStoreServer, que define um valor para uma chave.
func (s *KVStoreServer) Set(ctx context.Context, req *kvstorePB.SetRequest) (*kvstorePB.SetResponse, error) {
	s.paxosNode.mu.Lock()
	isLeader := s.paxosNode.isLeader
	s.paxosNode.mu.Unlock()

	if !isLeader {
		return &kvstorePB.SetResponse{
			Success:      false,
			ErrorMessage: "Não sou o líder. Não posso processar a requisição.",
		}, nil
	}

	cmd := &paxos.Command{Type: paxos.CommandType_SET, Key: req.GetKey(), Value: []byte(req.GetValue())}

	// Propor o comando ao Paxos
	success := s.paxosNode.ProposeCommand(cmd)

	if !success {
		return &kvstorePB.SetResponse{
			Success:      false,
			ErrorMessage: "Falha ao propor comando.",
		}, nil
	}
	return &kvstorePB.SetResponse{
		Success: true,
	}, nil
}

// Delete implementa o método Delete do KVStoreServer, que remove uma chave do KV Store.
func (s *KVStoreServer) Delete(ctx context.Context, req *kvstorePB.DeleteRequest) (*kvstorePB.DeleteResponse, error) {
	s.paxosNode.mu.Lock()
	isLeader := s.paxosNode.isLeader
	s.paxosNode.mu.Unlock()

	if !isLeader {
		return &kvstorePB.DeleteResponse{
			Success:      false,
			ErrorMessage: "Não sou o líder. Não posso processar a requisição.",
		}, nil
	}

	cmd := &paxos.Command{Type: paxos.CommandType_DELETE, Key: req.GetKey()}

	// Propor o comando ao Paxos
	success := s.paxosNode.ProposeCommand(cmd)

	if !success {
		return &kvstorePB.DeleteResponse{
			Success:      false,
			ErrorMessage: "Falha ao propor comando.",
		}, nil
	}
	return &kvstorePB.DeleteResponse{
		Success: true,
	}, nil
}

// List implementa o método List do KVStoreServer, que lista todos os pares chave-valor no KV Store.
func (s *KVStoreServer) List(ctx context.Context, req *kvstorePB.ListRequest) (*kvstorePB.ListResponse, error) {
	s.paxosNode.mu.RLock()
	defer s.paxosNode.mu.RUnlock()

	// Obtém o KV Store do PaxosNode
	kvstore := s.paxosNode.GetKVStore()

	pairs := make([]*kvstorePB.KeyValuePair, 0, len(kvstore))
	for key, value := range kvstore {
		pairs = append(pairs, &kvstorePB.KeyValuePair{
			Key:   key,
			Value: string(value),
		})
	}

	if len(pairs) == 0 {
		return &kvstorePB.ListResponse{
			Pairs:        nil,
			ErrorMessage: "Nenhum par chave-valor encontrado.",
		}, nil
	}

	response := &kvstorePB.ListResponse{
		Pairs: pairs,
	}

	return response, nil
}

// ListLog implementa o método ListLog do KVStoreServer, que lista todos os logs de slots.
func (s *KVStoreServer) ListLog(ctx context.Context, req *kvstorePB.ListRequest) (*kvstorePB.ListLogResponse, error) {
	s.paxosNode.mu.RLock()
	defer s.paxosNode.mu.RUnlock()

	logs := s.paxosNode.GetAllSlots()

	if len(logs) == 0 {
		return &kvstorePB.ListLogResponse{
			Entries:      nil,
			ErrorMessage: "Nenhum log encontrado.",
		}, nil
	}

	response := &kvstorePB.ListLogResponse{
		Entries: make([]*kvstorePB.LogEntry, 0, len(logs)),
	}

	for id, log := range logs {
		response.Entries = append(response.Entries, &kvstorePB.LogEntry{
			SlotId: id,
			Command: &kvstorePB.Command{
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

// TryElectSelf implementa o método TryElectSelf do KVStoreServer, que tenta eleger o nó atual como líder.
func (s *KVStoreServer) TryElectSelf(ctx context.Context, req *kvstorePB.TryElectRequest) (*kvstorePB.TryElectResponse, error) {
	s.paxosNode.mu.Lock()
	if s.paxosNode.isLeader {
		return &kvstorePB.TryElectResponse{
			Success:      false,
			ErrorMessage: "Já sou o líder.",
		}, nil
	}
	s.paxosNode.mu.Unlock()

	// Tenta iniciar uma eleição de líder
	success := s.paxosNode.LeaderElection()

	if !success {
		return &kvstorePB.TryElectResponse{
			Success:      false,
			ErrorMessage: "Falha ao tentar se eleger como líder.",
		}, nil
	}

	return &kvstorePB.TryElectResponse{
		Success: true,
	}, nil
}
