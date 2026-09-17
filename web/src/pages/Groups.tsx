import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Network, Plus, Trash2, Edit2, RefreshCw, Download, X, Save } from "lucide-react";
import { getGroups, getRobots, createGroup, updateGroup, deleteGroup, applyGroup, downloadRvizLauncher, GroupPayload } from "../api";
import { Group, Robot } from "../types";
import { useNotification } from "../contexts/NotificationContext";

const emptyForm: GroupPayload = {
    name: "",
    ros_domain_id: 0,
    robot_id: null,
    laptop_id: null,
    static_peers: false,
    notes: "",
};

export function Groups() {
    const { t } = useTranslation();
    const { success, error } = useNotification();
    const [groups, setGroups] = useState<Group[]>([]);
    const [robots, setRobots] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [showForm, setShowForm] = useState(false);
    const [editingId, setEditingId] = useState<number | null>(null);
    const [form, setForm] = useState<GroupPayload>(emptyForm);
    const [saving, setSaving] = useState(false);
    const [applyingId, setApplyingId] = useState<number | null>(null);

    const load = () => {
        setLoading(true);
        Promise.all([getGroups(), getRobots()])
            .then(([g, r]) => {
                setGroups(g);
                setRobots(r);
            })
            .catch((err) => error(err instanceof Error ? err.message : String(err)))
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        load();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const robotOptions = robots.filter(r => r.type !== 'laptop');
    const laptopOptions = robots.filter(r => r.type === 'laptop');
    const nameFor = (id?: number | null) => robots.find(r => r.id === id)?.name || t("groups.unassigned");

    const openCreate = () => {
        setEditingId(null);
        setForm(emptyForm);
        setShowForm(true);
    };

    const openEdit = (g: Group) => {
        setEditingId(g.id);
        setForm({
            name: g.name,
            ros_domain_id: g.ros_domain_id,
            robot_id: g.robot_id ?? null,
            laptop_id: g.laptop_id ?? null,
            static_peers: g.static_peers,
            notes: g.notes || "",
        });
        setShowForm(true);
    };

    const reportApply = (applied: string[], skipped: string[]) => {
        if (skipped.length === 0) {
            success(t("groups.applied"));
        } else {
            success(t("groups.applyPartial", { skipped: skipped.join("; ") }));
        }
        void applied;
    };

    const handleSave = async () => {
        if (!form.name.trim()) return;
        setSaving(true);
        try {
            const result = editingId
                ? await updateGroup(editingId, form)
                : await createGroup(form);
            setShowForm(false);
            load();
            reportApply(result.applied, result.skipped);
        } catch (err) {
            error(err instanceof Error ? err.message : t("groups.saveFailed"));
        } finally {
            setSaving(false);
        }
    };

    const handleDelete = async (id: number) => {
        if (!confirm(t("groups.deleteConfirm"))) return;
        try {
            await deleteGroup(id);
            load();
        } catch (err) {
            error(err instanceof Error ? err.message : t("groups.saveFailed"));
        }
    };

    const handleApply = async (id: number) => {
        setApplyingId(id);
        try {
            const result = await applyGroup(id);
            reportApply(result.applied, result.skipped);
        } catch (err) {
            error(err instanceof Error ? err.message : t("groups.applyFailed"));
        } finally {
            setApplyingId(null);
        }
    };

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-gray-900">{t("groups.title")}</h1>
                    <p className="text-gray-500 max-w-2xl">{t("groups.subtitle")}</p>
                </div>
                <div className="flex gap-2">
                    <button
                        onClick={() => downloadRvizLauncher()}
                        title={t("groups.downloadLauncherHelp") || ""}
                        className="bg-gray-100 text-gray-700 px-4 py-2 rounded-lg hover:bg-gray-200 transition-colors font-medium flex items-center gap-2 border border-gray-300"
                    >
                        <Download size={18} /> {t("groups.downloadLauncher")}
                    </button>
                    <button
                        onClick={openCreate}
                        className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors font-medium flex items-center gap-2"
                    >
                        <Plus size={18} /> {t("groups.newGroup")}
                    </button>
                </div>
            </div>

            {loading ? (
                <div className="text-center py-12 text-gray-500">{t("groups.loading")}</div>
            ) : groups.length === 0 ? (
                <div className="bg-white rounded-xl border border-gray-200 p-12 text-center">
                    <div className="w-16 h-16 bg-blue-50 rounded-full flex items-center justify-center mx-auto mb-4">
                        <Network size={32} className="text-blue-600" />
                    </div>
                    <h3 className="text-lg font-semibold text-gray-900 mb-2">{t("groups.emptyTitle")}</h3>
                    <p className="text-gray-500 max-w-md mx-auto mb-6">{t("groups.emptyDescription")}</p>
                    <button onClick={openCreate} className="text-blue-600 font-medium hover:text-blue-700">
                        {t("groups.createButton")}
                    </button>
                </div>
            ) : (
                <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
                    <table className="w-full text-sm">
                        <thead className="bg-gray-50 text-gray-500 text-xs uppercase">
                            <tr>
                                <th className="text-left px-4 py-3">{t("groups.name")}</th>
                                <th className="text-left px-4 py-3">{t("groups.domainId")}</th>
                                <th className="text-left px-4 py-3">{t("groups.robot")}</th>
                                <th className="text-left px-4 py-3">{t("groups.laptop")}</th>
                                <th className="text-left px-4 py-3">{t("groups.staticPeers")}</th>
                                <th className="text-right px-4 py-3"></th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-gray-100">
                            {groups.map(g => (
                                <tr key={g.id} className="hover:bg-gray-50">
                                    <td className="px-4 py-3 font-medium text-gray-900">{g.name}</td>
                                    <td className="px-4 py-3 font-mono text-gray-700">{g.ros_domain_id}</td>
                                    <td className="px-4 py-3 text-gray-600">{nameFor(g.robot_id)}</td>
                                    <td className="px-4 py-3 text-gray-600">{nameFor(g.laptop_id)}</td>
                                    <td className="px-4 py-3 text-gray-600">{g.static_peers ? "✓" : "—"}</td>
                                    <td className="px-4 py-3">
                                        <div className="flex items-center justify-end gap-1">
                                            <button
                                                onClick={() => handleApply(g.id)}
                                                disabled={applyingId === g.id}
                                                title={t("groups.applyTitle") || ""}
                                                className="p-2 hover:bg-blue-50 text-blue-600 rounded-lg transition-colors disabled:opacity-50"
                                            >
                                                <RefreshCw size={16} className={applyingId === g.id ? "animate-spin" : ""} />
                                            </button>
                                            <button
                                                onClick={() => openEdit(g)}
                                                className="p-2 hover:bg-gray-100 text-gray-600 rounded-lg transition-colors"
                                            >
                                                <Edit2 size={16} />
                                            </button>
                                            <button
                                                onClick={() => handleDelete(g.id)}
                                                className="p-2 hover:bg-red-50 text-red-600 rounded-lg transition-colors"
                                            >
                                                <Trash2 size={16} />
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {showForm && (
                <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
                    <div className="bg-white rounded-xl max-w-md w-full p-6 space-y-4">
                        <div className="flex items-center justify-between">
                            <h3 className="text-lg font-semibold text-gray-900">
                                {editingId ? t("groups.edit") : t("groups.newGroup")}
                            </h3>
                            <button onClick={() => setShowForm(false)} className="p-1 text-gray-400 hover:text-gray-600">
                                <X size={20} />
                            </button>
                        </div>

                        <div>
                            <label className="block text-xs font-medium text-gray-700 mb-1">{t("groups.name")}</label>
                            <input
                                autoFocus
                                value={form.name}
                                onChange={e => setForm({ ...form, name: e.target.value })}
                                placeholder={t("groups.namePlaceholder") || ""}
                                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                            />
                        </div>

                        <div>
                            <label className="block text-xs font-medium text-gray-700 mb-1">{t("groups.domainId")}</label>
                            <input
                                type="number"
                                value={form.ros_domain_id || ""}
                                onChange={e => setForm({ ...form, ros_domain_id: parseInt(e.target.value) || 0 })}
                                placeholder={t("groups.domainIdHelp") || ""}
                                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                            />
                            <p className="text-xs text-gray-500 mt-1">{t("groups.domainIdHelp")}</p>
                        </div>

                        <div>
                            <label className="block text-xs font-medium text-gray-700 mb-1">{t("groups.robot")}</label>
                            <select
                                value={form.robot_id ?? ""}
                                onChange={e => setForm({ ...form, robot_id: e.target.value ? parseInt(e.target.value) : null })}
                                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                            >
                                <option value="">{t("groups.selectRobot")}</option>
                                {robotOptions.map(r => (
                                    <option key={r.id} value={r.id}>{r.name}</option>
                                ))}
                            </select>
                        </div>

                        <div>
                            <label className="block text-xs font-medium text-gray-700 mb-1">{t("groups.laptop")}</label>
                            <select
                                value={form.laptop_id ?? ""}
                                onChange={e => setForm({ ...form, laptop_id: e.target.value ? parseInt(e.target.value) : null })}
                                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
                            >
                                <option value="">{t("groups.selectLaptop")}</option>
                                {laptopOptions.map(r => (
                                    <option key={r.id} value={r.id}>{r.name}</option>
                                ))}
                            </select>
                        </div>

                        <label className="flex items-start gap-2">
                            <input
                                type="checkbox"
                                checked={form.static_peers}
                                onChange={e => setForm({ ...form, static_peers: e.target.checked })}
                                className="mt-1"
                            />
                            <span>
                                <span className="block text-sm font-medium text-gray-900">{t("groups.staticPeers")}</span>
                                <span className="block text-xs text-gray-500">{t("groups.staticPeersHelp")}</span>
                            </span>
                        </label>

                        <div className="flex justify-end gap-2 pt-2">
                            <button
                                onClick={() => setShowForm(false)}
                                className="px-4 py-2 rounded-lg text-gray-700 hover:bg-gray-100"
                            >
                                {t("groups.cancel")}
                            </button>
                            <button
                                onClick={handleSave}
                                disabled={saving || !form.name.trim()}
                                className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700 transition-colors flex items-center gap-2 disabled:opacity-50"
                            >
                                <Save size={16} /> {t("groups.save")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
