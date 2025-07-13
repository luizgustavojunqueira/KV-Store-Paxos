# Key Value Store with Paxos

## Running

Para rodar o sistema, há dois scripts bash para facilitar.

Primeiro, execute o registry e o backend da interface:

```bash
bash run_servers.sh <seu_ip>:50051
```

Depois, execute os nós com:

```bash
bash run_paxos_nodes.sh <numero_de_nos_paxos> <ip_base_para_nos> <registry_ip_porta> <node_name>
```

Para o frontend, os seguintes:

```bash
cd frontend/
npm install
npm run dev
```

Então, poderá acessar o navegador em localhost:5173 para visualizar os nós e executar comandos.
