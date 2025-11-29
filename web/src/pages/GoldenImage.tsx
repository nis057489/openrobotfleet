import React, { useState, useEffect } from "react";
import { buildGoldenImage, getBuildStatus, getGoldenImageConfig, saveGoldenImageConfig } from "../api";
import { GoldenImageConfig } from "../types";
import { Save, Download, Wifi, Server, Radio, Hash, Loader2, HardDrive, ChevronDown, ChevronRight } from "lucide-react";

export function GoldenImage() {
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
            setMessage({ type: 'success', text: "Configuration saved successfully" });
        } catch (err) {
            setMessage({ type: 'error', text: err instanceof Error ? err.message : "Failed to save configuration" });
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
            setMessage({ type: 'error', text: "Failed to start build" });
        }
    };

    if (loading) return <div className="p-8">Loading...</div>;

    return (
        <div className="max-w-3xl mx-auto">
            <div className="mb-8">
                <h1 className="text-2xl font-bold text-gray-900">Golden Image Generator</h1>
                <p className="text-gray-500">Create a Cloud-Init configuration to automatically provision new robots.</p>
            </div>

            <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                <div className="p-6 border-b border-gray-100 bg-gray-50">
                    <h3 className="font-semibold text-gray-900">Configuration</h3>
                    <p className="text-sm text-gray-500">
                        These settings will be baked into the <code>user-data</code> file.
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
                                <Radio size={16} /> Target Hardware
                            </h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">Robot Model</label>
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
                                    <label className="block text-xs font-medium text-gray-700 mb-1">ROS Version</label>
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
                                <Wifi size={16} /> Network Settings
                            </h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">WiFi SSID</label>
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
                                    <label className="block text-xs font-medium text-gray-700 mb-1">WiFi Password</label>
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
                                <Server size={16} /> Controller Connection
                            </h4>
                            <div>
                                <label className="block text-xs font-medium text-gray-700 mb-1">Controller URL</label>
                                <input
                                    required
                                    type="text"
                                    value={config.controller_url}
                                    onChange={e => setConfig({ ...config, controller_url: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="http://192.168.1.100:8080"
                                />
                                <p className="text-xs text-gray-500 mt-1">The URL where robots can reach this controller.</p>
                            </div>
                            <div className="mt-4">
                                <label className="block text-xs font-medium text-gray-700 mb-1">MQTT Broker</label>
                                <input
                                    required
                                    type="text"
                                    value={config.mqtt_broker}
                                    onChange={e => setConfig({ ...config, mqtt_broker: e.target.value })}
                                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="tcp://192.168.1.100:1883"
                                />
                                <p className="text-xs text-gray-500 mt-1">The MQTT broker address.</p>
                            </div>
                        </div>

                        {/* Robot Settings */}
                        <div className="col-span-2">
                            <h4 className="text-sm font-medium text-gray-900 mb-4 flex items-center gap-2">
                                <Radio size={16} /> Robot Configuration
                            </h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-xs font-medium text-gray-700 mb-1">LDS Model</label>
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
                                        ROS Domain ID <Hash size={12} />
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
                                    <span>{buildStep || "Building..."}</span>
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
                                    {saving ? "Saving..." : "Save Configuration"}
                                </button>

                                <button
                                    type="button"
                                    onClick={handleBuild}
                                    className="bg-purple-600 text-white px-6 py-2 rounded-lg hover:bg-purple-700 transition-colors flex items-center gap-2"
                                >
                                    <HardDrive size={18} />
                                    Build Disk Image
                                </button>

                                <button
                                    type="button"
                                    onClick={handleDownload}
                                    className="bg-gray-100 text-gray-700 px-6 py-2 rounded-lg hover:bg-gray-200 transition-colors flex items-center gap-2 border border-gray-300"
                                >
                                    <Download size={18} />
                                    Download user-data
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
                                <span>Build Logs</span>
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
                <h3 className="font-semibold text-blue-900 mb-2">How to use</h3>
                <ol className="list-decimal list-inside space-y-2 text-sm text-blue-800">
                    <li>Configure your WiFi and Controller settings above.</li>
                    <li>Click <strong>Build Disk Image</strong> and wait for the process to complete (approx. 20-30 mins).</li>
                    <li>Download the generated image file.</li>
                    <li>Flash the image to an SD card using <strong>Raspberry Pi Imager</strong> (use "Custom" image).</li>
                    <li>Insert the SD card into the robot and power it on.</li>
                    <li>The robot will automatically connect to WiFi, start the agent, and appear in the dashboard.</li>
                </ol>
            </div>
        </div>
    );
}
