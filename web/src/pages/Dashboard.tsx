import { Activity, AlertCircle, CheckCircle2, Clock } from "lucide-react";
import { useEffect, useState } from "react";
import { getRobots, getJobs } from "../api";
import { Robot, Job } from "../types";

export function Dashboard() {
    const [stats, setStats] = useState({
        activeRobots: 0,
        totalRobots: 0,
        pendingJobs: 0,
        successRate: 100,
        issues: 0,
    });

    useEffect(() => {
        Promise.all([getRobots(), getJobs()]).then(([robots, jobs]) => {
            const active = robots.filter((r) => r.status !== "offline" && r.status !== "unknown").length;
            const pending = jobs.filter((j) => j.status === "pending" || j.status === "queued").length;
            const completed = jobs.filter((j) => j.status === "completed").length;
            const totalJobs = jobs.length;
            const rate = totalJobs > 0 ? Math.round((completed / totalJobs) * 100) : 100;

            // simplistic issue detection
            const issues = robots.filter(r => r.status === 'error').length;

            setStats({
                activeRobots: active,
                totalRobots: robots.length,
                pendingJobs: pending,
                successRate: rate,
                issues,
            });
        });
    }, []);

    return (
        <div className="space-y-6">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">Mission Control</h1>
                <p className="text-gray-500">Fleet status overview</p>
            </div>

            {/* Stats Grid */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                <StatCard
                    title="Active Robots"
                    value={`${stats.activeRobots}/${stats.totalRobots}`}
                    icon={Activity}
                    trend={stats.activeRobots === stats.totalRobots ? "All systems go" : `${stats.totalRobots - stats.activeRobots} offline`}
                    trendColor={stats.activeRobots === stats.totalRobots ? "text-green-600" : "text-orange-600"}
                />
                <StatCard
                    title="Success Rate"
                    value={`${stats.successRate}%`}
                    icon={CheckCircle2}
                    trend="Lifetime average"
                    trendColor="text-gray-500"
                />
                <StatCard
                    title="Pending Jobs"
                    value={stats.pendingJobs}
                    icon={Clock}
                    trend="In queue"
                    trendColor="text-blue-600"
                />
                <StatCard
                    title="Issues"
                    value={stats.issues}
                    icon={AlertCircle}
                    trend={stats.issues === 0 ? "No active alerts" : "Requires attention"}
                    trendColor={stats.issues === 0 ? "text-green-600" : "text-red-600"}
                />
            </div>

            {/* Recent Activity Placeholder */}
            <div className="bg-white rounded-xl border border-gray-200 p-6">
                <h2 className="text-lg font-semibold mb-4">Recent Activity</h2>
                <div className="text-center py-12 text-gray-500">
                    Activity feed coming soon...
                </div>
            </div>
        </div>
    );
}

function StatCard({ title, value, icon: Icon, trend, trendColor }: any) {
    return (
        <div className="bg-white p-6 rounded-xl border border-gray-200 shadow-sm">
            <div className="flex items-center justify-between mb-4">
                <span className="text-gray-500 text-sm font-medium">{title}</span>
                <div className="p-2 bg-gray-50 rounded-lg">
                    <Icon size={20} className="text-gray-700" />
                </div>
            </div>
            <div className="text-2xl font-bold text-gray-900 mb-1">{value}</div>
            <div className={`text-xs font-medium ${trendColor}`}>{trend}</div>
        </div>
    );
}
