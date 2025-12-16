import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { scanNetwork } from "../api";
import { DiscoveryCandidate } from "../types";
import { Search, Wifi, ArrowRight, Loader2, Turtle } from "lucide-react";
import { useWebSocket, WSEvent } from "../contexts/WebSocketContext";

export function Discovery() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const location = useLocation();
    const query = new URLSearchParams(location.search);
    const type = query.get("type") || "robot";
    const [scanning, setScanning] = useState(false);
    const [candidates, setCandidates] = useState<DiscoveryCandidate[]>([]);
    const [error, setError] = useState<string | null>(null);
    const { addListener } = useWebSocket();

    useEffect(() => {
        return addListener((event: WSEvent) => {
            if (event.type === 'scan_result') {
                setCandidates(prev => {
                    if (prev.find(c => c.ip === event.data.ip)) {
                        return prev;
                    }
                    return [...prev, event.data as DiscoveryCandidate];
                });
            }
        });
    }, [addListener]);

    const handleScan = async () => {
        setScanning(true);
        setError(null);
        setCandidates([]);
        try {
            const results = await scanNetwork();
            setCandidates(results || []);
        } catch (err) {
            setError(err instanceof Error ? err.message : t("discovery.scanFailed"));
        } finally {
            setScanning(false);
        }
    };

    const handleInstall = (ip: string) => {
        navigate(`/install?type=${type}`, { state: { ip } });
    };

    return (
        <div className="max-w-4xl mx-auto space-y-8">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">{t("discovery.title")}</h1>
                <p className="text-gray-500">{t("discovery.subtitle")}</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 p-8 text-center">
                <div className="w-16 h-16 bg-blue-50 rounded-full flex items-center justify-center mx-auto mb-4">
                    <Search size={32} className="text-blue-600" />
                </div>
                <h2 className="text-lg font-semibold text-gray-900 mb-2">{t("discovery.findRobots")}</h2>
                <p className="text-gray-500 mb-6 max-w-md mx-auto">
                    {t("discovery.description")}
                </p>
                <button
                    onClick={handleScan}
                    disabled={scanning}
                    className="bg-blue-600 text-white px-6 py-3 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 mx-auto disabled:opacity-50"
                >
                    {scanning ? <Loader2 className="animate-spin" /> : <Wifi size={20} />}
                    {scanning ? t("discovery.scanning") : t("discovery.startScan")}
                </button>
                {error && <p className="text-red-500 mt-4">{error}</p>}
            </div>

            {candidates.length > 0 && (
                <div className="space-y-4">
                    <h3 className="font-semibold text-gray-900">{t("discovery.discoveredDevices")}</h3>
                    <div className="grid gap-4">
                        {candidates.map((c) => (
                            <div key={c.ip} className={`bg-white p-4 rounded-xl border flex items-center justify-between ${c.status === 'enrolled' ? 'border-green-200 bg-green-50' : 'border-gray-200'}`}>
                                <div className="flex items-center gap-4">
                                    <div className={`w-10 h-10 rounded-lg flex items-center justify-center ${c.manufacturer === 'Raspberry Pi' ? 'bg-green-100 text-green-600' : 'bg-gray-100 text-gray-600'}`}>
                                        {c.manufacturer === 'Raspberry Pi' ? <Turtle size={20} /> : <Wifi size={20} />}
                                    </div>
                                    <div>
                                        <div className="flex items-center gap-2">
                                            <p className="font-medium text-gray-900">{c.ip}</p>
                                            {c.manufacturer && (
                                                <span className="text-xs px-2 py-0.5 bg-gray-100 text-gray-600 rounded-full">
                                                    {c.manufacturer}
                                                </span>
                                            )}
                                            {c.status === 'enrolled' && (
                                                <span className="text-xs px-2 py-0.5 bg-green-100 text-green-700 rounded-full">
                                                    {t("discovery.enrolled")}
                                                </span>
                                            )}
                                        </div>
                                        <div className="flex items-center gap-3 text-xs text-gray-500 mt-1">
                                            <span>{t("discovery.portOpen", { port: c.port })}</span>
                                            {c.mac && <span>MAC: {c.mac}</span>}
                                        </div>
                                    </div>
                                </div>
                                {c.status !== 'enrolled' ? (
                                    <button
                                        onClick={() => handleInstall(c.ip)}
                                        className="flex items-center gap-2 text-blue-600 hover:text-blue-700 font-medium text-sm px-3 py-2 hover:bg-blue-50 rounded-lg transition-colors"
                                    >
                                        {type === 'laptop' ? t("discovery.setupLaptop") : t("discovery.setupRobot")} <ArrowRight size={16} />
                                    </button>
                                ) : (
                                    <button
                                        disabled
                                        className="text-green-600 font-medium text-sm px-3 py-2 cursor-default"
                                    >
                                        {t("discovery.alreadyManaged")}
                                    </button>
                                )}
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}
