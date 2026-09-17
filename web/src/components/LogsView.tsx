import { useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import 'xterm/css/xterm.css';

interface LogsViewProps {
    robotId: number;
}

type LogSource = 'ros' | 'agent' | 'all';

const SOURCE_LABELS: Record<LogSource, string> = {
    ros: 'ROS',
    agent: 'Agent',
    all: 'All',
};

const SOURCE_BANNERS: Record<LogSource, string> = {
    ros: '*** Streaming ~/.ros/log/latest/launch.log ***',
    agent: '*** Streaming openrobotfleet-agent journal ***',
    all: '*** Streaming ROS log + openrobotfleet-agent journal ***',
};

export function LogsView({ robotId }: LogsViewProps) {
    const containerRef = useRef<HTMLDivElement>(null);
    const [source, setSource] = useState<LogSource>('ros');

    useEffect(() => {
        if (!containerRef.current) return;

        const term = new XTerm({
            cursorBlink: false,
            disableStdin: true,
            convertEol: true,
            theme: {
                background: '#1e1e1e',
            },
            fontSize: 13,
            fontFamily: 'Menlo, Monaco, "Courier New", monospace',
        });

        const fitAddon = new FitAddon();
        term.loadAddon(fitAddon);

        term.open(containerRef.current);
        fitAddon.fit();

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
        const wsUrl = `${protocol}//${host}/api/robots/${robotId}/logs?source=${source}`;

        const ws = new WebSocket(wsUrl);
        ws.binaryType = 'arraybuffer';

        ws.onopen = () => {
            term.writeln(`${SOURCE_BANNERS[source]}\r\n`);
        };

        ws.onmessage = (event) => {
            if (event.data instanceof ArrayBuffer) {
                term.write(new Uint8Array(event.data));
            } else {
                term.write(event.data);
            }
        };

        ws.onclose = () => {
            term.writeln('\r\n*** Connection closed ***');
        };

        ws.onerror = (err) => {
            console.error(err);
            term.writeln('\r\n*** Connection error ***');
        };

        const handleResize = () => {
            fitAddon.fit();
        };

        window.addEventListener('resize', handleResize);
        setTimeout(() => fitAddon.fit(), 100);

        return () => {
            window.removeEventListener('resize', handleResize);
            ws.close();
            term.dispose();
        };
    }, [robotId, source]);

    return (
        <div className="h-full w-full min-h-[400px] flex flex-col gap-2">
            <div className="flex gap-1">
                {(Object.keys(SOURCE_LABELS) as LogSource[]).map((key) => (
                    <button
                        key={key}
                        onClick={() => setSource(key)}
                        className={`px-2 py-1 text-xs rounded ${
                            source === key
                                ? 'bg-blue-600 text-white'
                                : 'bg-neutral-700 text-neutral-300 hover:bg-neutral-600'
                        }`}
                    >
                        {SOURCE_LABELS[key]}
                    </button>
                ))}
            </div>
            <div ref={containerRef} className="flex-1 w-full min-h-[380px] bg-[#1e1e1e] rounded-lg overflow-hidden" />
        </div>
    );
}
