import axios, { type AxiosResponse } from "axios";
import type {
    DefaultResponse,
    GetNodesResponse,
    ListLogResponse,
    ListResponse,
    Request,
} from "./types";

const api = axios.create({
    baseURL: import.meta.env.VITE_API_URL,
    headers: {
        "Content-Type": "application/json",
    },
});

export function GetNodes(): Promise<AxiosResponse<GetNodesResponse>> {
    return api.get("/");
}

export function Set(request: Request): Promise<AxiosResponse<DefaultResponse>> {
    return api.post("/set", request);
}

export function Delete(
    request: Request,
): Promise<AxiosResponse<DefaultResponse>> {
    return api.post("/delete", request);
}

export function GetKVStore(
    address: string,
): Promise<AxiosResponse<ListResponse>> {
    return api.get(`/get_store/${address}`);
}

export function GetLog(
    address: string,
): Promise<AxiosResponse<ListLogResponse>> {
    return api.get(`/get_log/${address}`);
}

export function TryElect(
    address: string,
): Promise<AxiosResponse<DefaultResponse>> {
    return api.get(`/try_elect/${address}`);
}
