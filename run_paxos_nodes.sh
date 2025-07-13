#!/bin/bash

DEFAULT_REGISTRY_ADDR="localhost:50051"
WEB_SERVER_PORT=8080
BASE_PAXOS_PORT=8081
LOG_DIR="paxos_logs"

if [ -z "$3" ]; then
    echo "Uso: $0 <numero_de_nos_paxos> <ip_base_para_nos> <registry_ip_porta> <node_name>"
    echo "Exemplo: $0 3 192.168.1.100 localhost:50051 node (3 nós em 192.168.1.100, registry em localhost)"
    echo "Exemplo: $0 3 localhost 192.168.1.50:50051 node (3 nós em localhost, registry em 192.168.1.50)"
    exit 1
fi

NUM_NODES=$1 
BASE_IP=$2
REGISTRY_ADDR=$3
BASE_NODE_NAME=${4:-"node"}

cleanup() {
    echo "Encerrando todos os nós Paxos..."
    for pid in "${PAXOS_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then 
            echo "  Matando nó Paxos com PID: $pid"
            kill -SIGINT "$pid" 
            sleep 1 
            if kill -0 "$pid" 2>/dev/null; then 
                echo "  Forçando a parada do nó Paxos com PID: $pid"
                kill -SIGKILL "$pid" 
            fi
        fi
    done
    echo "Processos Paxos encerrados."
}

trap cleanup SIGINT SIGTERM

echo "Iniciando cluster de Paxos..."

mkdir -p "$LOG_DIR"
echo "Diretório de logs criado/verificado: ${LOG_DIR}"

PAXOS_PIDS=() 

echo "Iniciando ${NUM_NODES} nós Paxos (Acceptors/Learners)..."

for i in $(seq 1 $NUM_NODES); do
    NODE_NAME="${BASE_NODE_NAME}-${i}"
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1))
    NODE_ADDR="${BASE_IP}:${NODE_PORT}"
    LOG_FILE="${LOG_DIR}/${NODE_NAME}_${BASE_IP}_${NODE_PORT}.log"

    echo "  Iniciando ${NODE_NAME} em ${NODE_ADDR} (Acceptor/Learner)... Logs em: ${LOG_FILE}"
    
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${NODE_NAME}" \
        --nodeAddr="${NODE_ADDR}" \
        --isProposer=false &> "${LOG_FILE}" & 
    
    PAXOS_PIDS+=($!)
done

echo "Todos os nós Paxos iniciados."
sleep 2
echo "------------------------------------------------------------------"
echo "Configuração do cluster:"
echo "  Registry Server: ${REGISTRY_ADDR}"
echo "  Servidor HTTP/Frontend: ${BASE_IP}:${WEB_SERVER_PORT}"
echo "  Nós Paxos (Acceptors/Learners):"
for i in $(seq 1 $NUM_NODES); do
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1))
    echo "    Node-${i}: ${BASE_IP}:${NODE_PORT} (Logs em: ${LOG_DIR}/node-${i}_${BASE_IP}_${NODE_PORT}.log)"
done
echo "------------------------------------------------------------------"
echo ""
echo "Pressione Ctrl+C para encerrar todos os processos."
echo "------------------------------------------------------------------"

wait
