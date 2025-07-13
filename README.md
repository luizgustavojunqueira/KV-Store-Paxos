# Key Value Store with Paxos

## Pré-requisitos

Antes de iniciar, certifique-se de ter as seguintes ferramentas instaladas:

- **Go (Golang):** Versão 1.20 ou superior.
- **Node.js e npm:** Versão LTS recomendada.
- **Make:** Ferramenta de automação de build.
- **Protocol Buffers Compiler (protoc):** Para a geração dos códigos gRPC.
    - Instale também os plugins do Go para gRPC e Protobuf:
        ```bash
        go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
        go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
        ```

## Rodando o Sistema

Primeiramente, na **raiz do projeto**, é necessário gerar os códigos gRPC para que os serviços possam se comunicar. Execute o seguinte comando:

```bash
make grpc
```

Em seguida, execute `go mod tidy` para garantir que todas as dependências Go estejam resolvidas.

Para rodar o sistema completo, siga estes passos:

### Iniciar o Registry Server e o Backend (Interface)

Este script inicia o serviço de registro de nós (Registry Server) e o servidor HTTP que serve o frontend.

    ```bash
    bash run_servers.sh <REGISTRY_IP>:50051
    ```

- Substitua <IP_DO_REGISTRY> pelo endereço IP da máquina onde o Registry Server irá escutar. Para testes locais, use 127.0.0.1 ou localhost.
- A porta padrão do Registry é 50051.
- O servidor HTTP (backend da interface) será executado em localhost:8080.

### Iniciar os nós Paxos

Após o Registry Server estar rodando, você pode iniciar os nós Paxos.

```bash
bash run_paxos_nodes.sh <NUMERO_DE_NOS> <IP_BASE_PARA_NOS> <IP_E_PORTA_DO_REGISTRY> <NOME_BASE_DO_NO>
```

- Substitua <NUMERO_DE_NOS> pelo total de nós Paxos que você deseja iniciar (ex: 3).
- Substitua <IP_BASE_PARA_NOS> pelo endereço IP onde esses nós Paxos irão escutar. Para testes na mesma máquina, use 127.0.0.1. As portas dos nós serão incrementadas automaticamente a partir de uma base (ex: 50052, 50053, etc.).
- Substitua <IP_E_PORTA_DO_REGISTRY> pelo mesmo valor utilizado no passo 1 (ex: 127.0.0.1:50051).
- Substitua <NOME_BASE_DO_NO> por um prefixo para o nome de cada nó (ex: paxos-node). Cada nó terá um nome único como paxos-node-1, paxos-node-2, etc.

### Rodar o frontend

Navegue para `frontend/` e crie um arquivo .env contendo a seguinte variável:

```env
VITE_API_URL=http://<BACKEND_IP>:8080
```

E então rode:

```bash
npm install
npm run dev
```

Após esses passos, você poderá acessar a interface no seu navegador em http://localhost:5173 para visualizar os nós, seus estados e executar comandos no KV-Store.
