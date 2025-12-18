import React, { createContext, useContext, useEffect, useState, useRef } from 'react';

export interface StatusPayload {
    status: string;
    ts: string;
    ip: string;
    name: string;
    type: string;
    job_id?: string;
    job_status?: string;
    job_error?: string;
}

export interface StatusUpdateEvent {
    type: 'status_update';
    agent_id: string;
    data: StatusPayload;
}

export interface ScanResultPayload {
    ip: string;
    port: number;
    mac?: string;
    manufacturer?: string;
    banner?: string;
    status: string;
}

export interface ScanResultEvent {
    type: 'scan_result';
    data: ScanResultPayload;
}

export interface BuildUpdatePayload {
    status: string;
    progress: number;
    step: string;
    logs: string[];
    error: string;
    image_name?: string;
}

export interface BuildUpdateEvent {
    type: 'build_update';
    data: BuildUpdatePayload;
}

export type WSEvent = StatusUpdateEvent | ScanResultEvent | BuildUpdateEvent;

type Listener = (event: WSEvent) => void;

interface WebSocketContextType {
    isConnected: boolean;
    addListener: (listener: Listener) => () => void;
}

const WebSocketContext = createContext<WebSocketContextType>({
    isConnected: false,
    addListener: () => () => { }
});

export const useWebSocket = () => useContext(WebSocketContext);

export const WebSocketProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    const [isConnected, setIsConnected] = useState(false);
    const wsRef = useRef<WebSocket | null>(null);
    const reconnectTimeoutRef = useRef<NodeJS.Timeout>();
    const listenersRef = useRef<Set<Listener>>(new Set());

    const addListener = (listener: Listener) => {
        listenersRef.current.add(listener);
        return () => {
            listenersRef.current.delete(listener);
        };
    };

    const connect = () => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/api/ws`;
        const ws = new WebSocket(wsUrl);

        ws.onopen = () => {
            console.log('WebSocket Connected');
            setIsConnected(true);
        };

        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                listenersRef.current.forEach(listener => listener(data));
            } catch (e) {
                console.error('Failed to parse WS message', e);
            }
        };

        ws.onclose = () => {
            console.log('WebSocket Disconnected');
            setIsConnected(false);
            // Reconnect after 3 seconds
            reconnectTimeoutRef.current = setTimeout(connect, 3000);
        };

        ws.onerror = (err) => {
            console.error('WebSocket Error', err);
            ws.close();
        };

        wsRef.current = ws;
    };

    useEffect(() => {
        connect();

        return () => {
            if (wsRef.current) {
                wsRef.current.close();
            }
            if (reconnectTimeoutRef.current) {
                clearTimeout(reconnectTimeoutRef.current);
            }
        };
    }, []);

    return (
        <WebSocketContext.Provider value={{ isConnected, addListener }}>
            {children}
        </WebSocketContext.Provider>
    );
};
