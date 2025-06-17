package main

import (
	"context"
	"flag"
	"log"
	"net"
	"time"

	"github.com/luizgustavojunqueira/KV-Store-Paxos/paxos/internal"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	registryAddr = flag.String("registryAddr", "localhost:50051", "Registry Server Address")
	name         = flag.String("nodeName", "node-1", "the name of the node")
	nodeAddress  = flag.String("nodeAddr", "localhost:8080", "the address of the node (e.g., localhost:8080)")
	isProposer   = flag.Bool("isProposer", false, "Set to true if this node should act as a proposer for a test key")
	key          = flag.String("key", "test_key", "the key to be used for the proposal (only relevant if isProposer is true)")
	value        = flag.String("value", "test_value", "the value to be used for the proposal (only relevant if isProposer is true)")
	proposalID   = flag.Int64("proposalID", 1, "the proposal ID to be used for the proposal (only relevant if isProposer is true)")
)

func main() {
	flag.Parse()

	log.Printf("Iniciando Paxos Node '%s' no endereço '%s'\n", *name, *nodeAddress)

	conn, err := grpc.NewClient(*registryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao Registry: %v", err)
	}
	defer conn.Close()

	// Connect to registry server
	registryClient := registry.NewRegistryClient(conn)

	log.Printf("Conectado ao Registry em %s", *registryAddr)

	// Register the node with the registry

	_, err = registryClient.Register(context.Background(), &registry.RegisterRequest{
		Name:    *name,
		Address: *nodeAddress,
	})
	if err != nil {
		log.Fatalf("Não foi possível registrar o nó: %v", err)
	}
	log.Printf("Nó '%s' registrado com sucesso no Registry", *name)

	// Start the Paxos server
	lis, err := net.Listen("tcp", *nodeAddress)
	if err != nil {
		log.Fatalf("Falha ao ouvir na porta %s: %v", *nodeAddress, err)
	}
	s := grpc.NewServer()
	paxosNode := internal.NewPaxosServer(registryClient, *nodeAddress) // Passar o cliente do registry e o próprio endereço
	paxos.RegisterPaxosServer(s, paxosNode)
	log.Printf("Paxos Server ouvindo em %v\n", lis.Addr())

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Falha ao iniciar o servidor: %v", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second) // A cada 5 segundos
		defer ticker.Stop()
		for range ticker.C {
			_, err := registryClient.Heartbeat(context.Background(), &registry.HeartbeatRequest{
				Name:    *name,
				Address: *nodeAddress,
			})
			if err != nil {
				log.Printf("Erro no heartbeat para %s: %v. Tentando re-registrar...\n", *name, err)
				_, err = registryClient.Register(context.Background(), &registry.RegisterRequest{
					Name:    *name,
					Address: *nodeAddress,
				})
				if err != nil {
					log.Printf("Falha ao re-registrar nó %s: %v\n", *name, err)
				} else {
					log.Printf("Nó %s re-registrado com sucesso.\n", *name)
				}
			}
		}
	}()

	log.Printf("Paxos Node '%s' iniciado com sucesso no endereço '%s'.\n", *name, *nodeAddress)

	// If this node is a proposer, simulate a proposal
	if *isProposer {
		log.Printf("Este nó está configurado como Proposer. Iniciando simulação de proposta...\n")
		time.Sleep(3 * time.Second)
		// Simulate a proposer for testing
		paxosNode.SimulateProposer(*key, []byte(*value), *proposalID)
		log.Printf("Proposta simulada para chave '%s' com valor '%s'.\n", *key, *value)
	} else {
		log.Printf("Este nó está configurado como Acceptor. Aguardando propostas...\n")
	}
	select {} // Keep the server running indefinitely
}
