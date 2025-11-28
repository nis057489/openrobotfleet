import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { scanNetwork } from "../api";
import { DiscoveryCandidate } from "../types";
import { Search, Wifi, ArrowRight, Loader2, Turtle } from "lucide-react";

export function Discovery() {
    const navigate = useNavigate();
    const [scanning, setScanning] = useState(false);
    const [candidates, setCandidates] = useState<DiscoveryCandidate[]>([]);
    const [error, setError] = useState<string | null>(null);

    const handleScan = async () => {
        setScanning(true);
        setError(null);
        setCandidates([]);
        try {
            const results = await scanNetwork();
            setCandidates(results || []);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Scan failed");
        } finally {
            setScanning(false);
        }
    };

    const handleInstall = (ip: string) => {
        navigate("/install", { state: { ip } });
    };

    return (
        <div className="max-w-4xl mx-auto space-y-8">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">Network Discovery</h1>
                <p className="text-gray-500">Scan the local network for available robots</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 p-8 text-center">
                <div className="w-16 h-16 bg-blue-50 rounded-full flex items-center justify-center mx-auto mb-4">
                    <Search size={32} className="text-blue-600" />
                </div>
                <h2 className="text-lg font-semibold text-gray-900 mb-2">Find Robots</h2>
                <p className="text-gray-500 mb-6 max-w-md mx-auto">
                    Scan the local subnet for devices with SSH (port 22) open.
                    This helps find robots that have acquired new IP addresses.
                </p>
                <button
                    onClick={handleScan}
                    disabled={scanning}
                    className="bg-blue-600 text-white px-6 py-3 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 mx-auto disabled:opacity-50"
                >
                    {scanning ? <Loader2 className="animate-spin" /> : <Wifi size={20} />}
                    {scanning ? "Scanning Network..." : "Start Scan"}
                </button>
                {error && <p className="text-red-500 mt-4">{error}</p>}
            </div>

            {candidates.length > 0 && (
                <div className="space-y-4">
                    <h3 className="font-semibold text-gray-900">Discovered Devices</h3>
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
                                                    Enrolled
                                                </span>
                                            )}
                                        </div>
                                        <div className="flex items-center gap-3 text-xs text-gray-500 mt-1">
                                            <span>Port {c.port} Open</span>
                                            {c.mac && <span>MAC: {c.mac}</span>}
                                        </div>
                                    </div>
                                </div>
                                {c.status !== 'enrolled' ? (
                                    <button
                                        onClick={() => handleInstall(c.ip)}
                                        className="flex items-center gap-2 text-blue-600 hover:text-blue-700 font-medium text-sm px-3 py-2 hover:bg-blue-50 rounded-lg transition-colors"
                                    >
                                        Setup Robot <ArrowRight size={16} />
                                    </button>
                                ) : (
                                    <button
                                        disabled
                                        className="text-green-600 font-medium text-sm px-3 py-2 cursor-default"
                                    >
                                        Already Managed
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
