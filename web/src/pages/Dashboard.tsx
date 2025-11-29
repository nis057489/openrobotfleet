import { Activity, AlertCircle, CheckCircle2, Clock, Laptop, Bot, FileText, GraduationCap, Disc, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { getRobots, getScenarios, getSemesterStatus, getBuildStatus } from "../api";
import { Link } from "react-router-dom";

export function Dashboard() {
    const [stats, setStats] = useState({
        robots: { total: 0, active: 0 },
        laptops: { total: 0, active: 0 },
        scenarios: 0,
        semester: { active: false, progress: "0/0" },
        goldenImage: { status: "idle" },
    });
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        Promise.all([
            getRobots(),
            getScenarios(),
            getSemesterStatus(),
            getBuildStatus()
        ]).then(([robots, scenarios, semester, build]) => {
            const robotList = robots.filter(r => r.type !== 'laptop');
            const laptopList = robots.filter(r => r.type === 'laptop');

            const activeRobots = robotList.filter(r => r.status !== "offline" && r.status !== "unknown").length;
            const activeLaptops = laptopList.filter(r => r.status !== "offline" && r.status !== "unknown").length;

            setStats({
                robots: { total: robotList.length, active: activeRobots },
                laptops: { total: laptopList.length, active: activeLaptops },
                scenarios: scenarios.length,
                semester: {
                    active: semester.active,
                    progress: `${semester.completed}/${semester.total}`
                },
                goldenImage: { status: build.status || "idle" }
            });
            setLoading(false);
        }).catch(console.error);
    }, []);

    if (loading) {
        return <div className="p-8 text-center text-gray-500">Loading mission control...</div>;
    }

    return (
        <div className="space-y-6">
            <div>
                <h1 className="text-2xl font-bold text-gray-900">Mission Control</h1>
                <p className="text-gray-500">Fleet status overview</p>
            </div>

            {/* Main Stats */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <StatCard
                    title="Robots"
                    value={`${stats.robots.active}/${stats.robots.total}`}
                    icon={Bot}
                    trend={stats.robots.active === stats.robots.total ? "All systems go" : `${stats.robots.total - stats.robots.active} offline`}
                    trendColor={stats.robots.active === stats.robots.total ? "text-green-600" : "text-orange-600"}
                    to="/robots"
                />
                <StatCard
                    title="Laptops"
                    value={`${stats.laptops.active}/${stats.laptops.total}`}
                    icon={Laptop}
                    trend={stats.laptops.active === stats.laptops.total ? "All connected" : `${stats.laptops.total - stats.laptops.active} offline`}
                    trendColor="text-blue-600"
                    to="/laptops"
                />
                <StatCard
                    title="Scenarios"
                    value={stats.scenarios}
                    icon={FileText}
                    trend="Available configurations"
                    trendColor="text-gray-500"
                    to="/scenarios"
                />
            </div>

            {/* Feature Status */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <FeatureCard
                    title="Semester Wizard"
                    description={stats.semester.active ? "Batch operation in progress" : "Ready to start new semester"}
                    status={stats.semester.active ? "Active" : "Idle"}
                    icon={GraduationCap}
                    to="/semester"
                    extra={stats.semester.active ? `Progress: ${stats.semester.progress}` : undefined}
                />
                <FeatureCard
                    title="Golden Image"
                    description={stats.goldenImage.status === 'building' ? "Building new image..." : "Manage OS images"}
                    status={stats.goldenImage.status === 'building' ? "Building" : "Ready"}
                    icon={Disc}
                    to="/golden-image"
                    loading={stats.goldenImage.status === 'building'}
                />
            </div>
        </div>
    );
}

function StatCard({ title, value, icon: Icon, trend, trendColor, to }: any) {
    return (
        <Link to={to} className="block group">
            <div className="bg-white p-6 rounded-xl border border-gray-200 shadow-sm hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between mb-4">
                    <span className="text-gray-500 text-sm font-medium">{title}</span>
                    <div className="p-2 bg-gray-50 rounded-lg group-hover:bg-gray-100 transition-colors">
                        <Icon size={20} className="text-gray-700" />
                    </div>
                </div>
                <div className="text-2xl font-bold text-gray-900 mb-1">{value}</div>
                <div className={`text-xs font-medium ${trendColor}`}>{trend}</div>
            </div>
        </Link>
    );
}

function FeatureCard({ title, description, status, icon: Icon, to, extra, loading }: any) {
    return (
        <Link to={to} className="block group">
            <div className="bg-white p-6 rounded-xl border border-gray-200 shadow-sm hover:shadow-md transition-shadow h-full">
                <div className="flex items-start justify-between">
                    <div className="flex items-center gap-3">
                        <div className="p-2 bg-blue-50 rounded-lg text-blue-600">
                            {loading ? <Loader2 size={24} className="animate-spin" /> : <Icon size={24} />}
                        </div>
                        <div>
                            <h3 className="font-semibold text-gray-900">{title}</h3>
                            <p className="text-sm text-gray-500">{description}</p>
                        </div>
                    </div>
                    <span className={`px-2.5 py-0.5 rounded-full text-xs font-medium ${
                        status === 'Active' || status === 'Building' 
                            ? 'bg-blue-100 text-blue-800' 
                            : 'bg-gray-100 text-gray-800'
                    }`}>
                        {status}
                    </span>
                </div>
                {extra && (
                    <div className="mt-4 pt-4 border-t border-gray-100">
                        <div className="text-sm font-medium text-gray-900">{extra}</div>
                    </div>
                )}
            </div>
        </Link>
    );
}
