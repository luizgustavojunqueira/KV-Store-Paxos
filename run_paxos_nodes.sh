#!/bin/bash

# --- Configurações Padrão (Fallback) ---
DEFAULT_REGISTRY_ADDR="localhost:50051"
WEB_SERVER_PORT=8080            # Porta para o servidor HTTP/Frontend
BASE_PAXOS_PORT=8081            # Porta inicial para os nós Paxos
LOG_DIR="paxos_logs"            # Diretório para armazenar os logs dos nós Paxos

# --- Argumentos da Linha de Comando ---
# Verifica se o número de nós, o IP base dos nós e o endereço do Registry foram passados como argumento
if [ -z "$3" ]; then
    echo "Uso: $0 <numero_de_nos_paxos> <ip_base_para_nos> <registry_ip_porta> <node_name>"
    echo "Exemplo: $0 3 192.168.1.100 localhost:50051 node (3 nós em 192.168.1.100, registry em localhost)"
    echo "Exemplo: $0 3 localhost 192.168.1.50:50051 node (3 nós em localhost, registry em 192.168.1.50)"
    exit 1
fi

NUM_NODES=$1 # Número de nós Paxos a serem iniciados (todos como Acceptors/Learners)
BASE_IP=$2   # Endereço IP base para os nós Paxos
REGISTRY_ADDR=$3 # Endereço IP e porta do Registry Server
BASE_NODE_NAME=${4:-"node"} # Nome base para os nós Paxos (padrão é "node")

# --- Funções Auxiliares ---

# Função para matar apenas os processos de node paxos ao sair
cleanup() {
    echo "Encerrando todos os nós Paxos..."
    for pid in "${PAXOS_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then # Verifica se o processo ainda existe
            echo "  Matando nó Paxos com PID: $pid"
            kill -SIGINT "$pid" # Envia SIGINT primeiro (para um shutdown gracioso)
            sleep 1 # Dá um tempo para SIGINT ser processado
            if kill -0 "$pid" 2>/dev/null; then # Se o processo ainda estiver ativo
                echo "  Forçando a parada do nó Paxos com PID: $pid"
                kill -SIGKILL "$pid" # Mata com SIGKILL
            fi
        fi
    done
    echo "Processos Paxos encerrados."
}

# Configura o trap para a função cleanup
trap cleanup SIGINT SIGTERM

# --- Início da Execução ---

echo "Iniciando cluster de Paxos..."

# Cria o diretório de logs se não existir
mkdir -p "$LOG_DIR"
echo "Diretório de logs criado/verificado: ${LOG_DIR}"

# --- 3. Iniciar os Nós Paxos (Acceptors/Learners) ---
PAXOS_PIDS=() # Array para armazenar os PIDs dos nós Paxos

echo "Iniciando ${NUM_NODES} nós Paxos (Acceptors/Learners)..."

for i in $(seq 1 $NUM_NODES); do
    NODE_NAME="${BASE_NODE_NAME}-${i}" # Nome do nó Paxos
    NODE_PORT=$((BASE_PAXOS_PORT + i - 1))
    # Combina o IP base com a porta para formar o endereço completo do nó
    NODE_ADDR="${BASE_IP}:${NODE_PORT}"
    LOG_FILE="${LOG_DIR}/${NODE_NAME}_${BASE_IP}_${NODE_PORT}.log" # Nome do arquivo de log

    echo "  Iniciando ${NODE_NAME} em ${NODE_ADDR} (Acceptor/Learner)... Logs em: ${LOG_FILE}"
    
    # Redireciona a saída padrão e de erro para o arquivo de log
    go run paxos/cmd/main.go \
        --registryAddr="${REGISTRY_ADDR}" \
        --nodeName="${NODE_NAME}" \
        --nodeAddr="${NODE_ADDR}" \
        --isProposer=false &> "${LOG_FILE}" & # Redireciona stdout e stderr para o log
    
    PAXOS_PIDS+=($!) # Adiciona o PID ao array
done

echo "Todos os nós Paxos iniciados."
sleep 2 # Dê um tempo para todos os nós se registrarem e enviarem heartbeats

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

# Manter o script rodando indefinidamente, esperando por Ctrl+C
wait
