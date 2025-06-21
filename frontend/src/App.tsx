import { useEffect, useState } from "react";
import "./App.css";
import "./componentes/item_card";
import Card_item from "./componentes/item_card";
import type { GetNodesResponse } from "./api/types";
import { GetNodes } from "./api/api";

function App() {
  const [nodes, setnodes] = useState<GetNodesResponse>({
    nodes: [],
    leader_address: '',
  });
  function callApiNodes() {
    GetNodes()
      .then((res) => {
        setnodes(res.data);
      })
      .catch((error) => console.log(error));
  }
  useEffect(()=>{
    callApiNodes()
  },[])
  return (
    <>
      {
        nodes.nodes.map(node => (
          <Card_item node={node}></Card_item>
        ))
      }
    </>
  );
}

export default App;
