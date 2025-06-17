package server

import (
	"context"
	"log"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/registry_server/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Service interface {
	Register(name, address string) error
	ListAll() ([]*model.Service, error)
}

// RegistryServer implements the gRPC server for the service registry
type RegistryServer struct {
	pb.UnimplementedRegistryServer
	service Service
}

// NewRegistryServer creates a new instance of RegistryServer
func NewRegistryServer(s Service) *RegistryServer {
	return &RegistryServer{service: s}
}

// Register registers a new service with the registry
func (s *RegistryServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	err := s.service.Register(req.Name, req.Address)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	log.Printf("Service registered: %s at %s", req.Name, req.Address)
	return &pb.RegisterResponse{Success: true}, nil
}

// ListAll returns a list of all registered services
func (s *RegistryServer) ListAll(ctx context.Context, _ *emptypb.Empty) (*pb.ListResponse, error) {
	services, err := s.service.ListAll()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.Println("Listing all services")

	response := &pb.ListResponse{}
	for _, svc := range services {
		response.Services = append(response.Services, &pb.ServiceEntry{
			Name:    svc.Name,
			Address: svc.Address,
		})
	}
	return response, nil
}

// Heartbeat updates the last heartbeat time for a service
func (s *RegistryServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	err := s.service.Register(req.Name, req.Address)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	log.Printf("Heartbeat received for service: %s at %s", req.Name, req.Address)
	return &pb.HeartbeatResponse{Success: true}, nil
}
