import { Save, Loader2, Power, RefreshCw, AlertTriangle } from "lucide-react";
import { useEffect, useState } from "react";
import { getInstallDefaults, updateInstallDefaults, broadcastCommand } from "../api";
import { InstallConfig } from "../types";

export function Settings() {
    const [config, setConfig] = useState<InstallConfig>({
        address: "",
        user: "",
        ssh_key: "",
    });
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [broadcasting, setBroadcasting] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

    useEffect(() => {
        getInstallDefaults()
            .then((data) => {
                if (data.install_config) {
                    setConfig(data.install_config);
                }
            })
            .catch((err) => console.error(err))
            .finally(() => setLoading(false));
    }, []);

    const handleSave = async () => {
        setSaving(true);
        setMessage(null);
        try {
            await updateInstallDefaults(config);
            setMessage({ type: 'success', text: 'Settings saved successfully' });
        } catch (err) {
            setMessage({ type: 'error', text: 'Failed to save settings' });
        } finally {
            setSaving(false);
        }
    };

    const handleBroadcast = async (type: string, confirmMsg: string) => {
        if (!confirm(confirmMsg)) return;
        setBroadcasting(true);
        setMessage(null);
        try {
            await broadcastCommand({ type, data: {} });
            setMessage({ type: 'success', text: `Broadcast command '${type}' sent successfully` });
        } catch (err) {
            setMessage({ type: 'error', text: 'Failed to send broadcast command' });
        } finally {
            setBroadcasting(false);
        }
    };

    if (loading) return <div className="p-8 text-gray-500">Loading settings...</div>;

    return (
        <div className="max-w-2xl space-y-8">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
                <p className="text-gray-500">Configure global fleet parameters</p>
            </div>

            {/* Install Defaults */}
            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="text-lg font-semibold text-gray-900">Install Defaults</h2>
                    <p className="text-sm text-gray-500 mt-1">
                        Default credentials used when provisioning new robots via SSH.
                    </p>
                </div>
                <div className="p-6 space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            Default SSH User
                        </label>
                        <input
                            type="text"
                            value={config.user}
                            onChange={(e) => setConfig({ ...config, user: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                            placeholder="ubuntu"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            Default SSH Key Path
                        </label>
                        <input
                            type="text"
                            value={config.ssh_key}
                            onChange={(e) => setConfig({ ...config, ssh_key: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                            placeholder="~/.ssh/id_rsa"
                        />
                    </div>
                </div>
                <div className="px-6 py-4 bg-gray-50 border-t border-gray-100 flex justify-end">
                    <button
                        onClick={handleSave}
                        disabled={saving}
                        className="flex items-center gap-2 bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50"
                    >
                        {saving ? <Loader2 className="animate-spin" size={18} /> : <Save size={18} />}
                        Save Defaults
                    </button>
                </div>
            </div>

            {/* Fleet Maintenance */}
            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
                        <AlertTriangle size={20} className="text-orange-500" />
                        Fleet Maintenance
                    </h2>
                    <p className="text-sm text-gray-500 mt-1">
                        Dangerous operations that affect the entire fleet. Use with caution.
                    </p>
                </div>
                <div className="p-6 grid grid-cols-1 md:grid-cols-2 gap-4">
                    <button
                        onClick={() => handleBroadcast("restart_ros", "Are you sure you want to restart ROS on ALL robots?")}
                        disabled={broadcasting}
                        className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                    >
                        <div>
                            <div className="font-medium text-gray-900">Restart All ROS</div>
                            <div className="text-xs text-gray-500">Restart ROS 2 daemon on all connected robots</div>
                        </div>
                        <RefreshCw size={20} className="text-gray-400" />
                    </button>

                    <button
                        onClick={() => handleBroadcast("reboot", "Are you sure you want to REBOOT ALL robots? This will interrupt all operations.")}
                        disabled={broadcasting}
                        className="flex items-center justify-between p-4 border border-red-200 rounded-lg hover:bg-red-50 transition-colors text-left group"
                    >
                        <div>
                            <div className="font-medium text-red-700 group-hover:text-red-800">Reboot Fleet</div>
                            <div className="text-xs text-red-500 group-hover:text-red-600">Reboot every robot in the fleet</div>
                        </div>
                        <Power size={20} className="text-red-400 group-hover:text-red-600" />
                    </button>
                </div>
            </div>

            {message && (
                <div className={`p-4 rounded-lg ${message.type === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>
                    {message.text}
                </div>
            )}
        </div>
    );
}
