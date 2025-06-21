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
	paxos "github.com/luizgustavojunqueira/KV-Store-Paxos/paxos/internal"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/kvstore"
	pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
	rpb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	registryAddr = flag.String("registryAddr", "localhost:50051", "Registry Server Address")
	name         = flag.String("nodeName", "node-1", "the name of the node") // Nome do nó para identificação
	nodeAddress  = flag.String("nodeAddr", "localhost:8080", "the address of the node (e.g., localhost:8080)")
	isProposer   = flag.Bool("isProposer", false, "Set to true if this node should try to become a proposer/leader")
)

func main() {
	flag.Parse()

	log.Printf("Iniciando Paxos Node '%s' no endereço '%s'\n", *name, *nodeAddress)

	// Conectar ao Registry Server
	conn, err := grpc.NewClient(*registryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Não foi possível conectar ao Registry: %v", err)
	}
	defer conn.Close()

	registryClient := rpb.NewRegistryClient(conn)

	log.Printf("Conectado ao Registry em %s", *registryAddr)

	// Registrar o nó no Registry
	_, err = registryClient.Register(context.Background(), &rpb.RegisterRequest{
		Name:    *name,
		Address: *nodeAddress,
	})
	if err != nil {
		log.Fatalf("Não foi possível registrar o nó: %v", err)
	}
	log.Printf("Nó '%s' registrado com sucesso no Registry", *name)

	// Iniciar o servidor gRPC para Paxos
	lis, err := net.Listen("tcp", *nodeAddress)
	if err != nil {
		log.Fatalf("Falha ao ouvir na porta %s: %v", *nodeAddress, err)
	}
	s := grpc.NewServer()

	// Criar a instância do PaxosServer (que agora gerencia todo o estado e lógica Paxos)
	paxosNode := paxos.NewPaxosServer(registryClient, *nodeAddress, *name)

	// Registrar os serviços gRPC
	pb.RegisterPaxosServer(s, paxosNode) // Registro dos métodos Prepare, Accept, ProposeLeader, SendHeartbeat

	kvstore.RegisterKVStoreServer(s, internal.NewKVStoreServer(paxosNode)) // Registro do KVStoreServer para operações de GET, SET, DELETE

	log.Printf("Paxos Server ouvindo em %v\n", lis.Addr())

	// Iniciar o servidor gRPC em uma goroutine
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Falha ao iniciar o servidor: %v", err)
		}
	}()

	// Iniciar o heartbeat para o Registry (para que o Registry saiba que o nó está vivo)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_, err := registryClient.Heartbeat(context.Background(), &rpb.HeartbeatRequest{
				Name:    *name,
				Address: *nodeAddress,
			})
			if err != nil {
				log.Printf("Erro no heartbeat para o Registry por %s: %v. Tentando re-registrar...\n", *name, err)
				_, err = registryClient.Register(context.Background(), &rpb.RegisterRequest{
					Name:    *name,
					Address: *nodeAddress,
				})
				if err != nil {
					log.Printf("Falha ao re-registrar nó %s no Registry: %v\n", *name, err)
				} else {
					log.Printf("Nó %s re-registrado com sucesso no Registry.\n", *name)
				}
			}
		}
	}()

	log.Printf("Paxos Node '%s' iniciado com sucesso no endereço '%s'.\n", *name, *nodeAddress)

	paxosNode.Start()

	// Se este nó for configurado para tentar ser o Proposer/Líder no início
	if *isProposer {
		// Dê um pequeno tempo para os outros nós iniciarem o monitoramento antes de uma possível eleição
		time.Sleep(2 * time.Second)
		paxosNode.LeaderElection() // Tentar iniciar a eleição de líder imediatamente ao iniciar
	}

	// Lógica da CLI para interagir com o nó (apenas se for configurado como proposer/líder)
	if *isProposer {
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("\n--- Paxos KV-Store Leader CLI ---")
		fmt.Println("Comandos:")
		fmt.Println("  set <key> <value>                    - Propoe um SET no próximo slot livre")
		fmt.Println("  delete <key>                         - Propoe um DELETE no próximo slot livre")
		fmt.Println("  get <key>                            - Consulta o valor local (do líder)")
		fmt.Println("  print                                - Mostra todo o KV Store local (do líder)")
		fmt.Println("  printlog 						  - Mostra o log de comandos propostos (do líder)")
		fmt.Println("  elect                                - Tenta iniciar uma eleição de líder (se você perdeu a liderança)")
		fmt.Println("  exit                                 - Sai da CLI e encerra o nó")
		fmt.Println("---------------------------------")

		for {
			fmt.Print("> ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input) // Remove espaços em branco e nova linha

			parts := strings.Fields(input) // Divide a string em palavras

			if len(parts) == 0 {
				continue
			}

			commandType := strings.ToLower(parts[0])
			var cmd *pb.Command
			var success bool

			switch commandType {
			case "set":
				if len(parts) < 3 {
					fmt.Println("Uso: set <key> <value>")
					continue
				}
				key := parts[1]
				value := []byte(strings.Join(parts[2:], " "))
				cmd = &pb.Command{
					Type:  pb.CommandType_SET,
					Key:   key,
					Value: value,
				}
				log.Printf("[CLI] Tentando SET key='%s' value='%s' (próximo slot)\n", key, string(value))
				success = paxosNode.ProposeCommand(cmd) // Usar a função para o próximo slot livre
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
				cmd = &pb.Command{
					Type: pb.CommandType_DELETE,
					Key:  key,
				}
				log.Printf("[CLI] Tentando DELETE key='%s' (próximo slot)\n", key)
				success = paxosNode.ProposeCommand(cmd) // Usar a função para o próximo slot livre
				if success {
					fmt.Printf("DELETE para '%s' proposta com sucesso!\n", key)
				} else {
					fmt.Printf("DELETE para '%s' falhou. Verifique os logs.\n", key)
				}

			case "print": // Imprimir todo o KV Store local
				kvStore := paxosNode.GetKVStore()
				fmt.Println("Estado atual do KV Store:")
				if len(kvStore) == 0 {
					fmt.Println("  (Vazio)")
				} else {
					for k, v := range kvStore {
						fmt.Printf("  %s: %s\n", k, string(v))
					}
				}

			case "elect": // Tenta iniciar uma eleição de líder
				paxosNode.LeaderElection()

			case "printlog":
				slots := paxosNode.GetAllSlots()
				fmt.Println("Log de comandos propostos:")
				if len(slots) == 0 {
					fmt.Println("  (Vazio)")
				} else {
					for slotID, state := range slots {
						fmt.Printf("  Slot %d: Promised ID: %d, Accepted ID: %d, Command: %+v\n",
							slotID, state.HighestPromisedID, state.AcceptedProposedID, state.AcceptedCommand)
					}
				}

			case "exit":
				fmt.Println("Saindo da CLI do líder.")
				return // Encerra a goroutine principal, permitindo que o programa termine

			default:
				fmt.Println("Comando desconhecido. Veja 'Comandos:' acima para opções.")
			}
		}

	} else {
		log.Println("[Main] Este nó está configurado como Acceptor/Learner.")
		// O select{} é necessário para manter a goroutine principal viva enquanto outras goroutines
		// (como o servidor gRPC, heartbeats do registry e o monitor de líder) estão rodando.

		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				kvStore := paxosNode.GetKVStore()
				fmt.Println("Estado atual do KV Store:")
				if len(kvStore) == 0 {
					fmt.Println("  (Vazio)")
				} else {
					for k, v := range kvStore {
						fmt.Printf("  %s: %s\n", k, string(v))
					}
				}
			}
		}()

		// Manter a goroutine principal viva
		select {}
	}
}
