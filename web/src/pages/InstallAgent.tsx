import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { installAgent, getInstallDefaults } from "../api";
import { Loader2, Terminal, Eye, EyeOff } from "lucide-react";

export function InstallAgent() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const location = useLocation();
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [showSudoPassword, setShowSudoPassword] = useState(false);
    const [showPassword, setShowPassword] = useState(false);
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

    return (
        <div className="max-w-2xl mx-auto">
            <div className="mb-8">
                <h1 className="text-2xl font-bold text-gray-900">{t("installAgent.title", { type: formData.type === 'laptop' ? t("common.laptops") : t("common.robots") })}</h1>
                <p className="text-gray-500">{t("installAgent.subtitle")}</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100 bg-gray-50">
                    <div className="flex items-start gap-3">
                        <Terminal className="text-blue-600 mt-1" size={20} />
                        <div>
                            <h3 className="font-semibold text-gray-900">{t("installAgent.detailsTitle")}</h3>
                            <p className="text-sm text-gray-500">
                                {t("installAgent.detailsDesc")}
                            </p>
                        </div>
                    </div>
                </div>

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
            </div>
        </div>
    );
}
