import { ChangeEvent, FormEvent, useCallback, useEffect, useState } from "react";
import {
    applyScenario,
    getInstallDefaults,
    getRobots,
    getScenarios,
    installAgent,
    saveInstallConfig,
    sendCommand,
    updateInstallDefaults,
} from "./api";
import { InstallAgentPayload, InstallConfig, Robot, Scenario } from "./types";

type Tab = "robots" | "scenarios" | "install";

const styles = {
    app: {
        fontFamily: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
        background: "#f7f7f7",
        minHeight: "100vh",
        padding: "1.5rem",
    },
    card: {
        background: "#fff",
        border: "1px solid #ddd",
        borderRadius: "8px",
        padding: "1rem",
    },
    tabs: {
        display: "flex" as const,
        gap: "0.5rem",
        marginBottom: "1rem",
    },
    tabButton: (active: boolean) => ({
        border: "1px solid #ccc",
        background: active ? "#2d6cdf" : "#fff",
        color: active ? "#fff" : "#333",
        padding: "0.5rem 1rem",
        borderRadius: "999px",
        cursor: "pointer",
    }),
    split: {
        display: "flex" as const,
        gap: "1rem",
        alignItems: "flex-start",
        flexWrap: "wrap" as const,
    },
    column: {
        flex: "1 1 300px",
    },
    label: {
        display: "flex",
        flexDirection: "column" as const,
        gap: "0.25rem",
        fontSize: "0.9rem",
        marginBottom: "0.75rem",
    },
    input: {
        padding: "0.5rem",
        borderRadius: "4px",
        border: "1px solid #ccc",
        fontSize: "1rem",
    },
    textarea: {
        padding: "0.5rem",
        borderRadius: "4px",
        border: "1px solid #ccc",
        fontSize: "1rem",
        minHeight: "120px",
        fontFamily: "monospace",
    },
    buttonRow: {
        display: "flex" as const,
        gap: "0.5rem",
        flexWrap: "wrap" as const,
    },
};

export function App() {
    const [activeTab, setActiveTab] = useState<Tab>("robots");
    const [selectedRobot, setSelectedRobot] = useState<Robot | null>(null);

    const handleRobotListChange = useCallback(
        (robots: Robot[]) => {
            if (!selectedRobot) return;
            const updated = robots.find((r) => r.id === selectedRobot.id);
            if (updated) {
                setSelectedRobot(updated);
            }
        },
        [selectedRobot],
    );

    return (
        <main style={styles.app}>
            <div style={{ maxWidth: 1200, margin: "0 auto" }}>
                <h1>Turtlebot Fleet Dashboard</h1>
                <div style={styles.tabs}>
                    {[
                        { id: "robots", label: "Robots" },
                        { id: "scenarios", label: "Scenarios" },
                        { id: "install", label: "Install Agent" },
                        { id: "settings", label: "Settings" },
                    ].map((tab) => (
                        <button
                            key={tab.id}
                            style={styles.tabButton(activeTab === tab.id)}
                            onClick={() => setActiveTab(tab.id as Tab)}
                        >
                            {tab.label}
                        </button>
                    ))}
                </div>
                {activeTab === "robots" && (
                    <div style={styles.split}>
                        <div style={{ ...styles.card, ...styles.column }}>
                            <RobotTable
                                onSelect={setSelectedRobot}
                                selectedRobotId={selectedRobot?.id}
                                onRobotsChange={handleRobotListChange}
                            />
                        </div>
                        {selectedRobot && (
                            <div style={{ ...styles.card, ...styles.column }}>
                                <RobotDetail robot={selectedRobot} />
                            </div>
                        )}
                    </div>
                )}
                {activeTab === "scenarios" && <ScenarioDeployer />}
                {activeTab === "install" && (
                    <div style={{ ...styles.card, maxWidth: 600 }}>
                        <InstallAgentForm />
                    </div>
                )}
                {activeTab === "settings" && (
                    <div style={{ ...styles.card, maxWidth: 600 }}>
                        <SettingsPage />
                    </div>
                )}
            </div>
        </main>
    );
}

type RobotTableProps = {
    onSelect?: (robot: Robot) => void;
    selectedRobotId?: number;
    onRobotsChange?: (robots: Robot[]) => void;
};

function RobotTable({ onSelect, selectedRobotId, onRobotsChange }: RobotTableProps) {
    const [robots, setRobots] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        let mounted = true;
        const load = async () => {
            try {
                const data = await getRobots();
                if (!mounted) return;
                setRobots(data);
                onRobotsChange?.(data);
            } catch (err) {
                if (!mounted) return;
                setError(err instanceof Error ? err.message : "Failed to load robots");
            } finally {
                if (mounted) {
                    setLoading(false);
                }
            }
        };
        load();
        return () => {
            mounted = false;
        };
    }, [onRobotsChange]);

    if (loading) return <p>Loading robots…</p>;
    if (error) return <p style={{ color: "red" }}>{error}</p>;

    return (
        <div>
            <h2>Robots</h2>
            <table style={{ width: "100%", borderCollapse: "collapse" }}>
                <thead>
                    <tr>
                        <th style={{ textAlign: "left", padding: "0.5rem" }}>Name</th>
                        <th style={{ textAlign: "left", padding: "0.5rem" }}>Status</th>
                        <th style={{ textAlign: "left", padding: "0.5rem" }}>Last Seen</th>
                        <th style={{ textAlign: "left", padding: "0.5rem" }}>IP</th>
                        <th style={{ textAlign: "left", padding: "0.5rem" }}>Scenario</th>
                    </tr>
                </thead>
                <tbody>
                    {robots.map((robot) => (
                        <tr
                            key={robot.id}
                            onClick={() => onSelect?.(robot)}
                            style={{
                                cursor: "pointer",
                                backgroundColor: robot.id === selectedRobotId ? "#e6f0ff" : "transparent",
                            }}
                        >
                            <td style={{ padding: "0.5rem", borderTop: "1px solid #eee" }}>{robot.name}</td>
                            <td style={{ padding: "0.5rem", borderTop: "1px solid #eee" }}>{robot.status ?? "unknown"}</td>
                            <td style={{ padding: "0.5rem", borderTop: "1px solid #eee" }}>{robot.last_seen ?? "—"}</td>
                            <td style={{ padding: "0.5rem", borderTop: "1px solid #eee" }}>{robot.ip ?? "—"}</td>
                            <td style={{ padding: "0.5rem", borderTop: "1px solid #eee" }}>{robot.last_scenario?.name ?? "—"}</td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

type RobotDetailProps = {
    robot: Robot;
};

function RobotDetail({ robot }: RobotDetailProps) {
    const [repo, setRepo] = useState("");
    const [branch, setBranch] = useState("");
    const [path, setPath] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [message, setMessage] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        setRepo("");
        setBranch("");
        setPath("");
        setMessage(null);
        setError(null);
    }, [robot.id]);

    const handleUpdateSubmit = async (event: FormEvent) => {
        event.preventDefault();
        setSubmitting(true);
        setMessage(null);
        setError(null);
        try {
            await sendCommand(robot.id, {
                type: "update_repo",
                data: { repo, branch, path },
            });
            setMessage("Update command queued");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to send command");
        } finally {
            setSubmitting(false);
        }
    };

    const handleQuickCommand = async (type: string) => {
        setSubmitting(true);
        setMessage(null);
        setError(null);
        try {
            await sendCommand(robot.id, { type, data: {} });
            setMessage(`${type} command sent`);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to send command");
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div>
            <h2>Robot Detail</h2>
            <p><strong>Name:</strong> {robot.name}</p>
            <p><strong>Status:</strong> {robot.status ?? "unknown"}</p>
            <p><strong>Last Seen:</strong> {robot.last_seen ?? "—"}</p>
            <p><strong>IP:</strong> {robot.ip ?? "—"}</p>
            <p><strong>Scenario:</strong> {robot.last_scenario?.name ?? "—"}</p>

            <h3>Update Repo</h3>
            <form onSubmit={handleUpdateSubmit}>
                <label style={styles.label}>
                    Repo URL
                    <input
                        style={styles.input}
                        value={repo}
                        onChange={(e) => setRepo(e.target.value)}
                        placeholder="https://github.com/..."
                        required
                    />
                </label>
                <label style={styles.label}>
                    Branch
                    <input
                        style={styles.input}
                        value={branch}
                        onChange={(e) => setBranch(e.target.value)}
                        placeholder="main"
                        required
                    />
                </label>
                <label style={styles.label}>
                    Path
                    <input
                        style={styles.input}
                        value={path}
                        onChange={(e) => setPath(e.target.value)}
                        placeholder="/home/ubuntu/app"
                        required
                    />
                </label>
                <div style={styles.buttonRow}>
                    <button type="submit" disabled={submitting}>
                        Send Update
                    </button>
                    <button type="button" onClick={() => handleQuickCommand("reset_logs")} disabled={submitting}>
                        Reset Logs
                    </button>
                    <button type="button" onClick={() => handleQuickCommand("restart_ros")} disabled={submitting}>
                        Restart ROS
                    </button>
                </div>
                {message && <p style={{ color: "green" }}>{message}</p>}
                {error && <p style={{ color: "red" }}>{error}</p>}
            </form>
        </div>
    );
}

function ScenarioDeployer() {
    const [scenarios, setScenarios] = useState<Scenario[]>([]);
    const [scenariosLoading, setScenariosLoading] = useState(true);
    const [scenariosError, setScenariosError] = useState<string | null>(null);
    const [selectedScenarioId, setSelectedScenarioId] = useState<number | null>(null);
    const [robots, setRobots] = useState<Robot[]>([]);
    const [robotsLoading, setRobotsLoading] = useState(true);
    const [robotsError, setRobotsError] = useState<string | null>(null);
    const [robotFilter, setRobotFilter] = useState("");
    const [selectedRobotIds, setSelectedRobotIds] = useState<number[]>([]);
    const [status, setStatus] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [applying, setApplying] = useState(false);

    const loadScenarios = useCallback(async () => {
        setScenariosLoading(true);
        setScenariosError(null);
        try {
            const data = await getScenarios();
            setScenarios(data);
            if (!data.length) {
                setSelectedScenarioId(null);
            } else if (!selectedScenarioId || !data.some((scenario: Scenario) => scenario.id === selectedScenarioId)) {
                setSelectedScenarioId(data[0].id);
            }
        } catch (err) {
            setScenariosError(err instanceof Error ? err.message : "Failed to load scenarios");
        } finally {
            setScenariosLoading(false);
        }
    }, [selectedScenarioId]);

    useEffect(() => {
        void loadScenarios();
    }, [loadScenarios]);

    const loadRobots = useCallback(async () => {
        setRobotsLoading(true);
        setRobotsError(null);
        try {
            const data = await getRobots();
            setRobots(data);
        } catch (err) {
            setRobotsError(err instanceof Error ? err.message : "Failed to load robots");
        } finally {
            setRobotsLoading(false);
        }
    }, []);

    useEffect(() => {
        void loadRobots();
    }, [loadRobots]);

    const selectedScenario = selectedScenarioId
        ? scenarios.find((scenario: Scenario) => scenario.id === selectedScenarioId) ?? null
        : null;

    const handleRobotFilterChange = (event: ChangeEvent<HTMLInputElement>) => {
        setRobotFilter(event.target.value);
    };

    const filteredRobots = robots.filter((robot: Robot) => {
        if (!robotFilter.trim()) return true;
        const needle = robotFilter.trim().toLowerCase();
        return (
            robot.name.toLowerCase().includes(needle) ||
            (robot.status ?? "").toLowerCase().includes(needle) ||
            (robot.ip ?? "").toLowerCase().includes(needle)
        );
    });

    const toggleRobotSelection = (robotId: number) => {
        setSelectedRobotIds((prev: number[]) =>
            prev.includes(robotId) ? prev.filter((id: number) => id !== robotId) : [...prev, robotId],
        );
    };

    const selectAllFiltered = () => {
        setSelectedRobotIds((prev: number[]) => {
            const combined = new Set<number>(prev);
            filteredRobots.forEach((robot: Robot) => combined.add(robot.id));
            return Array.from(combined);
        });
    };

    const clearRobotSelection = () => setSelectedRobotIds([]);

    const handleApplyScenario = async () => {
        if (!selectedScenario) {
            setError("Select a scenario first");
            return;
        }
        if (!selectedRobotIds.length) {
            setError("Select at least one robot");
            return;
        }
        setApplying(true);
        setStatus(null);
        setError(null);
        try {
            const result = await applyScenario(selectedScenario.id, { robot_ids: selectedRobotIds });
            const count = result.jobs.length;
            setStatus(`Scenario "${selectedScenario.name}" queued for ${count} robot${count === 1 ? "" : "s"}.`);
            await loadRobots();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to deploy scenario");
        } finally {
            setApplying(false);
        }
    };

    const handleScenarioSelect = (scenarioId: number) => {
        setSelectedScenarioId(scenarioId);
        setStatus(null);
        setError(null);
    };

    return (
        <div style={{ display: "flex", gap: "1rem", flexWrap: "wrap" }}>
            <div style={{ ...styles.card, flex: "0 0 260px" }}>
                <div
                    style={{
                        display: "flex",
                        justifyContent: "space-between",
                        alignItems: "center",
                        marginBottom: "0.5rem",
                        gap: "0.5rem",
                    }}
                >
                    <h2 style={{ margin: 0 }}>Scenario Library</h2>
                    <button
                        type="button"
                        onClick={() => {
                            void loadScenarios();
                        }}
                        disabled={scenariosLoading}
                    >
                        Refresh
                    </button>
                </div>
                {scenariosLoading ? (
                    <p>Loading scenarios…</p>
                ) : scenariosError ? (
                    <p style={{ color: "red" }}>{scenariosError}</p>
                ) : scenarios.length ? (
                    <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
                        {scenarios.map((scenario: Scenario) => (
                            <li key={scenario.id} style={{ marginBottom: "0.35rem" }}>
                                <button
                                    type="button"
                                    style={{
                                        width: "100%",
                                        textAlign: "left",
                                        padding: "0.5rem 0.75rem",
                                        borderRadius: "6px",
                                        border: "1px solid #ccc",
                                        backgroundColor:
                                            selectedScenarioId === scenario.id ? "#e6f0ff" : "#fff",
                                        fontWeight: selectedScenarioId === scenario.id ? 600 : 500,
                                    }}
                                    onClick={() => handleScenarioSelect(scenario.id)}
                                >
                                    <span>{scenario.name}</span>
                                    {scenario.description && (
                                        <span
                                            style={{
                                                display: "block",
                                                fontSize: "0.8rem",
                                                color: "#555",
                                                marginTop: "0.15rem",
                                            }}
                                        >
                                            {scenario.description}
                                        </span>
                                    )}
                                </button>
                            </li>
                        ))}
                    </ul>
                ) : (
                    <p>No scenarios yet.</p>
                )}
            </div>
            <div style={{ ...styles.card, flex: "1 1 420px", minWidth: 320 }}>
                {selectedScenario ? (
                    <>
                        <div
                            style={{
                                display: "flex",
                                justifyContent: "space-between",
                                flexWrap: "wrap",
                                gap: "0.5rem",
                                alignItems: "center",
                            }}
                        >
                            <div>
                                <h2 style={{ marginBottom: "0.25rem" }}>{selectedScenario.name}</h2>
                                {selectedScenario.description && (
                                    <p style={{ marginTop: 0 }}>{selectedScenario.description}</p>
                                )}
                            </div>
                            <button
                                type="button"
                                onClick={() => {
                                    void loadScenarios();
                                }}
                                style={{ height: "fit-content" }}
                            >
                                Refresh Scenarios
                            </button>
                        </div>
                        <h3>Configuration</h3>
                        <pre
                            style={{
                                background: "#f5f5f5",
                                padding: "0.75rem",
                                borderRadius: "6px",
                                fontSize: "0.85rem",
                                whiteSpace: "pre-wrap",
                                maxHeight: "260px",
                                overflow: "auto",
                                border: "1px solid #e0e0e0",
                            }}
                        >
                            {selectedScenario.config_yaml}
                        </pre>
                        <h3 style={{ marginTop: "1.25rem" }}>Target Robots</h3>
                        {robotsLoading ? (
                            <p>Loading robots…</p>
                        ) : robotsError ? (
                            <p style={{ color: "red" }}>{robotsError}</p>
                        ) : (
                            <>
                                <input
                                    style={{ ...styles.input, marginBottom: "0.5rem" }}
                                    placeholder="Filter by name, status, or IP"
                                    value={robotFilter}
                                    onChange={handleRobotFilterChange}
                                    type="search"
                                />
                                <div
                                    style={{
                                        border: "1px solid #ddd",
                                        borderRadius: "6px",
                                        maxHeight: "260px",
                                        overflowY: "auto",
                                        padding: "0.5rem",
                                    }}
                                >
                                    {filteredRobots.length ? (
                                        filteredRobots.map((robot: Robot) => (
                                            <label
                                                key={robot.id}
                                                style={{
                                                    display: "flex",
                                                    alignItems: "center",
                                                    gap: "0.5rem",
                                                    padding: "0.25rem 0",
                                                    borderBottom: "1px solid #f0f0f0",
                                                }}
                                            >
                                                <input
                                                    type="checkbox"
                                                    checked={selectedRobotIds.includes(robot.id)}
                                                    onChange={() => toggleRobotSelection(robot.id)}
                                                />
                                                <div>
                                                    <div style={{ fontWeight: 600 }}>{robot.name}</div>
                                                    <div style={{ fontSize: "0.85rem", color: "#555" }}>
                                                        {robot.status ?? "status unknown"} · {robot.ip ?? "no IP"}
                                                    </div>
                                                </div>
                                            </label>
                                        ))
                                    ) : (
                                        <p style={{ margin: 0 }}>No robots match that filter.</p>
                                    )}
                                </div>
                                <div style={{ ...styles.buttonRow, marginTop: "0.75rem" }}>
                                    <button type="button" onClick={selectAllFiltered} disabled={!filteredRobots.length}>
                                        Select All Shown
                                    </button>
                                    <button type="button" onClick={clearRobotSelection} disabled={!selectedRobotIds.length}>
                                        Clear Selection
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => {
                                            void loadRobots();
                                        }}
                                    >
                                        Refresh Robots
                                    </button>
                                </div>
                                <button
                                    type="button"
                                    onClick={handleApplyScenario}
                                    disabled={!selectedRobotIds.length || applying}
                                    style={{ marginTop: "1rem" }}
                                >
                                    {applying ? "Deploying\u2026" : "Deploy Scenario"}
                                </button>
                            </>
                        )}
                    </>
                ) : scenariosLoading ? (
                    <p>Loading scenarios…</p>
                ) : (
                    <p>Select a scenario to start deploying.</p>
                )}
                {status && <p style={{ color: "green" }}>{status}</p>}
                {error && <p style={{ color: "red" }}>{error}</p>}
            </div>
        </div>
    );
}

function InstallAgentForm() {
    const [form, setForm] = useState<InstallAgentPayload>({
        name: "",
        address: "",
        user: "",
        ssh_key: "",
    });
    const [status, setStatus] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [submitting, setSubmitting] = useState(false);
    const [robots, setRobots] = useState<Robot[]>([]);
    const [selectedRobotId, setSelectedRobotId] = useState<string>("");
    const [defaults, setDefaults] = useState<InstallConfig | null>(null);
    const [loadingRobots, setLoadingRobots] = useState(true);

    useEffect(() => {
        let mounted = true;
        const loadDefaults = async () => {
            try {
                const resp = await getInstallDefaults();
                if (!mounted) return;
                setDefaults(resp.install_config ?? null);
            } catch (err) {
                console.error("failed to load install defaults", err);
            }
        };
        const loadRobots = async () => {
            setLoadingRobots(true);
            try {
                const data = await getRobots();
                if (!mounted) return;
                setRobots(data);
            } catch (err) {
                if (!mounted) return;
                setError(err instanceof Error ? err.message : "Failed to load robots");
            } finally {
                if (mounted) {
                    setLoadingRobots(false);
                }
            }
        };
        loadDefaults();
        loadRobots();
        return () => {
            mounted = false;
        };
    }, []);

    useEffect(() => {
        if (!defaults) return;
        setForm((prev) => ({
            ...prev,
            address: prev.address || defaults.address || "",
            user: prev.user || defaults.user || "",
            ssh_key: prev.ssh_key || defaults.ssh_key || "",
        }));
    }, [defaults]);

    const refreshRobots = useCallback(async () => {
        try {
            const data = await getRobots();
            setRobots(data);
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load robots");
        }
    }, []);

    const selectedRobot = selectedRobotId ? robots.find((r) => r.id === Number(selectedRobotId)) : undefined;

    const handleSubmit = async (event: FormEvent) => {
        event.preventDefault();
        setStatus(null);
        setError(null);
        setSubmitting(true);
        try {
            await installAgent(form);
            setStatus("Install request submitted");
            setForm({
                name: "",
                address: defaults?.address ?? "",
                user: defaults?.user ?? "",
                ssh_key: defaults?.ssh_key ?? "",
            });
            await refreshRobots();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to install agent");
        } finally {
            setSubmitting(false);
        }
    };

    const requireRobotSelection = (): Robot | null => {
        if (!selectedRobot) {
            setError("Select a robot first");
            return null;
        }
        return selectedRobot;
    };

    const requireInstallFields = () => {
        if (!form.address.trim() || !form.user.trim() || !form.ssh_key.trim()) {
            setError("Address, user, and SSH key are required");
            return false;
        }
        return true;
    };

    const handleLoadRobotConfig = () => {
        setStatus(null);
        setError(null);
        const robot = requireRobotSelection();
        if (!robot) return;
        if (!robot.install_config) {
            setError("No saved credentials for this robot yet");
            return;
        }
        setForm({
            name: robot.name,
            address: robot.install_config.address || robot.ip || "",
            user: robot.install_config.user,
            ssh_key: robot.install_config.ssh_key || defaults?.ssh_key || "",
        });
        setStatus(`Loaded saved settings for ${robot.name}`);
    };

    const handleSaveRobotConfig = async () => {
        setStatus(null);
        setError(null);
        const robot = requireRobotSelection();
        if (!robot) return;
        if (!requireInstallFields()) return;
        try {
            await saveInstallConfig(robot.id, {
                address: form.address,
                user: form.user,
                ssh_key: form.ssh_key,
            });
            setStatus(`Saved settings for ${robot.name}`);
            await refreshRobots();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to save robot settings");
        }
    };

    const handleReinstall = async () => {
        setStatus(null);
        setError(null);
        const robot = requireRobotSelection();
        if (!robot) return;
        if (!requireInstallFields()) return;
        setSubmitting(true);
        try {
            await installAgent({ ...form, name: robot.name });
            setStatus(`Reinstall requested for ${robot.name}`);
            await refreshRobots();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to reinstall agent");
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div>
            <form onSubmit={handleSubmit}>
                <h2>Install Agent</h2>
                <label style={styles.label}>
                    Robot Name
                    <input
                        style={styles.input}
                        value={form.name}
                        onChange={(e) => setForm((prev) => ({ ...prev, name: e.target.value }))}
                        required
                    />
                </label>
                <label style={styles.label}>
                    Address
                    <input
                        style={styles.input}
                        value={form.address}
                        onChange={(e) => setForm((prev) => ({ ...prev, address: e.target.value }))}
                        required
                    />
                </label>
                <label style={styles.label}>
                    User
                    <input
                        style={styles.input}
                        value={form.user}
                        onChange={(e) => setForm((prev) => ({ ...prev, user: e.target.value }))}
                        required
                    />
                </label>
                <label style={styles.label}>
                    SSH Key
                    <textarea
                        style={styles.textarea}
                        value={form.ssh_key}
                        onChange={(e) => setForm((prev) => ({ ...prev, ssh_key: e.target.value }))}
                        required
                    />
                </label>
                <button type="submit" disabled={submitting}>
                    Submit
                </button>
            </form>
            <div style={{ marginTop: "1.5rem" }}>
                <h3>Reinstall Existing Robot</h3>
                {loadingRobots ? (
                    <p>Loading robots…</p>
                ) : (
                    <label style={{ ...styles.label, maxWidth: "100%" }}>
                        Target Robot
                        <select
                            style={styles.input}
                            value={selectedRobotId}
                            onChange={(e) => setSelectedRobotId(e.target.value)}
                        >
                            <option value="">Select robot…</option>
                            {robots.map((robot) => (
                                <option key={robot.id} value={robot.id.toString()}>
                                    {robot.name}
                                </option>
                            ))}
                        </select>
                    </label>
                )}
                <div style={styles.buttonRow}>
                    <button type="button" onClick={handleLoadRobotConfig} disabled={!selectedRobotId}>
                        Load Saved Settings
                    </button>
                    <button type="button" onClick={handleSaveRobotConfig} disabled={!selectedRobotId || submitting}>
                        Save Settings to Robot
                    </button>
                    <button type="button" onClick={handleReinstall} disabled={!selectedRobotId || submitting}>
                        Reinstall Agent
                    </button>
                </div>
            </div>
            {status && <p style={{ color: "green" }}>{status}</p>}
            {error && <p style={{ color: "red" }}>{error}</p>}
        </div>
    );
}

function SettingsPage() {
    const [form, setForm] = useState<InstallConfig>({ address: "", user: "", ssh_key: "" });
    const [loading, setLoading] = useState(true);
    const [status, setStatus] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        let mounted = true;
        const load = async () => {
            setError(null);
            try {
                const resp = await getInstallDefaults();
                if (!mounted) return;
                if (resp.install_config) {
                    setForm(resp.install_config);
                }
            } catch (err) {
                if (!mounted) return;
                setError(err instanceof Error ? err.message : "Failed to load defaults");
            } finally {
                if (mounted) {
                    setLoading(false);
                }
            }
        };
        load();
        return () => {
            mounted = false;
        };
    }, []);

    const handleSubmit = async (event: FormEvent) => {
        event.preventDefault();
        setStatus(null);
        setError(null);
        try {
            await updateInstallDefaults(form);
            setStatus("Defaults saved");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to save defaults");
        }
    };

    if (loading) {
        return <p>Loading settings…</p>;
    }

    return (
        <form onSubmit={handleSubmit}>
            <h2>Install Defaults</h2>
            <label style={styles.label}>
                Default Address
                <input
                    style={styles.input}
                    value={form.address}
                    onChange={(e) => setForm((prev) => ({ ...prev, address: e.target.value }))}
                    required
                />
            </label>
            <label style={styles.label}>
                Default User
                <input
                    style={styles.input}
                    value={form.user}
                    onChange={(e) => setForm((prev) => ({ ...prev, user: e.target.value }))}
                    required
                />
            </label>
            <label style={styles.label}>
                Default SSH Key
                <textarea
                    style={styles.textarea}
                    value={form.ssh_key}
                    onChange={(e) => setForm((prev) => ({ ...prev, ssh_key: e.target.value }))}
                    required
                />
            </label>
            <button type="submit">Save Defaults</button>
            {status && <p style={{ color: "green" }}>{status}</p>}
            {error && <p style={{ color: "red" }}>{error}</p>}
        </form>
    );
}

export default App;
