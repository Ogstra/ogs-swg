import React, { useState } from 'react';
import { useLocation, Link, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { LayoutDashboard, Users, Shield, Activity, Settings, Menu, LogOut, FileJson } from 'lucide-react';

export const Layout: React.FC = () => {
    const [sidebarOpen, setSidebarOpen] = useState(false);
    const location = useLocation();
    const navigate = useNavigate();
    const { logout } = useAuth();

    // Helper to check active route
    const isActive = (path: string) => location.pathname === path;

    const navItems = [
        { path: '/', label: 'Dashboard', icon: LayoutDashboard },
        { path: '/users', label: 'Users', icon: Users },
        { path: '/wireguard', label: 'WireGuard', icon: Shield },
        { path: '/logs', label: 'System Logs', icon: Activity },
        { path: '/raw-config', label: 'Raw Config', icon: FileJson },
        { path: '/settings', label: 'Settings', icon: Settings },
    ];

    const activeLabel = navItems.find(n => n.path === location.pathname)?.label || 'OGS-SWG';

    const handleLogout = () => {
        logout();
        navigate('/login');
    };

    return (
        <div className="h-screen bg-slate-950 text-slate-100 flex font-sans overflow-hidden">
            {/* Mobile Sidebar Overlay */}
            {sidebarOpen && (
                <div
                    className="fixed inset-0 bg-black/50 z-40 lg:hidden transition-opacity"
                    onClick={() => setSidebarOpen(false)}
                />
            )}

            {/* Sidebar */}
            <aside className={`
                fixed lg:static inset-y-0 left-0 z-50 w-64 bg-slate-900 border-r border-slate-800
                transform transition-transform duration-200 flex flex-col
                ${sidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
            `}>
                <div className="h-16 flex items-center px-6 border-b border-slate-800 shrink-0">
                    <span className="font-bold text-xl tracking-tight text-white">OGS-SWG</span>
                </div>

                <nav className="p-4 space-y-1 flex-1 overflow-y-auto">
                    {navItems.map(item => (
                        <Link
                            key={item.path}
                            to={item.path}
                            onClick={() => setSidebarOpen(false)}
                            className={`
                                w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium transition-colors
                                ${isActive(item.path)
                                    ? 'bg-slate-800 text-white'
                                    : 'text-slate-400 hover:text-slate-100 hover:bg-slate-800'}
                            `}
                        >
                            <item.icon size={18} className={isActive(item.path) ? 'text-blue-500' : 'text-slate-400'} />
                            {item.label}
                        </Link>
                    ))}
                </nav>

                <div className="p-4 border-t border-slate-800">
                    <button
                        onClick={handleLogout}
                        className="w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium text-red-400 hover:bg-red-500/10 transition-colors"
                    >
                        <LogOut size={18} />
                        Sign Out
                    </button>
                </div>
            </aside>

            {/* Main Content */}
            <div className="flex-1 flex flex-col min-w-0 h-full">
                {/* Top Header (Mobile only mostly) */}
                <header className="h-16 bg-slate-900 border-b border-slate-800 flex items-center justify-between px-4 lg:hidden sticky top-0 z-30 shrink-0">
                    <button
                        onClick={() => setSidebarOpen(true)}
                        className="p-2 text-slate-400 hover:text-white"
                        aria-label="Open navigation"
                    >
                        <Menu size={24} />
                    </button>
                    <span className="font-bold text-lg">{activeLabel}</span>
                    <div className="w-8"></div> {/* Spacer for center alignment */}
                </header>

                <main className="flex-1 overflow-y-auto p-4 lg:p-8 scroll-smooth">
                    <div className="max-w-7xl mx-auto">
                        <Outlet />
                    </div>
                </main>
            </div>
        </div>
    );
};
