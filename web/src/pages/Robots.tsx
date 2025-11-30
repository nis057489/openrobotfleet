import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { getRobots } from "../api";
import { Robot } from "../types";
import { Signal, Wifi, Clock } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { zhCN } from "date-fns/locale";
import { useTranslation } from "react-i18next";

export function Robots() {
    const navigate = useNavigate();
    const { t } = useTranslation();
    const [robots, setRobots] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        getRobots()
            .then(robots => setRobots(robots.filter(r => r.type !== 'laptop')))
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

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
                    <RobotCard key={robot.id} robot={robot} />
                ))}
            </div>
        </div>
    );
}

function RobotCard({ robot }: { robot: Robot }) {
    const navigate = useNavigate();
    const { t, i18n } = useTranslation();
    const isOnline = robot.status !== "offline" && robot.status !== "unknown";
    const lastSeen = robot.last_seen ? formatDistanceToNow(new Date(robot.last_seen), {
        addSuffix: true,
        locale: i18n.language.startsWith('zh') ? zhCN : undefined
    }) : t("common.never");

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
                        <Signal size={20} className={isOnline ? "text-green-600" : "text-gray-400"} />
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
                            <Clock size={16} /> {t("common.lastSeen")}
                        </span>
                        <span className="font-medium text-gray-700">{lastSeen}</span>
                    </div>
                </div>
            </div>

            <div className="bg-gray-50 px-6 py-3 border-t border-gray-100 flex justify-end gap-2">
                <button
                    onClick={() => navigate(`/robots/${robot.id}`, { state: { tab: 'logs' } })}
                    className="text-sm font-medium text-gray-600 hover:text-gray-900 px-3 py-1.5 rounded-md hover:bg-gray-200 transition-colors"
                >
                    Logs
                </button>
                <button
                    onClick={() => navigate(`/robots/${robot.id}`)}
                    className="text-sm font-medium text-blue-600 hover:text-blue-700 px-3 py-1.5 rounded-md hover:bg-blue-50 transition-colors"
                >
                    Manage
                </button>
            </div>
        </div>
    );
}
