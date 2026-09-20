import { Save, Loader2, Download, Upload, Database, Eye, EyeOff } from "lucide-react";
import { useEffect, useState, useRef } from "react";
import { getInstallDefaults, updateInstallDefaults, getSystemConfig } from "../api";
import { InstallConfig } from "../types";
import { useTranslation } from "react-i18next";
import { useNotification } from "../contexts/NotificationContext";

export function Settings() {
    const { t } = useTranslation();
    const { success, error } = useNotification();
    const [config, setConfig] = useState<InstallConfig>({
        address: "",
        user: "",
        ssh_key: "",
        password: "",
    });
    const [laptopConfig, setLaptopConfig] = useState<InstallConfig>({
        address: "",
        user: "",
        ssh_key: "",
        password: "",
        sudo_password: "",
    });
    const [demoMode, setDemoMode] = useState(false);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const fileInputRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        Promise.all([getInstallDefaults(), getSystemConfig()])
            .then(([data, sysConfig]) => {
                if (data.install_config) {
                    setConfig(data.install_config);
                }
                if (data.laptop_install_config) {
                    setLaptopConfig(data.laptop_install_config);
                }
                setDemoMode(sysConfig.demo_mode);
            })
            .catch((err) => console.error(err))
            .finally(() => setLoading(false));
    }, []);

    const handleSave = async () => {
        setSaving(true);
        try {
            // Laptop defaults are optional; sending them empty would fail the
            // server's "user required" check, so only include them once set.
            const payload: InstallConfig = { ...config };
            if (laptopConfig.user.trim()) {
                payload.laptop = laptopConfig;
            }
            await updateInstallDefaults(payload);
            success(t("settings.saveSuccess"));
        } catch (err) {
            error(t("settings.saveError"));
        } finally {
            setSaving(false);
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
            error(t("settings.restoreError"));
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
                <div className="p-6 space-y-8">
                    <CredentialSection
                        title={t("settings.robotCredentials")}
                        description={t("settings.robotCredentialsDesc")}
                        config={config}
                        onChange={setConfig}
                        userPlaceholder="ubuntu"
                    />
                    <div className="border-t border-gray-100 pt-8">
                        <CredentialSection
                            title={t("settings.laptopCredentials")}
                            description={t("settings.laptopCredentialsDesc")}
                            config={laptopConfig}
                            onChange={setLaptopConfig}
                            userPlaceholder="student"
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
        </div>
    );
}

function CredentialSection({
    title, description, config, onChange, userPlaceholder,
}: {
    title: string;
    description: string;
    config: InstallConfig;
    onChange: (cfg: InstallConfig) => void;
    userPlaceholder: string;
}) {
    const { t } = useTranslation();
    const [showPassword, setShowPassword] = useState(false);
    const [showSudo, setShowSudo] = useState(false);

    return (
        <div className="space-y-4">
            <div>
                <h3 className="font-semibold text-gray-900">{title}</h3>
                <p className="text-sm text-gray-500 mt-1">{description}</p>
            </div>

            <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                    {t("settings.sshUser")}
                </label>
                <input
                    type="text"
                    value={config.user}
                    onChange={(e) => onChange({ ...config, user: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                    placeholder={userPlaceholder}
                />
            </div>

            <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                    {t("settings.sshPassword")}
                </label>
                <div className="relative">
                    <input
                        type={showPassword ? "text" : "password"}
                        value={config.password || ""}
                        onChange={(e) => onChange({ ...config, password: e.target.value })}
                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 pr-10"
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

            <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                    {t("settings.sudoPassword")}
                </label>
                <div className="relative">
                    <input
                        type={showSudo ? "text" : "password"}
                        value={config.sudo_password || ""}
                        onChange={(e) => onChange({ ...config, sudo_password: e.target.value })}
                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 pr-10"
                    />
                    <button
                        type="button"
                        onClick={() => setShowSudo(!showSudo)}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                    >
                        {showSudo ? <EyeOff size={16} /> : <Eye size={16} />}
                    </button>
                </div>
                <p className="mt-1 text-xs text-gray-500">{t("settings.sudoPasswordDesc")}</p>
            </div>

            <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                    {t("settings.sshKey")}
                </label>
                <textarea
                    value={config.ssh_key}
                    onChange={(e) => onChange({ ...config, ssh_key: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500 font-mono text-xs h-32"
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----..."
                />
                <p className="mt-1 text-xs text-gray-500">{t("settings.sshKeyDesc")}</p>
            </div>

            {config.ssh_public_key && (
                <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">
                        {t("settings.sshPublicKey")}
                    </label>
                    <textarea
                        readOnly
                        value={config.ssh_public_key}
                        className="w-full px-3 py-2 border border-gray-300 rounded-lg bg-gray-50 text-gray-600 font-mono text-xs h-24 resize-none focus:outline-none"
                    />
                    <p className="mt-1 text-xs text-gray-500">{t("settings.sshPublicKeyDesc")}</p>
                </div>
            )}
        </div>
    );
}
