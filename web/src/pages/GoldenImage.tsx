import React, { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { buildGoldenImage, getBuildStatus, getGoldenImageConfig, saveGoldenImageConfig } from "../api";
import { GoldenImageConfig } from "../types";
import { Save, Download, Wifi, Server, Radio, Hash, Loader2, HardDrive, ChevronDown, ChevronRight } from "lucide-react";

export function GoldenImage() {
    const { t } = useTranslation();
    const [config, setConfig] = useState<GoldenImageConfig>({
        wifi_ssid: "",
        wifi_password: "",
        controller_url: window.location.origin,
        mqtt_broker: "tcp://" + window.location.hostname + ":1883",
        lds_model: "LDS-02",
        ros_domain_id: 30,
        robot_model: "TB3",
        ros_version: "Humble"
    });
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);
    const [buildStatus, setBuildStatus] = useState<string>("idle");
    const [buildError, setBuildError] = useState<string | null>(null);
    const [buildProgress, setBuildProgress] = useState<number>(0);
    const [buildStep, setBuildStep] = useState<string>("");
    const [buildLogs, setBuildLogs] = useState<string[]>([]);
    const [buildImageName, setBuildImageName] = useState<string | null>(null);
    const [showLogs, setShowLogs] = useState(false);

    useEffect(() => {
        getGoldenImageConfig()
            .then(data => {
                if (data.config) {
                    // Ensure defaults are set if missing from DB
                    setConfig({
                        ...data.config,
                        robot_model: data.config.robot_model || "TB3",
                        ros_version: data.config.ros_version || "Humble"
                    });
                }
            })
            .catch(console.error)
            .finally(() => setLoading(false));

        // Check initial build status
        getBuildStatus().then(status => {
            setBuildStatus(status.status);
            if (status.error) setBuildError(status.error);
            if (status.progress) setBuildProgress(status.progress);
            if (status.step) setBuildStep(status.step);
            if (status.logs) setBuildLogs(status.logs);
            if (status.image_name) setBuildImageName(status.image_name);
        }).catch(console.error);
    }, []);

    useEffect(() => {
        let interval: ReturnType<typeof setInterval>;
        if (buildStatus === "building") {
            interval = setInterval(async () => {
                try {
                    const status = await getBuildStatus();
                    setBuildStatus(status.status);
                    if (status.error) setBuildError(status.error);
                    if (status.progress) setBuildProgress(status.progress);
                    if (status.step) setBuildStep(status.step);
                    if (status.logs) setBuildLogs(status.logs);
                    if (status.image_name) setBuildImageName(status.image_name);

                    if (status.status === "success" || status.status === "error") {
                        clearInterval(interval);
                    }
                } catch (e) {
                    console.error(e);
                }
            }, 2000);
        }
        return () => clearInterval(interval);
    }, [buildStatus]);

    const handleSave = async (e: React.FormEvent) => {
        e.preventDefault();
        setSaving(true);
        setMessage(null);
        try {
            await saveGoldenImageConfig(config);
            setMessage({ type: 'success', text: t("goldenImage.saveSuccess") });
        } catch (err) {
            setMessage({ type: 'error', text: err instanceof Error ? err.message : t("goldenImage.saveError") });
        } finally {
            setSaving(false);
        }
    };

    const handleDownload = () => {
        window.location.href = "/api/golden-image/download";
    };

    const handleBuild = async () => {
        try {
            // Save configuration before building to ensure backend has latest settings
            setSaving(true);
            await saveGoldenImageConfig(config);
            setSaving(false);

            await buildGoldenImage();
            setBuildStatus("building");
            setBuildError(null);
        } catch (err) {
            setSaving(false);
            setMessage({ type: 'error', text: t("goldenImage.startBuildFailed") });
        }
    };

    if (loading) return <div className="p-8">{t("common.loading")}</div>;

    return (
        <div className="max-w-3xl mx-auto">
            <div className="mb-8">
                <h1 className="text-2xl font-bold text-gray-900">{t("goldenImage.title")}</h1>
                <p className="text-gray-500">{t("goldenImage.explanation")}</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100 bg-gray-50">
                    <h3 className="font-semibold text-gray-900">{t("goldenImage.title")}</h3>
                    <p className="text-sm text-gray-500">
                        {t("goldenImage.step1")}
                    </p>
                </div>

                <form onSubmit={handleSave} className="p-6 space-y-6">
                    {message && (
                        <div className={`p-4 rounded-lg text-sm ${message.type === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>
                            {message.text}
                        </div>
                    )}

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                        {/* Robot & ROS */}
                        <div className="col-span-2">
                            <h4 className="text-sm font-medium text-gray-900 mb-4 flex items-center gap-2">
                                <Radio size={16} /> {t("goldenImage.targetHardware")}
                            </h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.robotModel")}</label>
                                    <select
                                        value={config.robot_model || "TB3"}
                                        onChange={e => setConfig({ ...config, robot_model: e.target.value })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    >
                                        <option value="TB3">Turtlebot 3</option>
                                        <option value="TB4">Turtlebot 4</option>
                                    </select>
                                </div>
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.rosVersion")}</label>
                                    <select
                                        value={config.ros_version || "Humble"}
                                        onChange={e => setConfig({ ...config, ros_version: e.target.value })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    >
                                        <option value="Humble">Humble (Ubuntu 22.04)</option>
                                        <option value="Jazzy">Jazzy (Ubuntu 24.04)</option>
                                    </select>
                                </div>
                            </div>
                        </div>

                        {/* WiFi */}
                        <div className="col-span-2">
                            <h4 className="text-sm font-medium text-gray-900 mb-4 flex items-center gap-2">
                                <Wifi size={16} /> {t("goldenImage.networkSettings")}
                            </h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.wifiSsid")}</label>
                                    <input
                                        required
                                        type="text"
                                        value={config.wifi_ssid}
                                        onChange={e => setConfig({ ...config, wifi_ssid: e.target.value })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                        placeholder="Lab-WiFi"
                                    />
                                </div>
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.wifiPassword")}</label>
                                    <input
                                        required
                                        type="password"
                                        value={config.wifi_password}
                                        onChange={e => setConfig({ ...config, wifi_password: e.target.value })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                        placeholder="********"
                                    />
                                </div>
                            </div>
                        </div>

                        {/* Controller */}
                        <div className="col-span-2">
                            <h4 className="text-sm font-medium text-gray-900 mb-4 flex items-center gap-2">
                                <Server size={16} /> {t("goldenImage.controllerConnection")}
                            </h4>
                            <div>
                                <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.controllerUrl")}</label>
                                <input
                                    required
                                    type="text"
                                    value={config.controller_url}
                                    onChange={e => setConfig({ ...config, controller_url: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="http://192.168.1.100:8080"
                                />
                                <p className="text-xs text-gray-500 mt-1">{t("goldenImage.controllerUrlHelp")}</p>
                            </div>
                            <div className="mt-4">
                                <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.mqttBroker")}</label>
                                <input
                                    required
                                    type="text"
                                    value={config.mqtt_broker}
                                    onChange={e => setConfig({ ...config, mqtt_broker: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="tcp://192.168.1.100:1883"
                                />
                                <p className="text-xs text-gray-500 mt-1">{t("goldenImage.mqttBrokerHelp")}</p>
                            </div>
                        </div>

                        {/* Robot Settings */}
                        <div className="col-span-2">
                            <h4 className="text-sm font-medium text-gray-900 mb-4 flex items-center gap-2">
                                <Radio size={16} /> {t("goldenImage.robotConfig")}
                            </h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">{t("goldenImage.ldsModel")}</label>
                                    <select
                                        value={config.lds_model}
                                        onChange={e => setConfig({ ...config, lds_model: e.target.value })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    >
                                        <option value="LDS-01">LDS-01</option>
                                        <option value="LDS-02">LDS-02</option>
                                    </select>
                                </div>
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1 flex items-center gap-1">
                                        {t("goldenImage.rosDomainId")} <Hash size={12} />
                                    </label>
                                    <input
                                        required
                                        type="number"
                                        value={config.ros_domain_id}
                                        onChange={e => setConfig({ ...config, ros_domain_id: parseInt(e.target.value) })}
                                        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    />
                                </div>
                            </div>
                        </div>
                    </div>

                    <div className="pt-4 border-t border-gray-100">
                        {buildStatus === "building" ? (
                            <div className="space-y-2">
                                <div className="flex justify-between text-sm text-gray-600">
                                    <span>{buildStep || t("goldenImage.building")}</span>
                                    <span>{buildProgress}%</span>
                                </div>
                                <div className="w-full bg-gray-200 rounded-full h-2.5">
                                    <div
                                        className="bg-purple-600 h-2.5 rounded-full transition-all duration-500"
                                        style={{ width: `${buildProgress}%` }}
                                    ></div>
                                </div>
                                <p className="text-xs text-gray-400 text-center">
                                    You can navigate away from this page. The build will continue in the background.
                                </p>
                            </div>
                        ) : (
                            <div className="flex items-center justify-between">
                                <button
                                    type="submit"
                                    disabled={saving}
                                    className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 transition-colors flex items-center gap-2"
                                >
                                    <Save size={18} />
                                    {saving ? t("goldenImage.saving") : t("goldenImage.saveConfig")}
                                </button>

                                <button
                                    type="button"
                                    onClick={handleBuild}
                                    className="bg-purple-600 text-white px-6 py-2 rounded-lg hover:bg-purple-700 transition-colors flex items-center gap-2"
                                >
                                    <HardDrive size={18} />
                                    {t("goldenImage.buildImage")}
                                </button>

                                <button
                                    type="button"
                                    onClick={handleDownload}
                                    className="bg-gray-100 text-gray-700 px-6 py-2 rounded-lg hover:bg-gray-200 transition-colors flex items-center gap-2 border border-gray-300"
                                >
                                    <Download size={18} />
                                    {t("goldenImage.downloadImage")}
                                </button>
                            </div>
                        )}
                    </div>

                    {/* Logs Section */}
                    {buildLogs.length > 0 && (
                        <div className="mt-4 border rounded-lg overflow-hidden">
                            <button
                                type="button"
                                onClick={() => setShowLogs(!showLogs)}
                                className="w-full flex items-center justify-between px-4 py-2 bg-gray-50 text-xs font-medium text-gray-700 hover:bg-gray-100"
                            >
                                <span>{t("goldenImage.buildLogs")}</span>
                                {showLogs ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                            </button>
                            {showLogs && (
                                <div className="bg-gray-900 text-gray-300 p-4 text-xs font-mono h-64 overflow-y-auto whitespace-pre-wrap">
                                    {buildLogs.join('\n')}
                                </div>
                            )}
                        </div>
                    )}

                    {buildStatus === "success" && (
                        <div className="mt-4 p-4 bg-green-50 text-green-700 rounded-lg text-sm flex items-center justify-between">
                            <span>Image built successfully! {buildImageName && <strong>{buildImageName}</strong>}</span>
                            <a href={`/images/${buildImageName || 'turtlebot-golden.img'}`} className="underline font-bold">Download Image</a>
                        </div>
                    )}
                    {buildStatus === "error" && (
                        <div className="mt-4 p-4 bg-red-50 text-red-700 rounded-lg text-sm">
                            Build failed: {buildError}
                        </div>
                    )}
                </form>
            </div>

            <div className="mt-8 bg-blue-50 border border-blue-100 rounded-xl p-6">
                <h3 className="font-semibold text-blue-900 mb-2">{t("goldenImage.howToUse")}</h3>
                <ol className="list-decimal list-inside space-y-2 text-sm text-blue-800">
                    <li>{t("goldenImage.step1")}</li>
                    <li>{t("goldenImage.step2")}</li>
                    <li>{t("goldenImage.step3")}</li>
                    <li>{t("goldenImage.step4")}</li>
                    <li>{t("goldenImage.step5")}</li>
                    <li>{t("goldenImage.step6")}</li>
                </ol>
            </div>
        </div>
    );
}
