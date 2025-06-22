#!/bin/bash

# --- Configurações ---
REGISTRY_ADDR="localhost:50051"
WEB_SERVER_PORT=8080    # Porta para o servidor HTTP/Frontend (AGORA 8080)
BASE_PAXOS_PORT=8081    # Porta inicial para os nós Paxos (AGORA 8081)

# --- Argumentos da Linha de Comando ---
# Verifica se o número de nós foi passado como argumento
if [ -z "$1" ]; then
    echo "Uso: $0 <numero_de_nos_paxos>"
    echo "Exemplo: $0 3 (irá iniciar 3 nós Paxos, Registry e Web Server)"
    exit 1
fi

NUM_NODES=$1 # Número de nós Paxos a serem iniciados (todos como Acceptors/Learners)

# --- Funções Auxiliares ---

# Função para matar todos os processos em segundo plano ao sair
cleanup() {
    echo "Encerrando todos os processos em segundo plano..."
    # Mata todos os processos 'go run' de forma mais agressiva
    pkill -SIGINT -f "go run" # Envia SIGINT primeiro
    sleep 1 # Dá um tempo para SIGINT ser processado
    pkill -SIGKILL -f "go run" # Se ainda houver, mata com SIGKILL
    echo "Limpeza concluída."
    exit 0 # Sai do script após a limpeza
}

# Configura o trap para a função cleanup
trap cleanup SIGINT SIGTERM

# --- Início da Execução ---

echo "Iniciando cluster de Paxos..."

# --- 1. Iniciar o Registry Server ---
echo "Iniciando Registry Server em ${REGISTRY_ADDR}..."
go run registry_server/cmd/main.go &
REGISTRY_PID=$!
echo "Registry Server iniciado (PID: ${REGISTRY_PID})"
sleep 2 # Dê um tempo para o registry iniciar

# --- 2. Iniciar o Servidor HTTP/Frontend ---
echo "Iniciando Servidor HTTP/Frontend em localhost:${WEB_SERVER_PORT}..."
go run backend/cmd/main.go \
    --registryAddr="${REGISTRY_ADDR}" &
WEB_SERVER_PID=$!
echo "Servidor HTTP iniciado (PID: ${WEB_SERVER_PID})"
sleep 2 # Dê um tempo para o servidor web iniciar e conectar ao registry

# --- 3. Iniciar os Nós Paxos (Acceptors/Learners) ---
PAXOS_PIDS=() # Array para armazenar os PIDs dos nós Paxos

echo "Iniciando ${NUM_NODES} nós Paxos (Acceptors/Learners)..."

for i in $(seq 1 $NUM_NODES); do
    NODE_NAME="node-${i}"
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1))
    NODE_ADDR="localhost:${NODE_PORT}"

    echo "  Iniciando ${NODE_NAME} em ${NODE_ADDR} (Acceptor/Learner)..."
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${NODE_NAME}" \
        --nodeAddr="${NODE_ADDR}" \
        --isProposer=false &
    
    PAXOS_PIDS+=($!) # Adiciona o PID ao array
done

echo "Todos os nós Paxos iniciados."
sleep 2 # Dê um tempo para todos os nós se registrarem e enviarem heartbeats


echo "------------------------------------------------------------------"
echo "Cluster iniciado:"
echo "  Registry Server: ${REGISTRY_ADDR}"
echo "  Servidor HTTP/Frontend: localhost:${WEB_SERVER_PORT}"
echo "  Nós Paxos (${NUM_NODES}): ${BASE_PAXOS_PORT} em diante"
echo ""
echo "Pressione Ctrl+C para encerrar todos os processos."
echo "------------------------------------------------------------------"

# Manter o script rodando indefinidamente, esperando por Ctrl+C
wait
