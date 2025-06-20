#!/bin/bash

# --- Configurações ---
REGISTRY_ADDR="localhost:50051"
BASE_PAXOS_PORT=8080    # Porta inicial para os nós Paxos (Acceptors)
NUM_ACCEPTORS=2        # Número de nós Paxos que serão Acceptors/Learners
LOG_DIR="paxos_logs"    # Diretório para armazenar os logs
REGISTRY_LOG="${LOG_DIR}/registry.log"

# O Proposer será iniciado manualmente, mas definimos os dados para ele aqui
PROPOSER_NODE_NAME="leader-paxos"
PROPOSER_NODE_PORT=$((BASE_PAXOS_PORT + NUM_ACCEPTORS)) # A porta imediatamente após o último acceptor
PROPOSER_NODE_ADDR="localhost:${PROPOSER_NODE_PORT}"
# O log do Proposer estará no terminal onde ele for iniciado, não neste script.

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

# --- Preparação ---

# Cria o diretório de logs se não existir e limpa logs antigos
mkdir -p "$LOG_DIR"
rm -f "$LOG_DIR"/*.log
echo "Iniciando cluster Multi-Paxos (Acceptors e Registry apenas)..."
echo "Logs dos nós em segundo plano serão salvos em: $LOG_DIR"

# --- 1. Iniciar o Registry Server ---
echo "Iniciando Registry Server em ${REGISTRY_ADDR}..."
go run registry_server/cmd/main.go > "$REGISTRY_LOG" 2>&1 &
REGISTRY_PID=$!
echo "Registry Server iniciado (PID: ${REGISTRY_PID}), log: ${REGISTRY_LOG}"
sleep 10 # Dê um tempo para o registry iniciar

# --- 2. Iniciar os Nós Paxos (Acceptors/Learners) ---
PAXOS_PIDS=() # Array para armazenar os PIDs dos nós Paxos

echo "Iniciando ${NUM_ACCEPTORS} nós Paxos (Acceptors/Learners)..."

for i in $(seq 1 $NUM_ACCEPTORS); do
    NODE_NAME="acceptor-${i}"
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1))
    NODE_ADDR="localhost:${NODE_PORT}"
    NODE_LOG="${LOG_DIR}/${NODE_NAME}.log"

    echo "  Iniciando ${NODE_NAME} em ${NODE_ADDR} (Acceptor/Learner), log: ${NODE_LOG}"
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${NODE_NAME}" \
        --nodeAddr="${NODE_ADDR}" \
        --isProposer=false > "$NODE_LOG" 2>&1 &
    
    PAXOS_PIDS+=($!) # Adiciona o PID ao array
    sleep 0.5 # Pequeno atraso para evitar que todos batam no registry ao mesmo tempo
done

echo "Todos os nós Acceptors/Learners iniciados."
sleep 2 # Dê um tempo para todos os nós se registrarem e enviarem heartbeats

# --- 3. Instruções para Iniciar o Nó Proposer Manualmente ---
echo "------------------------------------------------------------------"
echo "INICIE O NÓ PROPOSER MANUALMENTE EM UM NOVO TERMINAL AGORA:"
echo "  go run paxos/cmd/main.go \\"
echo "    --registryAddr=\"${REGISTRY_ADDR}\" \\"
echo "    --nodeName=\"${PROPOSER_NODE_NAME}\" \\"
echo "    --nodeAddr=\"${PROPOSER_NODE_ADDR}\" \\"
echo "    --isProposer=true "
echo ""
echo "  (Use Ctrl+C neste terminal quando terminar, para encerrar os Acceptors e Registry.)"
echo "------------------------------------------------------------------"

# Manter o script rodando indefinidamente, esperando por Ctrl+C
wait # Este 'wait' irá esperar pelos PIDs que foram colocados em background.
     # Ele permite que o 'trap' funcione quando Ctrl+C é pressionado.
