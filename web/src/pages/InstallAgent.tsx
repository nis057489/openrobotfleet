import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { installAgent } from "../api";
import { Loader2, Terminal } from "lucide-react";

export function InstallAgent() {
    const navigate = useNavigate();
    const location = useLocation();
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [formData, setFormData] = useState({
        name: "",
        address: "",
        user: "ubuntu",
        ssh_key: "",
        sudo: true,
        sudo_password: "",
    });

    useEffect(() => {
        if (location.state && (location.state as any).ip) {
            setFormData(prev => ({
                ...prev,
                address: (location.state as any).ip
            }));
        }
    }, [location]);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError(null);

        try {
            await installAgent(formData);
            navigate("/robots");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to install agent");
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="max-w-2xl mx-auto">
            <div className="mb-8">
                <h1 className="text-2xl font-bold text-gray-900">Add New Robot</h1>
                <p className="text-gray-500">Install the agent on a remote machine via SSH</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100 bg-gray-50">
                    <div className="flex items-start gap-3">
                        <Terminal className="text-blue-600 mt-1" size={20} />
                        <div>
                            <h3 className="font-semibold text-gray-900">Installation Details</h3>
                            <p className="text-sm text-gray-500">
                                The controller will SSH into the target machine, upload the agent binary, and configure the systemd service.
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
                                Robot Name
                            </label>
                            <input
                                required
                                type="text"
                                value={formData.name}
                                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                placeholder="e.g. turtlebot-01"
                            />
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-gray-700 mb-1">
                                IP Address / Hostname
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
                                SSH User
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
                                SSH Private Key
                            </label>
                            <textarea
                                required
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
                                Use Sudo
                            </label>
                        </div>

                        {formData.sudo && (
                            <div>
                                <label className="block text-sm font-medium text-gray-700 mb-1">
                                    Sudo Password
                                </label>
                                <input
                                    type="password"
                                    value={formData.sudo_password}
                                    onChange={(e) => setFormData({ ...formData, sudo_password: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="Required for sudo"
                                />
                            </div>
                        )}
                    </div>

                    <div className="pt-4 flex justify-end gap-3">
                        <button
                            type="button"
                            onClick={() => navigate("/robots")}
                            className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg font-medium transition-colors"
                        >
                            Cancel
                        </button>
                        <button
                            type="submit"
                            disabled={loading}
                            className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 disabled:opacity-50"
                        >
                            {loading && <Loader2 size={18} className="animate-spin" />}
                            Install Agent
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
