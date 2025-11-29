import { useState, useEffect } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { createScenario, getScenario, updateScenario } from "../api";
import { Loader2, Save, ArrowLeft } from "lucide-react";

export function ScenarioEditor() {
    const { t } = useTranslation();
    const { id } = useParams();
    const navigate = useNavigate();
    const isNew = !id;

    const [loading, setLoading] = useState(false);
    const [fetching, setFetching] = useState(!isNew);
    const [error, setError] = useState<string | null>(null);
    const [formData, setFormData] = useState({
        name: "",
        description: "",
        config_yaml: "",
    });

    useEffect(() => {
        if (!isNew && id) {
            getScenario(id)
                .then((data) => {
                    setFormData({
                        name: data.name,
                        description: data.description || "",
                        config_yaml: data.config_yaml,
                    });
                })
                .catch((err) => setError(err.message))
                .finally(() => setFetching(false));
        }
    }, [id, isNew]);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError(null);

        try {
            if (isNew) {
                await createScenario(formData);
            } else if (id) {
                await updateScenario(id, formData);
            }
            navigate("/scenarios");
        } catch (err) {
            setError(err instanceof Error ? err.message : t("scenarioEditor.saveError"));
        } finally {
            setLoading(false);
        }
    };

    if (fetching) return <div className="p-8 text-gray-500">{t("scenarioEditor.loading")}</div>;

    return (
        <div className="max-w-4xl mx-auto">
            <div className="mb-6 flex items-center gap-4">
                <button
                    onClick={() => navigate("/scenarios")}
                    className="p-2 hover:bg-gray-100 rounded-lg text-gray-600 transition-colors"
                >
                    <ArrowLeft size={20} />
                </button>
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">
                        {isNew ? t("scenarioEditor.newTitle") : t("scenarioEditor.editTitle")}
                    </h1>
                    <p className="text-gray-500">{t("scenarioEditor.subtitle")}</p>
                </div>
            </div>

            <form onSubmit={handleSubmit} className="space-y-6">
                {error && (
                    <div className="p-4 bg-red-50 text-red-700 rounded-lg text-sm">
                        {error}
                    </div>
                )}

                <div className="bg-white rounded-xl border border-gray-200 p-6 space-y-6">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            {t("scenarioEditor.nameLabel")}
                        </label>
                        <input
                            required
                            type="text"
                            value={formData.name}
                            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                            placeholder={t("scenarioEditor.namePlaceholder")}
                        />
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            {t("scenarioEditor.descriptionLabel")}
                        </label>
                        <input
                            type="text"
                            value={formData.description}
                            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                            placeholder={t("scenarioEditor.descriptionPlaceholder")}
                        />
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            {t("scenarioEditor.configLabel")}
                        </label>
                        <div className="relative">
                            <textarea
                                required
                                value={formData.config_yaml}
                                onChange={(e) => setFormData({ ...formData, config_yaml: e.target.value })}
                                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none font-mono text-sm h-96 bg-gray-50"
                                placeholder={`repo:
  url: "https://github.com/..."
  branch: "main"
  path: "scenarios/patrol"
ros:
  launch: "ros2 launch patrol.launch.py"`}
                            />
                        </div>
                        <p className="mt-2 text-xs text-gray-500">
                            {t("scenarioEditor.configHelp")}
                        </p>
                    </div>
                </div>

                <div className="flex justify-end gap-3">
                    <button
                        type="button"
                        onClick={() => navigate("/scenarios")}
                        className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg font-medium transition-colors"
                    >
                        {t("common.cancel")}
                    </button>
                    <button
                        type="submit"
                        disabled={loading}
                        className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 disabled:opacity-50"
                    >
                        {loading ? <Loader2 size={18} className="animate-spin" /> : <Save size={18} />}
                        {t("common.save")}
                    </button>
                </div>
            </form>
        </div>
    );
}
