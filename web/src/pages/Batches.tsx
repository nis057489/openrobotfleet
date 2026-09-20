import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { getBatches, cancelBatch, getRobots, BatchRun } from "../api";
import { Robot } from "../types";
import { formatDistanceToNow } from "date-fns";
import { zhCN } from "date-fns/locale";
import { Check, RefreshCw, XCircle, Clock, Ban, Layers } from "lucide-react";

function StateIcon({ state }: { state: string }) {
    if (state === "success") return <Check className="text-green-500" size={18} />;
    if (state === "error") return <XCircle className="text-red-500" size={18} />;
    if (state === "cancelled") return <Ban className="text-gray-400" size={18} />;
    if (state === "pending") return <Clock className="text-gray-400" size={18} />;
    return <RefreshCw className="text-blue-500 animate-spin" size={18} />;
}

export function Batches() {
    const { t } = useTranslation();
    const [runs, setRuns] = useState<BatchRun[]>([]);
    const [robots, setRobots] = useState<Robot[]>([]);
    const [loading, setLoading] = useState(true);
    const [cancelling, setCancelling] = useState<string | null>(null);

    useEffect(() => {
        let stopped = false;
        const poll = async () => {
            try {
                const [batchData, robotData] = await Promise.all([getBatches(), getRobots()]);
                if (stopped) return;
                setRuns(batchData.batches);
                setRobots(robotData);
            } catch (e) {
                console.error(e);
            } finally {
                if (!stopped) setLoading(false);
            }
        };
        poll();
        const interval = setInterval(poll, 2000);
        return () => {
            stopped = true;
            clearInterval(interval);
        };
    }, []);

    const nameFor = (id: string) => robots.find(r => r.id.toString() === id)?.name || `#${id}`;

    const handleCancel = async (id: string) => {
        setCancelling(id);
        try {
            await cancelBatch(id);
        } catch (err) {
            alert(t("batches.cancelFailed", { reason: err instanceof Error ? err.message : String(err) }));
        } finally {
            setCancelling(null);
        }
    };

    if (loading) return <div className="p-8">{t("common.loading")}</div>;

    const active = runs.filter(r => r.active);
    const finished = runs.filter(r => !r.active);

    return (
        <div className="max-w-4xl mx-auto space-y-8">
            <div>
                <h2 className="text-2xl font-bold text-gray-900">{t("batches.title")}</h2>
                <p className="text-gray-500 mt-1">{t("batches.subtitle")}</p>
            </div>

            {runs.length === 0 && (
                <div className="bg-white border border-gray-200 rounded-xl p-12 text-center">
                    <Layers className="mx-auto text-gray-300 mb-3" size={40} />
                    <p className="text-gray-500">{t("batches.empty")}</p>
                </div>
            )}

            {active.length > 0 && (
                <section className="space-y-4">
                    <h3 className="font-semibold text-gray-900">{t("batches.active")}</h3>
                    {active.map(run => (
                        <BatchCard
                            key={run.id}
                            run={run}
                            nameFor={nameFor}
                            onCancel={handleCancel}
                            cancelling={cancelling === run.id}
                        />
                    ))}
                </section>
            )}

            {finished.length > 0 && (
                <section className="space-y-4">
                    <h3 className="font-semibold text-gray-900">{t("batches.recent")}</h3>
                    {finished.map(run => (
                        <BatchCard
                            key={run.id}
                            run={run}
                            nameFor={nameFor}
                        />
                    ))}
                </section>
            )}
        </div>
    );
}

function BatchCard({
    run, nameFor, onCancel, cancelling,
}: {
    run: BatchRun;
    nameFor: (id: string) => string;
    onCancel?: (id: string) => void;
    cancelling?: boolean;
}) {
    const { t, i18n } = useTranslation();
    const locale = i18n.language;
    // Device states a batch reports, mapped to how they should read on screen.
    const stateLabel = (state: string) => {
        const key = `batches.state.${state}`;
        const translated = t(key);
        return translated === key ? state.replace(/_/g, " ") : translated;
    };
    const started = formatDistanceToNow(new Date(run.started_at), {
        addSuffix: true,
        locale: locale.startsWith("zh") ? zhCN : undefined,
    });
    const pct = run.total > 0 ? Math.round((run.completed / run.total) * 100) : 0;
    const ids = Object.keys(run.robots);

    return (
        <div className="bg-white border border-gray-200 rounded-xl overflow-hidden">
            <div className="p-5 flex items-start justify-between gap-4">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <h4 className="font-semibold text-gray-900 truncate">{run.label}</h4>
                        {run.cancelled && (
                            <span className="text-xs px-2 py-0.5 bg-gray-100 text-gray-600 rounded-full">
                                {t("batches.cancelled")}
                            </span>
                        )}
                    </div>
                    <p className="text-sm text-gray-500 mt-1">
                        {t("batches.progress", { completed: run.completed, total: run.total })} · {started}
                    </p>
                </div>
                <div className="flex items-center gap-3 shrink-0">
                    {run.active
                        ? <RefreshCw className="animate-spin text-blue-600" size={20} />
                        : <Check className="text-green-600" size={20} />}
                    {run.active && onCancel && (
                        <button
                            onClick={() => onCancel(run.id)}
                            disabled={cancelling || run.cancelled}
                            className="text-sm px-3 py-1.5 rounded-lg border border-red-200 text-red-600 hover:bg-red-50 disabled:opacity-50"
                        >
                            {cancelling ? t("batches.cancelling") : t("batches.cancel")}
                        </button>
                    )}
                </div>
            </div>

            <div className="h-1.5 bg-gray-100">
                <div className="h-full bg-blue-500 transition-all" style={{ width: `${pct}%` }} />
            </div>

            <div className="divide-y divide-gray-100">
                {ids.map(id => {
                    const state = run.robots[id];
                    return (
                        <div key={id} className="flex items-center gap-3 px-5 py-3">
                            <StateIcon state={state} />
                            <span className="font-medium text-gray-900 flex-1 truncate">{nameFor(id)}</span>
                            <span className="text-sm text-gray-600 capitalize">{stateLabel(state)}</span>
                            {run.errors[id] && (
                                <span className="text-sm text-red-600 max-w-xs truncate" title={run.errors[id]}>
                                    {run.errors[id]}
                                </span>
                            )}
                        </div>
                    );
                })}
            </div>
        </div>
    );
}
