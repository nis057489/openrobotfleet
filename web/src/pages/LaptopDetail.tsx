import { useEffect, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getRobot, sendCommand, updateRobotTags, getSystemConfig, deleteRobot } from "../api";
import { Robot } from "../types";
import { ArrowLeft, Terminal, RefreshCw, Power, GitBranch, Save, Activity, Plus, X, Lightbulb, Trash2 } from "lucide-react";
import { Terminal as TerminalView } from "../components/Terminal";
import { useNotification } from "../contexts/NotificationContext";

export function LaptopDetail() {
    const { t } = useTranslation();
    const { id } = useParams();
    const navigate = useNavigate();
    const location = useLocation();
    const { success, error } = useNotification();
    const [robot, setRobot] = useState<Robot | null>(null);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState<"overview" | "logs" | "terminal">("overview");
    const [demoMode, setDemoMode] = useState(false);

    // Command state
    const [repoUrl, setRepoUrl] = useState("");
    const [branch, setBranch] = useState("main");
    const [path, setPath] = useState("");
    const [cmdLoading, setCmdLoading] = useState(false);

    // Tag state
    const [newTag, setNewTag] = useState("");
    const [isAddingTag, setIsAddingTag] = useState(false);

    useEffect(() => {
        if (location.state && (location.state as any).tab === 'logs') {
            setActiveTab('logs');
        }
    }, [location]);

    useEffect(() => {
        if (id) {
            Promise.all([getRobot(id), getSystemConfig()])
                .then(([robotData, sysConfig]) => {
                    setRobot(robotData);
                    setDemoMode(sysConfig.demo_mode);
                })
                .catch(console.error)
                .finally(() => setLoading(false));
        }
    }, [id]);

    const handleCommand = async (type: string, data: any = {}) => {
        if (!robot) return;
        setCmdLoading(true);
        try {
            await sendCommand(robot.id, { type, data });
            success(t("robotDetail.commandSent", { type }));
        } catch (err) {
            error(err instanceof Error ? err.message : t("robotDetail.commandFailed"));
        } finally {
            setCmdLoading(false);
        }
    };

    const handleAddTag = async () => {
        if (!robot || !newTag.trim()) return;
        const updatedTags = [...(robot.tags || []), newTag.trim()];
        try {
            const updated = await updateRobotTags(robot.id, updatedTags);
            setRobot(updated);
            setNewTag("");
            setIsAddingTag(false);
        } catch (err) {
            console.error("Failed to add tag", err);
        }
    };

    const handleRemoveTag = async (tagToRemove: string) => {
        if (!robot) return;
        const updatedTags = (robot.tags || []).filter(t => t !== tagToRemove);
        try {
            const updated = await updateRobotTags(robot.id, updatedTags);
            setRobot(updated);
        } catch (err) {
            console.error("Failed to remove tag", err);
        }
    };

    const handleDelete = async () => {
        if (!robot) return;
        if (!confirm(t("robotDetail.deleteConfirm"))) return;
        try {
            await deleteRobot(robot.id);
            navigate("/laptops");
        } catch (err) {
            console.error("Failed to delete laptop", err);
            alert(t("robotDetail.deleteFailed"));
        }
    };

    if (loading) return <div className="p-8 text-gray-500">{t("robots.loading")}</div>;
    if (!robot) return <div className="p-8 text-red-500">Laptop not found</div>;

    return (
        <div className="max-w-4xl mx-auto space-y-6">
            {/* Header */}
            <div className="flex items-center gap-4">
                <button onClick={() => navigate("/laptops")} className="p-2 hover:bg-gray-100 rounded-lg">
                    <ArrowLeft size={20} />
                </button>
                <div className="flex-1">
                    <div className="flex flex-col md:flex-row md:items-center justify-between gap-2">
                        <h1 className="text-2xl font-bold text-gray-900">{robot.name}</h1>
                        <div className="flex items-center gap-2 flex-wrap">
                            <button
                                onClick={() => handleCommand("identify")}
                                className="p-2 text-gray-500 hover:text-yellow-600 hover:bg-yellow-50 rounded-lg transition-colors"
                                title="Identify (Sound)"
                            >
                                <Lightbulb size={20} />
                            </button>
                            <button
                                onClick={handleDelete}
                                className="p-2 text-gray-500 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors"
                                title={t("robotDetail.delete")}
                            >
                                <Trash2 size={20} />
                            </button>
                            {robot.tags?.map(tag => (
                                <span key={tag} className="inline-flex items-center gap-1 px-2 py-1 rounded-full bg-blue-50 text-blue-700 text-xs font-medium border border-blue-100">
                                    {tag}
                                    <button onClick={() => handleRemoveTag(tag)} className="hover:text-blue-900"><X size={12} /></button>
                                </span>
                            ))}
                            {isAddingTag ? (
                                <div className="flex items-center gap-1">
                                    <input
                                        autoFocus
                                        value={newTag}
                                        onChange={e => setNewTag(e.target.value)}
                                        onKeyDown={e => e.key === 'Enter' && handleAddTag()}
                                        onBlur={() => setIsAddingTag(false)}
                                        className="w-24 px-2 py-1 text-xs border border-gray-300 rounded-md"
                                        placeholder="New tag..."
                                    />
                                </div>
                            ) : (
                                <button onClick={() => setIsAddingTag(true)} className="p-1 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded-full">
                                    <Plus size={16} />
                                </button>
                            )}
                        </div>
                    </div>
                    <div className="flex items-center gap-2 text-sm text-gray-500 mt-1">
                        <span className={`w-2 h-2 rounded-full ${robot.status !== 'offline' ? 'bg-green-500' : 'bg-gray-300'}`} />
                        <span className="capitalize">{t(`common.${robot.status}`) || robot.status || t("common.unknown")}</span>
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
                    {t("robotDetail.overview")}
                </button>
                <button
                    onClick={() => setActiveTab("logs")}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeTab === "logs" ? "border-blue-600 text-blue-600" : "border-transparent text-gray-500 hover:text-gray-700"
                        }`}
                >
                    {t("robotDetail.logs")}
                </button>
                <button
                    onClick={() => setActiveTab("terminal")}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeTab === "terminal" ? "border-blue-600 text-blue-600" : "border-transparent text-gray-500 hover:text-gray-700"
                        }`}
                >
                    {t("robotDetail.terminal")}
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
                                    <RefreshCw size={16} /> {t("settings.restartRos")}
                                </div>
                                <p className="text-xs text-gray-500">{t("settings.restartRosDesc")}</p>
                            </button>
                            <button
                                onClick={() => handleCommand("reboot")}
                                disabled={cmdLoading}
                                className="p-3 border border-gray-200 rounded-lg hover:bg-red-50 hover:border-red-100 text-left transition-colors group"
                            >
                                <div className="flex items-center gap-2 font-medium text-gray-700 group-hover:text-red-700 mb-1">
                                    <Power size={16} /> {t("robotDetail.rebootSystem")}
                                </div>
                                <p className="text-xs text-gray-500 group-hover:text-red-600">{t("robotDetail.rebootSystemDesc")}</p>
                            </button>
                            <button
                                onClick={() => navigate("/install", { state: { ip: robot.ip, name: robot.name } })}
                                className="p-3 border border-gray-200 rounded-lg hover:bg-blue-50 hover:border-blue-100 text-left transition-colors group col-span-2"
                            >
                                <div className="flex items-center gap-2 font-medium text-gray-700 group-hover:text-blue-700 mb-1">
                                    <Terminal size={16} /> {t("robotDetail.reinstallAgent")}
                                </div>
                                <p className="text-xs text-gray-500 group-hover:text-blue-600">{t("robotDetail.reinstallAgentDesc")}</p>
                            </button>
                            <button
                                onClick={() => handleCommand("identify")}
                                disabled={cmdLoading}
                                className="p-3 border border-gray-200 rounded-lg hover:bg-yellow-50 hover:border-yellow-100 text-left transition-colors group"
                            >
                                <div className="flex items-center gap-2 font-medium text-gray-700 group-hover:text-yellow-700 mb-1">
                                    <Lightbulb size={16} /> {t("robotDetail.identifyMe")}
                                </div>
                                <p className="text-xs text-gray-500 group-hover:text-yellow-600">{t("robotDetail.identifyMeDesc")}</p>
                            </button>
                            <button
                                onClick={handleDelete}
                                disabled={cmdLoading}
                                className="p-3 border border-gray-200 rounded-lg hover:bg-red-50 hover:border-red-100 text-left transition-colors group"
                            >
                                <div className="flex items-center gap-2 font-medium text-gray-700 group-hover:text-red-700 mb-1">
                                    <Trash2 size={16} /> {t("robotDetail.delete")}
                                </div>
                                <p className="text-xs text-gray-500 group-hover:text-red-600">{t("robotDetail.deleteDesc")}</p>
                            </button>
                        </div>
                    </div>

                    {/* Update Repo */}
                    <div className="bg-white rounded-xl border border-gray-200 p-6">
                        <h3 className="font-semibold text-gray-900 mb-4 flex items-center gap-2">
                            <GitBranch size={18} /> {t("semesterWizard.updateRepo")}
                        </h3>
                        <div className="space-y-4">
                            <div>
                                <label className="block text-xs font-medium text-gray-700 mb-1">{t("semesterWizard.repoUrl")}</label>
                                <input
                                    value={repoUrl}
                                    onChange={(e) => setRepoUrl(e.target.value)}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                                    placeholder="https://github.com/..."
                                />
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("robotDetail.branch")}</label>
                                    <input
                                        value={branch}
                                        onChange={(e) => setBranch(e.target.value)}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                                        placeholder="main"
                                    />
                                </div>
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("robotDetail.path")}</label>
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
                                <Save size={16} /> {t("robotDetail.updateCode")}
                            </button>
                        </div>
                    </div>
                </div>
            ) : activeTab === "logs" ? (
                demoMode ? (
                    <div className="bg-black rounded-xl p-6 font-mono text-sm text-gray-300 min-h-[400px] flex items-center justify-center">
                        <div className="text-center">
                            <Terminal size={48} className="mx-auto mb-4 text-gray-600" />
                            <p className="text-lg font-medium text-gray-400">{t("common.demoMode")}</p>
                            <p className="text-gray-600 mt-2">{t("robotDetail.logsDisabledDemo")}</p>
                        </div>
                    </div>
                ) : (
                    <div className="bg-black rounded-xl p-6 font-mono text-sm text-gray-300 min-h-[400px]">
                        <div className="flex items-center gap-2 text-gray-500 mb-4 border-b border-gray-800 pb-2">
                            <Terminal size={16} />
                            <span>/var/log/syslog</span>
                        </div>
                        <p>{t("robotDetail.logsNotImplemented")}</p>
                        <p className="text-gray-600 mt-2">
                            {t("robotDetail.logsHelp")}
                        </p>
                    </div>
                )
            ) : (
                demoMode ? (
                    <div className="bg-black rounded-xl p-6 font-mono text-sm text-gray-300 min-h-[400px] flex items-center justify-center">
                        <div className="text-center">
                            <Terminal size={48} className="mx-auto mb-4 text-gray-600" />
                            <p className="text-lg font-medium text-gray-400">{t("common.demoMode")}</p>
                            <p className="text-gray-600 mt-2">{t("robotDetail.terminalDisabledDemo")}</p>
                        </div>
                    </div>
                ) : (
                    <div className="h-[600px] bg-black rounded-xl overflow-hidden border border-gray-800">
                        <TerminalView robotId={robot.id} />
                    </div>
                )
            )}
        </div>
    );
}
