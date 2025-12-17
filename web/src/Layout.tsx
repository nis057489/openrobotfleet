import { LayoutDashboard, Bot, Laptop, FileCode, Settings, Menu, GraduationCap, Disc, Languages, X } from "lucide-react";
import { useState, useEffect } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { InterestSignup } from "./components/InterestSignup";
import { useTranslation } from "react-i18next";

function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

export function Layout() {
    const [sidebarOpen, setSidebarOpen] = useState(true);
    const [mobileOpen, setMobileOpen] = useState(false);
    const location = useLocation();
    const { t, i18n } = useTranslation();

    // Close mobile menu on route change
    useEffect(() => {
        setMobileOpen(false);
    }, [location.pathname]);

    const navItems = [
        { icon: LayoutDashboard, label: t("common.dashboard"), path: "/" },
        { icon: Bot, label: t("common.robots"), path: "/robots" },
        { icon: Laptop, label: t("common.laptops"), path: "/laptops" },
        { icon: FileCode, label: t("common.scenarios"), path: "/scenarios" },
        { icon: GraduationCap, label: t("common.semesterWizard"), path: "/semester-wizard" },
        { icon: Disc, label: t("common.goldenImage"), path: "/golden-image" },
        { icon: Settings, label: t("common.settings"), path: "/settings" },
    ];

    const toggleLanguage = () => {
        const newLang = i18n.language.startsWith('zh') ? 'en' : 'zh';
        i18n.changeLanguage(newLang);
    };

    const isExpanded = sidebarOpen || mobileOpen;

    return (
        <div className="flex h-screen bg-gray-50 text-gray-900 font-sans overflow-hidden">
            {/* Mobile Header */}
            <div className="md:hidden fixed top-0 left-0 right-0 h-16 bg-white border-b border-gray-200 flex items-center px-4 z-30">
                <button
                    onClick={() => setMobileOpen(true)}
                    className="p-2 -ml-2 hover:bg-gray-100 rounded-lg text-gray-600"
                >
                    <Menu size={24} />
                </button>
                <span className="ml-3 font-bold text-lg text-blue-600">
                    OpenRobotFleet
                </span>
            </div>

            {/* Mobile Sidebar Overlay */}
            {mobileOpen && (
                <div
                    className="fixed inset-0 bg-black/50 z-40 md:hidden"
                    onClick={() => setMobileOpen(false)}
                />
            )}

            {/* Sidebar */}
            <aside
                className={cn(
                    "fixed md:relative z-50 h-full bg-white border-r border-gray-200 transition-all duration-300 flex flex-col",
                    // Mobile: fixed width, transform to hide/show
                    "w-64 -translate-x-full md:translate-x-0",
                    mobileOpen && "translate-x-0",
                    // Desktop: variable width based on sidebarOpen
                    sidebarOpen ? "md:w-64" : "md:w-20"
                )}
            >
                <div className="h-16 flex items-center px-4 border-b border-gray-100 justify-between">
                    <div className="flex items-center">
                        {/* Desktop Toggle */}
                        <button
                            onClick={() => setSidebarOpen(!sidebarOpen)}
                            className="hidden md:block p-2 hover:bg-gray-100 rounded-lg text-gray-600"
                        >
                            <Menu size={20} />
                        </button>

                        {/* Mobile Close */}
                        <button
                            onClick={() => setMobileOpen(false)}
                            className="md:hidden p-2 -ml-2 hover:bg-gray-100 rounded-lg text-gray-600"
                        >
                            <X size={20} />
                        </button>

                        {isExpanded && (
                            <span className="ml-3 font-bold text-lg text-blue-600">
                                OpenRobotFleet
                            </span>
                        )}
                    </div>
                </div>

                <nav className="flex-1 p-4 space-y-2 overflow-y-auto">
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
                                <item.icon size={20} className="shrink-0" />
                                {isExpanded && <span className="ml-3 font-medium">{item.label}</span>}
                            </Link>
                        );
                    })}
                </nav>

                <div className="p-4 border-t border-gray-100">
                    {isExpanded && (
                        <div className="mb-4 pb-4 border-b border-gray-100">
                            <InterestSignup compact={true} />
                        </div>
                    )}

                    <button
                        onClick={toggleLanguage}
                        className="flex items-center w-full p-2 mb-2 text-gray-600 hover:bg-gray-100 rounded-lg transition-colors"
                    >
                        <Languages size={20} className="shrink-0" />
                        {isExpanded && (
                            <span className="ml-3 text-sm font-medium">
                                {i18n.language.startsWith('zh') ? '中文' : 'English'}
                            </span>
                        )}
                    </button>

                    <div className="flex items-center gap-3 overflow-hidden">
                        <div className="w-8 h-8 rounded-full bg-blue-100 flex items-center justify-center text-blue-600 font-bold text-xs shrink-0">
                            OP
                        </div>
                        {isExpanded && (
                            <div className="overflow-hidden">
                                <p className="text-sm font-medium truncate">{t("common.operator")}</p>
                                <p className="text-xs text-gray-500 truncate">admin@fleet.local</p>
                            </div>
                        )}
                    </div>
                </div>
            </aside>

            {/* Main Content */}
            <main className="flex-1 overflow-auto pt-16 md:pt-0 w-full">
                <div className="p-4 md:p-8 max-w-7xl mx-auto">
                    <Outlet />
                </div>
            </main>
        </div>
    );
}
