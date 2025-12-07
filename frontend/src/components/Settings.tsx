import { useState, useEffect } from 'react'
import { api, FeatureFlags } from '../api'
import { Save, RefreshCw, Power } from 'lucide-react'

export default function Settings() {
    const [loading, setLoading] = useState(false)
    const [samplerRunning, setSamplerRunning] = useState(false)
    const [dbInfo, setDbInfo] = useState<{ rows: number; sizeMB: number }>({ rows: 0, sizeMB: 0 })
    const [samplerHistory, setSamplerHistory] = useState<any[]>([])
    const [features, setFeatures] = useState<FeatureFlags>({
        enable_singbox: true,
        enable_wireguard: true,
        retention_enabled: false,
        retention_days: 90,
        sampler_interval_sec: 120,
        sampler_paused: false,
        active_threshold_bytes: 1024,
    })
    const [historyLimit, setHistoryLimit] = useState(5)
    const [serviceStatus, setServiceStatus] = useState<{ singbox: boolean; wireguard: boolean }>({ singbox: false, wireguard: false })

    useEffect(() => {
        loadAll()
    }, [historyLimit])

    const loadAll = async () => {
        setLoading(true)
        try {
            await Promise.all([loadFeatures(), loadDbStats(), loadSamplerHistory()])
        } finally {
            setLoading(false)
        }
    }

    const loadFeatures = async () => {
        try {
            const f = await api.getFeatures()
            setFeatures(f)
        } catch (err) {
            console.error(err)
        }
    }

    const loadDbStats = async () => {
        try {
            const status = await api.getSystemStatus()
            const sizeBytes = status.db_size_bytes ?? 0
            const rows = status.samples_count ?? 0
            setDbInfo({ rows, sizeMB: parseFloat((sizeBytes / (1024 * 1024)).toFixed(2)) })
            if (status.sampler_paused !== undefined) {
                setFeatures(f => ({ ...f, sampler_paused: status.sampler_paused }))
            }
            setServiceStatus({
                singbox: !!status.singbox,
                wireguard: !!status.wireguard
            })
        } catch (err) {
            console.error(err)
        }
    }

    const loadSamplerHistory = async () => {
        try {
            const h = await api.getSamplerHistory(historyLimit)
            setSamplerHistory(Array.isArray(h) ? h : [])
        } catch (err) {
            console.error(err)
            setSamplerHistory([])
        }
    }

    const handleSaveFeatures = async () => {
        try {
            await api.updateFeatures(features)
            alert('Feature toggles saved. Restart services if needed.')
        } catch (err) {
            alert('Failed to save feature toggles: ' + err)
        }
    }

    const handleRunSampler = async () => {
        try {
            setSamplerRunning(true)
            await api.runSampler()
            await loadDbStats()
            await loadSamplerHistory()
            alert('Sampler run triggered.')
        } catch (err) {
            alert('Failed to run sampler: ' + err)
        } finally {
            setSamplerRunning(false)
        }
    }

    const handleTogglePause = async () => {
        try {
            if (features.sampler_paused) {
                await api.resumeSampler()
                setFeatures(f => ({ ...f, sampler_paused: false }))
            } else {
                await api.pauseSampler()
                setFeatures(f => ({ ...f, sampler_paused: true }))
            }
        } catch (err) {
            alert('Failed to toggle sampler: ' + err)
        }
    }

    const handlePruneNow = async () => {
        if (!features.retention_enabled) {
            alert('Retention is disabled')
            return
        }
        if (!confirm('Prune old samples now?')) return
        try {
            const res = await api.pruneNow()
            alert(`Pruned ${res.deleted} samples (cutoff ${new Date(res.cutoff * 1000).toLocaleString()})`)
            await loadDbStats()
        } catch (err) {
            alert('Prune failed: ' + err)
        }
    }

    const handleServiceAction = async (service: string, action: 'restart' | 'stop' | 'start') => {
        if (!confirm(`Are you sure you want to ${action} ${service}?`)) return
        try {
            if (action === 'restart') {
                await api.restartService(service)
            } else if (action === 'start') {
                await api.startService(service)
            } else {
                await api.stopService(service)
            }
            await loadDbStats()
            alert(`${service} ${action}ed successfully`)
        } catch (err) {
            alert(`Failed to ${action} ${service}: ` + err)
        }
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white">Settings</h1>
                    <p className="text-slate-400 text-sm mt-1">System configuration and service control</p>
                </div>
                <div className="flex gap-3">
                    <button
                        onClick={loadAll}
                        className="px-3 py-2 bg-slate-800 text-slate-300 rounded-lg hover:bg-slate-700"
                    >
                        Refresh
                    </button>
                    <button
                        className={`p-2 rounded-lg bg-slate-800 text-slate-400 hover:text-white hover:bg-slate-700 transition-all ${loading ? 'animate-spin' : ''}`}
                    >
                        <RefreshCw size={18} />
                    </button>
                </div>
            </div>

            {/* Feature toggles */}
            <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 shadow-sm space-y-4">
                <div className="flex items-center justify-between gap-4 flex-wrap">
                    <div>
                        <h2 className="text-lg font-bold text-white">Features</h2>
                        <p className="text-slate-400 text-sm">Enable/disable endpoints, retention and sampler</p>
                    </div>
                    <button
                        onClick={handleSaveFeatures}
                        className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg flex items-center gap-2"
                    >
                        <Save size={16} />
                        Save
                    </button>
                </div>

                <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3 auto-rows-fr">
                    <label className="flex items-start gap-3 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer h-full">
                        <input
                            type="checkbox"
                            checked={features.enable_singbox}
                            onChange={e => setFeatures({ ...features, enable_singbox: e.target.checked })}
                            className="mt-1 h-4 w-4"
                        />
                        <div>
                            <p className="text-white font-semibold">Enable sing-box</p>
                            <p className="text-xs text-slate-400">Disable if you only run WireGuard</p>
                        </div>
                    </label>
                    <label className="flex items-start gap-3 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer h-full">
                        <input
                            type="checkbox"
                            checked={features.enable_wireguard}
                            onChange={e => setFeatures({ ...features, enable_wireguard: e.target.checked })}
                            className="mt-1 h-4 w-4"
                        />
                        <div>
                            <p className="text-white font-semibold">Enable WireGuard</p>
                            <p className="text-xs text-slate-400">Disable if you only run WireGuard</p>
                        </div>
                    </label>
                    <label className="flex items-start gap-3 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer h-full">
                        <input
                            type="checkbox"
                            checked={!!features.retention_enabled}
                            onChange={e => setFeatures({ ...features, retention_enabled: e.target.checked })}
                            className="mt-1 h-4 w-4"
                        />
                        <div>
                            <p className="text-white font-semibold">Retention (prune samples)</p>
                            <p className="text-xs text-slate-400">Prune traffic older than N days</p>
                        </div>
                    </label>

                    <div className="p-4 bg-slate-950 border border-slate-800 rounded-xl h-full flex flex-col justify-between">
                        <p className="text-white font-semibold">Retention days</p>
                        <p className="text-xs text-slate-400 mb-2">Only applies when retention is enabled</p>
                        <input
                            type="number"
                            min={1}
                            value={features.retention_days ?? 90}
                            onChange={e => setFeatures({ ...features, retention_days: parseInt(e.target.value) })}
                            className="w-full bg-slate-900 border border-slate-800 rounded px-3 py-2 text-white"
                        />
                    </div>
                    <div className="p-4 bg-slate-950 border border-slate-800 rounded-xl h-full flex flex-col justify-between">
                        <p className="text-white font-semibold">Active threshold (bytes)</p>
                        <p className="text-xs text-slate-400 mb-2">Min bytes in last 5m to count as active</p>
                        <input
                            type="number"
                            min={0}
                            value={features.active_threshold_bytes ?? 1024}
                            onChange={e => setFeatures({ ...features, active_threshold_bytes: parseInt(e.target.value) })}
                            className="w-full bg-slate-900 border border-slate-800 rounded px-3 py-2 text-white"
                        />
                    </div>
                    <div className="p-4 bg-slate-950 border border-slate-800 rounded-xl md:col-span-2 xl:col-span-1 flex flex-col gap-3">
                        <div>
                            <p className="text-white font-semibold">Sampler interval (sec)</p>
                            <p className="text-xs text-slate-400 mb-2">Sampling via StatsService</p>
                            <input
                                type="number"
                                min={15}
                                value={features.sampler_interval_sec ?? 120}
                                onChange={e => setFeatures({ ...features, sampler_interval_sec: parseInt(e.target.value) })}
                                className="w-full bg-slate-900 border border-slate-800 rounded px-3 py-2 text-white"
                            />
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                            <button
                                onClick={handleRunSampler}
                                className="flex-1 min-w-[140px] px-3 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg flex items-center justify-center gap-2 disabled:opacity-50"
                                disabled={samplerRunning}
                            >
                                <RefreshCw size={16} className={samplerRunning ? 'animate-spin' : ''} />
                                Run sampler now
                            </button>
                            <button
                                onClick={handleTogglePause}
                                className="px-3 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg disabled:opacity-50"
                            >
                                {features.sampler_paused ? 'Resume' : 'Pause'}
                            </button>
                        </div>
                    </div>
                </div>
            </div>

            {/* Service Control */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div className={`bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-sm ${!features.enable_singbox ? 'opacity-60' : ''}`}>
                    <h2 className="text-lg font-bold text-white mb-4 flex items-center gap-2">
                        <div className="w-2 h-2 rounded-full bg-blue-500"></div>
                        sing-box
                    </h2>
                    <div className="flex gap-3">
                        <button
                            onClick={() => handleServiceAction('sing-box', 'restart')}
                            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg transition-colors"
                            disabled={!features.enable_singbox || features.systemctl_available === false}
                        >
                            <RefreshCw size={16} />
                            Restart
                        </button>
                        <button
                            onClick={() => handleServiceAction('sing-box', serviceStatus.singbox ? 'stop' : 'start')}
                            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-red-500/10 text-red-400 hover:bg-red-500/20 rounded-lg transition-colors"
                            disabled={!features.enable_singbox || features.systemctl_available === false}
                        >
                            <Power size={16} />
                            {serviceStatus.singbox ? 'Stop' : 'Start'}
                        </button>
                    </div>
                </div>

                <div className={`bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-sm ${!features.enable_wireguard ? 'opacity-60' : ''}`}>
                    <h2 className="text-lg font-bold text-white mb-4 flex items-center gap-2">
                        <div className="w-2 h-2 rounded-full bg-emerald-500"></div>
                        WireGuard
                    </h2>
                    <div className="flex gap-3">
                        <button
                            onClick={() => handleServiceAction('wireguard', 'restart')}
                            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg transition-colors"
                            disabled={!features.enable_wireguard || features.systemctl_available === false}
                        >
                            <RefreshCw size={16} />
                            Restart
                        </button>
                        <button
                            onClick={() => handleServiceAction('wireguard', serviceStatus.wireguard ? 'stop' : 'start')}
                            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-red-500/10 text-red-400 hover:bg-red-500/20 rounded-lg transition-colors"
                            disabled={!features.enable_wireguard || features.systemctl_available === false}
                        >
                            <Power size={16} />
                            {serviceStatus.wireguard ? 'Stop' : 'Start'}
                        </button>
                    </div>
                </div>
                {features.systemctl_available === false && (
                    <div className="md:col-span-2 bg-amber-500/10 border border-amber-500/20 rounded-xl p-4 text-amber-400 text-sm">
                        Service control is disabled in this environment (systemctl not available, typical in Docker). Use container orchestration to start/stop services.
                    </div>
                )}
            </div>

            <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 items-start">
                {/* DB Info */}
                <div className="bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-sm h-full">
                    <div className="flex items-center justify-between gap-3">
                        <div>
                            <h2 className="text-lg font-bold text-white">Database</h2>
                            <p className="text-slate-400 text-sm">Traffic stored by sampler</p>
                        </div>
                        <div className="flex gap-2">
                            <button
                                onClick={loadDbStats}
                                className={`p-2 rounded-lg bg-slate-800 text-slate-400 hover:text-white hover:bg-slate-700 transition-all ${loading ? 'animate-spin' : ''}`}
                                title="Refresh DB stats"
                            >
                                <RefreshCw size={18} />
                            </button>
                            <button
                                onClick={handlePruneNow}
                                className="px-3 py-2 bg-red-500/20 text-red-300 rounded-lg hover:bg-red-500/30 disabled:opacity-50"
                                disabled={!features.retention_enabled}
                            >
                                Prune now
                            </button>
                        </div>
                    </div>
                    <div className="mt-4 grid grid-cols-2 gap-4 text-slate-200">
                        <div className="p-4 bg-slate-950 border border-slate-800 rounded-lg">
                            <p className="text-xs uppercase text-slate-500">Rows</p>
                            <p className="text-2xl font-semibold mt-1">{dbInfo.rows.toLocaleString()}</p>
                        </div>
                        <div className="p-4 bg-slate-950 border border-slate-800 rounded-lg">
                            <p className="text-xs uppercase text-slate-500">Size (MB)</p>
                            <p className="text-2xl font-semibold mt-1">{dbInfo.sizeMB}</p>
                        </div>
                    </div>
                </div>

                {/* Sampler History */}
                <div className="bg-slate-900 border border-slate-800 rounded-xl p-6 shadow-sm space-y-3 h-full">
                    <div className="flex items-center justify-between gap-3 flex-wrap">
                        <div>
                            <h2 className="text-lg font-bold text-white">Sampler history</h2>
                            <p className="text-slate-400 text-sm">Latest executions</p>
                        </div>
                        <div className="flex gap-2 items-center">
                            <select
                                value={historyLimit}
                                onChange={e => setHistoryLimit(parseInt(e.target.value))}
                                className="bg-slate-800 border border-slate-700 rounded px-2 py-1 text-slate-200 text-sm"
                            >
                                <option value={5}>5</option>
                                <option value={10}>10</option>
                                <option value={20}>20</option>
                            </select>
                            <button
                                onClick={loadSamplerHistory}
                                className="px-3 py-2 bg-slate-800 text-slate-200 rounded-lg hover:bg-slate-700"
                            >
                                Refresh
                            </button>
                        </div>
                    </div>
                    <div className="overflow-x-auto">
                        <table className="min-w-full text-sm text-slate-200">
                            <thead className="text-xs text-slate-400">
                                <tr>
                                    <th className="text-left py-2 pr-4">Time</th>
                                    <th className="text-left py-2 pr-4">Inserted</th>
                                    <th className="text-left py-2 pr-4">Duration</th>
                                    <th className="text-left py-2 pr-4">Error</th>
                                </tr>
                            </thead>
                            <tbody>
                                {samplerHistory.map((run, idx) => (
                                    <tr key={idx} className="border-t border-slate-800">
                                        <td className="py-2 pr-4">{new Date(run.timestamp * 1000).toLocaleString()}</td>
                                        <td className="py-2 pr-4">{run.inserted}</td>
                                        <td className="py-2 pr-4">{run.duration_ms} ms</td>
                                        <td className="py-2 pr-4 text-red-300 text-xs">{run.error}</td>
                                    </tr>
                                ))}
                                {samplerHistory.length === 0 && (
                                    <tr><td className="py-2 text-slate-500" colSpan={4}>No history</td></tr>
                                )}
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </div>
    )
}
