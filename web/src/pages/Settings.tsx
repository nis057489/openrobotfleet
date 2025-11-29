import { Save, Loader2, Power, RefreshCw, AlertTriangle, Download, Upload, Database, Bell } from "lucide-react";
import { useEffect, useState, useRef } from "react";
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
    const fileInputRef = useRef<HTMLInputElement>(null);

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
            // For "class_over", we send "stop" and "identify"
            if (type === "class_over") {
                await broadcastCommand({ type: "stop", data: {} });
                await broadcastCommand({ type: "identify", data: {} });
                setMessage({ type: 'success', text: `Class Over signal sent successfully` });
            } else {
                await broadcastCommand({ type, data: {} });
                setMessage({ type: 'success', text: `Broadcast command '${type}' sent successfully` });
            }
        } catch (err) {
            setMessage({ type: 'error', text: 'Failed to send broadcast command' });
        } finally {
            setBroadcasting(false);
        }
    };

    const handleBackup = () => {
        window.location.href = '/api/settings/backup';
    };

    const handleRestore = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        if (!file) return;

        if (!confirm("WARNING: This will overwrite the current database and restart the controller. All current data will be replaced. Are you sure?")) {
            if (fileInputRef.current) fileInputRef.current.value = '';
            return;
        }

        const formData = new FormData();
        formData.append('db_file', file);

        setSaving(true);
        try {
            const res = await fetch('/api/settings/restore', {
                method: 'POST',
                body: formData,
            });
            if (!res.ok) throw new Error("Restore failed");
            alert("Database restored successfully. The page will now reload.");
            window.location.reload();
        } catch (err) {
            setMessage({ type: 'error', text: 'Failed to restore database' });
        } finally {
            setSaving(false);
            if (fileInputRef.current) fileInputRef.current.value = '';
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
                            Default SSH Private Key
                        </label>
                        <textarea
                            value={config.ssh_key}
                            onChange={(e) => setConfig({ ...config, ssh_key: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 font-mono text-xs h-32"
                            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----..."
                        />
                        <p className="mt-1 text-xs text-gray-500">
                            Paste the private key content directly.
                        </p>
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

            {/* Database Management */}
            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
                        <Database size={20} className="text-blue-500" />
                        Database Management
                    </h2>
                    <p className="text-sm text-gray-500 mt-1">
                        Backup and restore the controller database.
                    </p>
                </div>
                <div className="p-6 grid grid-cols-1 md:grid-cols-2 gap-4">
                    <button
                        onClick={handleBackup}
                        className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                    >
                        <div>
                            <div className="font-medium text-gray-900">Backup Database</div>
                            <div className="text-xs text-gray-500">Download current .db file</div>
                        </div>
                        <Download size={20} className="text-gray-400" />
                    </button>

                    <button
                        onClick={() => fileInputRef.current?.click()}
                        className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                    >
                        <div>
                            <div className="font-medium text-gray-900">Restore Database</div>
                            <div className="text-xs text-gray-500">Upload .db file to replace current</div>
                        </div>
                        <Upload size={20} className="text-gray-400" />
                    </button>
                    <input
                        type="file"
                        ref={fileInputRef}
                        onChange={handleRestore}
                        className="hidden"
                        accept=".db"
                    />
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

                    <button
                        onClick={() => handleBroadcast("class_over", "Are you sure you want to STOP all robots and play the end-of-session sound?")}
                        disabled={broadcasting}
                        className="flex items-center justify-between p-4 border border-yellow-200 rounded-lg hover:bg-yellow-50 transition-colors text-left group"
                    >
                        <div>
                            <div className="font-medium text-yellow-700 group-hover:text-yellow-800">Class Over</div>
                            <div className="text-xs text-yellow-600 group-hover:text-yellow-700">Stop all robots & play sound</div>
                        </div>
                        <Bell size={20} className="text-yellow-500 group-hover:text-yellow-700" />
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
