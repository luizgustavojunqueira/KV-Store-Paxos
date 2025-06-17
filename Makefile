grpc-registry: 
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/registry/registry.proto

grpc-paxos:
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/paxos/paxos.proto

grpc: grpc-registry grpc-paxos
