package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/kvstore"
	"github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"
	rpb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

type NodeInfo struct {
	Name             string `json:"name"`
	Address          string `json:"address"`
	IsLeader         bool   `json:"is_leader"`
	LeaderProposalID int64  `json:"leader_proposal_id"`
	HighestSlotID    int64  `json:"highest_slot_id"`
}

type WebServer struct {
	registryClient rpb.RegistryClient
	nodes          []*NodeInfo
	leaderAddress  string
	mu             sync.RWMutex
}

func (ws *WebServer) updateNodesInfo() {
	log.Println("Atualizando informações dos nós...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ws.registryClient.ListAll(ctx, &emptypb.Empty{})
	if err != nil {
		log.Printf("Erro ao listar serviços do registry: %v", err)
		return
	}

	newNodes := make([]*NodeInfo, 0, len(resp.Services))
	currentLeaderID := int64(0)
	currentLeaderAddr := ""

	for _, service := range resp.Services {
		nodeInfo := &NodeInfo{
			Name:    service.Name,
			Address: service.Address,
		}

		conn, err := grpc.NewClient(service.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Erro ao conectar ao nó %s: %v", service.Name, err)
			nodeInfo.IsLeader = false
			newNodes = append(newNodes, nodeInfo)
			continue
		}
		defer conn.Close()
		client := paxos.NewPaxosClient(conn)

		statusResp, err := client.GetStatus(ctx, &paxos.GetStatusRequest{})
		if err != nil {
			log.Printf("Erro ao obter status do nó %s: %v", service.Name, err)
			nodeInfo.IsLeader = false
			newNodes = append(newNodes, nodeInfo)
			continue
		}

		nodeInfo.IsLeader = statusResp.IsLeader
		nodeInfo.LeaderProposalID = statusResp.LeaderProposalID
		nodeInfo.HighestSlotID = statusResp.HighestSlotID

		if nodeInfo.IsLeader && statusResp.LeaderProposalID > currentLeaderID {
			currentLeaderID = statusResp.LeaderProposalID
			currentLeaderAddr = service.Address
		}

		newNodes = append(newNodes, nodeInfo)
	}

	ws.mu.Lock()
	ws.nodes = newNodes
	ws.leaderAddress = currentLeaderAddr
	ws.mu.Unlock()
	log.Printf("Informações dos nós atualizadas. Líder atual: %s", currentLeaderAddr)
}

func (ws *WebServer) indexHandler(w http.ResponseWriter, r *http.Request) {
	ws.mu.Lock()
	data := struct {
		Nodes         []*NodeInfo `json:"nodes"`
		LeaderAddress string      `json:"leader_address"`
	}{
		Nodes:         ws.nodes,
		LeaderAddress: ws.leaderAddress,
	}
	ws.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Erro ao codificar resposta JSON", http.StatusInternalServerError)
		log.Printf("Erro ao codificar resposta JSON: %v", err)
		return
	}
	log.Println("Requisição recebida: /")
}

type Request struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	NodeAddress string `json:"node_address"`
}

func (ws *WebServer) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Erro ao decodificar JSON", http.StatusBadRequest)
		log.Printf("Erro ao decodificar JSON: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(req.NodeAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Erro ao conectar ao líder", http.StatusInternalServerError)
		log.Printf("Erro ao conectar ao líder: %v", err)
		return
	}
	defer conn.Close()

	client := kvstore.NewKVStoreClient(conn)

	kvStoreReq := &kvstore.SetRequest{
		Key:   req.Key,
		Value: req.Value,
	}
	resp, err := client.Set(ctx, kvStoreReq)
	if err != nil {
		http.Error(w, "Erro ao definir valor", http.StatusInternalServerError)
		log.Printf("Erro ao definir valor: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Erro ao codificar resposta JSON", http.StatusInternalServerError)
		log.Printf("Erro ao codificar resposta JSON: %v", err)
		return
	}
	log.Println("Requisição recebida: /set")
}

func (ws *WebServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Erro ao decodificar JSON", http.StatusBadRequest)
		log.Printf("Erro ao decodificar JSON: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(req.NodeAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Erro ao conectar ao líder", http.StatusInternalServerError)
		log.Printf("Erro ao conectar ao líder: %v", err)
		return
	}
	defer conn.Close()

	client := kvstore.NewKVStoreClient(conn)

	kvStoreReq := &kvstore.DeleteRequest{
		Key: req.Key,
	}
	resp, err := client.Delete(ctx, kvStoreReq)
	if err != nil {
		http.Error(w, "Erro ao deletar valor", http.StatusInternalServerError)
		log.Printf("Erro ao deletar valor: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Erro ao codificar resposta JSON", http.StatusInternalServerError)
		log.Printf("Erro ao codificar resposta JSON: %v", err)
		return
	}
	log.Println("Requisição recebida: /delete")
}

func (ws *WebServer) handleGetStore(w http.ResponseWriter, r *http.Request) {
	// Get the KV Store from the requested node
	if r.Method != http.MethodGet {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	nodeAddress := r.PathValue("node_address")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(nodeAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Erro ao conectar ao líder", http.StatusInternalServerError)
		log.Printf("Erro ao conectar ao líder: %v", err)
		return
	}
	defer conn.Close()

	client := kvstore.NewKVStoreClient(conn)
	resp, err := client.List(ctx, &kvstore.ListRequest{})
	if err != nil {
		http.Error(w, "Erro ao obter KV Store", http.StatusInternalServerError)
		log.Printf("Erro ao obter KV Store: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Erro ao codificar resposta JSON", http.StatusInternalServerError)
		log.Printf("Erro ao codificar resposta JSON: %v", err)
		return
	}
	log.Println("Requisição recebida: /get_store")
}

func (ws *WebServer) handleListLogs(w http.ResponseWriter, r *http.Request) {
	// Get the KV Store from the requested node
	if r.Method != http.MethodGet {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	nodeAddress := r.PathValue("node_address")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(nodeAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Erro ao conectar ao nó", http.StatusInternalServerError)
		log.Printf("Erro ao conectar ao nó: %v", err)
		return
	}
	defer conn.Close()

	client := kvstore.NewKVStoreClient(conn)
	resp, err := client.ListLog(ctx, &kvstore.ListRequest{})
	if err != nil {
		http.Error(w, "Erro ao obter log", http.StatusInternalServerError)
		log.Printf("Erro ao obter log: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Erro ao codificar resposta JSON", http.StatusInternalServerError)
		log.Printf("Erro ao codificar resposta JSON: %v", err)
		return
	}
	log.Println("Requisição recebida: /get_log")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // permite todas as origens
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	registryConn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Erro ao conectar ao registry: %v", err)
	}
	defer registryConn.Close()

	registryClient := rpb.NewRegistryClient(registryConn)

	webServer := &WebServer{
		registryClient: registryClient,
		nodes:          make([]*NodeInfo, 0),
		leaderAddress:  "",
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			webServer.updateNodesInfo()
		}
	}()

	webServer.updateNodesInfo()

	mux := http.NewServeMux()
	mux.HandleFunc("/", webServer.indexHandler)
	mux.HandleFunc("/set", webServer.handleSet)
	mux.HandleFunc("/delete", webServer.handleDelete)
	mux.HandleFunc("/get_store/", webServer.handleGetStore)
	mux.HandleFunc("/get_log/", webServer.handleListLogs)

	handler := corsMiddleware(mux)

	log.Println("Servidor web iniciado na porta 8080")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatalf("Erro ao iniciar o servidor web: %v", err)
	}
}
