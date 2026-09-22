import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { getRobots, getInstallDefaults, startSemesterBatch, getScenarios } from "../api";
import { Robot, InstallConfig, Scenario } from "../types";
import { Check, RefreshCw, GitBranch, Trash2, AlertTriangle, ArrowRight, Clock, Terminal, XCircle, Activity, FileText, RotateCcw, Camera, FileCode, Package, ArrowUpCircle } from "lucide-react";

export function SemesterWizard() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const [robots, setRobots] = useState<Robot[]>([]);
    const [scenarios, setScenarios] = useState<Scenario[]>([]);
    const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
    const [loading, setLoading] = useState(true);
    const [executing, setExecuting] = useState(false);

    // Actions
    const [doResetLogs, setDoResetLogs] = useState(false);
    const [doUpdateRepo, setDoUpdateRepo] = useState(false);
    const [doApplyScenario, setDoApplyScenario] = useState(false);
    const [selectedScenarioIds, setSelectedScenarioIds] = useState<Set<number>>(new Set());
    const [doReinstall, setDoReinstall] = useState(false);
    const [doSelfTest, setDoSelfTest] = useState(false);
    const [doInstallCameraSupport, setDoInstallCameraSupport] = useState(false);
    const [doResetBashrc, setDoResetBashrc] = useState(false);
    const [turtlebot3Model, setTurtlebot3Model] = useState("waffle_pi");
    const [qtQpaPlatform, setQtQpaPlatform] = useState("");
    const [doSystemUpgrade, setDoSystemUpgrade] = useState(false);
    const [doInstallPackages, setDoInstallPackages] = useState(false);
    const [packagesText, setPackagesText] = useState("");
    const [doFactoryReset, setDoFactoryReset] = useState(false);
    const [repoUrl, setRepoUrl] = useState("https://github.com/openrobot-fleet/openrobotfleet-agent.git");

    // Global install defaults
    const [installDefaults, setInstallDefaults] = useState<InstallConfig | null>(null);
    const [isDemoMode, setIsDemoMode] = useState(false);

    useEffect(() => {
        // Progress lives on the Batches page now, so the wizard only loads the
        // form and never blocks on an in-flight run.
        Promise.all([getRobots(), getInstallDefaults(), getScenarios()])
            .then(([robotsData, defaultsData, scenariosData]) => {
                setRobots(robotsData);
                setScenarios(scenariosData);
                setSelectedIds(new Set(robotsData.map(r => r.id)));
                if (defaultsData.install_config) {
                    setInstallDefaults(defaultsData.install_config);
                }
                if (defaultsData.demo_mode) {
                    setIsDemoMode(true);
                }
            })
            .finally(() => setLoading(false));
    }, []);


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

    // Space-, comma- or newline-separated, as typed after `apt install -y`.
    const packages = packagesText.split(/[\s,]+/).filter(Boolean);
    const aptCommand = [
        "sudo apt update",
        doSystemUpgrade && "sudo apt upgrade -y",
        doInstallPackages && packages.length > 0 && `sudo apt install -y ${packages.join(" ")}`,
    ].filter(Boolean).join(" && ");

    const anyAction = doResetLogs || doUpdateRepo || doReinstall || doSelfTest || doApplyScenario || doInstallCameraSupport || doResetBashrc || doSystemUpgrade || doInstallPackages || doFactoryReset;
    const canExecute = !executing && selectedIds.size > 0 && anyAction && !(doInstallPackages && packages.length === 0);

    const handleExecute = async () => {
        if (!canExecute) return;

        setExecuting(true);
        try {
            await startSemesterBatch({
                robot_ids: Array.from(selectedIds),
                reinstall: doReinstall,
                reset_logs: doResetLogs,
                update_repo: doUpdateRepo,
                run_self_test: doSelfTest,
                install_camera_support: doInstallCameraSupport,
                reset_bashrc: doResetBashrc,
                turtlebot3_model: turtlebot3Model,
                qt_qpa_platform: qtQpaPlatform,
                system_upgrade: doSystemUpgrade,
                install_packages: doInstallPackages,
                packages: doInstallPackages ? packages : [],
                repo_config: {
                    repo: repoUrl,
                    branch: "main",
                    path: ""
                },
                apply_scenarios: doApplyScenario,
                scenario_ids: doApplyScenario ? Array.from(selectedScenarioIds) : [],
                factory_reset: doFactoryReset
            });
            navigate("/batches");
        } catch (err) {
            console.error("Failed to start batch", err);
            // request() already unwraps the server's {"error": ...} body into the
            // message, so show it -- a bare "failed to start" leaves no way to tell
            // a stuck batch apart from a rejected payload.
            const reason = err instanceof Error ? err.message : String(err);
            alert(t("semesterWizard.startError", { reason }));
        } finally {
            setExecuting(false);
        }
    };

    if (loading) return <div className="p-8">{t("common.loading")}</div>;

    return (
        <div className="max-w-4xl mx-auto space-y-8">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">{t("semesterWizard.title")}</h1>
                <p className="text-gray-500">{t("semesterWizard.subtitle")}</p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
                {/* Left Column: Robot Selection */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h2 className="text-lg font-semibold">{t("semesterWizard.selectRobots")}</h2>
                        <button
                            onClick={toggleSelectAll}
                            className="text-sm text-blue-600 hover:underline"
                        >
                            {selectedIds.size === robots.length ? t("semesterWizard.deselectAll") : t("semesterWizard.selectAll")}
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
                                        <span>{robot.ip || t("common.unknown")}</span>
                                        {robot.tags && robot.tags.map(t => (
                                            <span key={t} className="bg-gray-100 px-1 rounded">{t}</span>
                                        ))}
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                    <div className="text-sm text-gray-500">
                        {t("semesterWizard.robotsSelected", { count: selectedIds.size })}
                    </div>
                </div>

                {/* Right Column: Actions */}
                <div className="space-y-6">
                    <div>
                        <h2 className="text-lg font-semibold mb-4">{t("semesterWizard.configureActions")}</h2>
                        <div className="bg-white border border-gray-200 rounded-lg p-4 space-y-4">

                            {/* Reset Logs */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doResetLogs ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doResetLogs && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doResetLogs}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoResetLogs(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Trash2 size={16} /> {t("semesterWizard.resetLogs")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.resetLogsDesc")}</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Run Self Test */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doSelfTest ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doSelfTest && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doSelfTest}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoSelfTest(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Activity size={16} /> {t("semesterWizard.runSelfTest")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.runSelfTestDesc")}</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* System Update & Upgrade */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doSystemUpgrade ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doSystemUpgrade && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doSystemUpgrade}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoSystemUpgrade(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <ArrowUpCircle size={16} /> {t("semesterWizard.systemUpgrade")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.systemUpgradeDesc")}</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Install Packages */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doInstallPackages ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doInstallPackages && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doInstallPackages}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoInstallPackages(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div className="flex-1">
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Package size={16} /> {t("semesterWizard.installPackages")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.installPackagesDesc")}</p>
                                    {doInstallPackages && (
                                        <textarea
                                            value={packagesText}
                                            onChange={e => setPackagesText(e.target.value)}
                                            placeholder="ros-humble-image-transport vim"
                                            rows={2}
                                            className="mt-2 w-full px-3 py-2 border border-gray-300 rounded-md font-mono text-sm"
                                        />
                                    )}
                                </div>
                            </label>

                            {(doSystemUpgrade || doInstallPackages) && (
                                <div className="text-xs text-gray-500">
                                    {t("semesterWizard.aptCommandPreview")}
                                    <code className="block mt-1 px-2 py-1 bg-gray-50 border border-gray-200 rounded font-mono text-gray-700 break-all">{aptCommand}</code>
                                </div>
                            )}

                            <hr className="border-gray-100" />

                            {/* Install Camera Support */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doInstallCameraSupport ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doInstallCameraSupport && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doInstallCameraSupport}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoInstallCameraSupport(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Camera size={16} /> {t("semesterWizard.installCameraSupport")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.installCameraSupportDesc")}</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Reset .bashrc */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doResetBashrc ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doResetBashrc && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doResetBashrc}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoResetBashrc(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div className="flex-1">
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <FileCode size={16} /> {t("semesterWizard.resetBashrc")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.resetBashrcDesc")}</p>
                                    {doResetBashrc && (
                                        <div className="mt-2 flex items-center gap-2 text-sm">
                                            <span className="text-gray-700">TURTLEBOT3_MODEL</span>
                                            <select
                                                value={turtlebot3Model}
                                                onChange={e => setTurtlebot3Model(e.target.value)}
                                                className="px-2 py-1 border border-gray-300 rounded-md bg-white"
                                            >
                                                <option value="burger">burger</option>
                                                <option value="waffle">waffle</option>
                                                <option value="waffle_pi">waffle_pi</option>
                                            </select>
                                        </div>
                                    )}
                                    {doResetBashrc && (
                                        <div className="mt-2 flex items-center gap-2 text-sm">
                                            <span className="text-gray-700">QT_QPA_PLATFORM</span>
                                            <select
                                                value={qtQpaPlatform}
                                                onChange={e => setQtQpaPlatform(e.target.value)}
                                                className="px-2 py-1 border border-gray-300 rounded-md bg-white"
                                            >
                                                <option value="">{t("robotDetail.qtQpaUnset")}</option>
                                                <option value="xcb">xcb</option>
                                                <option value="wayland">wayland</option>
                                            </select>
                                        </div>
                                    )}
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Update Repo */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doUpdateRepo ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doUpdateRepo && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doUpdateRepo}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoUpdateRepo(e.target.checked);
                                            if (e.target.checked) {
                                                setDoApplyScenario(false);
                                                setDoFactoryReset(false);
                                            }
                                        }}
                                    />
                                </div>
                                <div className="flex-1">
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <GitBranch size={16} /> {t("semesterWizard.updateRepo")}
                                    </div>
                                    <p className="text-sm text-gray-500 mb-2">{t("semesterWizard.updateRepoDesc")}</p>
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

                            {/* Apply Scenario */}
                            <label className={`flex items-start gap-3 ${doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doApplyScenario ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doApplyScenario && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doApplyScenario}
                                        disabled={doFactoryReset}
                                        onChange={e => {
                                            setDoApplyScenario(e.target.checked);
                                            if (e.target.checked) {
                                                setDoUpdateRepo(false);
                                                setDoFactoryReset(false);
                                            }
                                        }}
                                    />
                                </div>
                                <div className="flex-1">
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <FileText size={16} /> {t("semesterWizard.applyScenario")}
                                    </div>
                                    <p className="text-sm text-gray-500 mb-2">{t("semesterWizard.applyScenarioDesc")}</p>
                                    {doApplyScenario && (
                                        <div className="border border-gray-200 rounded-md max-h-40 overflow-y-auto">
                                            {scenarios.map(s => (
                                                <label key={s.id} className="flex items-center p-2 hover:bg-gray-50 cursor-pointer">
                                                    <input
                                                        type="checkbox"
                                                        className="mr-2"
                                                        checked={selectedScenarioIds.has(s.id)}
                                                        onChange={e => {
                                                            const next = new Set(selectedScenarioIds);
                                                            if (e.target.checked) next.add(s.id);
                                                            else next.delete(s.id);
                                                            setSelectedScenarioIds(next);
                                                        }}
                                                    />
                                                    <span className="text-sm text-gray-700">{s.name}</span>
                                                </label>
                                            ))}
                                            {scenarios.length === 0 && (
                                                <div className="p-2 text-sm text-gray-400 italic">No scenarios available</div>
                                            )}
                                        </div>
                                    )}
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Reinstall Agent */}
                            <label className={`flex items-start gap-3 ${isDemoMode || doFactoryReset ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`}>
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doReinstall ? "bg-blue-600 border-blue-600 text-white" : "border-gray-300"}`}>
                                    {doReinstall && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doReinstall}
                                        disabled={isDemoMode || doFactoryReset}
                                        onChange={e => {
                                            setDoReinstall(e.target.checked);
                                            if (e.target.checked) setDoFactoryReset(false);
                                        }}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-gray-900 flex items-center gap-2">
                                        <Terminal size={16} /> {t("semesterWizard.reinstallAgent")}
                                        {isDemoMode && <span className="text-xs bg-gray-200 text-gray-600 px-2 py-0.5 rounded">{t("semesterWizard.disabledInDemoMode")}</span>}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.reinstallAgentDesc")}</p>
                                </div>
                            </label>

                            <hr className="border-gray-100" />

                            {/* Factory Reset -- destructive, mutually exclusive with everything else */}
                            <label className="flex items-start gap-3 cursor-pointer">
                                <div className={`mt-1 w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${doFactoryReset ? "bg-red-600 border-red-600 text-white" : "border-gray-300"}`}>
                                    {doFactoryReset && <Check size={14} />}
                                    <input
                                        type="checkbox"
                                        className="hidden"
                                        checked={doFactoryReset}
                                        onChange={e => {
                                            const checked = e.target.checked;
                                            setDoFactoryReset(checked);
                                            if (checked) {
                                                setDoResetLogs(false);
                                                setDoSelfTest(false);
                                                setDoUpdateRepo(false);
                                                setDoApplyScenario(false);
                                                setDoReinstall(false);
                                                setDoResetBashrc(false);
                                                setDoSystemUpgrade(false);
                                                setDoInstallPackages(false);
                                            }
                                        }}
                                    />
                                </div>
                                <div>
                                    <div className="font-medium text-red-700 flex items-center gap-2">
                                        <RotateCcw size={16} /> {t("semesterWizard.factoryReset")}
                                    </div>
                                    <p className="text-sm text-gray-500">{t("semesterWizard.factoryResetDesc")}</p>
                                </div>
                            </label>
                        </div>
                    </div>

                    <button
                        onClick={handleExecute}
                        disabled={!canExecute}
                        className={`w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 ${!canExecute
                            ? "bg-gray-100 text-gray-400 cursor-not-allowed"
                            : doFactoryReset
                                ? "bg-red-600 text-white hover:bg-red-700 shadow-sm"
                                : "bg-blue-600 text-white hover:bg-blue-700 shadow-sm"
                            }`}
                    >
                        {executing ? (
                            <>
                                <RefreshCw className="animate-spin" size={20} /> {t("semesterWizard.processing")}
                            </>
                        ) : (
                            <>
                                {t("semesterWizard.startReset")} <ArrowRight size={20} />
                            </>
                        )}
                    </button>
                </div>
            </div>
        </div>
    );
}
