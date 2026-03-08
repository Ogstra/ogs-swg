import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, UnifiedChartPoint, Consumer, TrafficStats } from '../../services/api'
import { ArrowDown, ArrowUp, Clock, RefreshCw, Shield } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, Tooltip, XAxis, YAxis } from 'recharts'
import { Card } from '../../components/ui/Card'
import { Button } from '../../components/ui/Button'
import { formatBytes } from '../../utils/traffic'

const DASHBOARD_PREF_KEY = 'dashboard_prefs'

type DashboardPrefs = {
    defaultService?: 'singbox' | 'wireguard'
    refreshMs?: number
    defaultRange?: string
}

const loadDashboardPrefs = (): DashboardPrefs => {
    try {
        const raw = localStorage.getItem(DASHBOARD_PREF_KEY)
        if (!raw) return {}
        return JSON.parse(raw)
    } catch {
        return {}
    }
}

// Custom SVG Icons
const SingBoxIcon = ({ className = "w-6 h-6" }: { className?: string }) => (
    <svg viewBox="0 0 1027 1109" className={className} fill="currentColor">
        <path d="M0 336L0 720.69C0 785.41 39.499 796.19 39.499 796.19L456.03 1076.62C520.66 1119.77 502.71 1026.29 502.71 1026.29L502.71 673.96L0 336Z" fillOpacity="0.7" />
        <path d="M1006 336L1006 720.69C1006 785.41 966.5 796.19 966.5 796.19L614.6 1033.48L550 1076.62C485.34 1119.77 503.29 1026.29 503.29 1026.29L503.29 673.96L1006 336Z" fillOpacity="0.8" />
        <path d="M549.71 13.473C520.96-4.491 477.85-4.491 452.7 13.473L21.557 308.07C-7.186 326.04-7.186 358.37 21.557 376.33L452.7 674.53C481.44 692.49 524.56 692.49 549.71 674.53L984.44 372.74C1013.19 354.78 1013.19 322.44 984.44 304.48L549.71 13.473Z" fillOpacity="0.9" />
        <path d="M503 1094C481.4 1094 467 1080.19 467 1062.92L467 676.08C467 658.82 481.4 645 503 645C524.6 645 539 658.82 539 676.08L539 1059.46C539 1080.19 524.6 1094 503 1094Z" fillOpacity="0.9" />
        <path d="M861.92 580.92C861.92 616.89 865.5 631.27 826.03 656.45L736.32 713.99C696.85 739.17 682.5 717.59 682.5 685.22L682.5 591.71C682.5 584.52 682.5 580.92 671.73 573.73C578.43 508.99 219.591 260.84 155 214.09L320.07 99C366.72 127.772 707.61 354.35 847.56 451.45C854.74 455.05 858.33 462.24 858.33 465.84L858.33 580.92Z" />
        <path d="M851.41 455.21C707.82 358.17 366.79 131.752 323.72 103L259.103 142.534L155 214.41C219.615 261.14 578.59 505.53 671.92 570.22C679.1 573.81 679.1 577.41 679.1 581L855 458.8C855 458.8 855 455.21 851.41 455.21Z" fillOpacity="0.95" />
        <path d="M862.9 580.48L862.9 469.19C862.9 462.01 859.3 458.42 852.11 454.83C708.28 357.89 366.67 131.721 323.51 103L248 153.26C370.26 235.83 697.49 451.24 783.79 512.27C794.58 519.45 794.58 526.63 794.58 530.22L794.58 681L830.54 655.87C866.5 630.74 862.9 612.79 862.9 580.48Z" fillOpacity="0.95" />
        <path d="M851.23 454.95C707.6 357.98 366.49 131.731 323.4 103L248 153.28C370.08 235.88 696.83 451.36 783.01 512.41L786.6 516L862 462.13C854.82 458.54 854.82 454.95 851.23 454.95Z" />
    </svg>
)

const WireGuardIcon = ({ className = "w-6 h-6" }: { className?: string }) => (
    <svg role="img" viewBox="0 0 24 24" className={className} fill="currentColor">
        <path d="M23.98 11.645S24.533 0 11.735 0C0.418 0 0.064 11.17 0.064 11.17S-1.6 24 11.997 24C25.04 24 23.98 11.645 23.98 11.645zM8.155 7.576c2.4 -1.47 5.469 -0.571 6.618 1.638 0.218 0.419 0.246 1.063 0.108 1.503 -0.477 1.516 -1.601 2.366 -3.145 2.728 0.455 -0.39 0.817 -0.832 0.933 -1.442a2.112 2.112 0 0 0 -0.364 -1.677 2.14 2.14 0 0 0 -2.465 -0.75c-0.95 0.36 -1.47 1.228 -1.377 2.294 0.087 0.99 0.839 1.632 2.245 1.876 -0.21 0.111 -0.372 0.193 -0.53 0.281a5.113 5.113 0 0 0 -1.644 1.43c-0.143 0.192 -0.24 0.208 -0.458 0.075 -2.827 -1.729 -3.009 -6.067 0.078 -7.956zM6.04 18.258c-0.455 0.116 -0.895 0.286 -1.359 0.438 0.227 -1.532 2.021 -2.943 3.539 -2.782a3.91 3.91 0 0 0 -0.74 2.072c-0.504 0.093 -0.98 0.155 -1.44 0.272zM15.703 3.3c0.448 0.017 0.898 0.01 1.347 0.02a2.324 2.324 0 0 1 0.334 0.047 3.249 3.249 0 0 1 -0.34 0.434c-0.16 0.15 -0.341 0.296 -0.573 0.069 -0.055 -0.055 -0.187 -0.042 -0.283 -0.044 -0.447 -0.005 -0.894 -0.02 -1.34 -0.003a8.323 8.323 0 0 0 -1.154 0.118c-0.072 0.013 -0.178 0.25 -0.146 0.338 0.078 0.207 0.191 0.435 0.359 0.567 0.619 0.49 1.277 0.928 1.9 1.413 0.604 0.472 1.167 0.99 1.51 1.7 0.446 0.928 0.46 1.9 0.267 2.877 -0.322 1.63 -1.147 2.98 -2.483 3.962 -0.538 0.395 -1.205 0.62 -1.821 0.903 -0.543 0.25 -1.1 0.465 -1.644 0.712 -0.98 0.446 -1.53 1.51 -1.369 2.615 0.149 1.015 1.04 1.862 2.059 2.037 1.223 0.21 2.486 -0.586 2.785 -1.83 0.336 -1.397 -0.423 -2.646 -1.845 -3.024l-0.256 -0.066c0.38 -0.17 0.708 -0.291 1.012 -0.458q0.793 -0.437 1.558 -0.925c0.15 -0.096 0.231 -0.096 0.36 0.014 0.977 0.846 1.56 1.898 1.724 3.187 0.27 2.135 -0.74 4.096 -2.646 5.101 -2.948 1.555 -6.557 -0.215 -7.208 -3.484 -0.558 -2.8 1.418 -5.34 3.797 -5.83 1.023 -0.211 1.958 -0.637 2.685 -1.425 0.47 -0.508 0.697 -0.944 0.775 -1.141a3.165 3.165 0 0 0 0.217 -1.158 2.71 2.71 0 0 0 -0.237 -0.992c-0.248 -0.566 -1.2 -1.466 -1.435 -1.656l-2.24 -1.754c-0.079 -0.065 -0.168 -0.06 -0.36 -0.047 -0.23 0.016 -0.815 0.048 -1.067 -0.018 0.204 -0.155 0.76 -0.38 1 -0.56 -0.726 -0.49 -1.554 -0.314 -2.315 -0.46 0.176 -0.328 1.046 -0.831 1.541 -0.888a7.323 7.323 0 0 0 -0.135 -0.822c-0.03 -0.111 -0.154 -0.22 -0.263 -0.283 -0.262 -0.154 -0.541 -0.281 -0.843 -0.434a1.755 1.755 0 0 1 0.906 -0.28 3.385 3.385 0 0 1 0.908 0.088c0.54 0.123 0.97 0.042 1.399 -0.324 -0.338 -0.136 -0.676 -0.26 -1.003 -0.407a9.843 9.843 0 0 1 -0.942 -0.493c0.85 0.118 1.671 0.437 2.54 0.32l0.022 -0.118 -2.018 -0.47c1.203 -0.11 2.323 -0.128 3.384 0.388 0.299 0.146 0.61 0.266 0.897 0.432 0.14 0.08 0.233 0.24 0.348 0.365 0.09 0.098 0.164 0.23 0.276 0.29 0.424 0.225 0.89 0.234 1.366 0.223l0.01 -0.16c0.479 0.15 1.017 0.702 1.017 1.105 -0.776 0 -1.55 -0.003 -2.325 0.004 -0.083 0 -0.165 0.061 -0.247 0.094 0.078 0.046 0.155 0.128 0.235 0.131zm-1 -1.147a0.118 0.118 0 0 0 -0.016 0.19 0.179 0.179 0 0 0 0.246 0.065c0.075 -0.038 0.148 -0.078 0.238 -0.125 -0.072 -0.062 -0.13 -0.114 -0.19 -0.163 -0.106 -0.087 -0.193 -0.032 -0.278 0.033z" />
    </svg>
)

export default function Dashboard() {
    const [timeRange, setTimeRange] = useState('24h')
    const [chartMode, setChartMode] = useState<'singbox' | 'wireguard'>('singbox')
    const [refreshInterval, setRefreshInterval] = useState<number>(10000)
    const [prefsLoaded, setPrefsLoaded] = useState(false)

    const now = new Date()
    const today = now.toISOString().split('T')[0]
    const [customStart, setCustomStart] = useState(today)
    const [customEnd, setCustomEnd] = useState(today)
    const chartContainerRef = useRef<HTMLDivElement | null>(null)
    const [chartSize, setChartSize] = useState({ width: 0, height: 300 })

    const computeRangeSeconds = (range: string) => {
        const nowSec = Math.floor(Date.now() / 1000)
        const day = 24 * 60 * 60
        switch (range) {
            case '30m': return { start: nowSec - 30 * 60, end: nowSec }
            case '1h': return { start: nowSec - 60 * 60, end: nowSec }
            case '6h': return { start: nowSec - 6 * 60 * 60, end: nowSec }
            case '24h': return { start: nowSec - day, end: nowSec }
            case '1w': return { start: nowSec - 7 * day, end: nowSec }
            case '1m': return { start: nowSec - 30 * day, end: nowSec }
            default: return { start: nowSec - day, end: nowSec }
        }
    }

    useEffect(() => {
        const prefs = loadDashboardPrefs()
        if (prefs.defaultService === 'wireguard') setChartMode('wireguard')
        if (prefs.defaultRange) setTimeRange(prefs.defaultRange)
        if (prefs.refreshMs && prefs.refreshMs >= 1000) setRefreshInterval(prefs.refreshMs)
        setPrefsLoaded(true)
    }, [])

    useEffect(() => {
        const el = chartContainerRef.current
        if (!el || typeof ResizeObserver === 'undefined') return
        const update = () => {
            const nextWidth = Math.max(0, Math.floor(el.clientWidth))
            const nextHeight = Math.max(300, Math.floor(el.clientHeight))
            setChartSize({ width: nextWidth, height: nextHeight })
        }
        update()
        const ro = new ResizeObserver(update)
        ro.observe(el)
        return () => ro.disconnect()
    }, [])

    const requestWindow = useMemo(() => {
        if (timeRange === 'custom') {
            const start = Math.floor(new Date(customStart).getTime() / 1000)
            const end = Math.floor(new Date(customEnd).getTime() / 1000) + 24 * 60 * 60
            return { start, end, startText: String(start), endText: String(end), range: 'custom' as const }
        }
        const range = computeRangeSeconds(timeRange)
        return { start: range.start, end: range.end, startText: String(range.start), endText: String(range.end), range: timeRange }
    }, [timeRange, customStart, customEnd])

    const dashboardQuery = useQuery({
        queryKey: ['dashboard-data', requestWindow.range, requestWindow.startText, requestWindow.endText],
        queryFn: () => api.getDashboardData(requestWindow.range, requestWindow.startText, requestWindow.endText),
        enabled: prefsLoaded,
        refetchInterval: refreshInterval,
        placeholderData: previousData => previousData,
    })

    const loading = dashboardQuery.isFetching
    const lastUpdated = dashboardQuery.dataUpdatedAt ? new Date(dashboardQuery.dataUpdatedAt) : new Date()
    const chartData: UnifiedChartPoint[] = dashboardQuery.data?.chart_data || []
    const chartDomain: [number, number] = useMemo(() => {
        if (chartData.length > 1) {
            const first = chartData[0]?.ts ?? requestWindow.start
            const last = chartData[chartData.length - 1]?.ts ?? requestWindow.end
            if (last > first) return [first, last]
            return [first, first + 1]
        }
        return [requestWindow.start, requestWindow.end]
    }, [chartData, requestWindow.start, requestWindow.end])
    const statsCards: Record<string, TrafficStats> = dashboardQuery.data?.stats_cards || {
        singbox: { uplink: 0, downlink: 0 },
        wireguard: { uplink: 0, downlink: 0 }
    }
    const topConsumersMap: Record<string, Consumer[]> = dashboardQuery.data?.top_consumers || {
        singbox: [],
        wireguard: []
    }
    const status = dashboardQuery.data?.status || {
        singbox: null,
        wireguard: null,
        active_users_singbox: 0,
        active_users_wireguard: 0,
        active_users_singbox_list: [],
        active_users_wireguard_list: []
    }
    const singboxPendingChanges = !!dashboardQuery.data?.singbox_pending_changes

    const handleApplySingboxChanges = async () => {
        try {
            await api.applySingboxChanges()
            await dashboardQuery.refetch()
        } catch (err) {
            console.error('Failed to apply Sing-box changes:', err)
            alert('Failed to apply changes. Please try again.')
        }
    }

    // Derived values for UI
    const topConsumers = (topConsumersMap[chartMode] || []).slice(0, 20)

    return (
        <div className="space-y-4 sm:space-y-6 pb-4 sm:pb-0">
            {/* Pending Changes Banner */}
            {singboxPendingChanges && (
                <div className="bg-yellow-900/20 border border-yellow-600/50 rounded-lg p-4 flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <Shield className="text-yellow-500" size={20} />
                        <div>
                            <p className="text-sm font-medium text-yellow-200">Sing-box Configuration Changes Pending</p>
                            <p className="text-xs text-yellow-300/70 mt-0.5">Changes have been saved but not yet applied. Click "Apply Changes" to restart the service.</p>
                        </div>
                    </div>
                    <Button
                        onClick={handleApplySingboxChanges}
                        variant="primary"
                        size="sm"
                        className="whitespace-nowrap bg-yellow-600 hover:bg-yellow-700 text-white"
                    >
                        Apply Changes
                    </Button>
                </div>
            )}

            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-0 sm:gap-4">
                <div className="hidden sm:block">
                    <h1 className="text-2xl font-bold text-white">Dashboard</h1>
                    <div className="flex items-center gap-2 mt-1">
                        <span className="text-slate-500 text-xs">Updated {lastUpdated.toLocaleTimeString()}</span>
                    </div>
                </div>
                <div className="w-full sm:w-auto flex items-center gap-3 flex-wrap justify-end">
                    <div className="flex items-center gap-2 bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 shadow-sm">
                        <Clock size={14} className="text-slate-500" />
                        <select
                            value={timeRange}
                            onChange={(e) => setTimeRange(e.target.value)}
                            className="select-field bg-transparent text-xs text-slate-300 border-none focus:ring-0 p-1 outline-none w-28 font-medium cursor-pointer"
                        >
                            <option value="30m">Last 30 Minutes</option>
                            <option value="1h">Last Hour</option>
                            <option value="6h">Last 6 Hours</option>
                            <option value="24h">Last 24 Hours</option>
                            <option value="1w">Last Week</option>
                            <option value="1m">Last Month</option>
                            <option value="custom">Custom Range</option>
                        </select>
                    </div>

                    {timeRange === 'custom' && (
                        <div className="flex items-center gap-2 bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 shadow-sm">
                            <input
                                type="date"
                                value={customStart}
                                onChange={e => setCustomStart(e.target.value)}
                                className="bg-transparent text-xs text-slate-300 border-none outline-none focus:ring-0 cursor-pointer"
                            />
                            <span className="text-slate-500">-</span>
                            <input
                                type="date"
                                value={customEnd}
                                onChange={e => setCustomEnd(e.target.value)}
                                className="bg-transparent text-xs text-slate-300 border-none outline-none focus:ring-0 cursor-pointer"
                            />
                        </div>
                    )}

                    <Button
                        onClick={() => dashboardQuery.refetch()}
                        isLoading={loading}
                        variant="icon"
                        size="icon"
                        icon={<RefreshCw size={16} />}
                    />
                </div>
            </div>

            {/* Unified Service & Traffic Cards */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 sm:gap-6">
                {/* sing-box Card */}
                <Card className={`transition-all ${status.singbox === true ? 'bg-slate-900 border-slate-800' : status.singbox === false ? 'bg-red-900/10 border-red-900/20' : 'bg-slate-900 border-slate-800'}`}>
                    <div className="flex items-start justify-between mb-6">
                        <div className="flex items-center gap-4">
                            <div className={`p-3 rounded-xl ${status.singbox === true ? 'bg-slate-800 text-emerald-400' : status.singbox === false ? 'bg-red-500/10 text-red-400' : 'bg-slate-800 text-slate-400'}`}>
                                <SingBoxIcon className="w-6 h-6" />
                            </div>
                            <div>
                                <div className="flex items-center gap-2">
                                    <h3 className="text-lg font-bold text-white">sing-box</h3>
                                    <span className={`w-2 h-2 rounded-full ${status.singbox === true ? 'bg-emerald-500' : status.singbox === false ? 'bg-red-500' : 'bg-slate-500'}`}></span>
                                </div>
                            </div>
                        </div>
                        <div className="text-right">
                            <p className="text-xs text-slate-400 uppercase tracking-wider font-semibold">Total Traffic</p>
                            <p className="text-2xl font-bold text-white mt-1">{formatBytes(statsCards.singbox.uplink + statsCards.singbox.downlink)}</p>
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4 mb-6">
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-blue-400 text-xs font-medium mb-1">
                                <ArrowDown size={12} /> Received
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(statsCards.singbox.uplink)}</p>
                        </div>
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-emerald-400 text-xs font-medium mb-1">
                                <ArrowUp size={12} /> Sent
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(statsCards.singbox.downlink)}</p>
                        </div>
                    </div>

                    <div className="border-t border-slate-800 pt-4">
                        <div className="flex items-center justify-between mb-3">
                            <p className="text-sm font-semibold text-slate-300">Active Sessions</p>
                            <span className="text-xs font-mono bg-slate-800 text-white px-2 py-0.5 rounded-md border border-slate-700">
                                {status.active_users_singbox} clients
                            </span>
                        </div>
                        {status.active_users_singbox_list && status.active_users_singbox_list.length > 0 && (
                            <div className="flex flex-wrap gap-2 mt-2">
                                {status.active_users_singbox_list.map((user: string, idx: number) => (
                                    <span key={idx} className="text-[10px] font-medium bg-emerald-500/10 text-emerald-400 px-2 py-0.5 rounded border border-emerald-500/20">
                                        {user}
                                    </span>
                                ))}
                            </div>
                        )}
                    </div>
                </Card>

                {/* WireGuard Card */}
                <Card className={`transition-all ${status.wireguard === true ? 'bg-slate-900 border-slate-800' : status.wireguard === false ? 'bg-red-900/10 border-red-900/20' : 'bg-slate-900 border-slate-800'}`}>
                    <div className="flex items-start justify-between mb-6">
                        <div className="flex items-center gap-4">
                            <div className={`p-3 rounded-xl ${status.wireguard === true ? 'bg-slate-800 text-blue-400' : status.wireguard === false ? 'bg-red-500/10 text-red-400' : 'bg-slate-800 text-slate-400'}`}>
                                <WireGuardIcon className="w-6 h-6" />
                            </div>
                            <div>
                                <div className="flex items-center gap-2">
                                    <h3 className="text-lg font-bold text-white">WireGuard</h3>
                                    <span className={`w-2 h-2 rounded-full ${status.wireguard === true ? 'bg-emerald-500' : status.wireguard === false ? 'bg-red-500' : 'bg-slate-500'}`}></span>
                                </div>
                            </div>
                        </div>
                        <div className="text-right">
                            <p className="text-xs text-slate-400 uppercase tracking-wider font-semibold">Total Traffic</p>
                            <p className="text-2xl font-bold text-white mt-1">{formatBytes(statsCards.wireguard.uplink + statsCards.wireguard.downlink)}</p>
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4 mb-6">
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-blue-400 text-xs font-medium mb-1">
                                <ArrowDown size={12} /> Received
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(statsCards.wireguard.downlink)}</p>
                        </div>
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-emerald-400 text-xs font-medium mb-1">
                                <ArrowUp size={12} /> Sent
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(statsCards.wireguard.uplink)}</p>
                        </div>
                    </div>

                    <div className="border-t border-slate-800 pt-4">
                        <div className="flex items-center justify-between mb-3">
                            <p className="text-sm font-semibold text-slate-300">Active Peers</p>
                            <span className="text-xs font-mono bg-slate-800 text-white px-2 py-0.5 rounded-md border border-slate-700">
                                {status.active_users_wireguard} peers
                            </span>
                        </div>
                        {status.active_users_wireguard_list && status.active_users_wireguard_list.length > 0 && (
                            <div className="flex flex-wrap gap-2 mt-2">
                                {status.active_users_wireguard_list.map((peer: string, idx: number) => (
                                    <span key={idx} className="text-[10px] font-medium bg-blue-500/10 text-blue-400 px-2 py-0.5 rounded border border-blue-500/20">
                                        {peer}
                                    </span>
                                ))}
                            </div>
                        )}
                    </div>
                </Card>
            </div>


            <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 sm:gap-6">
                {/* Traffic Chart */}
                <div className="lg:col-span-2">
                    <Card
                        title="Traffic Overview"
                        className="h-full"
                        action={
                            <div className="flex bg-slate-950 p-0.5 rounded-lg border border-slate-800">
                                <button
                                    onClick={() => setChartMode('singbox')}
                                    className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${chartMode === 'singbox' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-300'}`}
                                >
                                    Sing-Box
                                </button>
                                <button
                                    onClick={() => setChartMode('wireguard')}
                                    className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${chartMode === 'wireguard' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-300'}`}
                                >
                                    WireGuard
                                </button>
                            </div>
                        }
                    >
                        <div ref={chartContainerRef} className="h-[300px] min-h-[300px] w-full min-w-0 mt-4">
                            {chartSize.width > 0 && (
                                <AreaChart
                                    width={chartSize.width}
                                    height={chartSize.height}
                                    data={chartData}
                                    margin={{ top: 0, right: 0, bottom: 0, left: 0 }}
                                >
                                    <defs>
                                        <linearGradient id="colorUp" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                                            <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
                                        </linearGradient>
                                        <linearGradient id="colorDown" x1="0" y1="0" x2="0" y2="1">
                                            <stop offset="5%" stopColor="#10b981" stopOpacity={0.3} />
                                            <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
                                        </linearGradient>
                                    </defs>
                                    <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" vertical={false} />
                                    <XAxis
                                        dataKey="ts"
                                        tickFormatter={(ts) => {
                                            const date = new Date(ts * 1000)
                                            if (['30m', '1h', '6h', '24h'].includes(timeRange)) {
                                                return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                                            }
                                            return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
                                        }}
                                        stroke="#64748b"
                                        fontSize={12}
                                        tickMargin={10}
                                        axisLine={false}
                                        tickLine={false}
                                        type="number"
                                        domain={chartDomain}
                                    />
                                    <YAxis
                                        tickFormatter={(bytes) => formatBytes(bytes, 0)}
                                        stroke="#64748b"
                                        fontSize={12}
                                        width={40}
                                        tickMargin={10}
                                        axisLine={false}
                                        tickLine={false}
                                    />
                                    <Tooltip
                                        contentStyle={{ backgroundColor: '#0f172a', borderColor: '#1e293b', borderRadius: '0.75rem', color: '#f8fafc' }}
                                        itemStyle={{ color: '#f8fafc' }}
                                        formatter={(val) => formatBytes(typeof val === 'number' ? val : 0)}
                                        labelFormatter={(ts) => new Date(ts * 1000).toLocaleString()}
                                    />
                                    <Area
                                        type="monotone"
                                        dataKey={chartMode === 'singbox' ? "up_sb" : "up_wg"}
                                        name={chartMode === 'singbox' ? "Received" : "Sent"}
                                        stroke={chartMode === 'singbox' ? "#3b82f6" : "#10b981"}
                                        strokeWidth={2}
                                        isAnimationActive={false}
                                        fillOpacity={1}
                                        fill={chartMode === 'singbox' ? "url(#colorUp)" : "url(#colorDown)"}
                                    />
                                    <Area
                                        type="monotone"
                                        dataKey={chartMode === 'singbox' ? "down_sb" : "down_wg"}
                                        name={chartMode === 'singbox' ? "Sent" : "Received"}
                                        stroke={chartMode === 'singbox' ? "#10b981" : "#3b82f6"}
                                        strokeWidth={2}
                                        isAnimationActive={false}
                                        fillOpacity={1}
                                        fill={chartMode === 'singbox' ? "url(#colorDown)" : "url(#colorUp)"}
                                    />
                                </AreaChart>
                            )}
                        </div>
                    </Card>
                </div>

                {/* Top Consumers */}
                <Card
                    title="Top Consumers"
                    className="h-full"
                    action={
                        <div className="flex flex-wrap bg-slate-950 p-0.5 rounded-lg border border-slate-800">
                            <button
                                onClick={() => setChartMode('singbox')}
                                className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${chartMode === 'singbox' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-300'}`}
                            >
                                Sing-Box
                            </button>
                            <button
                                onClick={() => setChartMode('wireguard')}
                                className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${chartMode === 'wireguard' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-300'}`}
                            >
                                WireGuard
                            </button>
                        </div>
                    }
                >
                    <div className="space-y-4 mt-2 max-h-72 overflow-y-auto">
                        {topConsumers.length === 0 ? (
                            <div className="text-center text-slate-500 py-8 text-sm italic">No active users</div>
                        ) : (
                            topConsumers.map((u, i) => (
                                <div key={u.key || u.name} className="flex-wrap flex items-center justify-between p-3 bg-slate-950/50 rounded-lg border border-slate-800/50 hover:border-slate-800 transition-colors">
                                    <div className="flex items-center gap-3">
                                        <div className={`w-8 h-8 rounded-full flex items-center justify-center font-bold text-xs ${i === 0 ? 'bg-yellow-500/20 text-yellow-400' : 'bg-slate-800 text-slate-400'}`}>
                                            {i + 1}
                                        </div>
                                        <div>
                                            <div className="font-medium text-slate-200 text-sm">{u.name}</div>
                                            <div className="text-[10px] text-slate-500">
                                                {chartMode === 'wireguard' ? 'Interface' : 'Inbound'}:{' '}
                                                {chartMode === 'wireguard'
                                                    ? (u.interface_name || (u.flow || '').replace(/^wireguard:/i, '').trim() || 'Default')
                                                    : (u.inbound_tags && u.inbound_tags.length > 0
                                                        ? u.inbound_tags.join(', ')
                                                        : (u.flow || 'Default'))}
                                            </div>
                                        </div>
                                    </div>
                                    <div className="text-right">
                                        <div className="font-mono text-sm text-blue-400">{formatBytes(u.total)}</div>
                                    </div>
                                </div>
                            ))
                        )}
                    </div>
                </Card>
            </div>
        </div >
    )
}
