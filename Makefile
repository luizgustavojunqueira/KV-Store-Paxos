# Makefile

# Define o diretório RAIZ dos seus arquivos .proto para a flag -I
# Isso é crucial para que os imports entre os .proto funcionem corretamente.
# Ex: "import "paxos/paxos.proto";" no kvstore.proto
PROTO_ROOT := ./proto

# Define o diretório de saída para TODOS os arquivos Go gerados.
# O ponto '.' significa o diretório de onde o 'make' está sendo executado (raiz do projeto).
# ESSA É A CHAVE para o comportamento que você quer.
OUT_DIR := ./proto

# Alvo para gerar os arquivos gRPC do Paxos
grpc-paxos:
	protoc -I=$(PROTO_ROOT) \
	--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	$(PROTO_ROOT)/paxos/paxos.proto # Caminho completo do .proto relativo à raiz do projeto

# Alvo para gerar os arquivos gRPC do KVStore
grpc-kvstore:
	protoc -I=$(PROTO_ROOT) \
	--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	$(PROTO_ROOT)/kvstore/kvstore.proto # Caminho completo do .proto relativo à raiz do projeto

# Alvo para gerar os arquivos gRPC do Registry
grpc-registry:
	protoc -I=$(PROTO_ROOT) \
	--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	$(PROTO_ROOT)/registry/registry.proto # Caminho completo do .proto relativo à raiz do projeto

# Alvo para gerar TODOS os arquivos gRPC de uma vez
grpc-all: grpc-paxos grpc-kvstore grpc-registry

.PHONY: clean
clean:
	# Remove os arquivos gerados em cada subdiretório do proto/
	@rm -f $(PROTO_ROOT)/paxos/*.pb.go $(PROTO_ROOT)/paxos/*_grpc.pb.go
	@rm -f $(PROTO_ROOT)/kvstore/*.pb.go $(PROTO_ROOT)/kvstore/*_grpc.pb.go
	@rm -f $(PROTO_ROOT)/registry/*.pb.go $(PROTO_ROOT)/registry/*_grpc.pb.go

.PHONY: all
all: grpc-all
