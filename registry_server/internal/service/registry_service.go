package service

import (
	"errors"
	"time"

	"github.com/luizgustavojunqueira/KV-Store-Paxos/registry_server/internal/model"
)

type Repository interface {
	Save(service *model.Service) error
	FindAll() ([]*model.Service, error)
	CleanupExpired(ttl time.Duration)
}

type RegistryService struct {
	repo Repository
	ttl  time.Duration
}

// NewRegistryService creates a new instance of RegistryService
func NewRegistryService(repo Repository, ttl time.Duration) *RegistryService {
	return &RegistryService{repo: repo}
}

// Register adds a new service to the registry
func (s *RegistryService) Register(name, address string) error {
	if name == "" {
		return errors.New("service name cannot be empty")
	}
	if address == "" {
		return errors.New("service address cannot be empty")
	}

	service := &model.Service{
		Name:    name,
		Address: address,
	}

	return s.repo.Save(service)
}

// ListAll retrieves all registered services from the repository
func (s *RegistryService) ListAll() ([]*model.Service, error) {
	services, err := s.repo.FindAll()
	if err != nil {
		return nil, err
	}
	return services, nil
}

func (s *RegistryService) StartCleanup() {
	ticker := time.NewTicker(5 * time.Second) // Define o intervalo de limpeza
	go func() {
		for range ticker.C {
			s.repo.CleanupExpired(5 * time.Second) // Limpa serviços expirados a cada 10 segundos
		}
	}()
}
