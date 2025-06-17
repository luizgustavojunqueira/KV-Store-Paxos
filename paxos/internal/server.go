package internal

import (
	"sync"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"

	rpb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
)

type PaxosServer struct {
	pb.UnimplementedPaxosServer
	mu             sync.RWMutex
	state          map[string]*PaxosState
	registryClient rpb.RegistryClient
	nodeAddress    string
}

func NewPaxosServer(registryClient rpb.RegistryClient, nodeAddress string) *PaxosServer {
	return &PaxosServer{
		state:          make(map[string]*PaxosState),
		registryClient: registryClient,
		nodeAddress:    nodeAddress,
	}
}

func (s *PaxosServer) GetState(key string) *PaxosState {
	if _, ok := s.state[key]; !ok {
		s.state[key] = &PaxosState{}
	}
	return s.state[key]
}
