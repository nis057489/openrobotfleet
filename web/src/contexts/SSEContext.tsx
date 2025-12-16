import React, { createContext, useContext, useEffect, useState } from 'react';

export interface StatusPayload {
    status: string;
    ts: string;
    ip: string;
    name: string;
    type: string;
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

export type SSEEvent = StatusUpdateEvent | ScanResultEvent;

interface SSEContextType {
    lastEvent: SSEEvent | null;
    isConnected: boolean;
}

const SSEContext = createContext<SSEContextType>({ lastEvent: null, isConnected: false });

export const useSSE = () => useContext(SSEContext);

export const SSEProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    const [lastEvent, setLastEvent] = useState<SSEEvent | null>(null);
    const [isConnected, setIsConnected] = useState(false);

    useEffect(() => {
        const eventSource = new EventSource('/api/events');

        eventSource.onopen = () => {
            console.log('SSE Connected');
            setIsConnected(true);
        };

        eventSource.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                setLastEvent(data);
            } catch (e) {
                console.error('Failed to parse SSE message', e);
            }
        };

        eventSource.onerror = (err) => {
            console.error('SSE Error', err);
            setIsConnected(false);
            // EventSource automatically attempts to reconnect
        };

        return () => {
            eventSource.close();
        };
    }, []);

    return (
        <SSEContext.Provider value={{ lastEvent, isConnected }}>
            {children}
        </SSEContext.Provider>
    );
};
