import { useEffect, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";
import { getRobot, sendCommand } from "../api";
import { Robot } from "../types";
import { ArrowLeft, Terminal, RefreshCw, Power, GitBranch, Save, Activity } from "lucide-react";

export function RobotDetail() {
    const { id } = useParams();
    const navigate = useNavigate();
    const location = useLocation();
    const [robot, setRobot] = useState<Robot | null>(null);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState<"overview" | "logs">("overview");

    // Command state
    const [repoUrl, setRepoUrl] = useState("");
    const [branch, setBranch] = useState("main");
    const [path, setPath] = useState("");
    const [cmdLoading, setCmdLoading] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

    useEffect(() => {
        if (location.state && (location.state as any).tab === 'logs') {
            setActiveTab('logs');
        }
    }, [location]);

    useEffect(() => {
        if (id) {
            getRobot(id)
                .then(setRobot)
                .catch(console.error)
                .finally(() => setLoading(false));
        }
    }, [id]);

    const handleCommand = async (type: string, data: any = {}) => {
        if (!robot) return;
        setCmdLoading(true);
        setMessage(null);
        try {
            await sendCommand(robot.id, { type, data });
            setMessage({ type: 'success', text: `Command '${type}' sent successfully` });
        } catch (err) {
            setMessage({ type: 'error', text: err instanceof Error ? err.message : "Failed to send command" });
        } finally {
            setCmdLoading(false);
        }
    };

    if (loading) return <div className="p-8 text-gray-500">Loading robot...</div>;
    if (!robot) return <div className="p-8 text-red-500">Robot not found</div>;

    return (
        <div className="max-w-4xl mx-auto space-y-6">
            {/* Header */}
            <div className="flex items-center gap-4">
                <button onClick={() => navigate("/robots")} className="p-2 hover:bg-gray-100 rounded-lg">
                    <ArrowLeft size={20} />
                </button>
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">{robot.name}</h1>
                    <div className="flex items-center gap-2 text-sm text-gray-500">
                        <span className={`w-2 h-2 rounded-full ${robot.status !== 'offline' ? 'bg-green-500' : 'bg-gray-300'}`} />
                        <span className="capitalize">{robot.status || "Unknown"}</span>
                        <span>•</span>
                        <span className="font-mono">{robot.ip}</span>
                    </div>
                </div>
            </div>

            {/* Tabs */}
            <div className="flex gap-1 border-b border-gray-200">
                <button
                    onClick={() => setActiveTab("overview")}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeTab === "overview" ? "border-blue-600 text-blue-600" : "border-transparent text-gray-500 hover:text-gray-700"
                        }`}
                >
                    Overview & Controls
                </button>
                <button
                    onClick={() => setActiveTab("logs")}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeTab === "logs" ? "border-blue-600 text-blue-600" : "border-transparent text-gray-500 hover:text-gray-700"
                        }`}
                >
                    Logs
                </button>
            </div>

            {activeTab === "overview" ? (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    {/* Quick Actions */}
                    <div className="bg-white rounded-xl border border-gray-200 p-6">
                        <h3 className="font-semibold text-gray-900 mb-4 flex items-center gap-2">
                            <Activity size={18} /> Quick Actions
                        </h3>
                        <div className="grid grid-cols-2 gap-3">
                            <button
                                onClick={() => handleCommand("restart_ros")}
                                disabled={cmdLoading}
                                className="p-3 border border-gray-200 rounded-lg hover:bg-gray-50 text-left transition-colors"
                            >
                                <div className="flex items-center gap-2 font-medium text-gray-700 mb-1">
                                    <RefreshCw size={16} /> Restart ROS
                                </div>
                                <p className="text-xs text-gray-500">Restart the ROS 2 daemon</p>
                            </button>
                            <button
                                onClick={() => handleCommand("reboot")}
                                disabled={cmdLoading}
                                className="p-3 border border-gray-200 rounded-lg hover:bg-red-50 hover:border-red-100 text-left transition-colors group"
                            >
                                <div className="flex items-center gap-2 font-medium text-gray-700 group-hover:text-red-700 mb-1">
                                    <Power size={16} /> Reboot System
                                </div>
                                <p className="text-xs text-gray-500 group-hover:text-red-600">Reboot the robot computer</p>
                            </button>
                        </div>
                    </div>

                    {/* Update Repo */}
                    <div className="bg-white rounded-xl border border-gray-200 p-6">
                        <h3 className="font-semibold text-gray-900 mb-4 flex items-center gap-2">
                            <GitBranch size={18} /> Update Repository
                        </h3>
                        <div className="space-y-4">
                            <div>
                                <label className="block text-xs font-medium text-gray-700 mb-1">Repo URL</label>
                                <input
                                    value={repoUrl}
                                    onChange={(e) => setRepoUrl(e.target.value)}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                                    placeholder="https://github.com/..."
                                />
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">Branch</label>
                                    <input
                                        value={branch}
                                        onChange={(e) => setBranch(e.target.value)}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                                        placeholder="main"
                                    />
                                </div>
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">Path</label>
                                    <input
                                        value={path}
                                        onChange={(e) => setPath(e.target.value)}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                                        placeholder="/workspace/src"
                                    />
                                </div>
                            </div>
                            <button
                                onClick={() => handleCommand("update_repo", { repo: repoUrl, branch, path })}
                                disabled={cmdLoading}
                                className="w-full bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors text-sm font-medium flex items-center justify-center gap-2"
                            >
                                <Save size={16} /> Update Code
                            </button>
                        </div>
                    </div>

                    {message && (
                        <div className={`col-span-full p-4 rounded-lg text-sm ${message.type === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>
                            {message.text}
                        </div>
                    )}
                </div>
            ) : (
                <div className="bg-black rounded-xl p-6 font-mono text-sm text-gray-300 min-h-[400px]">
                    <div className="flex items-center gap-2 text-gray-500 mb-4 border-b border-gray-800 pb-2">
                        <Terminal size={16} />
                        <span>/var/log/syslog</span>
                    </div>
                    <p>Logs are not yet implemented in the backend.</p>
                    <p className="text-gray-600 mt-2">
                        To view logs, you would typically need a log aggregation service or an API endpoint
                        that streams logs from the agent via MQTT or HTTP.
                    </p>
                </div>
            )}
        </div>
    );
}
