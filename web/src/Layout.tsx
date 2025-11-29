import { LayoutDashboard, Bot, Laptop, FileCode, Settings, Menu, GraduationCap, Disc } from "lucide-react";
import { useState } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

export function Layout() {
    const [sidebarOpen, setSidebarOpen] = useState(true);
    const location = useLocation();

    const navItems = [
        { icon: LayoutDashboard, label: "Dashboard", path: "/" },
        { icon: Bot, label: "Robots", path: "/robots" },
        { icon: Laptop, label: "Laptops", path: "/laptops" },
        { icon: FileCode, label: "Scenarios", path: "/scenarios" },
        { icon: GraduationCap, label: "Semester Wizard", path: "/semester-wizard" },
        { icon: Disc, label: "Golden Image", path: "/golden-image" },
        { icon: Settings, label: "Settings", path: "/settings" },
    ];

    return (
        <div className="flex h-screen bg-gray-50 text-gray-900 font-sans">
            {/* Sidebar */}
            <aside
                className={cn(
                    "bg-white border-r border-gray-200 transition-all duration-300 flex flex-col",
                    sidebarOpen ? "w-64" : "w-20"
                )}
            >
                <div className="h-16 flex items-center px-4 border-b border-gray-100">
                    <button
                        onClick={() => setSidebarOpen(!sidebarOpen)}
                        className="p-2 hover:bg-gray-100 rounded-lg text-gray-600"
                    >
                        <Menu size={20} />
                    </button>
                    {sidebarOpen && (
                        <span className="ml-3 font-bold text-lg text-blue-600">
                            TurtleFleet
                        </span>
                    )}
                </div>

                <nav className="flex-1 p-4 space-y-2">
                    {navItems.map((item) => {
                        const isActive = location.pathname === item.path;
                        return (
                            <Link
                                key={item.path}
                                to={item.path}
                                className={cn(
                                    "flex items-center px-3 py-3 rounded-lg transition-colors",
                                    isActive
                                        ? "bg-blue-50 text-blue-600"
                                        : "text-gray-600 hover:bg-gray-100 hover:text-gray-900"
                                )}
                            >
                                <item.icon size={20} />
                                {sidebarOpen && <span className="ml-3 font-medium">{item.label}</span>}
                            </Link>
                        );
                    })}
                </nav>

                <div className="p-4 border-t border-gray-100">
                    <div className="flex items-center gap-3">
                        <div className="w-8 h-8 rounded-full bg-blue-100 flex items-center justify-center text-blue-600 font-bold text-xs">
                            OP
                        </div>
                        {sidebarOpen && (
                            <div className="overflow-hidden">
                                <p className="text-sm font-medium truncate">Operator</p>
                                <p className="text-xs text-gray-500 truncate">admin@fleet.local</p>
                            </div>
                        )}
                    </div>
                </div>
            </aside>

            {/* Main Content */}
            <main className="flex-1 overflow-auto">
                <div className="p-8 max-w-7xl mx-auto">
                    <Outlet />
                </div>
            </main>
        </div>
    );
}
