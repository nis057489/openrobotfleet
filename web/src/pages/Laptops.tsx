import { useEffect, useState, MouseEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getRobots, sendCommand, identifyAll } from "../api";
import { Robot } from "../types";
import { Signal, Wifi, Clock, Laptop as LaptopIcon, Lightbulb, Loader2, Check, Activity } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { zhCN } from "date-fns/locale";
import { useWebSocket, WSEvent } from "../contexts/WebSocketContext";

export function Laptops() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const [laptops, setLaptops] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const { addListener } = useWebSocket();

    useEffect(() => {
        getRobots()
            .then(robots => setLaptops(robots.filter(r => r.type === 'laptop')))
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        return addListener((event: WSEvent) => {
            if (event.type === 'status_update') {
                setLaptops(prev => {
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
                    } else if (event.id && event.data.type === 'laptop') {
                        // New laptop
                        const newLaptop: Robot = {
                            id: event.id,
                            agent_id: event.agent_id,
                            name: event.data.name || event.agent_id,
                            type: 'laptop',
                            status: event.data.status,
                            ip: event.data.ip,
                            last_seen: event.data.ts,
                            job_id: event.data.job_id,
                            job_status: event.data.job_status,
                            job_error: event.data.job_error,
                            notes: '',
                            ssh_address: '',
                            ssh_user: '',
                            ssh_key: '',
                            tags: [],
                        };
                        return [...prev, newLaptop];
                    }
                    return prev;
                });
            }
        });
    }, [addListener]);

    const handleIdentifyAll = async () => {
        try {
            await identifyAll();
        } catch (err) {
            console.error("Failed to identify all:", err);
            alert(t("common.error"));
        }
    };

    if (loading) return <div className="p-8 text-center text-gray-500">{t("laptops.loading")}</div>;
    if (error) return <div className="p-8 text-center text-red-500">{t("common.error")}: {error}</div>;

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">{t("laptops.title")}</h1>
                    <p className="text-gray-500">{t("laptops.subtitle")}</p>
                </div>
                <div className="flex gap-3 w-full md:w-auto">
                    <button
                        onClick={handleIdentifyAll}
                        className="flex-1 md:flex-none justify-center bg-white border border-gray-300 text-gray-700 px-4 py-2 rounded-lg hover:bg-gray-50 transition-colors font-medium flex items-center gap-2"
                    >
                        <Lightbulb size={18} />
                        {t("common.identifyAll") || "Identify All"}
                    </button>
                    <button
                        onClick={() => navigate("/discovery?type=laptop")}
                        className="flex-1 md:flex-none justify-center bg-white border border-gray-300 text-gray-700 px-4 py-2 rounded-lg hover:bg-gray-50 transition-colors font-medium flex items-center gap-2"
                    >
                        {t("common.scanNetwork")}
                    </button>
                    <button
                        onClick={() => navigate("/install?type=laptop")}
                        className="flex-1 md:flex-none justify-center bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium"
                    >
                        {t("laptops.addLaptop")}
                    </button>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {laptops.map((laptop) => (
                    <LaptopCard
                        key={laptop.id}
                        robot={laptop}
                    />
                ))}
            </div>
        </div>
    );
}

function LaptopCard({ robot }: { robot: Robot }) {
    const { t, i18n } = useTranslation();
    const navigate = useNavigate();
    const [identifying, setIdentifying] = useState(false);
    const [success, setSuccess] = useState(false);
    const isOnline = robot.status !== "offline" && robot.status !== "unknown";
    const lastSeen = robot.last_seen ? formatDistanceToNow(new Date(robot.last_seen), {
        addSuffix: true,
        locale: i18n.language.startsWith('zh') ? zhCN : undefined
    }) : t("common.never");

    const handleIdentify = async (e: MouseEvent) => {
        e.stopPropagation();
        setIdentifying(true);
        setSuccess(false);
        try {
            await sendCommand(robot.id, { type: "identify", data: {} });
            setSuccess(true);
            setTimeout(() => setSuccess(false), 2000);
        } catch (err) {
            console.error("Failed to identify", err);
            alert(t("laptops.commandFailed", { error: err instanceof Error ? err.message : String(err) }));
        } finally {
            setIdentifying(false);
        }
    };

    return (
        <div className="bg-white rounded-xl border border-gray-200 overflow-hidden hover:shadow-md transition-shadow">
            <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                    <div>
                        <h3 className="font-bold text-lg text-gray-900">{robot.name}</h3>
                        <div className="flex items-center gap-2 mt-1">
                            <span
                                className={`w-2 h-2 rounded-full ${isOnline ? "bg-green-500" : "bg-gray-300"
                                    }`}
                            />
                            <span className="text-sm text-gray-500 capitalize">
                                {t(`common.${(robot.status || '').toLowerCase()}`) || robot.status || t("common.unknown")}
                            </span>
                        </div>
                    </div>
                    <div className="p-2 bg-gray-50 rounded-lg">
                        <LaptopIcon size={20} className={isOnline ? "text-blue-600" : "text-gray-400"} />
                    </div>
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
                            <Clock size={16} /> Last Seen
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

            <div className="bg-gray-50 px-6 py-3 border-t border-gray-100 flex justify-between items-center gap-2">
                <button
                    onClick={handleIdentify}
                    disabled={identifying}
                    className={`text-sm font-medium flex items-center gap-1 transition-colors ${success ? "text-green-600" : "text-blue-600 hover:text-blue-800"
                        }`}
                >
                    {identifying ? (
                        <Loader2 size={16} className="animate-spin" />
                    ) : success ? (
                        <Check size={16} />
                    ) : (
                        <Lightbulb size={16} />
                    )}
                    {success ? t("robotDetail.identifySent") : t("robotDetail.identifyMe")}
                </button>
                <button
                    onClick={() => navigate(`/laptops/${robot.id}`)}
                    className="text-sm font-medium text-gray-600 hover:text-gray-900"
                >
                    {t("common.viewDetails")}
                </button>
            </div>
        </div>
    );
}
