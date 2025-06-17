package main

import (
	"context"
	"flag"
	"log"
	"time"

	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	addr    = flag.String("addr", "localhost:50051", "the address to connect to")
	name    = flag.String("name", "example_service", "the name of the service to register")
	address = flag.String("address", "localhost:8080", "the address of the service to register")
)

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", &err)
	}

	defer conn.Close()

	client := pb.NewRegistryClient(conn)

	log.Printf("Connecting to registry at %s", *addr)
	// Register the service with the registry
	if *name == "" || *address == "" {
		log.Fatal("Service name and address must be provided")
	}

	log.Printf("Registering service: %s at %s", *name, *address)

	// Example usage of the client
	_, err = client.Register(context.Background(), &pb.RegisterRequest{
		Name:    *name,
		Address: *address,
	})
	if err != nil {
		log.Fatalf("could not register service: %v", err)
	}

	go func() {
		for range time.Tick(10 * time.Second) {
			log.Printf("Sending heartbeat for service: %s at %s", *name, *address)
			_, err := client.Heartbeat(context.Background(), &pb.HeartbeatRequest{
				Name:    *name,
				Address: *address,
			})
			if err != nil {
				log.Printf("could not send heartbeat: %v", err)
			}
		}
	}()

	go func() {
		for range time.Tick(5 * time.Second) {
			log.Println("Listing all services")
			resp, err := client.ListAll(context.Background(), &emptypb.Empty{})
			if err != nil {
				log.Printf("could not list services: %v", err)
				continue
			}
			for _, svc := range resp.Services {
				log.Printf("Service: %s at %s", svc.Name, svc.Address)
			}
		}
	}()

	time.Sleep(24 * time.Hour)
}
