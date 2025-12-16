import { useEffect, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getRobot, sendCommand, updateRobotTags, getSystemConfig, deleteRobot, updateRobotName } from "../api";
import { Robot } from "../types";
import { ArrowLeft, Terminal, RefreshCw, Power, GitBranch, Save, Activity, Tag, Plus, X, Camera, Play, Lightbulb, Trash2, Edit2 } from "lucide-react";
import { Terminal as TerminalView } from "../components/Terminal";
import { useNotification } from "../contexts/NotificationContext";
import { useWebSocket, WSEvent } from "../contexts/WebSocketContext";

export function RobotDetail() {
    const { t } = useTranslation();
    const { id } = useParams();
    const navigate = useNavigate();
    const location = useLocation();
    const { success, error } = useNotification();
    const { addListener } = useWebSocket();
    const [robot, setRobot] = useState<Robot | null>(null);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState<"overview" | "logs" | "terminal">("overview");
    const [demoMode, setDemoMode] = useState(false);

    // Command state
    const [repoUrl, setRepoUrl] = useState("");
    const [branch, setBranch] = useState("main");
    const [path, setPath] = useState("");
    const [cmdLoading, setCmdLoading] = useState(false);
    const [snapshotUrl, setSnapshotUrl] = useState<string | null>(null);

    // Tag state
    const [newTag, setNewTag] = useState("");
    const [isAddingTag, setIsAddingTag] = useState(false);

    // Rename state
    const [isEditingName, setIsEditingName] = useState(false);
    const [newName, setNewName] = useState("");

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

    useEffect(() => {
        return addListener((event: WSEvent) => {
            if (event.type === 'status_update' && robot && robot.agent_id === event.agent_id) {
                setRobot(prev => prev ? ({
                    ...prev,
                    status: event.data.status,
                    ip: event.data.ip,
                    last_seen: event.data.ts,
                }) : null);
            }
        });
    }, [addListener, robot]);

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

    const handleRename = async () => {
        if (!robot || !newName.trim()) return;
        try {
            const updated = await updateRobotName(robot.id, newName.trim());
            setRobot(updated);
            setIsEditingName(false);
            success(t("robotDetail.renamed") || "Robot renamed");
        } catch (err) {
            error(err instanceof Error ? err.message : "Failed to rename");
        }
    };

    const handleTestDrive = async () => {
        if (!robot) return;
        if (!confirm(t("robotDetail.testDriveWarning"))) return;
        await handleCommand("test_drive");
    };

    const handleCaptureImage = async () => {
        if (!robot) return;
        setCmdLoading(true);
        setSnapshotUrl(null);
        try {
            // The agent will upload to /api/robots/:id/upload
            // We need to tell the agent where to upload.
            // Since we are on the same network, we can use window.location.origin or a configured URL.
            // But the agent is on the robot, it needs to reach the controller.
            // The controller URL is not known by the frontend easily unless we assume relative to current page if agent can reach it.
            // Actually, the agent config has MQTT broker, but maybe not HTTP controller.
            // Let's assume the agent can reach the controller at the same IP as the browser is using, or we pass it.
            // For now, let's try passing the origin + /api/robots/:id/upload
            const uploadUrl = `${window.location.origin}/api/robots/${robot.id}/upload`;
            await sendCommand(robot.id, { type: "capture_image", data: { upload_url: uploadUrl } });

            // Poll for the image or just wait a bit and show it
            // Since the command is async (MQTT), we don't know when it's done.
            // We can just show a "Check back soon" or try to load the image with retries.
            success('Snapshot requested. It should appear below shortly.');

            // Simple retry mechanism to show the image
            let retries = 0;
            const checkImage = setInterval(() => {
                const url = `/snapshots/${robot.id}.jpg?t=${Date.now()}`;
                const img = new Image();
                img.onload = () => {
                    setSnapshotUrl(url);
                    clearInterval(checkImage);
                };
                img.src = url;
                retries++;
                if (retries > 10) clearInterval(checkImage);
            }, 1000);

        } catch (err) {
            error("Failed to request snapshot");
        } finally {
            setCmdLoading(false);
        }
    };

    const handleDelete = async () => {
        if (!robot) return;
        if (!confirm(t("robotDetail.deleteConfirm"))) return;
        try {
            await deleteRobot(robot.id);
            navigate("/robots");
        } catch (err) {
            console.error("Failed to delete robot", err);
            alert(t("robotDetail.deleteFailed"));
        }
    };

    if (loading) return <div className="p-8 text-gray-500">{t("robots.loading")}</div>;
    if (!robot) return <div className="p-8 text-red-500">Robot not found</div>;

    return (
        <div className="max-w-4xl mx-auto space-y-6">
            {/* Header */}
            <div className="flex items-center gap-4">
                <button onClick={() => navigate("/robots")} className="p-2 hover:bg-gray-100 rounded-lg">
                    <ArrowLeft size={20} />
                </button>
                <div className="flex-1">
                    <div className="flex flex-col md:flex-row md:items-center justify-between gap-2">
                        {isEditingName ? (
                            <div className="flex items-center gap-2">
                                <input
                                    autoFocus
                                    value={newName}
                                    onChange={e => setNewName(e.target.value)}
                                    onKeyDown={e => {
                                        if (e.key === 'Enter') handleRename();
                                        if (e.key === 'Escape') setIsEditingName(false);
                                    }}
                                    className="text-2xl font-bold text-gray-900 border border-gray-300 rounded px-2 py-1 w-64"
                                />
                                <button onClick={handleRename} className="p-1 text-green-600 hover:bg-green-50 rounded"><Save size={20} /></button>
                                <button onClick={() => setIsEditingName(false)} className="p-1 text-gray-500 hover:bg-gray-100 rounded"><X size={20} /></button>
                            </div>
                        ) : (
                            <div className="flex items-center gap-2 group">
                                <h1 className="text-2xl font-bold text-gray-900">{robot.name}</h1>
                                <button
                                    onClick={() => { setNewName(robot.name); setIsEditingName(true); }}
                                    className="opacity-0 group-hover:opacity-100 p-1 text-gray-400 hover:text-blue-600 transition-opacity"
                                >
                                    <Edit2 size={16} />
                                </button>
                            </div>
                        )}
                        <div className="flex items-center gap-2 flex-wrap">
                            <button
                                onClick={() => handleCommand("identify")}
                                className="p-2 text-gray-500 hover:text-yellow-600 hover:bg-yellow-50 rounded-lg transition-colors"
                                title="Identify Robot (Sound)"
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

                    {/* Test Drive & Snapshot */}
                    <div className="bg-white rounded-xl border border-gray-200 p-6">
                        <h3 className="font-semibold text-gray-900 mb-4 flex items-center gap-2">
                            <Lightbulb size={18} /> {t("robotDetail.testDriveSnapshot")}
                        </h3>
                        <div className="grid grid-cols-1 gap-4">
                            <button
                                onClick={handleTestDrive}
                                disabled={cmdLoading}
                                className="w-full bg-green-600 text-white px-4 py-2 rounded-lg hover:bg-green-700 transition-colors text-sm font-medium flex items-center justify-center gap-2"
                            >
                                <Play size={16} /> {t("robotDetail.startTestDrive")}
                            </button>
                            <button
                                onClick={handleCaptureImage}
                                disabled={cmdLoading}
                                className="w-full bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors text-sm font-medium flex items-center justify-center gap-2"
                            >
                                <Camera size={16} /> {t("robotDetail.captureImage")}
                            </button>
                        </div>
                    </div>

                    {/* Self Test Card */}
                    <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                        <div className="p-6 border-b border-gray-100">
                            <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
                                <Activity size={20} className="text-blue-500" />
                                {t("robotDetail.hardwareSelfTest")}
                            </h2>
                            <p className="text-sm text-gray-500 mt-1">
                                {t("robotDetail.hardwareSelfTestDesc")}
                            </p>
                        </div>
                        <div className="p-6 grid grid-cols-1 md:grid-cols-2 gap-6">
                            {/* Motor Test */}
                            <div className="space-y-4">
                                <div className="flex items-center justify-between">
                                    <h3 className="font-medium text-gray-900">{t("robotDetail.motorTest")}</h3>
                                    <span className="text-xs text-gray-500">{t("robotDetail.wiggleTest")}</span>
                                </div>
                                <p className="text-sm text-gray-500">
                                    {t("robotDetail.motorTestDesc")}
                                </p>
                                <button
                                    onClick={handleTestDrive}
                                    disabled={cmdLoading}
                                    className="w-full flex items-center justify-center gap-2 bg-gray-100 hover:bg-gray-200 text-gray-900 px-4 py-2 rounded-lg transition-colors disabled:opacity-50"
                                >
                                    {cmdLoading ? <RefreshCw className="animate-spin" size={18} /> : <Play size={18} />}
                                    {t("robotDetail.testMotors")}
                                </button>
                            </div>

                            {/* Camera Test */}
                            <div className="space-y-4">
                                <div className="flex items-center justify-between">
                                    <h3 className="font-medium text-gray-900">{t("robotDetail.cameraTest")}</h3>
                                    <span className="text-xs text-gray-500">{t("robotDetail.snapshot")}</span>
                                </div>
                                <p className="text-sm text-gray-500">
                                    {t("robotDetail.cameraTestDesc")}
                                </p>
                                <button
                                    onClick={handleCaptureImage}
                                    disabled={cmdLoading}
                                    className="w-full flex items-center justify-center gap-2 bg-gray-100 hover:bg-gray-200 text-gray-900 px-4 py-2 rounded-lg transition-colors disabled:opacity-50"
                                >
                                    {cmdLoading ? <RefreshCw className="animate-spin" size={18} /> : <Camera size={18} />}
                                    {t("robotDetail.testCamera")}
                                </button>
                                {snapshotUrl && (
                                    <div className="mt-4 rounded-lg overflow-hidden border border-gray-200">
                                        <img src={snapshotUrl} alt="Robot Snapshot" className="w-full h-auto" />
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>

                    {snapshotUrl && (
                        <div className="col-span-full">
                            <h4 className="font-semibold text-gray-900 mb-2">{t("robotDetail.snapshot")}</h4>
                            <div className="relative w-full h-0" style={{ paddingTop: "56.25%" }}>
                                <img
                                    src={snapshotUrl}
                                    alt="Robot Snapshot"
                                    className="absolute inset-0 w-full h-full object-cover rounded-xl border border-gray-200"
                                />
                            </div>
                        </div>
                    )}
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
