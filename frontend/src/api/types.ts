export type NodeInfo = {
    name: string;
    address: string;
    is_leader: boolean;
    leader_proposal_id: number;
    highest_slot_id: number;
};

export type GetNodesResponse = {
    nodes: NodeInfo[];
    leader_address: string;
};

export type Request = {
    key: string;
    value: string;
    node_address: string;
};

export type KeyValuePair = {
    key: string;
    value: string;
};

export type ListResponse = {
    pairs: KeyValuePair[];
    error_message?: string;
};

export type ListLogResponse = {
    entries: LogEntry[];
    errorMessage: string;
};

export type LogEntry = {
    slot_id: number;
    command: Command;
};

export type Command = {
    type: CommandType;
    key: string;
    value: string;
    proposal_id: number;
};

export const CommandTypeEnum = {
    UNKNOWN: 0,
    SET: 1,
    DELETE: 2,
} as const;

export type CommandType =
    (typeof CommandTypeEnum)[keyof typeof CommandTypeEnum];

export type DefaultResponse = {
    success: boolean;
    error_message?: string;
};

