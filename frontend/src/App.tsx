import { useEffect, useState } from "react";
import "./App.css";
import Card_item from "./componentes/item_card";
import type { GetNodesResponse } from "./api/types";
import { GetNodes } from "./api/api";

function App() {
    const [nodes, setnodes] = useState<GetNodesResponse>({
        nodes: [],
        leader_address: "",
    });

    function callApiNodes() {
        GetNodes()
            .then((res) => {
                // Make sure the leader is first
                const sortedNodes = res.data.nodes.sort((a, b) => {
                    if (a.is_leader && !b.is_leader) return -1;
                    if (!a.is_leader && b.is_leader) return 1;
                    return 0;
                });

                setnodes({
                    nodes: sortedNodes,
                    leader_address: res.data.leader_address,
                });
            })
            .catch((error) => console.log(error));
    }

    useEffect(() => {
        callApiNodes();
        const interval = setInterval(() => {
            callApiNodes();
        }, 2000);

        return () => clearInterval(interval);
    }, []);

    return (
        <>
            {nodes.nodes.map((node) => (
                <Card_item node={node}></Card_item>
            ))}
        </>
    );
}

export default App;
