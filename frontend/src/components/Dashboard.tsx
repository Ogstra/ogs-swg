import { useEffect, useState } from 'react'
import { api, UserStatus } from '../api'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts'
import { ArrowUp, ArrowDown, Activity, RefreshCw, Server, Shield, Clock } from 'lucide-react'

function formatBytes(bytes: number, decimals = 2) {
    if (!+bytes) return '0 B'
    const k = 1024
    const dm = decimals < 0 ? 0 : decimals
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

export default function Dashboard() {
    const [users, setUsers] = useState<UserStatus[]>([])
    const [stats, setStats] = useState<any[]>([])
    const [status, setStatus] = useState({
        singbox: false,
        wireguard: false,
        active_users_singbox: 0,
        active_users_wireguard: 0,
        active_users_singbox_list: [] as string[],
        active_users_wireguard_list: [] as string[],
        samples_count: 0,
        db_size_bytes: 0
    })
    const [loading, setLoading] = useState(true)
    const [lastUpdated, setLastUpdated] = useState<Date>(new Date())
    const [timeRange, setTimeRange] = useState('24h')
    const [chartDomain, setChartDomain] = useState<[number, number] | undefined>(undefined) // [startTs, endTs]

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
            let statsData = []
            let rangeDomain: [number, number] | undefined
            let reportStartDate = today
            let reportEndDate = today
            if (timeRange === 'custom') {
                const startSec = Math.floor(new Date(customStart).getTime() / 1000)
                const endSec = Math.floor(new Date(customEnd).getTime() / 1000) + 24 * 60 * 60 // include full end day
                statsData = await api.getStats('custom', startSec.toString(), endSec.toString())
                rangeDomain = [startSec, endSec]
                reportStartDate = startSec.toString()
                reportEndDate = endSec.toString()
            } else {
                // Force explicit start/end to ensure backend pulls the right window
                const { start, end } = computeRangeSeconds(timeRange)
                statsData = await api.getStats('custom', start.toString(), end.toString())
                rangeDomain = [start, end]
                reportStartDate = start.toString()
                reportEndDate = end.toString()
            }

            const [reportData, statusData] = await Promise.all([
                api.getReport(reportStartDate, reportEndDate),
                api.getSystemStatus()
            ])

            setUsers(Array.isArray(reportData) ? reportData : [])
            setStats(Array.isArray(statsData) ? statsData : [])
            setChartDomain(rangeDomain)
            setStatus({
                singbox: statusData?.singbox ?? false,
                wireguard: statusData?.wireguard ?? false,
                active_users_singbox: statusData?.active_users_singbox ?? 0,
                active_users_wireguard: statusData?.active_users_wireguard ?? 0,
                active_users_singbox_list: statusData?.active_users_singbox_list ?? [],
                active_users_wireguard_list: statusData?.active_users_wireguard_list ?? [],
                samples_count: statusData?.samples_count ?? 0,
                db_size_bytes: statusData?.db_size_bytes ?? 0
            })
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
    }, [timeRange, customStart, customEnd])

    // Calculate Total Traffic from Graph Data (Stats)
    const totalUp = stats.reduce((acc, p) => acc + p.uplink, 0)
    const totalDown = stats.reduce((acc, p) => acc + p.downlink, 0)

    // Format stats for chart
    const chartDataBase = stats.map(p => ({
        ts: p.timestamp,
        uplink: p.uplink,
        downlink: p.downlink
    }))
    const chartData = (() => {
        if (chartDataBase.length === 0) return []
        const data = [...chartDataBase]
        if (chartDomain && chartDataBase[chartDataBase.length - 1].ts < chartDomain[1]) {
            const last = chartDataBase[chartDataBase.length - 1]
            data.push({ ts: chartDomain[1], uplink: last.uplink, downlink: last.downlink })
        }
        return data
    })()

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white">Dashboard Overview</h1>
                    <p className="text-slate-400 text-sm mt-1">Real-time traffic monitoring</p>
                </div>
                <div className="flex items-center gap-3">
                    <div className="flex items-center gap-2 bg-slate-900 p-1 rounded-lg border border-slate-800">
                        <Clock size={14} className="text-slate-500 ml-2" />
                        <select
                            value={timeRange}
                            onChange={(e) => setTimeRange(e.target.value)}
                            className="bg-transparent text-xs text-slate-300 border-none focus:ring-0 p-1 outline-none"
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
                        <div className="flex items-center gap-2 bg-slate-900 p-1 rounded-lg border border-slate-800">
                            <input
                                type="date"
                                value={customStart}
                                onChange={e => setCustomStart(e.target.value)}
                                className="bg-transparent text-xs text-slate-300 border-none outline-none"
                            />
                            <span className="text-slate-500">-</span>
                            <input
                                type="date"
                                value={customEnd}
                                onChange={e => setCustomEnd(e.target.value)}
                                className="bg-transparent text-xs text-slate-300 border-none outline-none"
                            />
                        </div>
                    )}

                    <span className="text-xs text-slate-500 hidden sm:inline">
                        Updated: {lastUpdated.toLocaleTimeString()}
                    </span>
                    <button
                        onClick={fetchData}
                        className={`p-2 rounded-lg bg-slate-800 text-slate-400 hover:text-white hover:bg-slate-700 transition-all ${loading ? 'animate-spin' : ''}`}
                        title="Refresh Data"
                    >
                        <RefreshCw size={18} />
                    </button>
                </div>
            </div>

            {/* Service Status Cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className={`p-4 rounded-xl border flex flex-col gap-3 ${status.singbox ? 'bg-emerald-500/10 border-emerald-500/20' : 'bg-red-500/10 border-red-500/20'}`}>
                    <div className="flex items-center justify-between gap-3">
                        <div className="flex items-center gap-3">
                            <div className={`p-2 rounded-lg ${status.singbox ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
                                <Activity size={20} />
                            </div>
                            <div>
                                <p className="text-sm font-medium text-slate-200">sing-box</p>
                                <p className={`text-xs ${status.singbox ? 'text-emerald-400' : 'text-red-400'}`}>
                                    {status.singbox ? 'Active' : 'Stopped'}
                                </p>
                            </div>
                        </div>
                        <div className={`w-3 h-3 rounded-full ${status.singbox ? 'bg-emerald-500 animate-pulse' : 'bg-red-500'}`}></div>
                    </div>
                    <div className="flex items-center gap-2 flex-wrap text-xs text-slate-400">
                        <span className="font-semibold text-slate-200">Activos:</span>
                        <span>{status.active_users_singbox}</span>
                        {status.active_users_singbox_list.slice(0, 6).map(u => (
                            <span key={u} className="px-2 py-1 rounded bg-slate-800 text-slate-200 font-mono">{u}</span>
                        ))}
                        {status.active_users_singbox_list.length > 6 && (
                            <span className="px-2 py-1 rounded bg-slate-800 text-slate-300">+{status.active_users_singbox_list.length - 6} más</span>
                        )}
                    </div>
                </div>

                <div className={`p-4 rounded-xl border flex flex-col gap-3 ${status.wireguard ? 'bg-emerald-500/10 border-emerald-500/20' : 'bg-red-500/10 border-red-500/20'}`}>
                    <div className="flex items-center justify-between gap-3">
                        <div className="flex items-center gap-3">
                            <div className={`p-2 rounded-lg ${status.wireguard ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
                                <Shield size={20} />
                            </div>
                            <div>
                                <p className="text-sm font-medium text-slate-200">WireGuard</p>
                                <p className={`text-xs ${status.wireguard ? 'text-emerald-400' : 'text-red-400'}`}>
                                    {status.wireguard ? 'Active' : 'Stopped'}
                                </p>
                            </div>
                        </div>
                        <div className={`w-3 h-3 rounded-full ${status.wireguard ? 'bg-emerald-500 animate-pulse' : 'bg-red-500'}`}></div>
                    </div>
                    <div className="text-xs text-slate-400">
                        <span className="font-semibold text-slate-200">Activos:</span>{' '}
                        <span>{status.active_users_wireguard}</span>
                    </div>
                </div>
            </div>

            {/* Main Traffic Chart */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-sm">
                <h2 className="text-lg font-bold text-white mb-6">Network Traffic</h2>
                <div className="h-[300px] w-full">
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData}>
                            <defs>
                                <linearGradient id="colorUp" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.3} />
                                    <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
                                </linearGradient>
                                <linearGradient id="colorDown" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                                    <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
                                </linearGradient>
                            </defs>
                            <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" vertical={false} />
                            <XAxis
                                dataKey="ts"
                                type="number"
                                domain={chartDomain || ['dataMin', 'dataMax']}
                                stroke="#64748b"
                                fontSize={12}
                                tickLine={false}
                                axisLine={false}
                                minTickGap={30}
                                tickFormatter={(value: number) =>
                                    new Date(value * 1000).toLocaleString([], {
                                        month: 'short',
                                        day: '2-digit',
                                        hour: '2-digit',
                                        minute: '2-digit'
                                    })
                                }
                            />
                            <YAxis
                                stroke="#64748b"
                                fontSize={12}
                                tickLine={false}
                                axisLine={false}
                                tickFormatter={(value) => formatBytes(value, 0)}
                            />
                            <Tooltip
                                contentStyle={{ backgroundColor: '#0f172a', borderColor: '#334155', color: '#f1f5f9', borderRadius: '8px' }}
                                itemStyle={{ color: '#f1f5f9' }}
                                labelFormatter={(value: any) =>
                                    new Date(Number(value) * 1000).toLocaleString([], {
                                        month: 'short',
                                        day: '2-digit',
                                        hour: '2-digit',
                                        minute: '2-digit'
                                    })
                                }
                                formatter={(value: number) => formatBytes(value)}
                            />
                            <Area
                                type="monotone"
                                dataKey="uplink"
                                stroke="#10b981"
                                fillOpacity={1}
                                fill="url(#colorUp)"
                                strokeWidth={2}
                                name="Uplink"
                            />
                            <Area
                                type="monotone"
                                dataKey="downlink"
                                stroke="#3b82f6"
                                fillOpacity={1}
                                fill="url(#colorDown)"
                                strokeWidth={2}
                                name="Downlink"
                            />
                        </AreaChart>
                    </ResponsiveContainer>
                </div>
            </div>

            {/* Stats Cards */}
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4 lg:gap-6">
                <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-sm">
                    <div className="flex items-start justify-between">
                        <div>
                            <p className="text-sm font-medium text-slate-400">Total Traffic (Graph)</p>
                            <h3 className="text-2xl font-bold text-white mt-2">{formatBytes(totalUp + totalDown)}</h3>
                        </div>
                        <div className="p-2 bg-blue-500/10 rounded-lg text-blue-500">
                            <Server size={20} />
                        </div>
                    </div>
                    <div className="mt-4 flex items-center gap-4 text-sm">
                        <div className="flex items-center gap-1 text-emerald-400">
                            <ArrowUp size={14} />
                            <span>{formatBytes(totalUp)}</span>
                        </div>
                        <div className="flex items-center gap-1 text-indigo-400">
                            <ArrowDown size={14} />
                            <span>{formatBytes(totalDown)}</span>
                        </div>
                    </div>
                </div>

                <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-sm flex flex-col">
                    <h2 className="text-sm font-medium text-slate-400 mb-4">Top Consumers (same window)</h2>
                    <div className="flex-1 overflow-y-auto space-y-3 pr-2 custom-scrollbar max-h-[140px]">
                        {users.sort((a, b) => b.total - a.total).slice(0, 3).map((user, idx) => (
                            <div key={user.name} className="flex justify-between items-center text-sm">
                                <div className="flex items-center gap-2 overflow-hidden">
                                    <span className={`text-xs font-bold w-4 h-4 flex items-center justify-center rounded ${idx === 0 ? 'bg-yellow-500/20 text-yellow-400' : 'bg-slate-800 text-slate-400'}`}>
                                        {idx + 1}
                                    </span>
                                    <span className="text-slate-300 truncate max-w-[100px]" title={user.name}>{user.name}</span>
                                </div>
                                <span className="font-mono text-slate-400 text-xs">{formatBytes(user.total)}</span>
                            </div>
                        ))}
                        {users.length === 0 && <div className="text-center text-slate-500 text-xs">No active users</div>}
                    </div>
                </div>

                <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-sm flex flex-col">
                    <p className="text-sm font-medium text-slate-400">Active Users (last 5m)</p>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-3">
                        <div>
                            <div className="flex items-baseline gap-2 mb-2">
                                <h3 className="text-2xl font-bold text-white">{status.active_users_singbox}</h3>
                                <span className="text-xs text-slate-500 uppercase">sing-box</span>
                            </div>
                            {status.active_users_singbox_list.length > 0 ? (
                                <div className="flex flex-wrap gap-2">
                                    {status.active_users_singbox_list.map(u => (
                                        <span key={u} className="px-2 py-1 rounded bg-slate-800 text-slate-200 text-xs font-mono">{u}</span>
                                    ))}
                                </div>
                            ) : (
                                <div className="text-slate-500 text-xs">No active users.</div>
                            )}
                        </div>
                        <div>
                            <div className="flex items-baseline gap-2 mb-2">
                                <h3 className="text-2xl font-bold text-white">{status.active_users_wireguard}</h3>
                                <span className="text-xs text-slate-500 uppercase">WireGuard</span>
                            </div>
                            {status.active_users_wireguard_list.length > 0 ? (
                                <div className="flex flex-wrap gap-2">
                                    {status.active_users_wireguard_list.map(u => (
                                        <span key={u} className="px-2 py-1 rounded bg-slate-800 text-slate-200 text-xs font-mono">{u}</span>
                                    ))}
                                </div>
                            ) : (
                                <div className="text-slate-500 text-xs">No active users.</div>
                            )}
                        </div>
                    </div>
                </div>
            </div>
        </div>
    )
}
