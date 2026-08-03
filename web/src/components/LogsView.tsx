import { useEffect, useRef } from 'react';
import { Terminal as XTerm } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import 'xterm/css/xterm.css';

interface LogsViewProps {
    robotId: number;
}

export function LogsView({ robotId }: LogsViewProps) {
    const containerRef = useRef<HTMLDivElement>(null);

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
        const wsUrl = `${protocol}//${host}/api/robots/${robotId}/logs`;

        const ws = new WebSocket(wsUrl);
        ws.binaryType = 'arraybuffer';

        ws.onopen = () => {
            term.writeln('*** Streaming ~/.ros/log/latest/launch.log ***\r\n');
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
    }, [robotId]);

    return <div ref={containerRef} className="h-full w-full min-h-[400px] bg-[#1e1e1e] rounded-lg overflow-hidden" />;
}
