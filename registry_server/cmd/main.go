package main

import (
	"fmt"
	"log"
	"net"
	"time"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/registry_server/internal/repository"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/registry_server/internal/server"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/registry_server/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	repo := repository.NewInMemoryRepository()

	svc := service.NewRegistryService(repo, 30*time.Second) // Define o TTL para 30 segundos
	svc.StartCleanup()                                      // Inicia o processo de limpeza periódica
	registryServer := server.NewRegistryServer(svc)

	// Inicia o servidor gRPC
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	pb.RegisterRegistryServer(grpcServer, registryServer)

	reflection.Register(grpcServer)

	fmt.Println("Registry server is running on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
