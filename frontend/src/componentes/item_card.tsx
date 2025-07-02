import "./item_card.css";
import type { ListLogResponse, ListResponse, NodeInfo } from "../api/types";
import { useState } from "react";
import { Delete, GetKVStore, GetLog, Set, TryElect } from "../api/api";

function Card_item({ node }: { node: NodeInfo }) {
    const [kvStore, setKVStore] = useState<ListResponse>();
    const [logEntries, setLogEntries] = useState<ListLogResponse>();
    const [showKVStore, setShowKVStore] = useState(false);
    const [showLog, setShowLog] = useState(false);
    const [key, setKey] = useState("");
    const [value, setValue] = useState("");

    function callApiKVStore() {
        GetKVStore(node.address)
            .then((res) => {
                console.log(res.data);
                if (res.data.error_message) {
                    console.error(
                        "Error fetching KV Store:",
                        res.data.error_message,
                    );
                    return;
                }
                setKVStore(res.data);
                setShowLog(false);
                setShowKVStore(true);
            })
            .catch((error) => {
                console.error("Error fetching KV Store:", error);
            });
    }

    function callApiLog() {
        GetLog(node.address)
            .then((res) => {
                console.log(res.data);
                if (res.data.errorMessage) {
                    console.error("Error fetching Log:", res.data.errorMessage);
                    return;
                }
                setLogEntries(res.data);
                setShowKVStore(false);
                setShowLog(true);
            })
            .catch((error) => {
                console.error("Error fetching Log:", error);
            });
    }

    function handlePrintLog() {
        callApiLog();
    }

    function handlePrintKValue() {
        callApiKVStore();
    }

    function handleSetKValue() {
        if (!key || !value) {
            alert("Please enter both key and value.");
            return;
        }
        const request = {
            key: key,
            value: value,
            node_address: node.address,
        };

        Set(request)
            .then((res) => {
                if (res.data.error_message) {
                    console.error(res.data.error_message);
                    alert(res.data.error_message);
                    return;
                }

                if (res.status === 200) {
                    console.log("Set operation successful");
                } else {
                    console.error("Error setting key-value pair:", res.data);
                    alert("Error setting key-value pair: " + res.data);
                }
            })
            .catch((error) => {
                console.error("Error setting key-value pair:", error);
                alert("Error setting key-value pair: " + error.message);
            });
        setKey("");
        setValue("");
    }

    function handleDeleteKey() {
        if (!key) {
            alert("Please enter a key to delete.");
            return;
        }
        const request = {
            key: key,
            value: "", // Value is not needed for delete operation
            node_address: node.address,
        };

        Delete(request)
            .then((res) => {
                if (res.data.error_message) {
                    console.error(res.data.error_message);
                    alert(res.data.error_message);
                    return;
                }

                if (res.status === 200) {
                    console.log("Delete operation successful");
                } else {
                    console.error("Error deleting key:", res.data);
                    alert("Error deleting key: " + res.data);
                }
            })
            .catch((error) => {
                console.error("Error deleting key:", error);
                alert("Error deleting key: " + error.message);
            });
        setKey("");
    }

    function handleTryElect() {
        if (node.is_leader) {
            alert("This node is already a leader.");
            return;
        }

        TryElect(node.address)
            .then((res) => {
                if (res.data.error_message) {
                    console.error(res.data.error_message);
                    alert(res.data.error_message);
                    return;
                }

                if (res.status === 200) {
                    console.log("Election attempt successful");
                    alert(
                        "Election attempt successful. Check the node status.",
                    );
                } else {
                    console.error("Error attempting election:", res.data);
                    alert("Error attempting election: " + res.data);
                }
            })
            .catch((error) => {
                console.error("Error attempting election:", error);
                alert("Error attempting election: " + error.message);
            });

        console.log("Attempting election for node:", node.address);
    }

    return (
        <>
            <div className={"no_card " + (node.is_leader ? "isleader" : "")}>
                <section className="no_conteudo">
                    <div className="no_properties">
                        <h3>
                            {node.name} -{" "}
                            {node.is_leader ? "Líder" : "Seguidor"}
                        </h3>
                        <p>Address: {node.address}</p>
                    </div>

                    <div>
                        {showKVStore && kvStore && (
                            <div className="kv_store">
                                <h4>KV Store:</h4>
                                <ul>
                                    {kvStore.pairs.map((pair) => (
                                        <li key={pair.key}>
                                            {pair.key}: {pair.value}
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        )}
                        {showLog && logEntries && (
                            <div className="log_entries">
                                <h4>Log Entries:</h4>
                                <ul>
                                    {logEntries.entries.map((entry) => (
                                        <li key={entry.slot_id}>
                                            Slot ID: {entry.slot_id}, Command:{" "}
                                            {entry.command.type === 1
                                                ? "SET"
                                                : "DELETE"}{" "}
                                            - Key: {entry.command.key}, Value:{" "}
                                            {entry.command.value}, Proposal ID:{" "}
                                            {entry.command.proposal_id}
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        )}
                    </div>

                    <div className="no_actions">
                        <button className="print_log" onClick={handlePrintLog}>
                            Histórico
                        </button>
                        <button
                            className="print_kvalue"
                            onClick={handlePrintKValue}
                        >
                            Exibir K-Value
                        </button>
                    </div>
                </section>
                <section className="no_change_kvalue">
                    <div className="input_kvalue">
                        <label>KEY: </label>
                        <input
                            type="text"
                            className="input"
                            value={key}
                            onChange={(e) => setKey(e.target.value)}
                        />
                        <label>VALUE: </label>
                        <input
                            type="text"
                            className="input"
                            value={value}
                            onChange={(e) => setValue(e.target.value)}
                        />
                    </div>
                    <div className="apply_changes">
                        <button
                            className="set_kvalue"
                            onClick={handleSetKValue}
                        >
                            SET
                        </button>
                        <button
                            className="delete_kvalue"
                            onClick={handleDeleteKey}
                        >
                            DELETE
                        </button>

                        {!node.is_leader && (
                            <button
                                className="try_election"
                                onClick={handleTryElect}
                            >
                               TENTAR ELEIÇÂO
                            </button>
                        )}
                    </div>
                </section>
            </div>
        </>
    );
}

export default Card_item;
