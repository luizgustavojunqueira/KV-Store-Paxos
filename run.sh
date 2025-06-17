#!/bin/bash

REGISTRY_ADDR="localhost:50051"
BASE_PAXOS_PORT=8080 # Porta inicial para os nós Paxos
NUM_ACCEPTORS=9      # Número de nós Paxos que serão apenas Acceptors
NUM_TOTAL_NODES=$((NUM_ACCEPTORS + 1)) # Total de nós Paxos (Acceptors + 1 Proposer)
LOG_DIR="paxos_logs" # Diretório para armazenar os logs
REGISTRY_LOG="${LOG_DIR}/registry.log"

cleanup() {
    echo "Encerrando todos os processos em segundo plano..."
    kill $(jobs -p) 2>/dev/null
    echo "Limpeza concluída."
    exit 0
}

trap cleanup SIGINT SIGTERM

mkdir -p "$LOG_DIR"
rm -f "$LOG_DIR"/*.log # Limpa logs antigos

echo "Iniciando cluster Paxos..."
echo "Logs serão salvos em: $LOG_DIR"

echo "Iniciando Registry Server em ${REGISTRY_ADDR}..."
go run registry_server/cmd/main.go > "$REGISTRY_LOG" 2>&1 &
REGISTRY_PID=$!
echo "Registry Server iniciado (PID: ${REGISTRY_PID}), log: ${REGISTRY_LOG}"
sleep 2 # Dê um tempo para o registry iniciar completamente


PAXOS_PIDS=()

echo "Iniciando ${NUM_ACCEPTORS} nós Paxos (Acceptors)..."

for i in $(seq 1 $NUM_ACCEPTORS); do
    NODE_NAME="node-$i"
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1)) # Ex: 8080, 8081, ...
    NODE_ADDR="localhost:${NODE_PORT}"
    NODE_LOG="${LOG_DIR}/${NODE_NAME}.log"

    echo "  Iniciando ${NODE_NAME} em ${NODE_ADDR} (Acceptor), log: ${NODE_LOG}"
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${NODE_NAME}" \
        --nodeAddr="${NODE_ADDR}" \
        --isProposer=false > "$NODE_LOG" 2>&1 &
    
    PAXOS_PIDS+=($!) # Adiciona o PID ao array
    sleep 0.5 # Pequeno atraso para evitar que todos batam no registry ao mesmo tempo
done

echo "Todos os nós Acceptors iniciados. Aguardando para o registry atualizar e heartbeats..."
sleep 10 # Dê um tempo para todos os nós se registrarem e enviarem heartbeats

PROPOSER_NODE_INDEX=$((NUM_ACCEPTORS + 1)) # Índice do último nó
PROPOSER_NODE_NAME="node-${PROPOSER_NODE_INDEX}"
PROPOSER_NODE_PORT=$((BASE_PAXOS_PORT + NUM_ACCEPTORS)) # A porta imediatamente após o último acceptor
PROPOSER_NODE_ADDR="localhost:${PROPOSER_NODE_PORT}"
PROPOSER_LOG="${LOG_DIR}/${PROPOSER_NODE_NAME}.log"

echo "Iniciando o último nó Paxos (${PROPOSER_NODE_NAME}) como Proposer em ${PROPOSER_NODE_ADDR}, log: ${PROPOSER_LOG}"
go run paxos/cmd/main.go \
    --registryAddr="${REGISTRY_ADDR}" \
    --nodeName="${PROPOSER_NODE_NAME}" \
    --nodeAddr="${PROPOSER_NODE_ADDR}" \
    --key="example-key" \
    --value="example-value" \
    --isProposer=true > "$PROPOSER_LOG" 2>&1 &

PROPOSER_PID=$!
PAXOS_PIDS+=($!) # Adiciona o PID do proposer ao array de PIDs Paxos
echo "Proposer iniciado (PID: ${PROPOSER_PID})."

echo "Cluster Paxos rodando. Pressione Ctrl+C para encerrar."

# --- Manter o script rodando ---
wait # Espera por todos os processos em segundo plano (até que um seja encerrado ou Ctrl+C seja pressionado)
