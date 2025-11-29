import { useEffect, useState } from "react";
import { getRobots, getInstallDefaults, startSemesterBatch, getSemesterStatus } from "../api";
import { Robot, InstallConfig, SemesterStatus } from "../types";
import { Check, RefreshCw, GitBranch, Trash2, AlertTriangle, ArrowRight, Clock, Terminal, XCircle, Activity } from "lucide-react";

export function SemesterWizard() {
    const [robots, setRobots] = useState<Robot[]>([]);
    const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
    const [loading, setLoading] = useState(true);
    const [executing, setExecuting] = useState(false);
    const [batchStarted, setBatchStarted] = useState(false);
    const [status, setStatus] = useState<SemesterStatus | null>(null);

    // Actions
    const [doResetLogs, setDoResetLogs] = useState(false);
    const [doUpdateRepo, setDoUpdateRepo] = useState(false);
    const [doReinstall, setDoReinstall] = useState(false);
    const [doSelfTest, setDoSelfTest] = useState(false);
    const [repoUrl, setRepoUrl] = useState("https://github.com/turtlebot/turtlebot-agent.git");

    // Global install defaults
    const [installDefaults, setInstallDefaults] = useState<InstallConfig | null>(null);

    useEffect(() => {
        Promise.all([getRobots(), getInstallDefaults()])
            .then(([robotsData, defaultsData]) => {
                setRobots(robotsData);
                setSelectedIds(new Set(robotsData.map(r => r.id)));
                if (defaultsData.install_config) {
                    setInstallDefaults(defaultsData.install_config);
                }
            })
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        const poll = async () => {
            try {
                const s = await getSemesterStatus();
                if (s.active) {
                    setBatchStarted(true);
                    setStatus(s);
                } else if (batchStarted) {
                    setStatus(s);
                }
            } catch (e) {
                console.error(e);
            }
        };

        poll();
        const interval = setInterval(poll, 2000);
        return () => clearInterval(interval);
    }, [batchStarted]);

    const toggleSelect = (id: number) => {
        const next = new Set(selectedIds);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        setSelectedIds(next);
    };

    const toggleSelectAll = () => {
        if (selectedIds.size === robots.length) {
            setSelectedIds(new Set());
        } else {
            setSelectedIds(new Set(robots.map(r => r.id)));
        }
    };

    const handleExecute = async () => {
        if (selectedIds.size === 0) return;
        if (!doResetLogs && !doUpdateRepo && !doReinstall && !doSelfTest) return;

        setExecuting(true);
        try {
            await startSemesterBatch({
                robot_ids: Array.from(selectedIds),
                reinstall: doReinstall,
                reset_logs: doResetLogs,
                update_repo: doUpdateRepo,
                run_self_test: doSelfTest,
                repo_config: {
                    repo: repoUrl,
                    branch: "main",
                    path: ""
                }
            });
            setBatchStarted(true);
        } catch (err) {
            console.error("Failed to start batch", err);
            alert("Failed to start batch operation");
        } finally {
            setExecuting(false);
        }
    };

    if (loading) return <div className="p-8">Loading...</div>;

    if (batchStarted && !status) {
        return (
            <div className="flex flex-col items-center justify-center min-h-[400px]">
                <RefreshCw className="animate-spin text-blue-600 mb-4" size={48} />
                <h2 className="text-xl font-semibold text-gray-900">Initializing Batch Operation...</h2>
            </div>
        );
    }

    if (batchStarted && status) {
        return (
            <div className="max-w-4xl mx-auto space-y-8">
                <div className="text-center py-8">
                    <div className={`w-16 h-16 rounded-full flex items-center justify-center mx-auto mb-4 ${status.active ? 'bg-blue-100 text-blue-600' : 'bg-green-100 text-green-600'}`}>
                        {status.active ? <RefreshCw className="animate-spin" size={32} /> : <Check size={32} />}
                    </div>
                    <h2 className="text-2xl font-bold text-gray-900 mb-2">
                        {status.active ? "Batch Operation In Progress" : "Batch Operation Complete"}
                    </h2>
                    <p className="text-gray-500">
                        Processed {status.completed} of {status.total} robots
                    </p>
                </div>

                <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
                    {robots.filter(r => status.robots[r.id.toString()]).map(robot => (
                        <div key={robot.id} className="flex items-center p-4 border-b border-gray-100 last:border-0">
                            <div className="flex-1">
                                <div className="font-medium text-gray-900">{robot.name}</div>
                                <div className="text-sm text-gray-500">{robot.ip}</div>
                            </div>
                            <div className="flex items-center gap-3">
                                <span className="text-sm text-gray-600 capitalize">
                                    {status.robots[robot.id.toString()]?.replace(/_/g, ' ')}
                                </span>
                                {status.robots[robot.id.toString()] === 'success' && <Check className="text-green-500" size={20} />}
                                {status.robots[robot.id.toString()] === 'error' && <XCircle className="text-red-500" size={20} />}
                                {status.robots[robot.id.toString()] === 'processing' && <RefreshCw className="text-blue-500 animate-spin" size={20} />}
                                {status.robots[robot.id.toString()] === 'pending' && <Clock className="text-gray-400" size={20} />}
                            </div>
                            {status.errors[robot.id.toString()] && (
                                <div className="ml-4 text-sm text-red-600 max-w-xs truncate" title={status.errors[robot.id.toString()]}>
                                    {status.errors[robot.id.toString()]}
                                </div>
                            )}
                        </div>
                    ))}
                </div>

                {!status.active && (
                    <div className="text-center">
                        <button
                            onClick={() => window.location.reload()}
                            className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 transition-colors"
                        >
                            Start Another Batch
                        </button>
                    </div>
                )}
            </div>
        );
    }

    return (
        <div className="max-w-4xl mx-auto space-y-8">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">Semester Start Wizard</h1>
                <p className="text-gray-500">Prepare your fleet for a new semester by resetting logs and updating code.</p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
                {/* Left Column: Robot Selection */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h2 className="text-lg font-semibold">1. Select Robots</h2>
                        <button
                            onClick={toggleSelectAll}
                            className="text-sm text-blue-600 hover:underline"
                        >
                            {selectedIds.size === robots.length ? "Deselect All" : "Select All"}
                        </button>
                    </div>

                    <div className="bg-white border border-gray-200 rounded-lg overflow-hidden max-h-[500px] overflow-y-auto">
                        {robots.map(robot => (
                            <div
                                key={robot.id}
                                className={`flex items-center p-3 border-b border-gray-100 last:border-0 cursor-pointer hover:bg-gray-50 ${selectedIds.has(robot.id) ? "bg-blue-50" : ""}`}
                                onClick={() => toggleSelect(robot.id)}
                            >
                                <div className={`w-5 h-5 rounded border flex items-center justify-center mr-3 ${selectedIds.has(robot.id) ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {selectedIds.has(robot.id) && <Check size={14} />}
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900">{robot.name}</div>
                                    <div className="text-xs text-gray-500 flex gap-2">
                                        <span>{robot.ip || "No IP"}</span>
                                        {robot.tags && robot.tags.map(t => (
                                            <span key={t} className="bg-gray-100 px-1 rounded">{t}</span>
                                        ))}
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                    <div className="text-sm text-gray-500">
                        {selectedIds.size} robots selected
                    </div>
                </div>

                {/* Right Column: Actions */}
                <div className="space-y-6">
                    <div>
                        <h2 className="text-lg font-semibold mb-4">2. Configure Actions</h2>
                        <div className="bg-white border border-gray-200 rounded-lg p-4 space-y-4">

                            {/* Reset Logs */}
                            <label className="flex items-start gap-3 cursor-pointer">
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doResetLogs ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doResetLogs && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doResetLogs}
                                        onChange={e => setDoResetLogs(e.target.checked)}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Trash2 size={16} /> Reset Logs
                                    </div>
                                    <p className="text-sm text-gray-500">Clear all application logs on the robot to start fresh.</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Run Self Test */}
                            <label className="flex items-start gap-3 cursor-pointer">
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doSelfTest ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doSelfTest && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doSelfTest}
                                        onChange={e => setDoSelfTest(e.target.checked)}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Activity size={16} /> Run Self Test
                                    </div>
                                    <p className="text-sm text-gray-500">Verify motors and camera functionality.</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Update Repo */}
                            <label className="flex items-start gap-3 cursor-pointer">
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doUpdateRepo ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doUpdateRepo && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doUpdateRepo}
                                        onChange={e => setDoUpdateRepo(e.target.checked)}
                                    />
                                </div>
                                <div className="flex-1">
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <GitBranch size={16} /> Update Repository
                                    </div>
                                    <p className="text-sm text-gray-500 mb-2">Pull the latest code from a remote git repository.</p>
                                    {doUpdateRepo && (
                                        <input
                                            type="text"
                                            value={repoUrl}
                                            onChange={e => setRepoUrl(e.target.value)}
                                            className="w-full border border-gray-300 rounded px-3 py-2 text-sm"
                                            placeholder="https://github.com/..."
                                        />
                                    )}
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Reinstall Agent */}
                            <label className="flex items-start gap-3 cursor-pointer">
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doReinstall ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doReinstall && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doReinstall}
                                        onChange={e => setDoReinstall(e.target.checked)}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Terminal size={16} /> Reinstall Agent
                                    </div>
                                    <p className="text-sm text-gray-500">Re-run the installation script via SSH using stored credentials.</p>
                                </div>
                            </label>
                        </div>
                    </div>

                    <button
                        onClick={handleExecute}
                        disabled={executing || selectedIds.size === 0 || (!doResetLogs && !doUpdateRepo && !doReinstall)}
                        className={`w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 ${executing || selectedIds.size === 0 || (!doResetLogs && !doUpdateRepo && !doReinstall)
                            ? "bg-gray-100 text-gray-400 cursor-not-allowed"
                            : "bg-blue-600 text-white hover:bg-blue-700 shadow-sm"
                            }`}
                    >
                        {executing ? (
                            <>
                                <RefreshCw className="animate-spin" size={20} /> Processing...
                            </>
                        ) : (
                            <>
                                Start Semester Reset <ArrowRight size={20} />
                            </>
                        )}
                    </button>
                </div>
            </div>
        </div>
    );
}
