PROTO_ROOT := ./proto

OUT_DIR := ./proto

grpc-paxos:
	protoc -I=$(PROTO_ROOT) \
	--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	$(PROTO_ROOT)/paxos/paxos.proto 

grpc-kvstore:
	protoc -I=$(PROTO_ROOT) \
	--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	$(PROTO_ROOT)/kvstore/kvstore.proto 

grpc-registry:
	protoc -I=$(PROTO_ROOT) \
	--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	$(PROTO_ROOT)/registry/registry.proto 

grpc-all: grpc-paxos grpc-kvstore grpc-registry

.PHONY: clean
clean:
	@rm -f $(PROTO_ROOT)/paxos/*.pb.go $(PROTO_ROOT)/paxos/*_grpc.pb.go
	@rm -f $(PROTO_ROOT)/kvstore/*.pb.go $(PROTO_ROOT)/kvstore/*_grpc.pb.go
	@rm -f $(PROTO_ROOT)/registry/*.pb.go $(PROTO_ROOT)/registry/*_grpc.pb.go

.PHONY: all
all: grpc-all
