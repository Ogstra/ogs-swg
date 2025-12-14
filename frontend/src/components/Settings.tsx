import { useState, useEffect } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { api, FeatureFlags } from '../api'
import { Save, RefreshCw, Lock, User } from 'lucide-react'
import { useToast } from '../context/ToastContext'
import { Card } from './ui/Card'
import { Button } from './ui/Button'
import { Badge } from './ui/Badge'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import SingboxConfigEditor from './SingboxConfigEditor'
import { Tabs } from './ui/Tabs'
import { Database, Shield, Settings as SettingsIcon, Server } from 'lucide-react'

type ServiceStatus = { singbox: boolean; wireguard: boolean }
type DbInfo = { rows: number; sizeMB: number }
type PasswordData = { current: string; new: string; confirm: string }
type UsernameData = { password: string; newUsername: string }
type DashboardPrefs = { defaultService: 'singbox' | 'wireguard'; refreshMs: number; defaultRange: string }

export default function Settings() {
    const { success, error: toastError } = useToast()
    const { logout } = useAuth()
    const navigate = useNavigate()
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
        wg_sampler_interval_sec: 60,
        wg_retention_days: 30,
        aggregation_enabled: false,
        aggregation_days: 7,
    })
    const [historyLimit, setHistoryLimit] = useState(5)
    const [serviceStatus, setServiceStatus] = useState<{ singbox: boolean; wireguard: boolean }>({ singbox: false, wireguard: false })
    const [passwordData, setPasswordData] = useState({ current: '', new: '', confirm: '' })
    const [usernameData, setUsernameData] = useState({ password: '', newUsername: '' })
    const [publicIP, setPublicIP] = useState<string>('')
    const [dashboardPrefs, setDashboardPrefs] = useState<DashboardPrefs>({
        defaultService: 'singbox',
        refreshMs: 10000,
        defaultRange: '24h'
    })

    useEffect(() => {
        loadAll()
        const savedPrefs = localStorage.getItem('dashboard_prefs')
        if (savedPrefs) {
            try {
                const parsed = JSON.parse(savedPrefs)
                setDashboardPrefs({
                    defaultService: parsed.defaultService === 'wireguard' ? 'wireguard' : 'singbox',
                    refreshMs: parsed.refreshMs && parsed.refreshMs >= 1000 ? parsed.refreshMs : 10000,
                    defaultRange: parsed.defaultRange || '24h'
                })
            } catch {
                // ignore parse errors
            }
        }
    }, [historyLimit])

    const loadAll = async () => {
        setLoading(true)
        await Promise.all([loadFeatures(), loadDbStats(), loadSamplerHistory()])

        // Load public IP
        try {
            const dashData = await api.getDashboardData()
            setPublicIP(dashData.public_ip || '')
        } catch (err) {
            console.error('Failed to load public IP:', err)
        }

        setLoading(false)
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
            if (status.wg_sample_interval_sec) {
                setFeatures(f => ({ ...f, wg_sampler_interval_sec: status.wg_sample_interval_sec }))
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
            success('Feature toggles saved successfully')
        } catch (err) {
            toastError('Failed to save feature toggles: ' + err)
        }
    }

    const handleRunSampler = async () => {
        try {
            setSamplerRunning(true)
            await api.runSampler()
            await loadDbStats()
            await loadSamplerHistory()
            success('Sampler run triggered successfully')
        } catch (err) {
            toastError('Failed to run sampler: ' + err)
        } finally {
            setSamplerRunning(false)
        }
    }

    const handleTogglePause = async () => {
        try {
            if (features.sampler_paused) {
                await api.resumeSampler()
                setFeatures(f => ({ ...f, sampler_paused: false }))
                success('Sampler resumed')
            } else {
                await api.pauseSampler()
                setFeatures(f => ({ ...f, sampler_paused: true }))
                success('Sampler paused')
            }
        } catch (err) {
            toastError('Failed to toggle sampler: ' + err)
        }
    }

    const handlePruneNow = async () => {
        if (!features.retention_enabled) {
            toastError('Retention is disabled')
            return
        }
        if (!confirm('Prune old samples now?')) return
        try {
            const res = await api.pruneNow()
            success(`Pruned ${res.deleted} samples`)
            await loadDbStats()
        } catch (err) {
            toastError('Prune failed: ' + err)
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
            success(`${service} ${action}ed successfully`)
        } catch (err) {
            toastError(`Failed to ${action} ${service}: ` + err)
        }
    }

    const handleChangeUsername = async () => {
        if (!usernameData.password || !usernameData.newUsername) {
            toastError("All fields are required")
            return
        }
        try {
            await api.updateUsername(usernameData.password, usernameData.newUsername)
            success('Username updated successfully. Please login again.')
            setUsernameData({ password: '', newUsername: '' })
            // Logout and redirect to login
            logout()
            navigate('/login')
        } catch (err) {
            toastError('Failed to update username: ' + err)
        }
    }

    const handleChangePassword = async () => {
        if (!passwordData.current || !passwordData.new || !passwordData.confirm) {
            toastError("All fields are required")
            return
        }
        if (passwordData.new !== passwordData.confirm) {
            toastError("New passwords do not match")
            return
        }
        if (passwordData.new.length < 8) {
            toastError("New password must be at least 8 characters")
            return
        }
        try {
            await api.updatePassword(passwordData.current, passwordData.new)
            success('Password updated successfully')
            setPasswordData({ current: '', new: '', confirm: '' })
        } catch (err) {
            toastError('Failed to update password: ' + err)
        }
    }


    const tabs = [
        {
            id: 'general',
            label: <span className="flex items-center gap-2"><SettingsIcon size={16} /> General</span>,
            content: (
                <GeneralTab
                    features={features}
                    setFeatures={setFeatures}
                    handleSaveFeatures={handleSaveFeatures}
                    handleServiceAction={handleServiceAction}
                    serviceStatus={serviceStatus}
                    publicIP={publicIP}
                    setPublicIP={setPublicIP}
                    success={success}
                />
            )
        },
        { id: 'singbox', label: <span className="flex items-center gap-2"><Server size={16} /> Sing-box</span>, content: <SingboxConfigEditor /> },
        {
            id: 'dashboard',
            label: <span className="flex items-center gap-2"><SettingsIcon size={16} /> Dashboard</span>,
            content: (
                <DashboardTab
                    dashboardPrefs={dashboardPrefs}
                    setDashboardPrefs={setDashboardPrefs}
                    success={success}
                />
            )
        },
        {
            id: 'security',
            label: <span className="flex items-center gap-2"><Shield size={16} /> Security</span>,
            content: (
                <SecurityTab
                    passwordData={passwordData}
                    setPasswordData={setPasswordData}
                    usernameData={usernameData}
                    setUsernameData={setUsernameData}
                    handleChangePassword={handleChangePassword}
                    handleChangeUsername={handleChangeUsername}
                />
            )
        },
        {
            id: 'database',
            label: <span className="flex items-center gap-2"><Database size={16} /> Database</span>,
            content: (
                <DatabaseTab
                    features={features}
                    setFeatures={setFeatures}
                    dbInfo={dbInfo}
                    handlePruneNow={handlePruneNow}
                    handleRunSampler={handleRunSampler}
                    handleTogglePause={handleTogglePause}
                    samplerRunning={samplerRunning}
                    handleSaveFeatures={handleSaveFeatures}
                    loadDbStats={loadDbStats}
                    historyLimit={historyLimit}
                    setHistoryLimit={setHistoryLimit}
                    samplerHistory={samplerHistory}
                />
            )
        },
    ]

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white">Settings</h1>
                </div>
                <div className="flex gap-3">
                    <Button
                        onClick={loadAll}
                        variant="secondary"
                        isLoading={loading && !samplerRunning}
                        icon={<RefreshCw size={16} />}
                    >
                        Refresh
                    </Button>
                </div>
            </div>

            <Tabs tabs={tabs} />
        </div>
    )
}

function DashboardTab({
    dashboardPrefs,
    setDashboardPrefs,
    success,
}: {
    dashboardPrefs: DashboardPrefs
    setDashboardPrefs: Dispatch<SetStateAction<DashboardPrefs>>
    success: (msg: string) => void
}) {
    const handleSave = () => {
        const normalized = {
            defaultService: dashboardPrefs.defaultService || 'singbox',
            refreshMs: Math.max(1000, Number(dashboardPrefs.refreshMs) || 10000),
            defaultRange: dashboardPrefs.defaultRange || '24h'
        }
        setDashboardPrefs(normalized)
        localStorage.setItem('dashboard_prefs', JSON.stringify(normalized))
        success('Dashboard preferences saved')
    }

    return (
        <div className="space-y-6">
            <Card title="Dashboard Preferences">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Default Service</label>
                        <select
                            value={dashboardPrefs.defaultService}
                            onChange={e => setDashboardPrefs(prev => ({ ...prev, defaultService: e.target.value as 'singbox' | 'wireguard' }))}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                        >
                            <option value="singbox">Sing-box</option>
                            <option value="wireguard">WireGuard</option>
                        </select>
                    </div>
                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Refresh Interval (seconds)</label>
                        <input
                            type="number"
                            min={1}
                            value={Math.floor(dashboardPrefs.refreshMs / 1000)}
                            onChange={e => setDashboardPrefs(prev => ({ ...prev, refreshMs: Math.max(1000, (Number(e.target.value) || 10) * 1000) }))}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                        />
                    </div>
                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Default Time Range</label>
                        <select
                            value={dashboardPrefs.defaultRange}
                            onChange={e => setDashboardPrefs(prev => ({ ...prev, defaultRange: e.target.value }))}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                        >
                            <option value="30m">Last 30 Minutes</option>
                            <option value="1h">Last Hour</option>
                            <option value="6h">Last 6 Hours</option>
                            <option value="24h">Last 24 Hours</option>
                            <option value="1w">Last Week</option>
                            <option value="1m">Last Month</option>
                        </select>
                    </div>
                </div>
                <div className="flex justify-end mt-4">
                    <Button onClick={handleSave} size="sm" icon={<Save size={16} />}>
                        Save Preferences
                    </Button>
                </div>
            </Card>
        </div>
    )
}
function GeneralTab({
    features,
    setFeatures,
    handleSaveFeatures,
    handleServiceAction,
    serviceStatus,
    publicIP,
    setPublicIP,
    success,
}: {
    features: FeatureFlags
    setFeatures: Dispatch<SetStateAction<FeatureFlags>>
    handleSaveFeatures: () => void
    handleServiceAction: (service: string, action: 'restart' | 'stop' | 'start') => void
    serviceStatus: ServiceStatus
    publicIP: string
    setPublicIP: Dispatch<SetStateAction<string>>
    success: (msg: string) => void
}) {
    return (
        <div className="space-y-6">
            {/* Features & Configuration */}
            <Card
                title="System Features"
                action={
                    <Button onClick={handleSaveFeatures} size="sm" icon={<Save size={16} />}>
                        Save Changes
                    </Button>
                }
            >
                <div className="space-y-6">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={features.enable_singbox}
                                onChange={e => setFeatures(prev => ({ ...prev, enable_singbox: e.target.checked }))}
                                className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <div>
                                <div className="font-semibold text-white">Enable sing-box</div>
                                <div className="text-xs text-slate-400 mt-1">Core VLESS/Trojan proxy service</div>
                            </div>
                        </label>
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={features.enable_wireguard}
                                onChange={e => setFeatures(prev => ({ ...prev, enable_wireguard: e.target.checked }))}
                                className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <div>
                                <div className="font-semibold text-white">Enable WireGuard</div>
                                <div className="text-xs text-slate-400 mt-1">VPN tunnel interface</div>
                            </div>
                        </label>
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={!!features.retention_enabled}
                                onChange={e => setFeatures(prev => ({ ...prev, retention_enabled: e.target.checked }))}
                                className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <div>
                                <div className="font-semibold text-white">Data Retention</div>
                                <div className="text-xs text-slate-400 mt-1">Auto-prune old stats</div>
                            </div>
                        </label>
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={!!features.aggregation_enabled}
                                onChange={e => setFeatures(prev => ({ ...prev, aggregation_enabled: e.target.checked }))}
                                className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <div>
                                <div className="font-semibold text-white">Data Aggregation</div>
                                <div className="text-xs text-slate-400 mt-1">Compress old history</div>
                            </div>
                        </label>
                    </div>
                </div>
            </Card>

            {/* Server Configuration */}
            <Card title="Server Configuration">
                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Public IP Address
                        </label>
                        <input
                            type="text"
                            value={publicIP}
                            onChange={e => setPublicIP(e.target.value)}
                            placeholder="Auto-detected or enter manually"
                            className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono"
                        />
                        <p className="text-xs text-slate-500 mt-1">
                            This IP will be used in QR codes and connection links. Leave empty for auto-detection.
                        </p>
                    </div>
                    <div className="flex justify-end">
                        <Button onClick={() => {
                            // TODO: Create endpoint to save public_ip
                            success('Public IP saved (endpoint pending)')
                        }} size="sm" icon={<Save size={16} />}>
                            Save Public IP
                        </Button>
                    </div>
                </div>
            </Card>

            {/* Service Control */}
            <Card title="Service Control">
                {features.systemctl_available === false ? (
                    <div className="bg-amber-500/10 border border-amber-500/20 rounded-lg p-4 text-amber-400 text-sm">
                        Service control is disabled (systemctl unavailable).
                    </div>
                ) : (
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                        {/* Singbox Control */}
                        <div className={`p-4 bg-slate-950 rounded-lg border border-slate-800 flex flex-col gap-4 ${!features.enable_singbox ? 'opacity-50' : ''}`}>
                            <div className="flex items-center justify-between">
                                <div className="font-semibold text-white">sing-box</div>
                                <Badge variant={serviceStatus.singbox ? 'success' : 'error'}>
                                    <div className={`w-1.5 h-1.5 rounded-full ${serviceStatus.singbox ? 'bg-emerald-500' : 'bg-red-500'}`} />
                                    {serviceStatus.singbox ? 'Running' : 'Stopped'}
                                </Badge>
                            </div>
                            <div className="flex gap-2">
                                <Button
                                    onClick={() => handleServiceAction('sing-box', 'restart')}
                                    disabled={!features.enable_singbox}
                                    variant="secondary"
                                    size="sm"
                                    className="flex-1"
                                >
                                    Restart
                                </Button>
                                <Button
                                    onClick={() => handleServiceAction('sing-box', serviceStatus.singbox ? 'stop' : 'start')}
                                    disabled={!features.enable_singbox}
                                    variant={serviceStatus.singbox ? 'danger' : 'primary'}
                                    size="sm"
                                    className="flex-1"
                                >
                                    {serviceStatus.singbox ? 'Stop' : 'Start'}
                                </Button>
                            </div>
                        </div>

                        {/* WireGuard Control */}
                        <div className={`p-4 bg-slate-950 rounded-lg border border-slate-800 flex flex-col gap-4 ${!features.enable_wireguard ? 'opacity-50' : ''}`}>
                            <div className="flex items-center justify-between">
                                <div className="font-semibold text-white">WireGuard</div>
                                <Badge variant={serviceStatus.wireguard ? 'success' : 'error'}>
                                    <div className={`w-1.5 h-1.5 rounded-full ${serviceStatus.wireguard ? 'bg-emerald-500' : 'bg-red-500'}`} />
                                    {serviceStatus.wireguard ? 'Running' : 'Stopped'}
                                </Badge>
                            </div>
                            <div className="flex gap-2">
                                <Button
                                    onClick={() => handleServiceAction('wireguard', 'restart')}
                                    disabled={!features.enable_wireguard}
                                    variant="secondary"
                                    size="sm"
                                    className="flex-1"
                                >
                                    Restart
                                </Button>
                                <Button
                                    onClick={() => handleServiceAction('wireguard', serviceStatus.wireguard ? 'stop' : 'start')}
                                    disabled={!features.enable_wireguard}
                                    variant={serviceStatus.wireguard ? 'danger' : 'primary'}
                                    size="sm"
                                    className="flex-1"
                                >
                                    {serviceStatus.wireguard ? 'Stop' : 'Start'}
                                </Button>
                            </div>
                        </div>
                    </div>
                )}
            </Card>
        </div>
    )
}

function SecurityTab({
    handleChangePassword,
    passwordData,
    setPasswordData,
    handleChangeUsername,
    usernameData,
    setUsernameData,
}: {
    handleChangePassword: () => void
    passwordData: PasswordData
    setPasswordData: Dispatch<SetStateAction<PasswordData>>
    handleChangeUsername: () => void
    usernameData: UsernameData
    setUsernameData: Dispatch<SetStateAction<UsernameData>>
}) {
    return (
        <div className="space-y-6">
            <Card title="Account Security">
                <div className="space-y-6">
                    {/* Change Password */}
                    <div className="space-y-4">
                        <div className="flex items-center justify-between">
                            <h3 className="text-sm font-medium text-white">Change Password</h3>
                            <Button onClick={handleChangePassword} size="sm" icon={<Lock size={16} />}>
                                Update Password
                            </Button>
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Current Password</label>
                            <input
                                type="password"
                                value={passwordData.current}
                                onChange={e => setPasswordData(prev => ({ ...prev, current: e.target.value }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                placeholder="Enter current password"
                            />
                        </div>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">New Password</label>
                                <input
                                    type="password"
                                    value={passwordData.new}
                                    onChange={e => setPasswordData(prev => ({ ...prev, new: e.target.value }))}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                    placeholder="Min 8 characters"
                                />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Confirm New Password</label>
                                <input
                                    type="password"
                                    value={passwordData.confirm}
                                    onChange={e => setPasswordData(prev => ({ ...prev, confirm: e.target.value }))}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                    placeholder="Confirm new password"
                                />
                            </div>
                        </div>
                    </div>

                    <div className="w-full h-px bg-slate-800/50" />

                    {/* Change Username */}
                    <div className="space-y-4">
                        <div className="flex items-center justify-between">
                            <h3 className="text-sm font-medium text-white">Change Username</h3>
                            <Button onClick={handleChangeUsername} size="sm" icon={<User size={16} />}>
                                Update Username
                            </Button>
                        </div>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Current Password</label>
                                <input
                                    type="password"
                                    value={usernameData.password}
                                    onChange={e => setUsernameData(prev => ({ ...prev, password: e.target.value }))}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                    placeholder="Required to change username"
                                />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">New Username</label>
                                <input
                                    type="text"
                                    value={usernameData.newUsername}
                                    onChange={e => setUsernameData(prev => ({ ...prev, newUsername: e.target.value }))}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                    placeholder="Enter new username"
                                />
                            </div>
                        </div>
                    </div>
                </div>
            </Card>
        </div>
    )
}

function DatabaseTab({
    features,
    setFeatures,
    dbInfo,
    handlePruneNow,
    handleRunSampler,
    handleTogglePause,
    samplerRunning,
    handleSaveFeatures,
    loadDbStats,
    historyLimit,
    setHistoryLimit,
    samplerHistory,
}: {
    features: FeatureFlags
    setFeatures: Dispatch<SetStateAction<FeatureFlags>>
    dbInfo: DbInfo
    handlePruneNow: () => void
    handleRunSampler: () => void
    handleTogglePause: () => void
    samplerRunning: boolean
    handleSaveFeatures: () => void
    loadDbStats: () => Promise<void>
    historyLimit: number
    setHistoryLimit: Dispatch<SetStateAction<number>>
    samplerHistory: any[]
}) {
    return (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">
            <Card
                title="Database & Retention"
                action={
                    <div className="flex gap-2">
                        <Button onClick={handleSaveFeatures} size="sm" icon={<Save size={16} />}>
                            Save Changes
                        </Button>
                        <Button onClick={loadDbStats} variant="icon" size="icon" icon={<RefreshCw size={16} />} />
                    </div>
                }
            >
                <div className="grid grid-cols-2 gap-4 mb-6">
                    <div className="p-3 bg-slate-950 border border-slate-800 rounded-lg">
                        <p className="text-[10px] uppercase text-slate-500 font-bold">Total Rows</p>
                        <p className="text-xl font-mono text-white mt-1">{dbInfo.rows.toLocaleString()}</p>
                    </div>
                    <div className="p-3 bg-slate-950 border border-slate-800 rounded-lg">
                        <p className="text-[10px] uppercase text-slate-500 font-bold">Size (MB)</p>
                        <p className="text-xl font-mono text-white mt-1">{dbInfo.sizeMB}</p>
                    </div>
                </div>

                {/* Database Configuration Inputs */}
                <div className="space-y-4 mb-6 pt-4 border-t border-slate-800">
                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Retention Days (Sing-Box)</label>
                            <input
                                type="number"
                                min={1}
                                value={features.retention_days ?? 90}
                                onChange={e => setFeatures(prev => ({ ...prev, retention_days: parseInt(e.target.value) }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Retention Days (Wireguard)</label>
                            <input
                                type="number"
                                min={1}
                                value={features.wg_retention_days ?? 30}
                                onChange={e => setFeatures(prev => ({ ...prev, wg_retention_days: parseInt(e.target.value) }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">SB Interval (s)</label>
                            <input
                                type="number"
                                min={15}
                                value={features.sampler_interval_sec ?? 120}
                                onChange={e => setFeatures(prev => ({ ...prev, sampler_interval_sec: parseInt(e.target.value) }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">WG Interval (s)</label>
                            <input
                                type="number"
                                min={15}
                                value={features.wg_sampler_interval_sec ?? 60}
                                onChange={e => setFeatures(prev => ({ ...prev, wg_sampler_interval_sec: parseInt(e.target.value) }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Aggregation Days</label>
                            <input
                                type="number"
                                min={1}
                                value={features.aggregation_days ?? 7}
                                onChange={e => setFeatures(prev => ({ ...prev, aggregation_days: parseInt(e.target.value) }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                            />
                        </div>
                    </div>
                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Active Threshold (Bytes)</label>
                        <input
                            type="number"
                            min={0}
                            value={features.active_threshold_bytes ?? 1024}
                            onChange={e => setFeatures(prev => ({ ...prev, active_threshold_bytes: parseInt(e.target.value) }))}
                            className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                        />
                    </div>
                </div>

                <div className="space-y-3">
                    <Button
                        onClick={handlePruneNow}
                        disabled={!features.retention_enabled}
                        variant="secondary"
                        className="w-full"
                    >
                        Prune Database Now
                    </Button>
                    <div className="flex gap-2">
                        <Button
                            onClick={handleRunSampler}
                            disabled={samplerRunning}
                            className="flex-1"
                            isLoading={samplerRunning}
                            variant="primary"
                        >
                            Run Sampler
                        </Button>
                        <Button
                            onClick={handleTogglePause}
                            variant="secondary"
                            className={`flex-1 ${features.sampler_paused ? 'bg-emerald-900/20 text-emerald-400 border-emerald-900/30' : 'bg-amber-900/20 text-amber-400 border-amber-900/30'}`}
                        >
                            {features.sampler_paused ? 'Resume' : 'Pause'}
                        </Button>
                    </div>
                </div>
            </Card>

            <Card
                title="Sampler History"
                action={
                    <select
                        value={historyLimit}
                        onChange={e => setHistoryLimit(parseInt(e.target.value))}
                        className="bg-slate-950 border border-slate-800 rounded px-2 py-1 text-slate-400 text-xs outline-none focus:border-slate-700"
                    >
                        <option value={5}>Last 5</option>
                        <option value={10}>Last 10</option>
                        <option value={20}>Last 20</option>
                    </select>
                }
            >
                <div className="space-y-0 text-sm">
                    {samplerHistory.length === 0 ? (
                        <p className="text-slate-500 text-xs italic">No history available</p>
                    ) : (
                        samplerHistory.map((run, idx) => (
                            <div key={idx} className="flex justify-between items-center py-2 border-b border-slate-800/50 last:border-0">
                                <div>
                                    <div className="flex items-center gap-2">
                                        <div className="text-slate-300 text-xs">{new Date(run.timestamp * 1000).toLocaleTimeString()}</div>
                                        <span className={`text-[10px] px-1.5 py-0.5 rounded ${run.source === 'wireguard' ? 'bg-orange-900/20 text-orange-400 border border-orange-900/30' : 'bg-blue-900/20 text-blue-400 border border-blue-900/30'}`}>
                                            {run.source === 'wireguard' ? 'WG' : 'Proxy'}
                                        </span>
                                    </div>
                                    {run.error && <div className="text-red-400 text-[10px] truncate max-w-[150px]">{run.error}</div>}
                                </div>
                                <div className="text-right">
                                    <div className="font-mono text-emerald-400 text-xs">+{run.inserted} rows</div>
                                    <div className="text-slate-500 text-[10px]">{run.duration_ms}ms</div>
                                </div>
                            </div>
                        ))
                    )}
                </div>
            </Card>
        </div>
    )
}
