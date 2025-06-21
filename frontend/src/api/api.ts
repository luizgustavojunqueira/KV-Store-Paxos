import axios, { type AxiosResponse } from "axios"; 
import type { GetNodesResponse } from "./types";


const api = axios.create({
    baseURL: import.meta.env.VITE_API_URL, 
    headers: {
        'Content-Type': 'application/json'
    }
})

export function GetNodes (): Promise<AxiosResponse<GetNodesResponse>>{

    return api.get('/')

}