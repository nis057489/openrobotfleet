import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { getRobots, sendCommand } from "../api";
import { Robot } from "../types";
import { Signal, Wifi, Clock, Laptop as LaptopIcon } from "lucide-react";
import { formatDistanceToNow } from "date-fns";

export function Laptops() {
    const navigate = useNavigate();
    const [laptops, setLaptops] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        getRobots()
            .then(robots => setLaptops(robots.filter(r => r.type === 'laptop')))
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

    if (loading) return <div className="p-8 text-center text-gray-500">Loading fleet data...</div>;
    if (error) return <div className="p-8 text-center text-red-500">Error: {error}</div>;

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">Laptops</h1>
                    <p className="text-gray-500">Manage your development laptops</p>
                </div>
                <div className="flex gap-3">
                    <button
                        onClick={() => navigate("/install?type=laptop")}
                        className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium"
                    >
                        Add Laptop
                    </button>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {laptops.map((laptop) => (
                    <LaptopCard key={laptop.id} robot={laptop} />
                ))}
            </div>
        </div>
    );
}

function LaptopCard({ robot }: { robot: Robot }) {
    const navigate = useNavigate();
    const isOnline = robot.status !== "offline" && robot.status !== "unknown";
    const lastSeen = robot.last_seen ? formatDistanceToNow(new Date(robot.last_seen), { addSuffix: true }) : "Never";

    const handleWifiConnect = async (e: React.MouseEvent) => {
        e.stopPropagation();
        const ssid = prompt("Enter WiFi SSID:");
        if (!ssid) return;
        const password = prompt("Enter WiFi Password (optional):");

        try {
            await sendCommand(robot.id, {
                type: "wifi_profile",
                data: { ssid, password: password || "" }
            });
            alert("WiFi connection command sent!");
        } catch (err) {
            alert("Failed to send command: " + (err instanceof Error ? err.message : String(err)));
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
                                {robot.status || "Unknown"}
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
                            <Wifi size={16} /> IP Address
                        </span>
                        <span className="font-mono text-gray-700">{robot.ip || "—"}</span>
                    </div>
                    <div className="flex items-center justify-between text-sm">
                        <span className="text-gray-500 flex items-center gap-2">
                            <Clock size={16} /> Last Seen
                        </span>
                        <span className="font-medium text-gray-700">{lastSeen}</span>
                    </div>
                </div>
            </div>

            <div className="bg-gray-50 px-6 py-3 border-t border-gray-100 flex justify-between items-center gap-2">
                <button
                    onClick={handleWifiConnect}
                    className="text-sm font-medium text-blue-600 hover:text-blue-800"
                >
                    Connect WiFi
                </button>
                <button
                    onClick={() => navigate(`/robots/${robot.id}`)}
                    className="text-sm font-medium text-gray-600 hover:text-gray-900"
                >
                    View Details
                </button>
            </div>
        </div>
    );
}
