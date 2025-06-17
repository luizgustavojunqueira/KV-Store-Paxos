#!/bin/bash

# --- Configurações ---
REGISTRY_ADDR="localhost:50051"
BASE_PAXOS_PORT=8080 # Porta inicial para os nós Paxos
NUM_ACCEPTORS=2      # Reduzido para 2 para testar quorum com 2 proposers (3 nós no total se cada proposer for também acceptor)
NUM_TOTAL_NODES=$((NUM_ACCEPTORS + 2)) # Total de nós Paxos (Acceptors + 2 Proposers)
LOG_DIR="paxos_logs" # Diretório para armazenar os logs
REGISTRY_LOG="${LOG_DIR}/registry.log"

# Proposer 1
PROPOSER1_NODE_NAME="proposer-A"
PROPOSER1_NODE_PORT=$((BASE_PAXOS_PORT + NUM_ACCEPTORS)) # Ex: 8082 se NUM_ACCEPTORS=2
PROPOSER1_NODE_ADDR="localhost:${PROPOSER1_NODE_PORT}"
PROPOSER1_LOG="${LOG_DIR}/${PROPOSER1_NODE_NAME}.log"
PROPOSER1_VALUE="Valor_Proposer_A"
PROPOSER1_START_DELAY=3 # Inicia 3 segundos após acceptors estarem prontos

# Proposer 2
PROPOSER2_NODE_NAME="proposer-B"
PROPOSER2_NODE_PORT=$((BASE_PAXOS_PORT + NUM_ACCEPTORS + 1)) # Ex: 8083
PROPOSER2_NODE_ADDR="localhost:${PROPOSER2_NODE_PORT}"
PROPOSER2_LOG="${LOG_DIR}/${PROPOSER2_NODE_NAME}.log"
PROPOSER2_VALUE="Valor_Proposer_B"
PROPOSER2_START_DELAY=4 # Inicia 4 segundos após acceptors estarem prontos (um pouco depois do A)


# --- Funções Auxiliares ---
cleanup() {
    echo "Encerrando todos os processos em segundo plano..."
    kill $(jobs -p) 2>/dev/null
    echo "Limpeza concluída."
    exit 0
}

trap cleanup SIGINT SIGTERM

# --- Preparação ---
mkdir -p "$LOG_DIR"
rm -f "$LOG_DIR"/*.log
echo "Iniciando cluster Paxos para teste de concorrência..."
echo "Logs serão salvos em: $LOG_DIR"

# --- 1. Iniciar o Registry Server ---
echo "Iniciando Registry Server em ${REGISTRY_ADDR}..."
go run registry_server/cmd/main.go > "$REGISTRY_LOG" 2>&1 &
REGISTRY_PID=$!
echo "Registry Server iniciado (PID: ${REGISTRY_PID}), log: ${REGISTRY_LOG}"
sleep 2

# --- 2. Iniciar os Nós Paxos (Acceptors) ---
PAXOS_PIDS=()
echo "Iniciando ${NUM_ACCEPTORS} nós Paxos (Acceptors)..."

for i in $(seq 1 $NUM_ACCEPTORS); do
    NODE_NAME="acceptor-${i}" # Nomes mais claros para acceptors
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1))
    NODE_ADDR="localhost:${NODE_PORT}"
    NODE_LOG="${LOG_DIR}/${NODE_NAME}.log"

    echo "  Iniciando ${NODE_NAME} em ${NODE_ADDR} (Acceptor), log: ${NODE_LOG}"
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${NODE_NAME}" \
        --nodeAddr="${NODE_ADDR}" \
        --isProposer=false > "$NODE_LOG" 2>&1 &
    
    PAXOS_PIDS+=($!)
    sleep 0.5
done

echo "Todos os nós Acceptors iniciados. Aguardando para o registry atualizar e heartbeats..."
sleep 5 # Dê um tempo para todos os nós se registrarem e enviarem heartbeats

# --- 3. Iniciar o Proposer A ---
echo "Iniciando ${PROPOSER1_NODE_NAME} como Proposer em ${PROPOSER1_NODE_ADDR}, log: ${PROPOSER1_LOG}"
(
    sleep "${PROPOSER1_START_DELAY}"
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${PROPOSER1_NODE_NAME}" \
        --nodeAddr="${PROPOSER1_NODE_ADDR}" \
        --isProposer=true \
        --key="example-key" \
        --proposalID=10 \
        --value="${PROPOSER1_VALUE}" > "$PROPOSER1_LOG" 2>&1
) &
PROPOSER1_PID=$!
PAXOS_PIDS+=($!)
echo "Proposer A iniciado (PID: ${PROPOSER1_PID}). Valor a propor: '${PROPOSER1_VALUE}'"


# --- 4. Iniciar o Proposer B ---
echo "Iniciando ${PROPOSER2_NODE_NAME} como Proposer em ${PROPOSER2_NODE_ADDR}, log: ${PROPOSER2_LOG}"
(
    sleep "${PROPOSER2_START_DELAY}"
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${PROPOSER2_NODE_NAME}" \
        --nodeAddr="${PROPOSER2_NODE_ADDR}" \
        --isProposer=true \
        --key="example-key" \
        --proposalID=5 \
        --value="${PROPOSER2_VALUE}" > "$PROPOSER2_LOG" 2>&1
) &
PROPOSER2_PID=$!
PAXOS_PIDS+=($!)
echo "Proposer B iniciado (PID: ${PROPOSER2_PID}). Valor a propor: '${PROPOSER2_VALUE}'"

echo "Cluster Paxos rodando. Pressione Ctrl+C para encerrar e verificar os logs."

wait
