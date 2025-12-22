import { useState, useEffect, useRef } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { installAgent, getInstallDefaults } from "../api";
import { Loader2, Terminal, Eye, EyeOff, Usb, Network } from "lucide-react";
import { SerialTerminal, SerialTerminalRef } from "../components/SerialTerminal";

export function InstallAgent() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const location = useLocation();
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [showSudoPassword, setShowSudoPassword] = useState(false);
    const [showPassword, setShowPassword] = useState(false);
    const [mode, setMode] = useState<'ssh' | 'usb'>('ssh');
    const serialRef = useRef<SerialTerminalRef>(null);
    const [serialConnected, setSerialConnected] = useState(false);

    const [formData, setFormData] = useState({
        name: "",
        type: "robot",
        address: "",
        user: "ubuntu",
        ssh_key: "",
        password: "",
        sudo: true,
        sudo_password: "",
    });

    useEffect(() => {
        const query = new URLSearchParams(location.search);
        const type = query.get("type") || "robot";

        getInstallDefaults().then((data) => {
            if (data.install_config) {
                setFormData(prev => ({
                    ...prev,
                    type,
                    user: data.install_config?.user || prev.user,
                    ssh_key: data.install_config?.ssh_key || prev.ssh_key,
                }));
            } else {
                setFormData(prev => ({ ...prev, type }));
            }
        });

        if (location.state) {
            const state = location.state as any;
            setFormData(prev => ({
                ...prev,
                address: state.ip || prev.address,
                name: state.name || prev.name
            }));
        }
    }, [location]);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError(null);

        try {
            await installAgent(formData);
            navigate(formData.type === 'laptop' ? "/laptops" : "/robots");
        } catch (err) {
            setError(err instanceof Error ? err.message : t("installAgent.installFailed"));
        } finally {
            setLoading(false);
        }
    };

    const handleSerialConnect = async () => {
        if (serialRef.current) {
            await serialRef.current.connect();
            setSerialConnected(true);
        }
    };

    const handleSerialProvision = async () => {
        if (!serialRef.current) return;
        setLoading(true);

        const controllerUrl = window.location.origin;
        const brokerUrl = controllerUrl.replace(/^http/, 'ws').replace(/^https/, 'wss') + "/mqtt"; // This might need adjustment based on your broker setup
        // Actually, for the agent config, we usually want the TCP broker address or a public WS address.
        // Let's assume the user knows the broker URL or we default to a sensible one.
        // For now, let's use a placeholder or try to derive it.
        // The backend `agentBrokerURL` logic is: env var -> public broker -> local IP.
        // Since we are in the browser, we can't easily know the internal IP if we are on a public URL.
        // We'll use the window.location.hostname for now, assuming the broker is reachable there on 1883 or similar.
        // But wait, the agent needs a TCP connection usually.
        // Let's use a standard placeholder that the user might need to edit, or fetch it from the backend?
        // The backend `install_agent.go` uses `agentBrokerURL()`.
        // We can't easily get that from here without an API call.
        // Let's just use a hardcoded guess for now or ask the user?
        // Better: The script below uses a placeholder.

        // We need to fetch the correct broker URL from the server or let the user input it.
        // For simplicity in this iteration, we'll assume the server is at window.location.hostname
        const brokerHost = window.location.hostname;
        const mqttBroker = `tcp://${brokerHost}:1883`;

        const script = [
            `echo "Starting provisioning..."`,
            `ARCH=$(dpkg --print-architecture)`,
            `echo "Detected architecture: $ARCH"`,
            `echo "Downloading agent..."`,
            `curl -L -o /usr/local/bin/openrobotfleet-agent "${controllerUrl}/api/agent/download?arch=$ARCH"`,
            `chmod +x /usr/local/bin/openrobotfleet-agent`,
            `mkdir -p /etc/openrobotfleet-agent`,
            `echo "Writing config..."`,
            `cat <<EOF > /etc/openrobotfleet-agent/config.yaml`,
            `agent_id: "${formData.name}"`,
            `type: "${formData.type}"`,
            `mqtt_broker: "${mqttBroker}"`,
            `workspace_path: "/home/${formData.user}/ros_ws/src/course"`,
            `workspace_owner: "${formData.user}"`,
            `EOF`,
            `echo "Writing systemd unit..."`,
            `cat <<EOF > /etc/systemd/system/openrobotfleet-agent.service`,
            `[Unit]`,
            `Description=OpenRobot Agent`,
            `After=network-online.target`,
            `[Service]`,
            `ExecStart=/usr/local/bin/openrobotfleet-agent`,
            `Environment=AGENT_CONFIG_PATH=/etc/openrobotfleet-agent/config.yaml`,
            `Restart=always`,
            `[Install]`,
            `WantedBy=multi-user.target`,
            `EOF`,
            `echo "Enabling service..."`,
            `systemctl daemon-reload`,
            `systemctl enable openrobotfleet-agent`,
            `systemctl restart openrobotfleet-agent`,
            `echo "Provisioning complete! Agent should appear in dashboard shortly."`,
        ];

        try {
            for (const line of script) {
                await serialRef.current.write(line + "\n");
                // Small delay to prevent buffer overflow on the device
                await new Promise(r => setTimeout(r, 100));
            }
            navigate(formData.type === 'laptop' ? "/laptops" : "/robots");
        } catch (err) {
            console.error(err);
            setError("Failed to write to serial port");
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="max-w-2xl mx-auto">
            <div className="mb-8">
                <h1 className="text-2xl font-bold text-gray-900">{t("installAgent.title", { type: formData.type === 'laptop' ? t("common.laptops") : t("common.robots") })}</h1>
                <p className="text-gray-500">{t("installAgent.subtitle")}</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100 bg-gray-50">
                    <div className="flex items-center justify-between mb-4">
                        <div className="flex items-start gap-3">
                            {mode === 'ssh' ? <Network className="text-blue-600 mt-1" size={20} /> : <Usb className="text-blue-600 mt-1" size={20} />}
                            <div>
                                <h3 className="font-semibold text-gray-900">{mode === 'ssh' ? "Network Installation (SSH)" : "USB Provisioning (Serial)"}</h3>
                                <p className="text-sm text-gray-500">
                                    {mode === 'ssh'
                                        ? t("installAgent.detailsDesc")
                                        : "Connect via USB cable to provision the agent directly."}
                                </p>
                            </div>
                        </div>
                        <div className="flex bg-gray-200 rounded-lg p-1">
                            <button
                                onClick={() => setMode('ssh')}
                                className={`px-3 py-1.5 rounded-md text-sm font-medium transition-all ${mode === 'ssh' ? 'bg-white shadow text-gray-900' : 'text-gray-600 hover:text-gray-900'}`}
                            >
                                SSH
                            </button>
                            <button
                                onClick={() => setMode('usb')}
                                className={`px-3 py-1.5 rounded-md text-sm font-medium transition-all ${mode === 'usb' ? 'bg-white shadow text-gray-900' : 'text-gray-600 hover:text-gray-900'}`}
                            >
                                USB
                            </button>
                        </div>
                    </div>
                </div>

                {mode === 'ssh' ? (
                    <form onSubmit={handleSubmit} className="p-6 space-y-6">
                        {error && (
                            <div className="p-4 bg-red-50 text-red-700 rounded-lg text-sm">
                                {error}
                            </div>
                        )}

                        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                            <div className="col-span-2">
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    {t("installAgent.robotName")}
                                </label>
                                <input
                                    required
                                    type="text"
                                    value={formData.name}
                                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="e.g., openrobot-01"
                                />
                            </div>

                            <div>
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    {t("installAgent.ipAddress")}
                                </label>
                                <input
                                    required
                                    type="text"
                                    value={formData.address}
                                    onChange={(e) => setFormData({ ...formData, address: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="192.168.1.x"
                                />
                            </div>

                            <div>
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    {t("installAgent.sshUser")}
                                </label>
                                <input
                                    required
                                    type="text"
                                    value={formData.user}
                                    onChange={(e) => setFormData({ ...formData, user: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="ubuntu"
                                />
                            </div>

                            <div className="col-span-2">
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    SSH Password (Optional if Key provided)
                                </label>
                                <div className="relative">
                                    <input
                                        type={showPassword ? "text" : "password"}
                                        value={formData.password}
                                        onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none pr-10"
                                        placeholder="SSH Password"
                                    />
                                    <button
                                        type="button"
                                        onClick={() => setShowPassword(!showPassword)}
                                        className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                                    >
                                        {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                                    </button>
                                </div>
                            </div>

                            <div className="col-span-2">
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    {t("installAgent.sshKey")} (Optional if Password provided)
                                </label>
                                <textarea
                                    required={!formData.password}
                                    value={formData.ssh_key}
                                    onChange={(e) => setFormData({ ...formData, ssh_key: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none font-mono text-sm h-32"
                                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----..."
                                />
                                <p className="mt-1 text-xs text-gray-500">
                                    Paste the private key content directly. It is not stored permanently.
                                </p>
                            </div>

                            <div>
                                <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-1">
                                    <input
                                        type="checkbox"
                                        checked={formData.sudo}
                                        onChange={(e) => setFormData({ ...formData, sudo: e.target.checked })}
                                        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                                    />
                                    {t("installAgent.enableSudo")}
                                </label>
                            </div>

                            {formData.sudo && (
                                <div>
                                    <label className="block text-sm font-medium text-gray-700 mb-1">
                                        {t("installAgent.sudoPassword")}
                                    </label>
                                    <div className="relative">
                                        <input
                                            type={showSudoPassword ? "text" : "password"}
                                            value={formData.sudo_password}
                                            onChange={(e) => setFormData({ ...formData, sudo_password: e.target.value })}
                                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none pr-10"
                                            placeholder={t("installAgent.sudoPlaceholder")}
                                        />
                                        <button
                                            type="button"
                                            onClick={() => setShowSudoPassword(!showSudoPassword)}
                                            className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                                        >
                                            {showSudoPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                                        </button>
                                    </div>
                                </div>
                            )}
                        </div>

                        <div className="pt-4 flex justify-end gap-3">
                            <button
                                type="button"
                                onClick={() => navigate("/robots")}
                                className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg font-medium transition-colors"
                            >
                                {t("common.cancel")}
                            </button>
                            <button
                                type="submit"
                                disabled={loading}
                                className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 disabled:opacity-50"
                            >
                                {loading && <Loader2 size={18} className="animate-spin" />}
                                {t("installAgent.install")}
                            </button>
                        </div>
                    </form>
                ) : (
                    <div className="p-6 space-y-6">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                            <div className="col-span-2">
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    {t("installAgent.robotName")}
                                </label>
                                <input
                                    required
                                    type="text"
                                    value={formData.name}
                                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="e.g., openrobot-01"
                                />
                            </div>
                            <div className="col-span-2">
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    Workspace Owner (User)
                                </label>
                                <input
                                    required
                                    type="text"
                                    value={formData.user}
                                    onChange={(e) => setFormData({ ...formData, user: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="ubuntu"
                                />
                            </div>
                        </div>

                        <div className="border rounded-lg overflow-hidden">
                            <div className="bg-gray-800 text-gray-200 px-4 py-2 text-xs font-mono flex justify-between items-center">
                                <span>Serial Console</span>
                                {!serialConnected && (
                                    <button
                                        onClick={handleSerialConnect}
                                        className="bg-blue-600 hover:bg-blue-500 text-white px-3 py-1 rounded text-xs"
                                    >
                                        Connect Robot
                                    </button>
                                )}
                            </div>
                            <div className="h-[400px]">
                                <SerialTerminal ref={serialRef} />
                            </div>
                        </div>

                        <div className="pt-4 flex justify-end gap-3">
                            <button
                                type="button"
                                onClick={() => navigate("/robots")}
                                className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg font-medium transition-colors"
                            >
                                {t("common.cancel")}
                            </button>
                            <button
                                type="button"
                                onClick={handleSerialProvision}
                                disabled={loading || !serialConnected || !formData.name}
                                className="bg-green-600 text-white px-6 py-2 rounded-lg hover:bg-green-700 transition-colors font-medium flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                {loading && <Loader2 size={18} className="animate-spin" />}
                                Provision via USB
                            </button>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

