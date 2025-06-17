package repository

import (
	"sync"
	"time"

	"github.com/luizgustavojunqueira/KV-Store-Paxos/registry_server/internal/model"
)

// InMemoryRepository implements a simple in-memory service registry
type InMemoryRepository struct {
	mu       sync.RWMutex
	services map[string]*model.Service
}

// NewInMemoryRepository creates a new instance of InMemoryRepository
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		services: make(map[string]*model.Service),
	}
}

// Save adds or updates a service in the repository
func (r *InMemoryRepository) Save(service *model.Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	service.LastHeartBeat = time.Now().Unix()
	r.services[service.Name] = service
	return nil
}

// FindAll retrieves all services from the repository
func (r *InMemoryRepository) FindAll() ([]*model.Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	services := make([]*model.Service, 0, len(r.services))
	for _, svc := range r.services {
		services = append(services, svc)
	}
	return services, nil
}

// Remove todos os serviços cujo LastSeen < agora - ttl
func (r *InMemoryRepository) CleanupExpired(ttl time.Duration) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, service := range r.services {
		if now.Unix()-service.LastHeartBeat > int64(ttl.Seconds()) {
			delete(r.services, name)
		}
	}
	if len(r.services) == 0 {
		r.services = make(map[string]*model.Service) // Limpa o mapa se estiver vazio
	}
}
