import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { getRobots, identifyAll } from "../api";
import { Robot } from "../types";
import { Signal, Wifi, Clock, Eye, Settings, Activity } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { zhCN } from "date-fns/locale";
import { useTranslation } from "react-i18next";
import { FleetActionsModal } from "../components/FleetActionsModal";
import { useWebSocket, WSEvent } from "../contexts/WebSocketContext";
import { getRobotMood } from "../utils/mood";

export function Robots() {
    const navigate = useNavigate();
    const { t } = useTranslation();
    const [robots, setRobots] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [patterns, setPatterns] = useState<Record<number, string>>({});
    const [showFleetActions, setShowFleetActions] = useState(false);
    const { addListener } = useWebSocket();

    useEffect(() => {
        getRobots()
            .then(robots => setRobots(robots.filter(r => r.type !== 'laptop')))
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        return addListener((event: WSEvent) => {
            if (event.type === 'status_update') {
                setRobots(prev => {
                    const index = prev.findIndex(r => r.agent_id === event.agent_id);
                    if (index !== -1) {
                        const updated = [...prev];
                        updated[index] = {
                            ...updated[index],
                            status: event.data.status,
                            ip: event.data.ip,
                            last_seen: event.data.ts,
                            job_id: event.data.job_id,
                            job_status: event.data.job_status,
                            job_error: event.data.job_error,
                        };
                        return updated;
                    }
                    return prev;
                });
            }
        });
    }, [addListener]);

    const handleIdentifyAll = async () => {
        try {
            const newPatterns = await identifyAll();
            setPatterns(newPatterns);
            // Clear after 10 seconds (matching backend duration)
            setTimeout(() => setPatterns({}), 10000);
        } catch (err) {
            console.error("Failed to identify all:", err);
        }
    };

    if (loading) return <div className="p-8 text-center text-gray-500">{t("robots.loading")}</div>;
    if (error) return <div className="p-8 text-center text-red-500">{t("common.error")}: {error}</div>;

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">{t("common.robots")}</h1>
                    <p className="text-gray-500">{t("robots.subtitle")}</p>
                </div>
                <div className="flex gap-3 w-full md:w-auto">
                    <button
                        onClick={() => setShowFleetActions(true)}
                        className="flex-1 md:flex-none justify-center bg-white border border-gray-300 text-gray-700 px-4 py-2 rounded-lg hover:bg-gray-50 transition-colors font-medium flex items-center gap-2"
                    >
                        <Settings size={18} />
                        {t("settings.fleetMaintenance")}
                    </button>
                    <button
                        onClick={handleIdentifyAll}
                        className="flex-1 md:flex-none justify-center bg-white border border-gray-300 text-gray-700 px-4 py-2 rounded-lg hover:bg-gray-50 transition-colors font-medium flex items-center gap-2"
                    >
                        <Eye size={18} />
                        {t("common.identifyAll") || "Identify All"}
                    </button>
                    <button
                        onClick={() => navigate("/discovery")}
                        className="flex-1 md:flex-none justify-center bg-white border border-gray-300 text-gray-700 px-4 py-2 rounded-lg hover:bg-gray-50 transition-colors font-medium flex items-center gap-2"
                    >
                        {t("common.scanNetwork")}
                    </button>
                    <button
                        onClick={() => navigate("/install")}
                        className="flex-1 md:flex-none justify-center bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2"
                    >
                        {t("common.addRobot")}
                    </button>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {robots.map((robot) => (
                    <RobotCard key={robot.id} robot={robot} pattern={patterns[robot.id]} />
                ))}
            </div>

            {showFleetActions && (
                <FleetActionsModal onClose={() => setShowFleetActions(false)} />
            )}
        </div>
    );
}

function BlinkStatus({ pattern }: { pattern: string }) {
    const [step, setStep] = useState(0);

    useEffect(() => {
        const interval = setInterval(() => {
            setStep(s => (s + 1) % pattern.length);
        }, 200);
        return () => clearInterval(interval);
    }, [pattern]);

    const char = pattern[step];
    const green = char === 'g' || char === 'b';
    const red = char === 'r' || char === 'b';

    return (
        <div className="flex gap-2 items-center bg-gray-900 px-3 py-1.5 rounded-full border border-gray-700">
            <div className={`w-3 h-3 rounded-full transition-all duration-100 ${green ? 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.8)] scale-110' : 'bg-green-900/30'}`} />
            <div className={`w-3 h-3 rounded-full transition-all duration-100 ${red ? 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.8)] scale-110' : 'bg-red-900/30'}`} />
        </div>
    );
}

function RobotCard({ robot, pattern }: { robot: Robot, pattern?: string }) {
    const navigate = useNavigate();
    const { t, i18n } = useTranslation();
    const isOnline = robot.status !== "offline" && robot.status !== "unknown";
    const lastSeen = robot.last_seen ? formatDistanceToNow(new Date(robot.last_seen), {
        addSuffix: true,
        locale: i18n.language.startsWith('zh') ? zhCN : undefined
    }) : t("common.never");

    return (
        <div className="bg-white rounded-xl border border-gray-200 overflow-hidden hover:shadow-md transition-shadow relative">
            {pattern && (
                <div className="absolute top-4 right-4 z-10">
                    <BlinkStatus pattern={pattern} />
                </div>
            )}
            <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                    <div>
                        <h3 className="font-bold text-lg text-gray-900">
                            {robot.name}
                        </h3>
                        <div className="flex items-center gap-2 mt-1">
                            <span
                                className={`w-2 h-2 rounded-full ${isOnline ? "bg-green-500" : "bg-gray-300"
                                    }`}
                            />
                            <span className="text-sm text-gray-500 capitalize">
                                {t(`common.${(robot.status || '').toLowerCase()}`) || robot.status || t("common.unknown")}
                            </span>
                            <span className="ml-2" title="Robot Mood">{getRobotMood(robot)}</span>
                        </div>
                    </div>
                    {!pattern && (
                        <div className="p-2 bg-gray-50 rounded-lg">
                            <Signal size={20} className={isOnline ? "text-green-600" : "text-gray-400"} />
                        </div>
                    )}
                </div>

                <div className="space-y-3">
                    <div className="flex items-center justify-between text-sm">
                        <span className="text-gray-500 flex items-center gap-2">
                            <Wifi size={16} /> {t("common.ipAddress")}
                        </span>
                        <span className="font-mono text-gray-700">{robot.ip || "—"}</span>
                    </div>
                    <div className="flex items-center justify-between text-sm">
                        <span className="text-gray-500 flex items-center gap-2">
                            <Clock size={16} /> {t("common.lastSeen")}
                        </span>
                        <span className="font-medium text-gray-700">{lastSeen}</span>
                    </div>
                    {robot.job_status && (
                        <div className="flex items-center justify-between text-sm">
                            <span className="text-gray-500 flex items-center gap-2">
                                <Activity size={16} /> Config
                            </span>
                            <span className={`font-medium ${robot.job_status === 'success' ? 'text-green-600' :
                                robot.job_status === 'failed' ? 'text-red-600' :
                                    'text-blue-600'
                                }`}>
                                {robot.job_status === 'success' ? 'Applied' :
                                    robot.job_status === 'failed' ? 'Failed' :
                                        'Applying...'}
                            </span>
                        </div>
                    )}
                </div>
            </div>

            <div className="bg-gray-50 px-6 py-3 border-t border-gray-100 flex justify-end gap-2">
                <button
                    onClick={() => navigate(`/robots/${robot.id}`, { state: { tab: 'logs' } })}
                    className="text-sm font-medium text-gray-600 hover:text-gray-900 px-3 py-1.5 rounded-md hover:bg-gray-200 transition-colors"
                >
                    {t("common.logs")}
                </button>
                <button
                    onClick={() => navigate(`/robots/${robot.id}`)}
                    className="text-sm font-medium text-blue-600 hover:text-blue-700 px-3 py-1.5 rounded-md hover:bg-blue-50 transition-colors"
                >
                    {t("common.manage")}
                </button>
            </div>
        </div>
    );
}
