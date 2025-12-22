import { useEffect, useRef, useImperativeHandle, forwardRef, useState } from 'react';
import { Terminal as XTerm } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import 'xterm/css/xterm.css';

export interface SerialTerminalRef {
    connect: () => Promise<void>;
    write: (data: string) => Promise<void>;
    isConnected: boolean;
}

export const SerialTerminal = forwardRef<SerialTerminalRef, {}>((props, ref) => {
    const containerRef = useRef<HTMLDivElement>(null);
    const termRef = useRef<XTerm | null>(null);
    const portRef = useRef<any>(null);
    const readerRef = useRef<ReadableStreamDefaultReader | null>(null);
    const [isConnected, setIsConnected] = useState(false);
    const isReadingRef = useRef(false);

    useImperativeHandle(ref, () => ({
        connect: async () => {
            const nav = navigator as any;
            if (!nav.serial) {
                termRef.current?.writeln('\r\nError: Web Serial API not supported in this browser.\r\n');
                return;
            }

            try {
                const port = await nav.serial.requestPort();
                await port.open({ baudRate: 115200 });
                portRef.current = port;
                setIsConnected(true);
                termRef.current?.writeln('\r\n*** Connected to Serial Device ***\r\n');

                // Start reading loop
                readLoop();
            } catch (err: any) {
                console.error('Error connecting to serial port:', err);
                termRef.current?.writeln(`\r\nError: ${err.message}\r\n`);
            }
        },
        write: async (data: string) => {
            if (!portRef.current || !portRef.current.writable) return;

            const encoder = new TextEncoder();
            const writer = portRef.current.writable.getWriter();
            try {
                await writer.write(encoder.encode(data));
            } finally {
                writer.releaseLock();
            }
        },
        isConnected
    }));

    const readLoop = async () => {
        if (!portRef.current || !portRef.current.readable) return;

        if (isReadingRef.current) return;
        isReadingRef.current = true;

        const textDecoder = new TextDecoderStream();
        const readableStreamClosed = portRef.current.readable.pipeTo(textDecoder.writable);
        const reader = textDecoder.readable.getReader();
        readerRef.current = reader;

        try {
            while (true) {
                const { value, done } = await reader.read();
                if (done) {
                    // Allow the serial port to be closed later.
                    break;
                }
                if (value) {
                    termRef.current?.write(value);
                }
            }
        } catch (error) {
            console.error('Read error:', error);
        } finally {
            reader.releaseLock();
            isReadingRef.current = false;
            setIsConnected(false);
        }
    };

    useEffect(() => {
        if (!containerRef.current) return;

        const term = new XTerm({
            cursorBlink: true,
            theme: {
                background: '#1e1e1e',
            },
            fontSize: 14,
            fontFamily: 'Menlo, Monaco, "Courier New", monospace',
        });

        const fitAddon = new FitAddon();
        term.loadAddon(fitAddon);

        term.open(containerRef.current);
        fitAddon.fit();

        termRef.current = term;

        // Handle terminal input (typing)
        term.onData((data) => {
            // Echo back to the serial port if connected
            if (portRef.current && portRef.current.writable) {
                // We need to access the write method exposed via ref, but we are inside the component.
                // We can duplicate the write logic or just use the port directly.
                const writeToPort = async () => {
                    const encoder = new TextEncoder();
                    const writer = portRef.current.writable.getWriter();
                    try {
                        await writer.write(encoder.encode(data));
                    } catch (e) {
                        console.error('Write error', e);
                    } finally {
                        writer.releaseLock();
                    }
                };
                writeToPort();
            }
        });

        // Handle resize
        const handleResize = () => {
            fitAddon.fit();
        };
        window.addEventListener('resize', handleResize);

        return () => {
            window.removeEventListener('resize', handleResize);
            term.dispose();
            // Cleanup serial port if needed
            if (portRef.current) {
                portRef.current.close().catch(console.error);
            }
        };
    }, []);

    return (
        <div
            ref={containerRef}
            className="w-full h-full min-h-[400px] bg-[#1e1e1e] rounded-lg overflow-hidden"
        />
    );
});

SerialTerminal.displayName = 'SerialTerminal';
