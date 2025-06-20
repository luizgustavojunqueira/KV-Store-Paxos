package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
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
	// testKeyValue = flag.String("testKeyValue", "default_value", "The value to propose if this node is a proposer")
	// testKey      = flag.String("testKey", "example-key", "The key to use for the test proposal")
	// fixedProposalID = flag.Int64("fixedProposalID", 0, "Use a fixed proposal ID for testing (0 means generate dynamically)")
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
		paxosNode.TryBecomeLeader()

		reader := bufio.NewReader(os.Stdin)
		fmt.Println("\n--- Paxos KV-Store Leader CLI ---")
		fmt.Println("Comandos disponíveis:")
		fmt.Println("  set <key> <value> - Propor um comando SET")
		fmt.Println("  delete <key> - Propor um comando DELETE")
		fmt.Println("  print - Imprimir o estado atual do KV Store")
		fmt.Println("  printlog - Imprimir o estado atual dos slots")
		fmt.Println("  exit - Sair da CLI")
		fmt.Println("---------------------------------")

		for {
			fmt.Print(">")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			parts := strings.Fields(input)

			if len(parts) < 1 {
				continue
			}

			commandType := strings.ToLower(parts[0])
			var cmd *paxos.Command
			var success bool

			switch commandType {
			case "set":
				if len(parts) != 3 {
					fmt.Println("Uso: set <key> <value>")
					continue
				}
				key := parts[1]
				value := []byte(strings.Join(parts[2:], " ")) // Junta o resto como valor
				cmd = &paxos.Command{
					Type:  paxos.CommandType_SET,
					Key:   key,
					Value: value,
				}
				log.Printf("[CLI] Tentando SET key='%s' value='%s'\n", key, string(value))
				success = paxosNode.ProposeCommand(cmd) // Chamar a função de proposta
				if success {
					fmt.Printf("SET para '%s' (%s) proposta com sucesso!\n", key, string(value))
				} else {
					fmt.Printf("SET para '%s' (%s) falhou. Verifique os logs.\n", key, string(value))
				}

			case "delete":
				if len(parts) < 2 {
					fmt.Println("Uso: delete <key>")
					continue
				}
				key := parts[1]
				cmd = &paxos.Command{
					Type: paxos.CommandType_DELETE,
					Key:  key,
				}
				log.Printf("[CLI] Tentando DELETE key='%s'\n", key)
				success = paxosNode.ProposeCommand(cmd) // Chamar a função de proposta
				if success {
					fmt.Printf("DELETE para '%s' proposta com sucesso!\n", key)
				} else {
					fmt.Printf("DELETE para '%s' falhou. Verifique os logs.\n", key)
				}

			case "exit":
				fmt.Println("Saindo da CLI do líder.")
				return // Sai da função main, o processo será encerrado pelo select{} ou trap

			case "print":
				keyValue := paxosNode.GetKVStore()
				fmt.Println("Estado atual do KV Store:")
				for k, v := range keyValue {
					fmt.Printf("  %s: %s\n", k, string(v))
				}
			case "printlog":
				slots := paxosNode.GetAllSlots()
				fmt.Println("Estado atual dos slots:")
				for slotID, state := range slots {
					if state.AcceptedCommand != nil {
						fmt.Printf("  Slot %d: ProposalId=%d, CommandType=%s, Key=%s, Value=%s\n",
							slotID, state.AcceptedProposedID, state.AcceptedCommand.Type,
							state.AcceptedCommand.Key, string(state.AcceptedCommand.Value))
					} else {
						fmt.Printf("  Slot %d: ProposalId=%d, Nenhum comando aceito\n",
							slotID, state.AcceptedProposedID)
					}
				}

			default:
				fmt.Printf("Comando desconhecido: %s\n", commandType)
			}

		}
	} else {
		log.Printf("Este nó está configurado como Acceptor. Aguardando propostas...\n")
		select {} // Mantém o servidor rodando indefinidamente
	}
}
