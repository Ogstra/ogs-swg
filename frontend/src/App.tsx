import { useEffect, useMemo, useState } from 'react'
import Dashboard from './components/Dashboard'
import LogViewer from './components/LogViewer'
import UserManagement from './components/UserManagement'
import WireGuard from './components/WireGuard'
import SettingsPage from './components/Settings'
import RawConfig from './components/RawConfig'
import { LayoutDashboard, Users, Settings, Activity, Menu, Shield, Code } from 'lucide-react'

function App() {
    const [sidebarOpen, setSidebarOpen] = useState(false)
    const validTabs = useMemo(() => new Set(['dashboard', 'users', 'wireguard', 'logs', 'settings', 'raw-config']), [])

    const getTabFromUrl = (): string => {
        const hash = window.location.hash.replace('#', '')
        if (hash && validTabs.has(hash)) return hash
        return 'dashboard'
    }

    const [activeTab, setActiveTab] = useState<string>(getTabFromUrl)

    useEffect(() => {
        const tab = getTabFromUrl()
        if (tab !== activeTab) setActiveTab(tab)
        const onPop = () => {
            const t = getTabFromUrl()
            if (t !== activeTab) setActiveTab(t)
        }
        window.addEventListener('popstate', onPop)
        return () => window.removeEventListener('popstate', onPop)
    }, [activeTab, validTabs])

    const updateUrlTab = (tab: string) => {
        const url = new URL(window.location.href)
        url.hash = tab
        url.searchParams.delete('tab')
        window.history.pushState({}, '', url.toString())
    }

    const navItems = [
        { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
        { id: 'users', label: 'Users', icon: Users },
        { id: 'wireguard', label: 'WireGuard', icon: Shield },
        { id: 'logs', label: 'System Logs', icon: Activity },
        { id: 'raw-config', label: 'Raw Config', icon: Code },
        { id: 'settings', label: 'Settings', icon: Settings },
    ]

    const activeLabel = navItems.find(n => n.id === activeTab)?.label || 'OGS-SWG'

    return (
        <div className="h-screen bg-slate-950 text-slate-100 flex font-sans overflow-hidden">
            {/* Mobile Sidebar Overlay */}
            {sidebarOpen && (
                <div
                    className="fixed inset-0 bg-black/50 z-40 lg:hidden backdrop-blur-sm"
                    onClick={() => setSidebarOpen(false)}
                />
            )}

            {/* Sidebar */}
            <aside className={`
                fixed lg:static inset-y-0 left-0 z-50 w-64 bg-slate-900 border-r border-slate-800 
                transform transition-transform duration-200 ease-in-out flex flex-col
                ${sidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
            `}>
                <div className="h-16 flex items-center px-6 border-b border-slate-800 shrink-0">
                    <div className="w-8 h-8 bg-blue-600 rounded-lg flex items-center justify-center font-bold text-white mr-3 shadow-lg shadow-blue-900/20">
                        O
                    </div>
                    <span className="font-bold text-xl tracking-tight text-white">OGS-SWG</span>
                </div>

                <nav className="p-4 space-y-1 flex-1 overflow-y-auto">
                    {navItems.map(item => (
                        <button
                            key={item.id}
                            onClick={() => {
                                setActiveTab(item.id)
                                updateUrlTab(item.id)
                                setSidebarOpen(false)
                            }}
                            className={`
                                w-full flex items-center gap-3 px-4 py-3 rounded-lg text-sm font-medium transition-all duration-200
                                ${activeTab === item.id
                                    ? 'bg-blue-600 text-white shadow-md shadow-blue-900/20'
                                    : 'text-slate-400 hover:bg-slate-800 hover:text-slate-100'}
                            `}
                        >
                            <item.icon size={18} />
                            {item.label}
                        </button>
                    ))}
                </nav>

                <div className="p-4 border-t border-slate-800 shrink-0">
                    <div className="flex items-center gap-3 px-2">
                        <div className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></div>
                        <div className="text-xs text-slate-400">
                            <p className="font-medium text-slate-300">System Online</p>
                            <p>v1.0.0</p>
                        </div>
                    </div>
                </div>
            </aside>

            {/* Main Content */}
            <div className="flex-1 flex flex-col min-w-0 h-full">
                {/* Top Header (Mobile only mostly) */}
                <header className="h-16 bg-slate-900/50 border-b border-slate-800 flex items-center justify-between px-4 lg:hidden backdrop-blur-md sticky top-0 z-30 shrink-0">
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
                        {activeTab === 'dashboard' && <Dashboard />}
                        {activeTab === 'users' && <UserManagement />}
                        {activeTab === 'wireguard' && <WireGuard />}
                        {activeTab === 'logs' && <LogViewer />}
                        {activeTab === 'raw-config' && <RawConfig />}
                        {activeTab === 'settings' && <SettingsPage />}
                    </div>
                </main>
            </div>
        </div>
    )
}

export default App
