import { FileCode, Play, Plus, Trash2, Edit } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { getScenarios, deleteScenario } from "../api";
import { Scenario } from "../types";
import { DeployModal } from "../components/DeployModal";
import { useTranslation } from "react-i18next";

export function Scenarios() {
    const navigate = useNavigate();
    const { t } = useTranslation();
    const [scenarios, setScenarios] = useState<Scenario[]>([]);
    const [loading, setLoading] = useState(true);
    const [deployTarget, setDeployTarget] = useState<Scenario | null>(null);

    const loadScenarios = () => {
        setLoading(true);
        getScenarios()
            .then(setScenarios)
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        loadScenarios();
    }, []);

    const handleDelete = async (id: number) => {
        if (confirm(t("scenarios.deleteConfirm"))) {
            await deleteScenario(id);
            loadScenarios();
        }
    };

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">{t("common.scenarios")}</h1>
                    <p className="text-gray-500">{t("scenarios.subtitle")}</p>
                </div>
                <button
                    onClick={() => navigate("/scenarios/new")}
                    className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2"
                >
                    <Plus size={18} /> {t("common.newScenario")}
                </button>
            </div>

            {loading ? (
                <div className="text-center py-12 text-gray-500">{t("scenarios.loading")}</div>
            ) : scenarios.length === 0 ? (
                <div className="bg-white rounded-xl border border-gray-200 p-12 text-center">
                    <div className="w-16 h-16 bg-blue-50 rounded-full flex items-center justify-center mx-auto mb-4">
                        <FileCode size={32} className="text-blue-600" />
                    </div>
                    <h3 className="text-lg font-semibold text-gray-900 mb-2">{t("scenarios.emptyTitle")}</h3>
                    <p className="text-gray-500 max-w-md mx-auto mb-6">
                        {t("scenarios.emptyDescription")}
                    </p>
                    <button
                        onClick={() => navigate("/scenarios/new")}
                        className="text-blue-600 font-medium hover:text-blue-700"
                    >
                        {t("scenarios.createButton")}
                    </button>
                </div>
            ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                    {scenarios.map((scenario) => (
                        <div key={scenario.id} className="bg-white rounded-xl border border-gray-200 p-6 hover:shadow-md transition-shadow">
                            <div className="flex items-start justify-between mb-4">
                                <div className="p-2 bg-blue-50 rounded-lg">
                                    <FileCode size={24} className="text-blue-600" />
                                </div>
                                <div className="flex gap-2">
                                    <button
                                        onClick={() => setDeployTarget(scenario)}
                                        className="p-2 hover:bg-green-50 text-green-600 rounded-lg transition-colors"
                                        title={t("common.deploy")}
                                    >
                                        <Play size={18} />
                                    </button>
                                    <button
                                        onClick={() => navigate(`/scenarios/${scenario.id}`)}
                                        className="p-2 hover:bg-blue-50 text-blue-600 rounded-lg transition-colors"
                                        title={t("common.edit")}
                                    >
                                        <Edit size={18} />
                                    </button>
                                    <button
                                        onClick={() => handleDelete(scenario.id)}
                                        className="p-2 hover:bg-red-50 text-red-600 rounded-lg transition-colors"
                                        title={t("common.delete")}
                                    >
                                        <Trash2 size={18} />
                                    </button>
                                </div>
                            </div>
                            <h3 className="font-bold text-lg text-gray-900 mb-1">{scenario.name}</h3>
                            <p className="text-sm text-gray-500 line-clamp-2 mb-4">
                                {scenario.description || t("common.noDescription")}
                            </p>
                            <div className="bg-gray-50 rounded-lg p-3 font-mono text-xs text-gray-600 overflow-hidden h-24 whitespace-pre">
                                {scenario.config_yaml}
                            </div>
                        </div>
                    ))}
                </div>
            )}

            {deployTarget && (
                <DeployModal
                    scenarioId={deployTarget.id}
                    scenarioName={deployTarget.name}
                    onClose={() => setDeployTarget(null)}
                    onSuccess={() => {
                        // Maybe show a toast?
                        setDeployTarget(null);
                    }}
                />
            )}
        </div>
    );
}
