import { FormEvent, useCallback, useEffect, useState } from "react";
import {
    createScenario,
    deleteScenario,
    getRobots,
    getScenarios,
    installAgent,
    ScenarioPayload,
    sendCommand,
    updateScenario,
} from "./api";
import { InstallAgentPayload, Robot, Scenario } from "./types";

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
                {activeTab === "scenarios" && (
                    <div style={styles.card}>
                        <ScenarioEditor />
                    </div>
                )}
                {activeTab === "install" && (
                    <div style={{ ...styles.card, maxWidth: 600 }}>
                        <InstallAgentForm />
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

function ScenarioEditor() {
    const [scenarios, setScenarios] = useState<Scenario[]>([]);
    const [selected, setSelected] = useState<Scenario | null>(null);
    const [form, setForm] = useState({ name: "", description: "", config_yaml: "" });
    const [status, setStatus] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);

    const loadScenarios = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const data = await getScenarios();
            setScenarios(data);
            if (selected) {
                const updated = data.find((s) => s.id === selected.id);
                if (updated) {
                    setSelected(updated);
                    setForm({
                        name: updated.name,
                        description: updated.description ?? "",
                        config_yaml: updated.config_yaml,
                    });
                }
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load scenarios");
        } finally {
            setLoading(false);
        }
    }, [selected]);

    useEffect(() => {
        loadScenarios();
    }, [loadScenarios]);

    const handleSelect = (scenario: Scenario) => {
        setSelected(scenario);
        setForm({
            name: scenario.name,
            description: scenario.description ?? "",
            config_yaml: scenario.config_yaml,
        });
        setStatus(null);
        setError(null);
    };

    const handleNew = () => {
        setSelected(null);
        setForm({ name: "", description: "", config_yaml: "" });
        setStatus(null);
        setError(null);
    };

    const buildPayload = (): ScenarioPayload => {
        const base: ScenarioPayload = {
            name: form.name,
            config_yaml: form.config_yaml,
        };
        if (form.description.trim()) {
            return { ...base, description: form.description };
        }
        return base;
    };

    const handleSave = async () => {
        if (!form.name.trim()) {
            setError("Name is required");
            return;
        }
        setStatus(null);
        setError(null);
        try {
            if (selected) {
                const updated = await updateScenario(selected.id, buildPayload());
                setSelected(updated);
                setStatus("Scenario updated");
            } else {
                const created = await createScenario(buildPayload());
                setSelected(created);
                setStatus("Scenario created");
            }
            await loadScenarios();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to save scenario");
        }
    };

    const handleDelete = async () => {
        if (!selected) return;
        setStatus(null);
        setError(null);
        try {
            await deleteScenario(selected.id);
            handleNew();
            await loadScenarios();
            setStatus("Scenario deleted");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to delete scenario");
        }
    };

    return (
        <div style={{ display: "flex", gap: "1rem", flexWrap: "wrap" }}>
            <div style={{ flex: "0 0 220px" }}>
                <h2>Scenarios</h2>
                {loading ? (
                    <p>Loading scenarios…</p>
                ) : (
                    <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
                        {scenarios.map((scenario) => (
                            <li key={scenario.id}>
                                <button
                                    style={{
                                        width: "100%",
                                        textAlign: "left",
                                        padding: "0.5rem",
                                        marginBottom: "0.25rem",
                                        borderRadius: "4px",
                                        border: "1px solid #ccc",
                                        backgroundColor:
                                            selected?.id === scenario.id ? "#e6f0ff" : "#fff",
                                    }}
                                    onClick={() => handleSelect(scenario)}
                                    type="button"
                                >
                                    {scenario.name}
                                </button>
                            </li>
                        ))}
                        {!scenarios.length && <li>No scenarios yet.</li>}
                    </ul>
                )}
                <button style={{ marginTop: "0.5rem" }} onClick={handleNew} type="button">
                    New
                </button>
            </div>
            <div style={{ flex: "1 1 300px" }}>
                <h2>{selected ? "Edit Scenario" : "New Scenario"}</h2>
                <label style={styles.label}>
                    Name
                    <input
                        style={styles.input}
                        value={form.name}
                        onChange={(e) => setForm((prev) => ({ ...prev, name: e.target.value }))}
                        required
                    />
                </label>
                <label style={styles.label}>
                    Description
                    <input
                        style={styles.input}
                        value={form.description}
                        onChange={(e) => setForm((prev) => ({ ...prev, description: e.target.value }))}
                    />
                </label>
                <label style={styles.label}>
                    Config YAML
                    <textarea
                        style={styles.textarea}
                        value={form.config_yaml}
                        onChange={(e) => setForm((prev) => ({ ...prev, config_yaml: e.target.value }))}
                    />
                </label>
                <div style={styles.buttonRow}>
                    <button onClick={handleSave} type="button">
                        Save
                    </button>
                    <button onClick={handleDelete} disabled={!selected} type="button">
                        Delete
                    </button>
                </div>
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

    const handleSubmit = async (event: FormEvent) => {
        event.preventDefault();
        setStatus(null);
        setError(null);
        setSubmitting(true);
        try {
            await installAgent(form);
            setStatus("Install request submitted");
            setForm({ name: "", address: "", user: "", ssh_key: "" });
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to install agent");
        } finally {
            setSubmitting(false);
        }
    };

    return (
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
            {status && <p style={{ color: "green" }}>{status}</p>}
            {error && <p style={{ color: "red" }}>{error}</p>}
        </form>
    );
}

export default App;
