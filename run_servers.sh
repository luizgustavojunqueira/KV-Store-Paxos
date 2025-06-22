
#!/bin/bash

# --- Configurações ---
REGISTRY_ADDR="localhost:50051"
WEB_SERVER_PORT=8080    # Porta para o servidor HTTP/Frontend (AGORA 8080)

# --- Funções Auxiliares ---

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

echo "Iniciando registry e backend..."

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

echo "Registry e Backend iniciados com sucesso!"

echo "------------------------------------------------------------------"
echo "Configuração do cluster:"
echo "  Registry Server: ${REGISTRY_ADDR}"
echo "  Servidor HTTP/Frontend: localhost:${WEB_SERVER_PORT}"
echo "------------------------------------------------------------------"
echo "Para iniciar os nós Paxos, execute o script 'run_paxos_nodes.sh' com o número de nós desejado."
echo "Exemplo: ./run_paxos_nodes.sh 3 (irá iniciar 3 nós Paxos como Acceptors/Learners)"
echo "------------------------------------------------------------------"

# Manter o script rodando indefinidamente, esperando por Ctrl+C
wait
