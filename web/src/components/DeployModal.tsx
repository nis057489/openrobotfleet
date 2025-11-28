import { useState, useEffect } from "react";
import { getRobots, applyScenario } from "../api";
import { Robot } from "../types";
import { X, Play, Loader2, CheckCircle2 } from "lucide-react";

interface DeployModalProps {
    scenarioId: number;
    scenarioName: string;
    onClose: () => void;
    onSuccess: () => void;
}

export function DeployModal({ scenarioId, scenarioName, onClose, onSuccess }: DeployModalProps) {
    const [robots, setRobots] = useState<Robot[]>([]);
    const [selectedIds, setSelectedIds] = useState<number[]>([]);
    const [loading, setLoading] = useState(true);
    const [deploying, setDeploying] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        getRobots()
            .then((data) => {
                // Filter out offline robots if you want, or just show them as disabled
                setRobots(data);
            })
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

    const toggleRobot = (id: number) => {
        setSelectedIds((prev) =>
            prev.includes(id) ? prev.filter((i) => i !== id) : [...prev, id]
        );
    };

    const handleDeploy = async () => {
        if (selectedIds.length === 0) return;
        setDeploying(true);
        setError(null);
        try {
            await applyScenario(scenarioId, { robot_ids: selectedIds });
            onSuccess();
            onClose();
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to deploy scenario");
            setDeploying(false);
        }
    };

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
            <div className="bg-white rounded-xl shadow-xl max-w-lg w-full overflow-hidden flex flex-col max-h-[90vh]">
                <div className="p-6 border-b border-gray-100 flex items-center justify-between">
                    <div>
                        <h2 className="text-xl font-bold text-gray-900">Deploy Scenario</h2>
                        <p className="text-sm text-gray-500">Target: {scenarioName}</p>
                    </div>
                    <button onClick={onClose} className="text-gray-400 hover:text-gray-600">
                        <X size={24} />
                    </button>
                </div>

                <div className="p-6 overflow-y-auto flex-1">
                    {loading ? (
                        <div className="text-center py-8 text-gray-500">Loading robots...</div>
                    ) : error ? (
                        <div className="text-red-600 bg-red-50 p-4 rounded-lg">{error}</div>
                    ) : robots.length === 0 ? (
                        <div className="text-center py-8 text-gray-500">No robots available.</div>
                    ) : (
                        <div className="space-y-3">
                            <p className="text-sm font-medium text-gray-700 mb-2">Select Target Robots:</p>
                            {robots.map((robot) => {
                                const isSelected = selectedIds.includes(robot.id);
                                const isOnline = robot.status !== "offline" && robot.status !== "unknown";
                                return (
                                    <div
                                        key={robot.id}
                                        onClick={() => toggleRobot(robot.id)}
                                        className={`flex items-center justify-between p-3 rounded-lg border cursor-pointer transition-all ${isSelected
                                                ? "border-blue-500 bg-blue-50"
                                                : "border-gray-200 hover:border-blue-300"
                                            }`}
                                    >
                                        <div className="flex items-center gap-3">
                                            <div
                                                className={`w-2 h-2 rounded-full ${isOnline ? "bg-green-500" : "bg-gray-300"
                                                    }`}
                                            />
                                            <div>
                                                <p className="font-medium text-gray-900">{robot.name}</p>
                                                <p className="text-xs text-gray-500">{robot.ip || "No IP"}</p>
                                            </div>
                                        </div>
                                        {isSelected && <CheckCircle2 size={20} className="text-blue-600" />}
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </div>

                <div className="p-6 border-t border-gray-100 bg-gray-50 flex justify-end gap-3">
                    <button
                        onClick={onClose}
                        className="px-4 py-2 text-gray-700 hover:bg-gray-200 rounded-lg font-medium transition-colors"
                    >
                        Cancel
                    </button>
                    <button
                        onClick={handleDeploy}
                        disabled={deploying || selectedIds.length === 0}
                        className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        {deploying ? <Loader2 size={18} className="animate-spin" /> : <Play size={18} />}
                        Deploy ({selectedIds.length})
                    </button>
                </div>
            </div>
        </div>
    );
}
