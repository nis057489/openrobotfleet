import { Save, Loader2, Power, RefreshCw, AlertTriangle, Download, Upload, Database, Bell } from "lucide-react";
import { useEffect, useState, useRef } from "react";
import { getInstallDefaults, updateInstallDefaults, broadcastCommand, getSystemConfig } from "../api";
import { InstallConfig } from "../types";
import { useTranslation } from "react-i18next";

export function Settings() {
    const { t } = useTranslation();
    const [config, setConfig] = useState<InstallConfig>({
        address: "",
        user: "",
        ssh_key: "",
    });
    const [demoMode, setDemoMode] = useState(false);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [broadcasting, setBroadcasting] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        Promise.all([getInstallDefaults(), getSystemConfig()])
            .then(([data, sysConfig]) => {
                if (data.install_config) {
                    setConfig(data.install_config);
                }
                setDemoMode(sysConfig.demo_mode);
            })
            .catch((err) => console.error(err))
            .finally(() => setLoading(false));
    }, []);

    const handleSave = async () => {
        setSaving(true);
        setMessage(null);
        try {
            await updateInstallDefaults(config);
            setMessage({ type: 'success', text: t("settings.saveSuccess") });
        } catch (err) {
            setMessage({ type: 'error', text: t("settings.saveError") });
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
                setMessage({ type: 'success', text: t("settings.classOverSent") });
            } else {
                await broadcastCommand({ type, data: {} });
                setMessage({ type: 'success', text: t("settings.broadcastSent", { type }) });
            }
        } catch (err) {
            setMessage({ type: 'error', text: t("settings.broadcastError") });
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

        if (!confirm(t("settings.restoreConfirm"))) {
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
            alert(t("settings.restoreSuccess"));
            window.location.reload();
        } catch (err) {
            setMessage({ type: 'error', text: t("settings.restoreError") });
        } finally {
            setSaving(false);
            if (fileInputRef.current) fileInputRef.current.value = '';
        }
    };

    if (loading) return <div className="p-8 text-gray-500">{t("common.loading")}</div>;

    return (
        <div className="max-w-2xl space-y-8">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">{t("common.settings")}</h1>
                <p className="text-gray-500">{t("settings.subtitle")}</p>
            </div>

            {/* Install Defaults */}
            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="text-lg font-semibold text-gray-900">{t("settings.installDefaults")}</h2>
                    <p className="text-sm text-gray-500 mt-1">
                        {t("settings.installDefaultsDesc")}
                    </p>
                </div>
                <div className="p-6 space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            {t("settings.sshUser")}
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
                            {t("settings.sshKey")}
                        </label>
                        <textarea
                            value={config.ssh_key}
                            onChange={(e) => setConfig({ ...config, ssh_key: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 font-mono text-xs h-32"
                            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----..."
                        />
                        <p className="mt-1 text-xs text-gray-500">
                            {t("settings.sshKeyDesc")}
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
                        {t("settings.saveDefaults")}
                    </button>
                </div>
            </div>

            {/* Database Management */}
            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
                        <Database size={20} className="text-blue-500" />
                        {t("settings.dbManagement")}
                    </h2>
                    <p className="text-sm text-gray-500 mt-1">
                        {t("settings.dbManagementDesc")}
                    </p>
                </div>
                <div className="p-6 grid grid-cols-1 md:grid-cols-2 gap-4">
                    {demoMode ? (
                        <div className="col-span-2 p-4 bg-gray-50 text-gray-500 rounded-lg text-center italic border border-gray-200">
                            {t("settings.demoModeDisabled")}
                        </div>
                    ) : (
                        <>
                            <button
                                onClick={handleBackup}
                                className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                            >
                                <div>
                                    <div className="font-medium text-gray-900">{t("settings.backup")}</div>
                                    <div className="text-xs text-gray-500">{t("settings.backupDesc")}</div>
                                </div>
                                <Download size={20} className="text-gray-400" />
                            </button>

                            <button
                                onClick={() => fileInputRef.current?.click()}
                                className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                            >
                                <div>
                                    <div className="font-medium text-gray-900">{t("settings.restore")}</div>
                                    <div className="text-xs text-gray-500">{t("settings.restoreDesc")}</div>
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
                        </>
                    )}
                </div>
            </div>

            {/* Fleet Maintenance */}
            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100">
                    <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
                        <AlertTriangle size={20} className="text-orange-500" />
                        {t("settings.fleetMaintenance")}
                    </h2>
                    <p className="text-sm text-gray-500 mt-1">
                        {t("settings.fleetMaintenanceDesc")}
                    </p>
                </div>
                <div className="p-6 grid grid-cols-1 md:grid-cols-2 gap-4">
                    <button
                        onClick={() => handleBroadcast("restart_ros", t("settings.restartConfirm"))}
                        disabled={broadcasting}
                        className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                    >
                        <div>
                            <div className="font-medium text-gray-900">{t("settings.restartRos")}</div>
                            <div className="text-xs text-gray-500">{t("settings.restartRosDesc")}</div>
                        </div>
                        <RefreshCw size={20} className="text-gray-400" />
                    </button>

                    <button
                        onClick={() => handleBroadcast("reboot", t("settings.rebootConfirm"))}
                        disabled={broadcasting}
                        className="flex items-center justify-between p-4 border border-red-200 rounded-lg hover:bg-red-50 transition-colors text-left group"
                    >
                        <div>
                            <div className="font-medium text-red-700 group-hover:text-red-800">{t("settings.rebootFleet")}</div>
                            <div className="text-xs text-red-500 group-hover:text-red-600">{t("settings.rebootFleetDesc")}</div>
                        </div>
                        <Power size={20} className="text-red-400 group-hover:text-red-600" />
                    </button>

                    <button
                        onClick={() => handleBroadcast("class_over", t("settings.classOverConfirm"))}
                        disabled={broadcasting}
                        className="flex items-center justify-between p-4 border border-yellow-200 rounded-lg hover:bg-yellow-50 transition-colors text-left group"
                    >
                        <div>
                            <div className="font-medium text-yellow-700 group-hover:text-yellow-800">{t("settings.classOver")}</div>
                            <div className="text-xs text-yellow-600 group-hover:text-yellow-700">{t("settings.classOverDesc")}</div>
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
