import { useState } from "react";
import { useTranslation } from "react-i18next";
import { broadcastCommand } from "../api";
import { X, Power, RefreshCw, Bell, AlertTriangle, Loader2 } from "lucide-react";
import { useNotification } from "../contexts/NotificationContext";

interface FleetActionsModalProps {
    onClose: () => void;
}

type ActionType = "restart_ros" | "reboot" | "class_over" | null;

export function FleetActionsModal({ onClose }: FleetActionsModalProps) {
    const { t } = useTranslation();
    const { success, error } = useNotification();
    const [selectedAction, setSelectedAction] = useState<ActionType>(null);
    const [confirmText, setConfirmText] = useState("");
    const [processing, setProcessing] = useState(false);

    const handleAction = async () => {
        if (!selectedAction) return;

        setProcessing(true);
        try {
            if (selectedAction === "class_over") {
                await broadcastCommand({ type: "stop", data: {} });
                await broadcastCommand({ type: "identify", data: {} });
                success(t("settings.classOverSent"));
            } else {
                await broadcastCommand({ type: selectedAction, data: {} });
                success(t("settings.broadcastSent", { type: selectedAction }));
            }
            onClose();
        } catch (err) {
            error(t("settings.broadcastError"));
        } finally {
            setProcessing(false);
        }
    };

    const renderConfirmation = () => {
        if (!selectedAction) return null;

        const isReboot = selectedAction === "reboot";
        const confirmKeyword = "REBOOT";

        return (
            <div className="space-y-4">
                <div className={`p-4 rounded-lg border ${isReboot ? "bg-red-50 border-red-200" : "bg-yellow-50 border-yellow-200"}`}>
                    <div className="flex items-start gap-3">
                        <AlertTriangle className={`shrink-0 ${isReboot ? "text-red-600" : "text-yellow-600"}`} size={20} />
                        <div>
                            <h3 className={`font-medium ${isReboot ? "text-red-900" : "text-yellow-900"}`}>
                                {t(`settings.${selectedAction === "class_over" ? "classOver" : selectedAction === "restart_ros" ? "restartRos" : "rebootFleet"}`)}
                            </h3>
                            <p className={`text-sm mt-1 ${isReboot ? "text-red-700" : "text-yellow-700"}`}>
                                {t(`settings.${selectedAction === "class_over" ? "classOverDesc" : selectedAction === "restart_ros" ? "restartRosDesc" : "rebootFleetDesc"}`)}
                            </p>
                        </div>
                    </div>
                </div>

                {isReboot && (
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                            Type <span className="font-mono font-bold text-red-600">{confirmKeyword}</span> to confirm
                        </label>
                        <input
                            type="text"
                            value={confirmText}
                            onChange={(e) => setConfirmText(e.target.value)}
                            className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-red-500 focus:border-red-500"
                            placeholder={confirmKeyword}
                        />
                    </div>
                )}

                <div className="flex gap-3 justify-end mt-6">
                    <button
                        onClick={() => {
                            setSelectedAction(null);
                            setConfirmText("");
                        }}
                        className="px-4 py-2 text-gray-600 hover:bg-gray-100 rounded-lg transition-colors"
                    >
                        {t("common.cancel")}
                    </button>
                    <button
                        onClick={handleAction}
                        disabled={processing || (isReboot && confirmText !== confirmKeyword)}
                        className={`flex items-center gap-2 px-4 py-2 rounded-lg text-white transition-colors ${isReboot
                                ? "bg-red-600 hover:bg-red-700 disabled:bg-red-300"
                                : "bg-yellow-600 hover:bg-yellow-700 disabled:bg-yellow-300"
                            }`}
                    >
                        {processing && <Loader2 className="animate-spin" size={18} />}
                        {t("common.confirm")}
                    </button>
                </div>
            </div>
        );
    };

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
            <div className="bg-white rounded-xl shadow-xl max-w-lg w-full overflow-hidden flex flex-col">
                <div className="p-6 border-b border-gray-100 flex items-center justify-between">
                    <h2 className="text-xl font-bold text-gray-900">
                        {selectedAction ? t("common.confirmAction") : t("settings.fleetMaintenance")}
                    </h2>
                    <button onClick={onClose} className="text-gray-400 hover:text-gray-600">
                        <X size={24} />
                    </button>
                </div>

                <div className="p-6">
                    {selectedAction ? (
                        renderConfirmation()
                    ) : (
                        <div className="grid gap-4">
                            <button
                                onClick={() => setSelectedAction("class_over")}
                                className="flex items-center justify-between p-4 border border-yellow-200 rounded-lg hover:bg-yellow-50 transition-colors text-left group"
                            >
                                <div>
                                    <div className="font-medium text-yellow-900 group-hover:text-yellow-800">{t("settings.classOver")}</div>
                                    <div className="text-xs text-yellow-600 group-hover:text-yellow-700">{t("settings.classOverDesc")}</div>
                                </div>
                                <Bell size={20} className="text-yellow-500 group-hover:text-yellow-700" />
                            </button>

                            <button
                                onClick={() => setSelectedAction("restart_ros")}
                                className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors text-left"
                            >
                                <div>
                                    <div className="font-medium text-gray-900">{t("settings.restartRos")}</div>
                                    <div className="text-xs text-gray-500">{t("settings.restartRosDesc")}</div>
                                </div>
                                <RefreshCw size={20} className="text-gray-400" />
                            </button>

                            <button
                                onClick={() => setSelectedAction("reboot")}
                                className="flex items-center justify-between p-4 border border-red-200 rounded-lg hover:bg-red-50 transition-colors text-left group"
                            >
                                <div>
                                    <div className="font-medium text-red-900 group-hover:text-red-800">{t("settings.rebootFleet")}</div>
                                    <div className="text-xs text-red-600 group-hover:text-red-700">{t("settings.rebootFleetDesc")}</div>
                                </div>
                                <Power size={20} className="text-red-500 group-hover:text-red-700" />
                            </button>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
