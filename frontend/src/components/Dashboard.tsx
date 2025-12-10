import { useEffect, useState } from 'react'
import { api, UserStatus } from '../api'
import { ArrowDown, ArrowUp, Clock, RefreshCw, Shield, Zap } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Card } from './ui/Card'
import { Button } from './ui/Button'
import { Badge } from './ui/Badge'

function formatBytes(bytes: number, decimals = 2) {
    if (!+bytes) return '0 B'
    const k = 1024
    const dm = decimals < 0 ? 0 : decimals
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

type StatusState = {
    singbox: boolean
    wireguard: boolean
    active_users_singbox: number
    active_users_wireguard: number
    active_users_singbox_list: string[]
    active_users_wireguard_list: string[]
    samples_count: number
    db_size_bytes: number
    systemctl_available?: boolean
    journalctl_available?: boolean
}

export default function Dashboard() {
    const [users, setUsers] = useState<UserStatus[]>([])
    const [stats, setStats] = useState<any[]>([])
    const [status, setStatus] = useState<StatusState>({
        singbox: false,
        wireguard: false,
        active_users_singbox: 0,
        active_users_wireguard: 0,
        active_users_singbox_list: [] as string[],
        active_users_wireguard_list: [] as string[],
        samples_count: 0,
        db_size_bytes: 0,
        systemctl_available: true,
        journalctl_available: true,
    })
    const [loading, setLoading] = useState(true)
    const [lastUpdated, setLastUpdated] = useState<Date>(new Date())
    const [timeRange, setTimeRange] = useState('24h')
    const [wgTraffic, setWgTraffic] = useState<{ rx: number; tx: number }>({ rx: 0, tx: 0 })
    const [chartMode, setChartMode] = useState<'singbox' | 'wireguard'>('singbox')
    const [chartData, setChartData] = useState<any[]>([])

    // Custom Date Range
    const now = new Date()
    const today = now.toISOString().split('T')[0]
    const [customStart, setCustomStart] = useState(today)
    const [customEnd, setCustomEnd] = useState(today)

    const computeRangeSeconds = (range: string) => {
        const nowSec = Math.floor(Date.now() / 1000)
        const day = 24 * 60 * 60
        switch (range) {
            case '30m':
                return { start: nowSec - 30 * 60, end: nowSec }
            case '1h':
                return { start: nowSec - 60 * 60, end: nowSec }
            case '6h':
                return { start: nowSec - 6 * 60 * 60, end: nowSec }
            case '24h':
                return { start: nowSec - day, end: nowSec }
            case '1w':
                return { start: nowSec - 7 * day, end: nowSec }
            case '1m':
                return { start: nowSec - 30 * day, end: nowSec }
            default:
                return { start: nowSec - day, end: nowSec }
        }
    }

    const fetchData = async () => {
        setLoading(true)
        try {
            let startSec: number
            let endSec: number
            let statsData = []
            let reportStartDate = today
            let reportEndDate = today
            if (timeRange === 'custom') {
                startSec = Math.floor(new Date(customStart).getTime() / 1000)
                endSec = Math.floor(new Date(customEnd).getTime() / 1000) + 24 * 60 * 60 // include full end day
                statsData = await api.getStats('custom', startSec.toString(), endSec.toString())
                reportStartDate = startSec.toString()
                reportEndDate = endSec.toString()
            } else {
                // Force explicit start/end to ensure backend pulls the right window
                const range = computeRangeSeconds(timeRange)
                startSec = range.start
                endSec = range.end
                statsData = await api.getStats('custom', startSec.toString(), endSec.toString())
                reportStartDate = startSec.toString()
                reportEndDate = endSec.toString()
            }

            const [reportData, statusData, wgTrafficRes, wgSeries] = await Promise.all([
                api.getReport(reportStartDate, reportEndDate),
                api.getSystemStatus(),
                api.getWireGuardTrafficRange(startSec, endSec).catch(() => ({})),
                api.getWireGuardTrafficSeries(undefined, undefined, 2000, startSec, endSec).catch(() => ({}))
            ])
            let wgRx = 0
            let wgTx = 0
            Object.values(wgTrafficRes || {}).forEach((v: any) => {
                wgRx += v?.rx || 0
                wgTx += v?.tx || 0
            })
            // Build WG chart data (delta per timestamp summed across peers)
            const wgChartAccumulator: Record<number, { uplink: number; downlink: number }> = {}
            Object.values(wgSeries || {}).forEach((arr: any) => {
                const series = Array.isArray(arr) ? arr : []
                for (let i = 1; i < series.length; i++) {
                    const prev = series[i - 1]
                    const curr = series[i]
                    const dx = Math.max(0, (curr.tx || 0) - (prev.tx || 0))
                    const dr = Math.max(0, (curr.rx || 0) - (prev.rx || 0))
                    const ts = curr.timestamp || curr.ts || 0
                    if (!ts) continue
                    if (!wgChartAccumulator[ts]) wgChartAccumulator[ts] = { uplink: 0, downlink: 0 }
                    wgChartAccumulator[ts].uplink += dx
                    wgChartAccumulator[ts].downlink += dr
                }
            })
            const wgChart = Object.entries(wgChartAccumulator)
                .map(([k, v]) => ({ ts: Number(k), uplink: v.uplink, downlink: v.downlink }))
                .sort((a, b) => a.ts - b.ts)

            setUsers(Array.isArray(reportData) ? reportData : [])
            setStats(Array.isArray(statsData) ? statsData : [])
            setStatus({
                singbox: statusData?.singbox ?? false,
                wireguard: statusData?.wireguard ?? false,
                active_users_singbox: statusData?.active_users_singbox ?? 0,
                active_users_wireguard: statusData?.active_users_wireguard ?? 0,
                active_users_singbox_list: statusData?.active_users_singbox_list ?? [],
                active_users_wireguard_list: statusData?.active_users_wireguard_list ?? [],
                samples_count: statusData?.samples_count ?? 0,
                db_size_bytes: statusData?.db_size_bytes ?? 0,
                systemctl_available: statusData?.systemctl_available ?? true,
                journalctl_available: statusData?.journalctl_available ?? true,
            })
            setWgTraffic({ rx: wgRx, tx: wgTx })
            setChartData(chartMode === 'singbox' ? statsData.map((p: any) => ({ ts: p.timestamp, uplink: p.uplink, downlink: p.downlink })) : wgChart)
            setLastUpdated(new Date())
        } catch (err) {
            console.error(err)
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        fetchData()
        const interval = setInterval(fetchData, 10000)
        return () => clearInterval(interval)
    }, [timeRange, customStart, customEnd, chartMode])

    // Calculate Total Traffic from Graph Data (Stats)
    const totalUp = stats.reduce((acc, p) => acc + p.uplink, 0)
    const totalDown = stats.reduce((acc, p) => acc + p.downlink, 0)

    const topConsumers = [...users].sort((a, b) => b.total - a.total).slice(0, 5)

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white hidden sm:block">Dashboard</h1>
                    <div className="flex items-center gap-2 mt-1">
                        <Badge variant={status.singbox ? 'success' : 'error'} className="text-[10px]">
                            sing-box: {status.singbox ? 'Running' : 'Stopped'}
                        </Badge>
                        <Badge variant={status.wireguard ? 'success' : 'error'} className="text-[10px]">
                            WireGuard: {status.wireguard ? 'Running' : 'Stopped'}
                        </Badge>
                        <span className="text-slate-500 text-xs hidden sm:inline">Updated {lastUpdated.toLocaleTimeString()}</span>
                    </div>
                </div>
                <div className="flex items-center gap-3 flex-wrap">
                    <div className="flex items-center gap-2 bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 shadow-sm">
                        <Clock size={14} className="text-slate-500" />
                        <select
                            value={timeRange}
                            onChange={(e) => setTimeRange(e.target.value)}
                            className="bg-transparent text-xs text-slate-300 border-none focus:ring-0 p-1 outline-none w-28 font-medium cursor-pointer"
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
                        onClick={fetchData}
                        isLoading={loading}
                        variant="secondary"
                        size="sm"
                        icon={<RefreshCw size={14} />}
                    >
                        Refresh
                    </Button>
                </div>
            </div>

            {/* Unified Service & Traffic Cards */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                {/* sing-box Card */}
                <Card className={`transition-all ${status.singbox ? 'bg-slate-900 border-slate-800' : 'bg-red-900/10 border-red-900/20'}`}>
                    <div className="flex items-start justify-between mb-6">
                        <div className="flex items-center gap-4">
                            <div className={`p-3 rounded-xl ${status.singbox ? 'bg-slate-800 text-emerald-400' : 'bg-red-500/10 text-red-400'}`}>
                                <Zap size={24} fill={status.singbox ? "currentColor" : "none"} />
                            </div>
                            <div>
                                <h3 className="text-lg font-bold text-white">sing-box</h3>
                                <div className="flex items-center gap-2 mt-1">
                                    <span className={`w-2 h-2 rounded-full ${status.singbox ? 'bg-emerald-500' : 'bg-red-500'}`}></span>
                                    <span className={`text-xs font-medium ${status.singbox ? 'text-emerald-500' : 'text-red-500'}`}>
                                        {status.singbox ? 'Operational' : 'Stopped'}
                                    </span>
                                </div>
                            </div>
                        </div>
                        <div className="text-right">
                            <p className="text-xs text-slate-400 uppercase tracking-wider font-semibold">Total Traffic</p>
                            <p className="text-2xl font-bold text-white mt-1">{formatBytes(totalUp + totalDown)}</p>
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4 mb-6">
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-emerald-400 text-xs font-medium mb-1">
                                <ArrowUp size={12} /> Uplink
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(totalUp)}</p>
                        </div>
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-blue-400 text-xs font-medium mb-1">
                                <ArrowDown size={12} /> Downlink
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(totalDown)}</p>
                        </div>
                    </div>

                    <div className="border-t border-slate-800 pt-4">
                        <div className="flex items-center justify-between mb-3">
                            <p className="text-sm font-semibold text-slate-300">Active Sessions</p>
                            <span className="text-xs font-mono bg-slate-800 text-white px-2 py-0.5 rounded-md border border-slate-700">
                                {status.active_users_singbox} clients
                            </span>
                        </div>
                        <div className="flex flex-wrap gap-2 max-h-[100px] overflow-y-auto custom-scrollbar">
                            {status.active_users_singbox_list.length > 0 ? (
                                status.active_users_singbox_list.map(u => (
                                    <span key={u} className="px-2 py-1 rounded bg-slate-800 text-slate-300 text-xs font-mono border border-slate-700">
                                        {u}
                                    </span>
                                ))
                            ) : (
                                <span className="text-xs text-slate-500 italic">No active sessions</span>
                            )}
                        </div>
                    </div>
                </Card>

                {/* WireGuard Card */}
                <Card className={`transition-all ${status.wireguard ? 'bg-slate-900 border-slate-800' : 'bg-red-900/10 border-red-900/20'}`}>
                    <div className="flex items-start justify-between mb-6">
                        <div className="flex items-center gap-4">
                            <div className={`p-3 rounded-xl ${status.wireguard ? 'bg-slate-800 text-blue-400' : 'bg-red-500/10 text-red-400'}`}>
                                <Shield size={24} fill={status.wireguard ? "currentColor" : "none"} />
                            </div>
                            <div>
                                <h3 className="text-lg font-bold text-white">WireGuard</h3>
                                <div className="flex items-center gap-2 mt-1">
                                    <span className={`w-2 h-2 rounded-full ${status.wireguard ? 'bg-blue-500' : 'bg-red-500'}`}></span>
                                    <span className={`text-xs font-medium ${status.wireguard ? 'text-blue-500' : 'text-red-500'}`}>
                                        {status.wireguard ? 'Operational' : 'Stopped'}
                                    </span>
                                </div>
                            </div>
                        </div>
                        <div className="text-right">
                            <p className="text-xs text-slate-400 uppercase tracking-wider font-semibold">Total Traffic</p>
                            <p className="text-2xl font-bold text-white mt-1">{formatBytes(wgTraffic.rx + wgTraffic.tx)}</p>
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4 mb-6">
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-emerald-400 text-xs font-medium mb-1">
                                <ArrowUp size={12} /> Uplink
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(wgTraffic.tx)}</p>
                        </div>
                        <div className="bg-slate-950/50 rounded-lg p-3 border border-slate-800/50">
                            <div className="flex items-center gap-2 text-blue-400 text-xs font-medium mb-1">
                                <ArrowDown size={12} /> Downlink
                            </div>
                            <p className="text-lg font-mono text-white">{formatBytes(wgTraffic.rx)}</p>
                        </div>
                    </div>

                    <div className="border-t border-slate-800 pt-4">
                        <div className="flex items-center justify-between mb-3">
                            <p className="text-sm font-semibold text-slate-300">Active Peers</p>
                            <span className="text-xs font-mono bg-slate-800 text-white px-2 py-0.5 rounded-md border border-slate-700">
                                {status.active_users_wireguard} peers
                            </span>
                        </div>
                        <div className="flex flex-wrap gap-2 max-h-[100px] overflow-y-auto custom-scrollbar">
                            {status.active_users_wireguard_list.length > 0 ? (
                                status.active_users_wireguard_list.map(u => (
                                    <span key={u} className="px-2 py-1 rounded bg-slate-800 text-slate-300 text-xs font-mono border border-slate-700">
                                        {u}
                                    </span>
                                ))
                            ) : (
                                <span className="text-xs text-slate-500 italic">No active peers</span>
                            )}
                        </div>
                    </div>
                </Card>
            </div>


            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
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
                                    sing-box
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
                        <div className="h-[300px] w-full mt-4">
                            <ResponsiveContainer width="100%" height="100%">
                                <AreaChart data={chartData}>
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
                                        tickFormatter={(ts) => new Date(ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                        stroke="#64748b"
                                        fontSize={12}
                                        tickMargin={10}
                                        axisLine={false}
                                        tickLine={false}
                                    />
                                    <YAxis
                                        tickFormatter={(bytes) => formatBytes(bytes, 0)}
                                        stroke="#64748b"
                                        fontSize={12}
                                        tickMargin={10}
                                        axisLine={false}
                                        tickLine={false}
                                    />
                                    <Tooltip
                                        contentStyle={{ backgroundColor: '#0f172a', borderColor: '#1e293b', borderRadius: '0.75rem', color: '#f8fafc' }}
                                        itemStyle={{ color: '#f8fafc' }}
                                        formatter={(val: number) => formatBytes(val)}
                                        labelFormatter={(ts) => new Date(ts * 1000).toLocaleString()}
                                    />
                                    <Area
                                        type="monotone"
                                        dataKey="uplink"
                                        name="Uplink"
                                        stroke="#3b82f6"
                                        strokeWidth={2}
                                        fillOpacity={1}
                                        fill="url(#colorUp)"
                                    />
                                    <Area
                                        type="monotone"
                                        dataKey="downlink"
                                        name="Downlink"
                                        stroke="#10b981"
                                        strokeWidth={2}
                                        fillOpacity={1}
                                        fill="url(#colorDown)"
                                    />
                                </AreaChart>
                            </ResponsiveContainer>
                        </div>
                    </Card>
                </div>

                {/* Top Consumers */}
                <Card title="Top Consumers" className="h-full">
                    <div className="space-y-4 mt-2">
                        {topConsumers.length === 0 ? (
                            <div className="text-center text-slate-500 py-8 text-sm italic">No active users</div>
                        ) : (
                            topConsumers.map((u, i) => (
                                <div key={u.name} className="flex items-center justify-between p-3 bg-slate-950/50 rounded-lg border border-slate-800/50 hover:border-slate-800 transition-colors">
                                    <div className="flex items-center gap-3">
                                        <div className={`w-8 h-8 rounded-full flex items-center justify-center font-bold text-xs ${i === 0 ? 'bg-yellow-500/20 text-yellow-400' : 'bg-slate-800 text-slate-400'}`}>
                                            {i + 1}
                                        </div>
                                        <div>
                                            <div className="font-medium text-slate-200 text-sm">{u.name}</div>
                                            <div className="text-[10px] text-slate-500 uppercase">{u.flow || 'Default'}</div>
                                        </div>
                                    </div>
                                    <div className="text-right">
                                        <div className="font-mono text-sm text-blue-400">{formatBytes(u.total)}</div>
                                        <div className="text-[10px] text-slate-500">
                                            {u.quota_limit ? `${((u.total / u.quota_limit) * 100).toFixed(1)}%` : '∞'}
                                        </div>
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
