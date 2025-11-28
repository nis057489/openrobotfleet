import { Save, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { getInstallDefaults, updateInstallDefaults } from "../api";
import { InstallConfig } from "../types";

export function Settings() {
    const [config, setConfig] = useState<InstallConfig>({
        address: "",
        user: "",
        ssh_key: "",
    });
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
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

    if (loading) return <div className="p-8 text-gray-500">Loading settings...</div>;

    return (
        <div className="max-w-2xl">
            <div className="mb-8">
                <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
                <p className="text-gray-500">Configure global fleet parameters</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="font-semibold text-gray-900">Default Install Configuration</h2>
                    <p className="text-sm text-gray-500 mt-1">
                        These settings will be pre-filled when installing the agent on new robots.
                    </p>
                </div>

                <div className="p-6 space-y-6">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            Robot Address (IP/Hostname)
                        </label>
                        <input
                            type="text"
                            value={config.address}
                            onChange={(e) => setConfig({ ...config, address: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                            placeholder="e.g. 192.168.1.100"
                        />
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            SSH User
                        </label>
                        <input
                            type="text"
                            value={config.user}
                            onChange={(e) => setConfig({ ...config, user: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                            placeholder="e.g. pi"
                        />
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            SSH Key Path (on controller)
                        </label>
                        <input
                            type="text"
                            value={config.ssh_key}
                            onChange={(e) => setConfig({ ...config, ssh_key: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none transition-all"
                            placeholder="e.g. /root/.ssh/id_rsa"
                        />
                    </div>

          <div className="grid grid-cols-2 gap-4">
            {/* WiFi settings removed as they are not supported by the backend yet */}
          </div>
                    {message && (
                        <div className={`p-3 rounded-lg text-sm ${message.type === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>
                            {message.text}
                        </div>
                    )}

                    <div className="pt-4">
                        <button
                            onClick={handleSave}
                            disabled={saving}
                            className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 disabled:opacity-50"
                        >
                            {saving ? <Loader2 size={18} className="animate-spin" /> : <Save size={18} />}
                            Save Changes
                        </button>
                    </div>
                </div>
            </div>
        </div>
    );
}
