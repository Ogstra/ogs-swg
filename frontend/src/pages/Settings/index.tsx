import { useState, useEffect, useRef } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, FeatureFlags, SamplerHistoryEntry, SubscriptionRequestHistoryEntry } from '../../services/api'
import type { WireGuardInterfaceSummary } from '../../services/api'
import { Save, RefreshCw, UserCog, Shield, Plus, Trash2, Power, FileJson, Edit } from 'lucide-react'
import { useToast } from '../../context/ToastContext'
import { Card } from '../../components/ui/Card'
import { Button } from '../../components/ui/Button'
import { Badge } from '../../components/ui/Badge'
import { Modal } from '../../components/ui/Modal'
import { ConfirmModal } from '../../components/ui/ConfirmModal'
import { useAuth } from '../../context/AuthContext'
import SingboxConfigEditor from '../../components/SingboxConfigEditor'
import { Tabs } from '../../components/ui/Tabs'
import { Database, Settings as SettingsIcon, Server } from 'lucide-react'
import PanelUsers from './components/PanelUsers'
import { WireGuardRawConfigModal } from './components/WireGuardRawConfigModal'
import {
    WG_INTERFACE_DEFAULTS,
    normalizeWireGuardInterfaceCreateInput,
    normalizeWireGuardInterfaceEditInput,
    validateWireGuardInterfaceCreate,
    validateWireGuardInterfaceEdit,
} from '../../utils/wireguardForms'

type ServiceStatus = { singbox: boolean | null; wireguard: boolean | null }
type DbInfo = { rows: number; sizeMB: number }
type DashboardPrefs = { defaultService: 'singbox' | 'wireguard'; refreshMs: number; defaultRange: string }

export default function Settings() {
    const { success, error: toastError } = useToast()
    const { permissions } = useAuth()
    const canWriteSettings = !!permissions?.can_write_settings
    const canWriteConfig = !!permissions?.can_write_config
    const [loading, setLoading] = useState(false)
    const [samplerRunning, setSamplerRunning] = useState(false)
    const [dbInfo, setDbInfo] = useState<{ rows: number; sizeMB: number }>({ rows: 0, sizeMB: 0 })
    const [samplerHistory, setSamplerHistory] = useState<SamplerHistoryEntry[]>([])
    const [subscriptionRequestHistory, setSubscriptionRequestHistory] = useState<SubscriptionRequestHistoryEntry[]>([])
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
    const [historyLimit, setHistoryLimit] = useState(10)
    const [serviceStatus, setServiceStatus] = useState<{ singbox: boolean | null; wireguard: boolean | null }>({ singbox: null, wireguard: null })
    const [publicIP, setPublicIP] = useState<string>('')
    const [subscriptionDomain, setSubscriptionDomain] = useState<string>('')
    const [dashboardPrefs, setDashboardPrefs] = useState<DashboardPrefs>({
        defaultService: 'singbox',
        refreshMs: 10000,
        defaultRange: '24h'
    })
    const featuresQuery = useQuery({
        queryKey: ['settings-features'],
        queryFn: () => api.getFeatures(),
        placeholderData: previousData => previousData,
    })

    const statusQuery = useQuery({
        queryKey: ['settings-system-status'],
        queryFn: () => api.getSystemStatus(),
        placeholderData: previousData => previousData,
    })

    const samplerHistoryQuery = useQuery({
        queryKey: ['settings-sampler-history', historyLimit],
        queryFn: () => api.getSamplerHistory(historyLimit),
        placeholderData: previousData => previousData,
        refetchInterval: Math.max(15_000, Math.min(features.sampler_interval_sec ?? 120, features.wg_sampler_interval_sec ?? 60) * 1000),
    })

    const subscriptionRequestHistoryQuery = useQuery({
        queryKey: ['settings-subscription-request-history', historyLimit],
        queryFn: () => api.getSubscriptionRequestHistory(historyLimit),
        placeholderData: previousData => previousData,
        refetchInterval: Math.max(15_000, Math.min(features.sampler_interval_sec ?? 120, features.wg_sampler_interval_sec ?? 60) * 1000),
    })

    const publicIPQuery = useQuery({
        queryKey: ['settings-public-ip'],
        queryFn: () => api.getPublicIP(),
        placeholderData: previousData => previousData,
    })

    const subDomainQuery = useQuery({
        queryKey: ['settings-subscription-domain'],
        queryFn: () => api.getSubscriptionDomain(),
        placeholderData: previousData => previousData,
    })

    useEffect(() => {
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
    }, [])

    useEffect(() => {
        if (!featuresQuery.data) return
        setFeatures(featuresQuery.data)
    }, [featuresQuery.data])

    useEffect(() => {
        const status = statusQuery.data
        if (!status) return
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
            singbox: status.singbox ?? null,
            wireguard: status.wireguard ?? null
        })
    }, [statusQuery.data])

    useEffect(() => {
        const h = samplerHistoryQuery.data
        if (!h) return
        setSamplerHistory(Array.isArray(h) ? h : [])
    }, [samplerHistoryQuery.data])

    useEffect(() => {
        const h = subscriptionRequestHistoryQuery.data
        if (!h) return
        setSubscriptionRequestHistory(Array.isArray(h) ? h : [])
    }, [subscriptionRequestHistoryQuery.data])

    useEffect(() => {
        if (typeof publicIPQuery.data !== 'string') return
        setPublicIP(publicIPQuery.data || '')
    }, [publicIPQuery.data])

    useEffect(() => {
        if (typeof subDomainQuery.data !== 'string') return
        setSubscriptionDomain(subDomainQuery.data || '')
    }, [subDomainQuery.data])

    const loadFeatures = async () => {
        await featuresQuery.refetch()
    }

    const loadDbStats = async () => {
        await statusQuery.refetch()
    }

    const loadSamplerHistory = async () => {
        await Promise.all([
            samplerHistoryQuery.refetch(),
            subscriptionRequestHistoryQuery.refetch(),
        ])
    }

    const loadAll = async () => {
        setLoading(true)
        try {
            await Promise.all([
                loadFeatures(),
                loadDbStats(),
                loadSamplerHistory(),
                publicIPQuery.refetch(),
                subDomainQuery.refetch(),
            ])
        } finally {
            setLoading(false)
        }
    }

    const handleSaveFeatures = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        try {
            await api.updateFeatures(features)
            success('Feature toggles saved successfully')
        } catch (err) {
            toastError('Failed to save feature toggles: ' + err)
        }
    }

    const handleSavePublicIP = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        try {
            await api.updatePublicIP(publicIP.trim())
            success('Public IP saved')
        } catch (err) {
            toastError('Failed to save public IP: ' + err)
        }
    }

    const handleSaveSubscriptionDomain = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        try {
            await api.updateSubscriptionDomain(subscriptionDomain.trim())
            success('Subscription Domain saved')
        } catch (err) {
            toastError('Failed to save subscription domain: ' + err)
        }
    }

    const handleRunSampler = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
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
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
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
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
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
        if (!canWriteConfig) {
            toastError('No write permission for service control')
            return
        }
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

    const tabs = [
        {
            id: 'general',
            label: <span className="flex items-center gap-2"><SettingsIcon size={16} /> General</span>,
            content: (
                <GeneralTab
                    features={features}
                    setFeatures={setFeatures}
                    handleSaveFeatures={handleSaveFeatures}
                    handleSavePublicIP={handleSavePublicIP}
                    handleSaveSubscriptionDomain={handleSaveSubscriptionDomain}
                    handleServiceAction={handleServiceAction}
                    serviceStatus={serviceStatus}
                    publicIP={publicIP}
                    setPublicIP={setPublicIP}
                    subscriptionDomain={subscriptionDomain}
                    setSubscriptionDomain={setSubscriptionDomain}
                    canWriteSettings={canWriteSettings}
                    canWriteConfig={canWriteConfig}
                />
            )
        },
        { id: 'singbox', label: <span className="flex items-center gap-2"><Server size={16} /> Sing-box</span>, content: <SingboxConfigEditor /> },
        {
            id: 'wireguard-interfaces',
            label: <span className="flex items-center gap-2"><Shield size={16} /> WireGuard</span>,
            content: <WireGuardInterfacesTab />,
        },
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
            id: 'database',
            label: <span className="flex items-center gap-2"><Database size={16} /> Database</span>,
            content: (
                <DatabaseTab
                    features={features}
                    setFeatures={setFeatures}
                    dbInfo={dbInfo}
                    canWriteSettings={canWriteSettings}
                    handlePruneNow={handlePruneNow}
                    handleRunSampler={handleRunSampler}
                    handleTogglePause={handleTogglePause}
                    samplerRunning={samplerRunning}
                    handleSaveFeatures={handleSaveFeatures}
                    loadDatabasePanel={loadAll}
                    historyLimit={historyLimit}
                    setHistoryLimit={setHistoryLimit}
                    samplerHistory={samplerHistory}
                    subscriptionRequestHistory={subscriptionRequestHistory}
                />
            )
        },
        ...(permissions?.can_read_panel_users ? [{
            id: 'panel-users',
            label: <span className="flex items-center gap-2"><UserCog size={16} /> Admins</span>,
            content: <PanelUsers />,
        }] : []),
    ].filter(tab => {
        if (tab.id === 'singbox') return !!permissions?.can_read_config
        if (tab.id === 'wireguard-interfaces') return !!permissions?.can_read_wireguard
        return true
    })

    return (
        <div className="h-full min-h-0 flex flex-col gap-0 sm:gap-6">
            <div className="flex items-center justify-between">

                <div className="flex gap-3">
                    <Button
                        onClick={loadAll}
                        variant="secondary"
                        isLoading={loading && !samplerRunning}
                        icon={<RefreshCw size={16} />}
                        className="hidden sm:inline-flex"
                    >
                        Refresh
                    </Button>
                </div>
            </div>

            <Tabs
                tabs={tabs}
                className="flex-1 min-h-0"
                headerRight={
                    <button
                        onClick={() => loadAll()}
                        className="sm:hidden w-[38px] h-[38px] inline-flex items-center justify-center rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 border border-slate-700 transition-colors"
                        title="Refresh"
                        aria-label="Refresh settings"
                    >
                        <RefreshCw size={16} className={loading && !samplerRunning ? 'animate-spin' : ''} />
                    </button>
                }
            />
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
        <div className="space-y-4 sm:space-y-6">
            <Card title="Dashboard Preferences">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Default Service</label>
                        <select
                            value={dashboardPrefs.defaultService}
                            onChange={e => setDashboardPrefs(prev => ({ ...prev, defaultService: e.target.value as 'singbox' | 'wireguard' }))}
                            className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
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
                            className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
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
    handleSavePublicIP,
    handleSaveSubscriptionDomain,
    handleServiceAction,
    serviceStatus,
    publicIP,
    setPublicIP,
    subscriptionDomain,
    setSubscriptionDomain,
    canWriteSettings,
    canWriteConfig,
}: {
    features: FeatureFlags
    setFeatures: Dispatch<SetStateAction<FeatureFlags>>
    handleSaveFeatures: () => void
    handleSavePublicIP: () => void
    handleSaveSubscriptionDomain: () => void
    handleServiceAction: (service: string, action: 'restart' | 'stop' | 'start') => void
    serviceStatus: ServiceStatus
    publicIP: string
    setPublicIP: Dispatch<SetStateAction<string>>
    subscriptionDomain: string
    setSubscriptionDomain: Dispatch<SetStateAction<string>>
    canWriteSettings: boolean
    canWriteConfig: boolean
}) {
    return (
        <div className="space-y-4 sm:space-y-6 pb-4 sm:pb-0">
            {/* Service Control */}
            <Card title="Service Control">
                {features.systemctl_available === false ? (
                    <div className="bg-amber-500/10 border border-amber-500/20 rounded-lg p-4 text-amber-400 text-sm">
                        Service control is disabled (systemctl unavailable).
                    </div>
                ) : (
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 sm:gap-6">
                        {/* Singbox Control */}
                        <div className={`p-4 bg-slate-950 rounded-lg border border-slate-800 flex flex-col gap-4 ${!features.enable_singbox ? 'opacity-50' : ''}`}>
                            <div className="flex items-center justify-between">
                                <div className="font-semibold text-white">sing-box</div>
                                <Badge variant={serviceStatus.singbox === true ? 'success' : serviceStatus.singbox === false ? 'error' : 'neutral'}>
                                    <div className={`w-1.5 h-1.5 rounded-full ${serviceStatus.singbox === true ? 'bg-emerald-500' : serviceStatus.singbox === false ? 'bg-red-500' : 'bg-slate-500'}`} />
                                    {serviceStatus.singbox === true ? 'Running' : serviceStatus.singbox === false ? 'Stopped' : 'Unknown'}
                                </Badge>
                            </div>
                            <div className="flex gap-2">
                                <Button
                                    onClick={() => handleServiceAction('sing-box', 'restart')}
                                    disabled={!features.enable_singbox || !canWriteConfig}
                                    variant="secondary"
                                    size="sm"
                                    className="flex-1"
                                >
                                    Restart
                                </Button>
                                <Button
                                    onClick={() => handleServiceAction('sing-box', serviceStatus.singbox ? 'stop' : 'start')}
                                    disabled={!features.enable_singbox || !canWriteConfig}
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
                                <Badge variant={serviceStatus.wireguard === true ? 'success' : serviceStatus.wireguard === false ? 'error' : 'neutral'}>
                                    <div className={`w-1.5 h-1.5 rounded-full ${serviceStatus.wireguard === true ? 'bg-emerald-500' : serviceStatus.wireguard === false ? 'bg-red-500' : 'bg-slate-500'}`} />
                                    {serviceStatus.wireguard === true ? 'Running' : serviceStatus.wireguard === false ? 'Stopped' : 'Unknown'}
                                </Badge>
                            </div>
                            <div className="flex gap-2">
                                <Button
                                    onClick={() => handleServiceAction('wireguard', 'restart')}
                                    disabled={!features.enable_wireguard || !canWriteConfig}
                                    variant="secondary"
                                    size="sm"
                                    className="flex-1"
                                >
                                    Restart
                                </Button>
                                <Button
                                    onClick={() => handleServiceAction('wireguard', serviceStatus.wireguard ? 'stop' : 'start')}
                                    disabled={!features.enable_wireguard || !canWriteConfig}
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

            {/* Server Configuration */}
            <Card title="Server Configuration">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Public IP Address / Domain
                        </label>
                        <input
                            type="text"
                            value={publicIP}
                            onChange={e => setPublicIP(e.target.value)}
                            disabled={!canWriteSettings}
                            placeholder="Auto-detected or enter manually"
                            className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono"
                        />
                        <p className="text-xs text-slate-500 mt-1">
                            Used in QR codes and connection links. Leave empty for auto-detection.
                        </p>
                        <div className="flex justify-end mt-2">
                            <Button onClick={handleSavePublicIP} size="sm" icon={<Save size={16} />} disabled={!canWriteSettings}>
                                Save
                            </Button>
                        </div>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Subscription API Domain
                        </label>
                        <input
                            type="text"
                            value={subscriptionDomain}
                            onChange={e => setSubscriptionDomain(e.target.value)}
                            disabled={!canWriteSettings}
                            placeholder="e.g. sub.example.com"
                            className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono"
                        />
                        <p className="text-xs text-slate-500 mt-1">
                            Domain for subscription links (/s/[token]).
                        </p>
                        <div className="flex justify-end mt-2">
                            <Button onClick={handleSaveSubscriptionDomain} size="sm" icon={<Save size={16} />} disabled={!canWriteSettings}>
                                Save
                            </Button>
                        </div>
                    </div>
                </div>
            </Card>

            {/* Features & Configuration */}
                <Card
                title="System Features"
                action={
                    <Button onClick={handleSaveFeatures} size="sm" icon={<Save size={16} />} disabled={!canWriteSettings}>
                        Save Changes
                    </Button>
                }
            >
                <div className="space-y-4 sm:space-y-6">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={features.enable_singbox}
                                onChange={e => setFeatures(prev => ({ ...prev, enable_singbox: e.target.checked }))}
                                disabled={!canWriteSettings}
                                className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <div>
                                <div className="font-semibold text-white">Enable sing-box</div>
                            </div>
                        </label>
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={features.enable_wireguard}
                                onChange={e => setFeatures(prev => ({ ...prev, enable_wireguard: e.target.checked }))}
                                disabled={!canWriteSettings}
                                className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <div>
                                <div className="font-semibold text-white">Enable WireGuard</div>
                            </div>
                        </label>
                        <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                            <input
                                type="checkbox"
                                checked={!!features.retention_enabled}
                                onChange={e => setFeatures(prev => ({ ...prev, retention_enabled: e.target.checked }))}
                                disabled={!canWriteSettings}
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
                                disabled={!canWriteSettings}
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
        </div>
    )
}

function WireGuardInterfacesTab() {
    const queryClient = useQueryClient()
    const { success, error: toastError } = useToast()
    const { permissions } = useAuth()
    const canWriteWG = !!permissions?.can_write_wireguard

    const interfacesQuery = useQuery({
        queryKey: ['settings-wg-interfaces-status'],
        queryFn: () => api.getWireGuardInterfacesStatus(),
        placeholderData: (previousData: WireGuardInterfaceSummary[] | undefined) => previousData,
        refetchInterval: 30_000,
    })
    const interfaces = interfacesQuery.data ?? []
    const [createOpen, setCreateOpen] = useState(false)
    const [editOpen, setEditOpen] = useState(false)
    const [editTarget, setEditTarget] = useState<string | null>(null)
    const [deleteTarget, setDeleteTarget] = useState<WireGuardInterfaceSummary | null>(null)
    const [rawConfigTarget, setRawConfigTarget] = useState<string | null>(null)
    const [rawConfigOpen, setRawConfigOpen] = useState(false)
    const [busyKey, setBusyKey] = useState<string | null>(null)

    const [createName, setCreateName] = useState('')
    const [createSubnet, setCreateSubnet] = useState('')
    const [createListenPort, setCreateListenPort] = useState(String(WG_INTERFACE_DEFAULTS.listenPort))
    const [createErrors, setCreateErrors] = useState<Record<string, string>>({})

    const [editAddress, setEditAddress] = useState('')
    const [editBindAddress, setEditBindAddress] = useState('')
    const [editListenPort, setEditListenPort] = useState(String(WG_INTERFACE_DEFAULTS.listenPort))
    const [editPostUp, setEditPostUp] = useState('')
    const [editPostDown, setEditPostDown] = useState('')
    const [editMTU, setEditMTU] = useState('')
    const [editDNS, setEditDNS] = useState('')
    const [editErrors, setEditErrors] = useState<Record<string, string>>({})

    const refreshInterfaces = async () => {
        await queryClient.invalidateQueries({ queryKey: ['settings-wg-interfaces-status'] })
    }

    const resetCreateForm = () => {
        setCreateName('')
        setCreateSubnet('')
        setCreateListenPort(String(WG_INTERFACE_DEFAULTS.listenPort))
        setCreateErrors({})
    }

    const closeCreateModal = () => {
        if (busyKey === 'create') return
        setCreateOpen(false)
        resetCreateForm()
    }

    const closeEditModal = () => {
        if (busyKey === 'edit') return
        setEditOpen(false)
        setEditTarget(null)
        setEditErrors({})
    }

    const handleCreateInterface = async () => {
        if (!canWriteWG) {
            toastError('No write permission for WireGuard')
            return
        }
        const createInput = {
            name: createName,
            subnet: createSubnet,
            listenPort: createListenPort,
        }
        const errors = validateWireGuardInterfaceCreate(createInput)
        setCreateErrors(errors)
        if (Object.keys(errors).length > 0) return

        const normalized = normalizeWireGuardInterfaceCreateInput(createInput)
        const payload = {
            name: normalized.name,
            subnet: normalized.subnet,
            listen_port: normalized.listenPort,
        }
        setBusyKey('create')
        try {
            await api.createWireGuardInterface(payload)
            success(`Interface ${payload.name} created`)
            setCreateOpen(false)
            resetCreateForm()
            await refreshInterfaces()
        } catch (err) {
            toastError('Failed to create interface: ' + err)
        } finally {
            setBusyKey(null)
        }
    }

    const handleOpenEdit = async (iface: WireGuardInterfaceSummary) => {
        if (!canWriteWG) {
            toastError('No write permission for WireGuard')
            return
        }
        const key = `load-edit:${iface.name}`
        setBusyKey(key)
        setEditErrors({})
        try {
            const cfg = await api.getWireGuardInterfaceForInterface(iface.name)
            setEditTarget(iface.name)
            setEditAddress(String(cfg?.address || ''))
            setEditBindAddress(String(cfg?.bind_address || ''))
            setEditListenPort(String(cfg?.listen_port ?? WG_INTERFACE_DEFAULTS.listenPort))
            setEditPostUp(String(cfg?.post_up || ''))
            setEditPostDown(String(cfg?.post_down || ''))
            setEditMTU(cfg?.mtu === undefined || cfg?.mtu === null ? String(WG_INTERFACE_DEFAULTS.mtu) : String(cfg.mtu))
            setEditDNS(String(cfg?.dns ?? WG_INTERFACE_DEFAULTS.dns))
            setEditOpen(true)
        } catch (err) {
            toastError('Failed to load interface config: ' + err)
        } finally {
            setBusyKey(null)
        }
    }

    const handleSaveEdit = async () => {
        if (!canWriteWG) {
            toastError('No write permission for WireGuard')
            return
        }
        if (!editTarget) return
        const editInput = {
            address: editAddress,
            listenPort: editListenPort,
            mtu: editMTU,
        }
        const errors = validateWireGuardInterfaceEdit(editInput)
        setEditErrors(errors)
        if (Object.keys(errors).length > 0) return

        const normalized = normalizeWireGuardInterfaceEditInput(editInput)
        const payload = {
            address: normalized.address,
            bind_address: editBindAddress.trim(),
            listen_port: normalized.listenPort,
            post_up: editPostUp.trim(),
            post_down: editPostDown.trim(),
            mtu: normalized.mtu,
            dns: editDNS.trim(),
        }

        setBusyKey('edit')
        try {
            await api.updateWireGuardInterfaceForInterface(editTarget, payload)
            success(`Interface ${editTarget} updated`)
            closeEditModal()
            await refreshInterfaces()
        } catch (err) {
            toastError('Failed to update interface: ' + err)
        } finally {
            setBusyKey(null)
        }
    }

    const handleToggleInterface = async (iface: WireGuardInterfaceSummary) => {
        if (!canWriteWG) {
            toastError('No write permission for WireGuard')
            return
        }
        const key = `toggle:${iface.name}`
        setBusyKey(key)
        try {
            if (iface.is_up) {
                await api.disableWireGuardInterface(iface.name)
                success(`Interface ${iface.name} disabled`)
            } else {
                await api.enableWireGuardInterface(iface.name)
                success(`Interface ${iface.name} enabled`)
            }
            await refreshInterfaces()
        } catch (err) {
            toastError(`Failed to ${iface.is_up ? 'disable' : 'enable'} interface: ` + err)
        } finally {
            setBusyKey(null)
        }
    }

    const handleConfirmDelete = async () => {
        if (!canWriteWG) {
            toastError('No write permission for WireGuard')
            return
        }
        if (!deleteTarget) return
        const target = deleteTarget
        setBusyKey(`delete:${target.name}`)
        try {
            await api.deleteWireGuardInterface(target.name)
            success(`Interface ${target.name} deleted`)
            setDeleteTarget(null)
            await refreshInterfaces()
        } catch (err) {
            toastError('Failed to delete interface: ' + err)
        } finally {
            setBusyKey(null)
        }
    }

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <h3 className="text-lg font-semibold text-white">WireGuard Interfaces</h3>
                <Button
                    onClick={() => setCreateOpen(true)}
                    size="sm"
                    icon={<Plus size={16} />}
                    disabled={!canWriteWG}
                    title={canWriteWG ? 'Create interface' : 'No write permission'}
                >
                    Add Interface
                </Button>
            </div>

            {interfacesQuery.isLoading ? (
                <div className="text-slate-400 text-sm animate-pulse">Loading interfaces...</div>
            ) : interfaces.length === 0 ? (
                <div className="p-8 border border-dashed border-slate-800 rounded-xl text-center text-slate-500">
                    No WireGuard interfaces configured. Click "Add Interface" to create one.
                </div>
            ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {interfaces.map(iface => (
                        <div
                            key={iface.name}
                            className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex flex-col justify-between gap-4 group shadow-sm hover:border-slate-700 hover:shadow-md transition-all"
                        >
                            <div className="space-y-2">
                                <div className="flex items-center justify-between">
                                    <Badge variant={iface.is_up ? 'success' : 'neutral'}>
                                        <div className={`w-1.5 h-1.5 rounded-full ${iface.is_up ? 'bg-emerald-500' : 'bg-slate-500'}`} />
                                        {iface.is_up ? 'Up' : 'Down'}
                                    </Badge>
                                    <div className="flex gap-1.5">
                                        <button
                                            onClick={() => handleOpenEdit(iface)}
                                            disabled={!canWriteWG || busyKey === `load-edit:${iface.name}`}
                                            title={canWriteWG ? 'Edit interface config' : 'No write permission'}
                                            className="w-10 h-10 flex items-center justify-center text-slate-300 hover:text-white rounded-xl border border-slate-700 bg-slate-800 hover:bg-slate-700 shadow-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                        >
                                            <Edit size={15} strokeWidth={1.6} />
                                        </button>
                                        <button
                                            onClick={() => {
                                                setRawConfigTarget(iface.name)
                                                setRawConfigOpen(true)
                                            }}
                                            disabled={!canWriteWG}
                                            title={canWriteWG ? 'Edit raw config' : 'No write permission'}
                                            className="w-10 h-10 flex items-center justify-center text-slate-300 hover:text-white rounded-xl border border-slate-700 bg-slate-800 hover:bg-slate-700 shadow-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                        >
                                            <FileJson size={15} strokeWidth={1.6} />
                                        </button>
                                        <button
                                            onClick={() => handleToggleInterface(iface)}
                                            disabled={!canWriteWG || busyKey === `toggle:${iface.name}`}
                                            title={canWriteWG ? (iface.is_up ? 'Disable interface' : 'Enable interface') : 'No write permission'}
                                            className="w-10 h-10 flex items-center justify-center text-slate-300 hover:text-white rounded-xl border border-slate-700 bg-slate-800 hover:bg-slate-700 shadow-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                        >
                                            <Power size={15} strokeWidth={1.6} />
                                        </button>
                                        <button
                                            onClick={() => setDeleteTarget(iface)}
                                            disabled={!canWriteWG || busyKey === `delete:${iface.name}`}
                                            title={canWriteWG ? 'Delete interface' : 'No write permission'}
                                            className="w-10 h-10 flex items-center justify-center text-slate-300 hover:text-white rounded-xl border border-slate-700 bg-slate-800 hover:bg-slate-700 shadow-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                        >
                                            <Trash2 size={15} strokeWidth={1.6} />
                                        </button>
                                    </div>
                                </div>
                                <div>
                                    <div className="text-white font-semibold font-mono truncate" title={iface.name}>
                                        {iface.name}
                                    </div>
                                    <div className="text-slate-500 text-xs mt-1 font-mono">
                                        {iface.address || '—'}{iface.listen_port > 0 ? ` · :${iface.listen_port}` : ''}
                                    </div>
                                </div>
                            </div>
                            <div className="pt-3 border-t border-slate-800/50 flex gap-4 text-xs text-slate-400 items-center">
                                <span>{iface.peer_count} peer{iface.peer_count !== 1 ? 's' : ''}</span>
                            </div>
                        </div>
                    ))}
                </div>
            )}

            <Modal
                isOpen={createOpen}
                onClose={closeCreateModal}
                title="Create WireGuard Interface"
                footer={
                    <>
                        <Button variant="ghost" onClick={closeCreateModal} disabled={busyKey === 'create'}>Cancel</Button>
                        <Button variant="primary" onClick={handleCreateInterface} isLoading={busyKey === 'create'} disabled={!canWriteWG}>
                            Create Interface
                        </Button>
                    </>
                }
            >
                <div className="space-y-4 modal-form-uniform">
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Name</label>
                        <input
                            type="text"
                            value={createName}
                            onChange={(e) => setCreateName(e.target.value)}
                            placeholder="wg1"
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                        {createErrors.name && <p className="text-xs text-amber-400 mt-1">{createErrors.name}</p>}
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Subnet (CIDR)</label>
                        <input
                            type="text"
                            value={createSubnet}
                            onChange={(e) => setCreateSubnet(e.target.value)}
                            placeholder="10.20.0.0/24"
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                        {createErrors.subnet && <p className="text-xs text-amber-400 mt-1">{createErrors.subnet}</p>}
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Listen Port</label>
                        <input
                            type="number"
                            min={1}
                            max={65535}
                            value={createListenPort}
                            onChange={(e) => setCreateListenPort(e.target.value)}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                        {createErrors.listen_port && <p className="text-xs text-amber-400 mt-1">{createErrors.listen_port}</p>}
                    </div>
                </div>
            </Modal>

            <Modal
                isOpen={editOpen}
                onClose={closeEditModal}
                title={editTarget ? `Edit ${editTarget}` : 'Edit Interface'}
                footer={
                    <>
                        <Button variant="ghost" onClick={closeEditModal} disabled={busyKey === 'edit'}>Cancel</Button>
                        <Button variant="primary" onClick={handleSaveEdit} isLoading={busyKey === 'edit'} disabled={!canWriteWG || !editTarget}>
                            Save Changes
                        </Button>
                    </>
                }
            >
                <div className="space-y-4 modal-form-uniform">
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Address</label>
                        <input
                            type="text"
                            value={editAddress}
                            onChange={(e) => setEditAddress(e.target.value)}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                        {editErrors.address && <p className="text-xs text-amber-400 mt-1">{editErrors.address}</p>}
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Bind Address</label>
                        <input
                            type="text"
                            value={editBindAddress}
                            onChange={(e) => setEditBindAddress(e.target.value)}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Listen Port</label>
                            <input
                                type="number"
                                min={1}
                                max={65535}
                                value={editListenPort}
                                onChange={(e) => setEditListenPort(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                            />
                            {editErrors.listen_port && <p className="text-xs text-amber-400 mt-1">{editErrors.listen_port}</p>}
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">MTU</label>
                            <input
                                type="number"
                                min={0}
                                value={editMTU}
                                onChange={(e) => setEditMTU(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                            />
                            {editErrors.mtu && <p className="text-xs text-amber-400 mt-1">{editErrors.mtu}</p>}
                        </div>
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">DNS</label>
                        <input
                            type="text"
                            value={editDNS}
                            onChange={(e) => setEditDNS(e.target.value)}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Post Up</label>
                        <input
                            type="text"
                            value={editPostUp}
                            onChange={(e) => setEditPostUp(e.target.value)}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-slate-400 mb-1">Post Down</label>
                        <input
                            type="text"
                            value={editPostDown}
                            onChange={(e) => setEditPostDown(e.target.value)}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-sm"
                        />
                    </div>
                </div>
            </Modal>

            <ConfirmModal
                isOpen={!!deleteTarget}
                onClose={() => setDeleteTarget(null)}
                onConfirm={handleConfirmDelete}
                title="Delete interface?"
                message={deleteTarget ? `This will permanently delete ${deleteTarget.name}.` : 'This action cannot be undone.'}
                confirmLabel="Delete"
                confirmTone="danger"
                isLoading={!!deleteTarget && busyKey === `delete:${deleteTarget.name}`}
            />

            <WireGuardRawConfigModal
                isOpen={rawConfigOpen}
                iface={rawConfigTarget}
                canWriteWG={canWriteWG}
                onClose={() => {
                    setRawConfigOpen(false)
                    setRawConfigTarget(null)
                }}
                onSaved={refreshInterfaces}
            />
        </div>
    )
}

function DatabaseTab({
    features,
    setFeatures,
    dbInfo,
    canWriteSettings,
    handlePruneNow,
    handleRunSampler,
    handleTogglePause,
    samplerRunning,
    handleSaveFeatures,
    loadDatabasePanel,
    historyLimit,
    setHistoryLimit,
    samplerHistory,
    subscriptionRequestHistory,
}: {
    features: FeatureFlags
    setFeatures: Dispatch<SetStateAction<FeatureFlags>>
    dbInfo: DbInfo
    canWriteSettings: boolean
    handlePruneNow: () => void
    handleRunSampler: () => void
    handleTogglePause: () => void
    samplerRunning: boolean
    handleSaveFeatures: () => void
    loadDatabasePanel: () => Promise<void>
    historyLimit: number
    setHistoryLimit: Dispatch<SetStateAction<number>>
    samplerHistory: SamplerHistoryEntry[]
    subscriptionRequestHistory: SubscriptionRequestHistoryEntry[]
}) {
    const [databaseCardHeight, setDatabaseCardHeight] = useState<number | null>(null)
    const databaseCardRef = useRef<HTMLDivElement | null>(null)

    useEffect(() => {
        const element = databaseCardRef.current
        if (!element) return

        const updateHeight = () => {
            if (window.innerWidth < 1024) {
                setDatabaseCardHeight(null)
                return
            }
            setDatabaseCardHeight(element.getBoundingClientRect().height)
        }

        updateHeight()

        if (typeof ResizeObserver === 'undefined') {
            window.addEventListener('resize', updateHeight)
            return () => window.removeEventListener('resize', updateHeight)
        }

        const observer = new ResizeObserver(() => updateHeight())
        observer.observe(element)

        return () => observer.disconnect()
    }, [features, dbInfo.rows, dbInfo.sizeMB, canWriteSettings, samplerRunning])

    const formatHistoryTime = (value?: number | null) => {
        if (!value || Number.isNaN(value)) return 'Unknown'
        return new Date(value * 1000).toLocaleTimeString()
    }

    const formatClientLabel = (run: SubscriptionRequestHistoryEntry) => {
        if (run.device_model && run.user_agent) return `${run.user_agent} on ${run.device_model}`
        if (run.device_model) return run.device_model
        if (run.user_agent) return run.user_agent
        return 'Unknown client'
    }

    const formatDeviceDetails = (run: SubscriptionRequestHistoryEntry) => {
        const details = [run.device_os, run.device_os_version].filter(Boolean).join(' ')
        return details || ''
    }

    return (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 sm:gap-6 items-start pb-4 sm:pb-0">
            <div className="flex flex-col gap-4 sm:gap-6">
            <div ref={databaseCardRef}>
            <Card
                    title="Database & Retention"
                    action={
                        <div className="flex gap-2">
                            <Button onClick={handleSaveFeatures} size="sm" icon={<Save size={16} />} iconNoGap disabled={!canWriteSettings}>
                                <span className="sm:hidden sr-only">Save Changes</span>
                                <span className="hidden sm:inline">Save Changes</span>
                            </Button>
                            <Button onClick={loadDatabasePanel} variant="icon" size="icon" icon={<RefreshCw size={16} />} />
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
                                    disabled={!canWriteSettings}
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
                                    disabled={!canWriteSettings}
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
                                    disabled={!canWriteSettings}
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
                                    disabled={!canWriteSettings}
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
                                    disabled={!canWriteSettings}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Active Threshold (Bytes)</label>
                                <input
                                    type="number"
                                    min={0}
                                    value={features.active_threshold_bytes ?? 1024}
                                    onChange={e => setFeatures(prev => ({ ...prev, active_threshold_bytes: parseInt(e.target.value) }))}
                                    disabled={!canWriteSettings}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500/50 transition-colors"
                                />
                            </div>
                        </div>
                    </div>

                    <div className="space-y-3">
                        <div className="flex gap-2">
                            <Button
                                onClick={handleRunSampler}
                                disabled={!canWriteSettings || samplerRunning}
                                className="flex-[2]"
                                isLoading={samplerRunning}
                                variant="primary"
                            >
                                Run Sampler
                            </Button>
                            <Button
                                onClick={handlePruneNow}
                                disabled={!canWriteSettings || !features.retention_enabled}
                                variant="secondary"
                                className="flex-1"
                            >
                                Prune
                            </Button>
                        </div>
                        <Button
                            onClick={handleTogglePause}
                            disabled={!canWriteSettings}
                            variant="secondary"
                            className={`w-full ${features.sampler_paused ? 'bg-emerald-900/20 text-emerald-400 border-emerald-900/30' : 'bg-amber-900/20 text-amber-400 border-amber-900/30'}`}
                        >
                            {features.sampler_paused ? 'Resume' : 'Pause'}
                        </Button>
                    </div>
                </Card>
            </div>

            <Card
                    title="Sampler History"
                    className="flex flex-col min-h-[208px] lg:h-[416px]"
                    action={
                        <select
                            value={historyLimit}
                            onChange={e => setHistoryLimit(parseInt(e.target.value))}
                            className="select-field bg-slate-950 border border-slate-800 rounded px-2 py-1 text-slate-400 text-xs outline-none focus:border-slate-700"
                        >
                            <option value={10}>Last 10</option>
                            <option value={20}>Last 20</option>
                            <option value={30}>Last 30</option>
                            <option value={40}>Last 40</option>
                            <option value={50}>Last 50</option>
                        </select>
                    }
                >
                    <div className="flex-1 min-h-0">
                        <div className="space-y-0 text-sm h-full overflow-y-auto pr-2">
                            {samplerHistory.length === 0 ? (
                                <p className="text-slate-500 text-xs italic">No history available</p>
                            ) : (
                                samplerHistory.map((run, idx) => (
                                    <div key={idx} className="flex justify-between items-center gap-3 py-2 border-b border-slate-800/50 last:border-0">
                                        <div className="min-w-0">
                                            <div className="flex items-center gap-2">
                                                <div className="text-slate-300 text-xs">{formatHistoryTime(run.timestamp ?? run.ts)}</div>
                                                <span className={`text-[10px] px-1.5 py-0.5 rounded ${run.source === 'wireguard' ? 'bg-orange-900/20 text-orange-400 border border-orange-900/30' : 'bg-blue-900/20 text-blue-400 border border-blue-900/30'}`}>
                                                    {run.source === 'wireguard' ? 'WG' : 'Proxy'}
                                                </span>
                                            </div>
                                            {run.error && <div className="text-red-400 text-[10px] truncate max-w-[150px]">{run.error}</div>}
                                        </div>
                                        <div className="shrink-0 text-right">
                                            <div className="font-mono text-emerald-400 text-xs">+{run.inserted} rows</div>
                                            <div className="text-slate-500 text-[10px]">{run.duration_ms}ms</div>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>
                    </div>
                </Card>
            </div>

            <div
                className="min-h-0"
                style={databaseCardHeight ? { height: `${databaseCardHeight}px` } : undefined}
            >
                <Card
                    title="Subscriptions History"
                    className="flex h-full min-h-[208px] min-h-0 flex-col overflow-hidden"
                    action={
                        <select
                            value={historyLimit}
                            onChange={e => setHistoryLimit(parseInt(e.target.value))}
                            className="select-field bg-slate-950 border border-slate-800 rounded px-2 py-1 text-slate-400 text-xs outline-none focus:border-slate-700"
                        >
                            <option value={10}>Last 10</option>
                            <option value={20}>Last 20</option>
                            <option value={30}>Last 30</option>
                            <option value={40}>Last 40</option>
                            <option value={50}>Last 50</option>
                        </select>
                    }
                >
                    <div className="flex-1 min-h-0 overflow-hidden">
                        <div className="h-full min-h-0 space-y-0 overflow-y-auto pr-2 text-sm">
                            {subscriptionRequestHistory.length === 0 ? (
                                <p className="text-slate-500 text-xs italic">No history available</p>
                            ) : (
                                subscriptionRequestHistory.map((run) => (
                                    <div key={run.id} className="flex justify-between items-center gap-3 py-2 border-b border-slate-800/50 last:border-0">
                                        <div className="min-w-0">
                                            <div className="flex items-center gap-2">
                                                <div className="truncate text-slate-200 text-xs font-medium" title={run.name}>{run.name}</div>
                                                <span className={`shrink-0 text-[10px] px-1.5 py-0.5 rounded ${run.served_from_cache ? 'bg-amber-900/20 text-amber-400 border border-amber-900/30' : 'bg-emerald-900/20 text-emerald-400 border border-emerald-900/30'}`}>
                                                    {run.served_from_cache ? 'Cache' : 'Fresh'}
                                                </span>
                                            </div>
                                            <div className="truncate text-slate-500 text-[10px]" title={run.user_name || 'No users'}>
                                                {run.user_name || 'No users'}
                                            </div>
                                            <div className="truncate text-slate-400 text-[10px]" title={formatClientLabel(run)}>
                                                {formatClientLabel(run)}
                                            </div>
                                            {(formatDeviceDetails(run) || run.app_version || run.country || run.request_host || run.hwid_prefix) && (
                                                <div className="truncate text-slate-500 text-[10px]" title={[formatDeviceDetails(run), run.app_version ? `App ${run.app_version}` : '', run.country, run.request_host, run.hwid_prefix ? `HWID ${run.hwid_prefix}` : ''].filter(Boolean).join(' • ')}>
                                                    {[formatDeviceDetails(run), run.app_version ? `App ${run.app_version}` : '', run.country, run.request_host, run.hwid_prefix ? `HWID ${run.hwid_prefix}` : ''].filter(Boolean).join(' • ')}
                                                </div>
                                            )}
                                        </div>
                                        <div className="shrink-0 text-right">
                                            <div className="font-mono text-blue-400 text-xs">{run.request_ip || '-'}</div>
                                            <div className="text-slate-500 text-[10px]">{formatHistoryTime(run.requested_at)}</div>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>
                    </div>
                </Card>
            </div>
        </div>
    )
}
