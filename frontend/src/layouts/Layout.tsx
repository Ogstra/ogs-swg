import React, { useEffect, useState } from 'react';
import { useLocation, Link, Outlet, useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../context/AuthContext';
import { api } from '../services/api';
import { LayoutDashboard, Users, Shield, Activity, Settings, Menu, LogOut, FileJson, Link as LinkIcon, ChevronDown, ChevronRight, Server, Database, ShieldAlert, UserCog } from 'lucide-react';

export const Layout: React.FC = () => {
    const [sidebarOpen, setSidebarOpen] = useState(false);
    const location = useLocation();
    const [settingsOpen, setSettingsOpen] = useState(location.pathname === '/settings');
    const navigate = useNavigate();
    const { logout, permissions } = useAuth();

    const isActive = (path: string) => location.pathname === path;
    const wireGuardQueryIface = new URLSearchParams(location.search).get('iface') || '';
    const wireGuardActive = location.pathname === '/wireguard';
    const settingsActiveTab = new URLSearchParams(location.search).get('tab') || 'general';
    const settingsActive = location.pathname === '/settings';

    const interfacesQuery = useQuery({
        queryKey: ['layout-wireguard-interfaces'],
        queryFn: () => api.getWireGuardInterfaces(),
        enabled: !!permissions?.can_read_wireguard,
        refetchInterval: 30_000,
        placeholderData: previousData => previousData,
    });
    const wireGuardInterfaces = Array.isArray(interfacesQuery.data) ? (interfacesQuery.data as string[]) : [];

    const allNavItems = [
        { path: '/', label: 'Dashboard', icon: LayoutDashboard, permission: null },
        { path: '/users', label: 'sing-box Users', icon: Users, permission: 'can_read_users' as const },
        { path: '/subscriptions', label: 'Subscriptions', icon: LinkIcon, permission: 'can_read_users' as const },
        { path: '/wireguard', label: 'WireGuard', icon: Shield, permission: 'can_read_wireguard' as const },
        { path: '/logs', label: 'System Logs', icon: Activity, permission: 'can_read_logs' as const },
        { path: '/raw-config', label: 'Raw Config', icon: FileJson, permission: 'can_read_config' as const },
        { path: '/settings', label: 'Settings', icon: Settings, permission: 'can_read_settings' as const },
    ];

    const navItems = allNavItems.filter(item => {
        if (!item.permission) return true;
        return permissions?.[item.permission] === true;
    });
    const settingsSubtabs = [
        { id: 'general', label: 'General', icon: Settings, permission: 'can_read_settings' as const },
        { id: 'singbox', label: 'Sing-box', icon: Server, permission: 'can_read_config' as const },
        { id: 'wireguard-interfaces', label: 'WireGuard', icon: Shield, permission: 'can_read_wireguard' as const },
        { id: 'dashboard', label: 'Dashboard', icon: Settings, permission: 'can_read_settings' as const },
        { id: 'database', label: 'Database', icon: Database, permission: 'can_read_settings' as const },
        { id: 'security', label: 'Sub Security', icon: ShieldAlert, permission: 'can_read_settings' as const },
        { id: 'panel-users', label: 'Admins', icon: UserCog, permission: 'can_read_panel_users' as const },
    ].filter(item => permissions?.[item.permission] === true);

    const activeLabel = allNavItems.find(n => n.path === location.pathname)?.label || 'OGS-SWG';
    const rawCommit = (import.meta.env.VITE_APP_COMMIT as string | undefined) || '';
    const commitLabel = rawCommit ? rawCommit.slice(0, 7) : 'local';

    useEffect(() => {
        if (settingsActive) {
            setSettingsOpen(true);
        }
    }, [settingsActive]);

    const handleLogout = () => {
        logout();
        navigate('/login');
    };

    return (
        <div className="app-shell bg-slate-950 text-slate-100 flex font-sans overflow-hidden">
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
                    {navItems.map(item => {
                        const itemActive = isActive(item.path);
                        const isWireGuardItem = item.path === '/wireguard';
                        const isSettingsItem = item.path === '/settings';

                        return (
                            <div key={item.path} className="space-y-1">
                                {isSettingsItem ? (
                                    <>
                                        <button
                                            type="button"
                                            onClick={() => setSettingsOpen(prev => !prev)}
                                            className={`
                                                w-full flex items-center justify-between gap-3 px-4 py-3 rounded-lg text-sm font-medium transition-colors
                                                ${itemActive
                                                    ? 'bg-slate-800 text-white'
                                                    : 'text-slate-400 hover:text-slate-100 hover:bg-slate-800'}
                                            `}
                                        >
                                            <span className="flex items-center gap-3">
                                                <item.icon size={18} className={itemActive ? 'text-blue-500' : 'text-slate-400'} />
                                                {item.label}
                                            </span>
                                            {settingsOpen ? <ChevronDown size={16} className="text-slate-500" /> : <ChevronRight size={16} className="text-slate-500" />}
                                        </button>

                                        {settingsOpen && settingsSubtabs.length > 0 && (
                                            <div className="ml-6 space-y-0.5">
                                                {settingsSubtabs.map(subtab => {
                                                    const subtabActive = settingsActive && settingsActiveTab === subtab.id;
                                                    return (
                                                        <Link
                                                            key={subtab.id}
                                                            to={`/settings?tab=${encodeURIComponent(subtab.id)}`}
                                                            onClick={() => setSidebarOpen(false)}
                                                            className={`
                                                                w-full flex items-center gap-2 px-3 py-2 rounded-lg text-xs font-medium transition-colors
                                                                ${subtabActive
                                                                    ? 'bg-slate-800/60 text-white'
                                                                    : 'text-slate-500 hover:text-slate-300 hover:bg-slate-800/40'}
                                                            `}
                                                        >
                                                            <subtab.icon size={14} />
                                                            <span>{subtab.label}</span>
                                                        </Link>
                                                    );
                                                })}
                                            </div>
                                        )}
                                    </>
                                ) : (
                                    <Link
                                        to={item.path}
                                        onClick={() => setSidebarOpen(false)}
                                        className={`
                                            w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium transition-colors
                                            ${itemActive
                                                ? 'bg-slate-800 text-white'
                                                : 'text-slate-400 hover:text-slate-100 hover:bg-slate-800'}
                                        `}
                                    >
                                        <item.icon size={18} className={itemActive ? 'text-blue-500' : 'text-slate-400'} />
                                        {item.label}
                                    </Link>
                                )}

                                {isWireGuardItem && wireGuardInterfaces.length > 0 && (
                                    <div className="ml-6 space-y-0.5">
                                        {wireGuardInterfaces.map(iface => {
                                            const ifaceActive = wireGuardActive && wireGuardQueryIface === iface;
                                            return (
                                                <Link
                                                    key={iface}
                                                    to={`/wireguard?iface=${encodeURIComponent(iface)}`}
                                                    onClick={() => setSidebarOpen(false)}
                                                    className={`
                                                        w-full flex items-center gap-2 px-3 py-2 rounded-lg text-xs font-medium transition-colors
                                                        ${ifaceActive
                                                            ? 'bg-slate-800/60 text-white'
                                                            : 'text-slate-500 hover:text-slate-300 hover:bg-slate-800/40'}
                                                    `}
                                                >
                                                    <span className="font-mono">{iface}</span>
                                                </Link>
                                            );
                                        })}
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </nav>
                <div className="pb-2 pr-2 text-right text-[10px] text-slate-500">
                    {commitLabel}
                </div>
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
                    <div className="w-8"></div>
                </header>

                <main className="flex-1 min-h-0 overflow-y-auto p-4 lg:p-8 scroll-smooth">
                    <div className="max-w-7xl mx-auto h-full min-h-0 flex flex-col">
                        <Outlet />
                    </div>
                </main>
            </div>
        </div>
    );
};
