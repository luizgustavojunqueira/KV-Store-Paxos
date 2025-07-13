#!/bin/bash

WEB_SERVER_PORT=8080

cleanup() {
    echo "Encerrando todos os processos em segundo plano..."
    pkill -SIGKILL -f "go run backend/cmd/main.go" 
    sleep 1 
    pkill -SIGINT -f "go run registry_server/cmd/main.go" 
    echo "Limpeza concluída."
    exit 0 
}

trap cleanup SIGINT SIGTERM


if [ -z "$1" ]; then
    echo "Uso: $0 <REGISTRY_SERVER_ADDRESS>"
    echo "Exemplo: $0 192.168.166.217:50051"
    exit 1
fi

REGISTRY_ADDR="$1" 

echo "Iniciando registry e backend..."

echo "Iniciando Registry Server em ${REGISTRY_ADDR}..."
go run registry_server/cmd/main.go --registryAddr="${REGISTRY_ADDR}" &
REGISTRY_PID=$!
echo "Registry Server iniciado (PID: ${REGISTRY_PID})"
sleep 2 

echo "Iniciando Servidor HTTP/Frontend em localhost:${WEB_SERVER_PORT}..."
go run backend/cmd/main.go \
    --registryAddr="${REGISTRY_ADDR}" &
WEB_SERVER_PID=$!
echo "Servidor HTTP iniciado (PID: ${WEB_SERVER_PID})"
sleep 2 

echo "Registry e Backend iniciados com sucesso!"
echo "------------------------------------------------------------------"
echo "Configuração do cluster:"
echo "  Registry Server: ${REGISTRY_ADDR}"
echo "  Servidor HTTP/Frontend: localhost:${WEB_SERVER_PORT}"
echo "------------------------------------------------------------------"
echo "Para iniciar os nós Paxos, execute o script 'run_paxos_nodes.sh' com o número de nós desejado."
echo "Exemplo: ./run_paxos_nodes.sh 3 (irá iniciar 3 nós Paxos como Acceptors/Learners)"
echo "------------------------------------------------------------------"

wait
