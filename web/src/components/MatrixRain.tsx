import { useEffect, useRef } from 'react';
import { useWebSocket, WSEvent } from '../contexts/WebSocketContext';

export function MatrixRain() {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const { addListener } = useWebSocket();

    // Store pending logs in a queue
    const logQueueRef = useRef<string[]>([]);

    // Store active log message for each column: { text: string, index: number } | null
    const columnLogsRef = useRef<({ text: string, index: number } | null)[]>([]);

    // Track build log index to avoid duplicates
    const lastLogCountRef = useRef<number>(0);

    useEffect(() => {
        return addListener((event: WSEvent) => {
            if (event.type === 'status_update') {
                logQueueRef.current.push(`[STATUS] ${event.agent_id} ${event.data.status} ${event.data.ip}`);
            } else if (event.type === 'scan_result') {
                logQueueRef.current.push(`[SCAN] ${event.data.ip} ${event.data.manufacturer || ''}`);
            } else if (event.type === 'build_update') {
                // Push main status
                logQueueRef.current.push(`[BUILD] ${event.data.step} (${event.data.status})`);

                // Process new logs
                if (event.data.logs) {
                    if (event.data.logs.length < lastLogCountRef.current) {
                        lastLogCountRef.current = 0; // Reset detected
                    }

                    const newLogs = event.data.logs.slice(lastLogCountRef.current);
                    if (newLogs.length > 0) {
                        newLogs.forEach(line => {
                            // Clean up line and truncate
                            const cleanLine = line.replace(/\s+/g, ' ').trim();
                            if (cleanLine) {
                                logQueueRef.current.push(`> ${cleanLine.substring(0, 60)}`);
                            }
                        });
                        lastLogCountRef.current = event.data.logs.length;
                    }
                }
            }

            // Keep queue size manageable
            if (logQueueRef.current.length > 100) {
                logQueueRef.current = logQueueRef.current.slice(-100);
            }
        });
    }, [addListener]);

    useEffect(() => {
        const canvas = canvasRef.current;
        if (!canvas) return;

        const ctx = canvas.getContext('2d');
        if (!ctx) return;

        let width = window.innerWidth;
        let height = window.innerHeight;
        canvas.width = width;
        canvas.height = height;

        const columns = Math.floor(width / 20);
        const drops: number[] = [];

        // Initialize column logs
        columnLogsRef.current = new Array(columns).fill(null);

        for (let i = 0; i < columns; i++) {
            drops[i] = Math.floor(Math.random() * -100); // Start at random heights above screen
        }

        const draw = () => {
            // Reset shadow to prevent "green screen" accumulation
            ctx.shadowBlur = 0;
            ctx.shadowColor = 'transparent';

            // Lower opacity = longer trails (persistence)
            ctx.fillStyle = 'rgba(0, 0, 0, 0.03)';
            ctx.fillRect(0, 0, width, height);

            ctx.font = '15px monospace';

            for (let i = 0; i < drops.length; i++) {
                let text = '';
                let isLog = false;

                // Check if this column has an active log
                const activeLog = columnLogsRef.current[i];

                if (activeLog) {
                    // Use next char from log
                    text = activeLog.text[activeLog.index];
                    activeLog.index++;
                    isLog = true;

                    // If finished log, clear it
                    if (activeLog.index >= activeLog.text.length) {
                        columnLogsRef.current[i] = null;
                    }
                } else {
                    // Try to assign a new log from queue if we are near top of screen
                    // This ensures the log starts reading from top
                    if (drops[i] < 2 && logQueueRef.current.length > 0 && Math.random() > 0.5) {
                        const nextLog = logQueueRef.current.shift();
                        if (nextLog) {
                            columnLogsRef.current[i] = { text: nextLog, index: 0 };
                            // Use first char immediately
                            text = nextLog[0];
                            columnLogsRef.current[i]!.index++;
                            isLog = true;
                        }
                    }
                }

                if (!isLog) {
                    // Katakana or standard matrix chars
                    text = String.fromCharCode(0x30A0 + Math.random() * 96);
                }

                if (isLog) {
                    ctx.fillStyle = '#FFF'; // Standard green
                    ctx.shadowBlur = 4;     // Strong glow
                    ctx.shadowColor = '#0F0';
                } else {
                    ctx.fillStyle = '#0F0'; // Standard green
                    ctx.shadowBlur = 0;
                }

                ctx.fillText(text, i * 20, drops[i] * 20);

                // Reset drop if it goes off screen
                // But don't reset if we are in the middle of printing a log!
                if (drops[i] * 20 > height && Math.random() > 0.975 && !columnLogsRef.current[i]) {
                    drops[i] = 0;
                }
                drops[i]++;
            }
        };

        const interval = setInterval(draw, 33);

        const handleResize = () => {
            width = window.innerWidth;
            height = window.innerHeight;
            canvas.width = width;
            canvas.height = height;
        };

        window.addEventListener('resize', handleResize);

        return () => {
            clearInterval(interval);
            window.removeEventListener('resize', handleResize);
        };
    }, []);

    return (
        <canvas
            ref={canvasRef}
            className="fixed top-0 left-0 w-full h-full pointer-events-none z-0 opacity-50"
        />
    );
}
