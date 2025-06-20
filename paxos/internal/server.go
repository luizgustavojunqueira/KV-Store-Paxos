package internal

import (
	"log"
	"maps"
	"sync"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"

	rpb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
)

type PaxosServer struct {
	pb.UnimplementedPaxosServer
	mu             sync.RWMutex
	slots          map[int64]*PaxosState
	kvStore        map[string][]byte
	registryClient rpb.RegistryClient
	nodeAddress    string
	leaderID       string
	isLeader       bool
	highestSlotID  int64
}

func NewPaxosServer(registryClient rpb.RegistryClient, nodeAddress string) *PaxosServer {
	return &PaxosServer{
		slots:          make(map[int64]*PaxosState),
		kvStore:        make(map[string][]byte),
		registryClient: registryClient,
		nodeAddress:    nodeAddress,
		leaderID:       "",
		isLeader:       false,
		highestSlotID:  0,
	}
}

func (s *PaxosServer) GetSlotState(slotID int64) *PaxosState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.slots[slotID]; !ok {
		s.slots[slotID] = &PaxosState{}
	}
	return s.slots[slotID]
}

func (s *PaxosServer) ApplyCommand(cmd *pb.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd.Type {
	case pb.CommandType_SET:
		log.Printf("[KVStore] Aplicando SET: %s = %s\n", cmd.Key, string(cmd.Value))
		s.kvStore[cmd.Key] = cmd.Value
	case pb.CommandType_DELETE:
		log.Printf("[KVStore] Aplicando DELETE: %s\n", cmd.Key)
		delete(s.kvStore, cmd.Key)
	default:
		log.Printf("[KVStore] Comando desconhecido: %+v\n", cmd)
	}
}

func (s *PaxosServer) GetKVStore() map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Retorna uma cópia do KV Store para evitar modificações externas
	kvStoreCopy := make(map[string][]byte)
	maps.Copy(kvStoreCopy, s.kvStore)
	return kvStoreCopy
}

func (s *PaxosServer) GetAllSlots() map[int64]*PaxosState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Retorna uma cópia dos slots para evitar modificações externas
	slotsCopy := make(map[int64]*PaxosState)
	for k, v := range s.slots {
		slotsCopy[k] = &PaxosState{
			HighestPromisedID:  v.HighestPromisedID,
			AcceptedProposedID: v.AcceptedProposedID,
			AcceptedCommand:    v.AcceptedCommand,
		}
	}
	return slotsCopy
}
