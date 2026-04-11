import { useState, useEffect, useRef } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, FeatureFlags, SamplerHistoryEntry, SubscriptionRequestHistoryEntry, DashboardPreferences as StoredDashboardPreferences } from '../../services/api'
import type { WireGuardInterfaceSummary } from '../../services/api'
import { Save, RefreshCw, UserCog, Shield, ShieldAlert, Plus, Trash2, Power, FileJson, Edit } from 'lucide-react'
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
import SecurityTab from './components/SecurityTab'
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
type DashboardPrefs = { defaultService: 'singbox' | 'wireguard'; refreshMs: number; defaultRange: string; detailChartTargetPoints: number }

const normalizeDashboardPrefs = (prefs?: Partial<StoredDashboardPreferences> | null): DashboardPrefs => ({
    defaultService: prefs?.default_service === 'wireguard' ? 'wireguard' : 'singbox',
    refreshMs: prefs?.refresh_ms && prefs.refresh_ms >= 1000 ? prefs.refresh_ms : 10000,
    defaultRange: prefs?.default_range || '24h',
    detailChartTargetPoints: [50, 100, 150, 200].includes(Number(prefs?.detail_chart_target_points))
        ? Number(prefs?.detail_chart_target_points)
        : 200,
})

export default function Settings() {
    const queryClient = useQueryClient()
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
        defaultRange: '24h',
        detailChartTargetPoints: 200,
    })
    const dashboardPrefsQuery = useQuery({
        queryKey: ['dashboard-preferences'],
        queryFn: () => api.getDashboardPreferences(),
        placeholderData: previousData => previousData,
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
        if (!dashboardPrefsQuery.data) return
        setDashboardPrefs(normalizeDashboardPrefs(dashboardPrefsQuery.data))
    }, [dashboardPrefsQuery.data])

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
                    toastError={toastError}
                    queryClient={queryClient}
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
        {
            id: 'security',
            label: <span className="flex items-center gap-2"><ShieldAlert size={16} /> Sub Security</span>,
            content: (
                <SecurityTab
                    canWriteSettings={canWriteSettings}
                    success={success}
                    toastError={toastError}
                />
            ),
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
    toastError,
    queryClient,
}: {
    dashboardPrefs: DashboardPrefs
    setDashboardPrefs: Dispatch<SetStateAction<DashboardPrefs>>
    success: (msg: string) => void
    toastError: (msg: string) => void
    queryClient: ReturnType<typeof useQueryClient>
}) {
    const handleSave = async () => {
        const normalized = {
            defaultService: dashboardPrefs.defaultService || 'singbox',
            refreshMs: Math.max(1000, Number(dashboardPrefs.refreshMs) || 10000),
            defaultRange: dashboardPrefs.defaultRange || '24h',
            detailChartTargetPoints: [50, 100, 150, 200].includes(Number(dashboardPrefs.detailChartTargetPoints))
                ? Number(dashboardPrefs.detailChartTargetPoints)
                : 200,
        }
        try {
            await api.updateDashboardPreferences({
                default_service: normalized.defaultService,
                refresh_ms: normalized.refreshMs,
                default_range: normalized.defaultRange as StoredDashboardPreferences['default_range'],
                detail_chart_target_points: normalized.detailChartTargetPoints,
            })
            setDashboardPrefs(normalized)
            await queryClient.invalidateQueries({ queryKey: ['dashboard-preferences'] })
            success('Dashboard preferences saved')
        } catch (err) {
            toastError('Failed to save dashboard preferences: ' + err)
        }
    }

    return (
        <div className="space-y-4 sm:space-y-6">
            <Card title="Dashboard Preferences">
                <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
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
                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Selected User Chart Samples</label>
                        <select
                            value={dashboardPrefs.detailChartTargetPoints}
                            onChange={e => setDashboardPrefs(prev => ({ ...prev, detailChartTargetPoints: Number(e.target.value) || 200 }))}
                            className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                        >
                            <option value={50}>50 samples</option>
                            <option value={100}>100 samples</option>
                            <option value={150}>150 samples</option>
                            <option value={200}>200 samples</option>
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

    const formatHistoryDateTime = (value?: number | null) => {
        if (!value || Number.isNaN(value)) return 'Unknown'
        return new Date(value * 1000).toLocaleString()
    }

    const parseClientIdentity = (userAgent?: string) => {
        const ua = (userAgent || '').trim()
        if (!ua) return { clientName: '', clientVersion: '', deviceModel: '', deviceOS: '', deviceOSVersion: '', darwinVersion: '', architecture: '' }

        const desktopClientMatch = ua.match(/^\s*([A-Za-z][A-Za-z0-9 _.-]{0,63})\/([A-Za-z][A-Za-z0-9 _.-]{0,31})\/([A-Za-z0-9._-]{1,64})/)
        const productMatch = ua.match(/^\s*([A-Za-z][A-Za-z0-9 _.-]{0,63})\/([A-Za-z0-9._-]{1,64})/)
        const desktopClientName = desktopClientMatch && !['mozilla', 'dalvik'].includes(desktopClientMatch[1].toLowerCase()) ? desktopClientMatch[1].trim() : ''
        const browserMatch = (() => {
            if (ua.includes('Edg/')) return { name: 'Edge', version: (ua.match(/Edg\/([0-9A-Za-z._-]+)/)?.[1] || '').trim() }
            if (ua.includes('OPR/')) return { name: 'Opera', version: (ua.match(/OPR\/([0-9A-Za-z._-]+)/)?.[1] || '').trim() }
            if (ua.includes('Chrome/')) return { name: 'Chrome', version: (ua.match(/Chrome\/([0-9A-Za-z._-]+)/)?.[1] || '').trim() }
            if (ua.includes('Firefox/')) return { name: 'Firefox', version: (ua.match(/Firefox\/([0-9A-Za-z._-]+)/)?.[1] || '').trim() }
            if (ua.includes('Safari/') && ua.includes('Version/')) return { name: 'Safari', version: (ua.match(/Version\/([0-9A-Za-z._-]+)/)?.[1] || '').trim() }
            return null
        })()
        const clientName = desktopClientName || (productMatch && !['mozilla', 'dalvik'].includes(productMatch[1].toLowerCase()) ? productMatch[1].trim() : '') || browserMatch?.name || ''
        const clientVersion = desktopClientName && desktopClientMatch ? desktopClientMatch[3].trim() : (clientName && productMatch ? productMatch[2].trim() : '')
        const resolvedClientVersion = browserMatch?.name ? browserMatch.version : clientVersion
        const clientPlatform = desktopClientName && desktopClientMatch ? desktopClientMatch[2].trim() : ''

        const appleModelMatch = ua.match(/\b(iPhone\d{1,2},\d+|iPad\d{1,2},\d+|iPod\d{1,2},\d+|MacBook(?:Air|Pro)?\d{1,2},\d+|Mac\d{1,2},\d+)\b/)
        const samsungModelMatch = ua.match(/\b(SM-[A-Z0-9]+)\b/)
        const androidModelMatch = ua.match(/Android\s+[0-9][0-9A-Za-z._-]*\s*;\s*([A-Za-z0-9 _.-]{2,64}?)(?:\s+Build\/|[;)])/)
        const androidVersionMatch = ua.match(/Android\s+([0-9][0-9A-Za-z._-]*)/)
        const macOSVersionMatch = ua.match(/Mac OS X\s+([0-9_]+)/)
        const darwinVersionMatch = ua.match(/Darwin\/([0-9.]+)/)
        const windowsVersionMatch = ua.match(/Windows NT\s+([0-9.]+)/)
        const architectureMatch = ua.match(/\b(arm64|aarch64|x86_64|amd64)\b/i)

        let deviceModel = appleModelMatch?.[1] || samsungModelMatch?.[1] || ''
        if (!deviceModel && androidModelMatch?.[1]) {
            const model = androidModelMatch[1].trim()
            if (!['wv', 'mobile'].includes(model.toLowerCase())) {
                deviceModel = model
            }
        }
        if (!deviceModel && clientPlatform) {
            if (/^(pc|desktop)$/i.test(clientPlatform)) deviceModel = 'PC'
            else if (/^(mac|macos)$/i.test(clientPlatform)) deviceModel = 'Mac'
            else if (/^windows$/i.test(clientPlatform)) deviceModel = 'PC'
            else if (/^linux$/i.test(clientPlatform)) deviceModel = 'PC'
            else deviceModel = clientPlatform
        }
        if (!deviceModel && ua.includes('Macintosh')) {
            deviceModel = 'Mac'
        }

        let deviceOS = ''
        if (deviceModel.startsWith('iPhone') || deviceModel.startsWith('iPod')) deviceOS = 'iOS'
        else if (deviceModel.startsWith('iPad')) deviceOS = 'iPadOS'
        else if (deviceModel.startsWith('Mac') || ua.includes('Macintosh')) deviceOS = 'macOS'
        else if (ua.includes('Android')) deviceOS = 'Android'
        else if (ua.includes('Windows NT')) deviceOS = 'Windows'
        else if (/^mac(os)?$/i.test(clientPlatform)) deviceOS = 'macOS'
        else if (/^windows$/i.test(clientPlatform)) deviceOS = 'Windows'
        else if (/^linux$/i.test(clientPlatform)) deviceOS = 'Linux'
        else if (/^android$/i.test(clientPlatform)) deviceOS = 'Android'
        else if (/^ios$/i.test(clientPlatform)) deviceOS = 'iOS'
        else if (/^ipados$/i.test(clientPlatform)) deviceOS = 'iPadOS'

        return {
            clientName,
            clientVersion: resolvedClientVersion,
            deviceModel,
            deviceOS,
            deviceOSVersion: androidVersionMatch?.[1]?.trim() || (macOSVersionMatch?.[1] || '').replace(/_/g, '.') || windowsVersionMatch?.[1]?.trim() || '',
            darwinVersion: darwinVersionMatch?.[1]?.trim() || '',
            architecture: architectureMatch?.[1]?.toLowerCase() || '',
        }
    }

    const normalizeVerboseAppleOS = (primary?: string, secondary?: string) => {
        const normalizeHistoryValue = (value?: string) => {
            const normalized = (value || '').trim()
            if (!normalized) return ''
            if (['0', 'unknown', 'n/a', 'null', 'nil', 'none', '-'].includes(normalized.toLowerCase())) return ''
            return normalized
        }
        const first = normalizeHistoryValue(primary)
        const second = normalizeHistoryValue(secondary)
        const parseVerbose = (value: string) => value.match(/^(macOS|iOS|iPadOS)\s+Version\s+([0-9.]+)(?:\s+\(Build\s+([^)]+)\))?$/i)
        const firstMatch = parseVerbose(first)
        const secondMatch = parseVerbose(second)
        const match = firstMatch || secondMatch
        return {
            osName: match?.[1] || first || '',
            osVersion: match?.[2] || second || '',
            osBuild: match?.[3] || '',
        }
    }

    const resolveDisplayedDevice = (run: SubscriptionRequestHistoryEntry) => {
        const normalizeHistoryValue = (value?: string) => {
            const normalized = (value || '').trim()
            if (!normalized) return ''
            if (['0', 'unknown', 'n/a', 'null', 'nil', 'none', '-'].includes(normalized.toLowerCase())) return ''
            return normalized
        }
        const parsed = parseClientIdentity(run.user_agent)
        const verboseOS = normalizeVerboseAppleOS(run.device_os, run.device_os_version)
        const osName = verboseOS.osName || parsed.deviceOS
        let deviceModel = normalizeHistoryValue(run.device_model) || parsed.deviceModel

        if (osName === 'macOS') {
            if (!deviceModel || /^(iphone|ipad|ipod)$/i.test(deviceModel)) {
                deviceModel = parsed.deviceModel || 'Mac'
            }
        }

        return {
            parsed,
            deviceModel,
            osName,
            osVersion: verboseOS.osVersion || parsed.deviceOSVersion,
            osBuild: verboseOS.osBuild,
        }
    }

    const formatClientLabel = (run: SubscriptionRequestHistoryEntry) => {
        const { parsed, deviceModel } = resolveDisplayedDevice(run)
        const clientName = parsed.clientName || run.user_agent
        if (deviceModel && clientName) return `${clientName} on ${deviceModel}`
        if (deviceModel) return deviceModel
        if (clientName) return clientName
        return 'Unknown client'
    }

    const formatDeviceDetails = (run: SubscriptionRequestHistoryEntry) => {
        const { parsed, osName, osVersion } = resolveDisplayedDevice(run)
        const details: string[] = []
        if (osName && osVersion) details.push(`${osName} ${osVersion}`)
        else if (osName) details.push(osName)
        else if (osVersion) details.push(osVersion)
        if (parsed.darwinVersion && parsed.darwinVersion !== osVersion) {
            details.push(`Darwin ${parsed.darwinVersion}`)
        }
        if (osName === 'macOS' && parsed.architecture) {
            details.push(parsed.architecture)
        }
        return details.join(' • ')
    }

    const formatAppVersion = (run: SubscriptionRequestHistoryEntry) => {
        const parsed = parseClientIdentity(run.user_agent)
        const appVersion = (run.app_version || '').trim()
        if (appVersion && appVersion !== '0') return `App ${appVersion}`
        if (parsed.clientVersion) return `Build ${parsed.clientVersion}`
        return ''
    }

    const getSubscriptionHistoryBadge = (run: SubscriptionRequestHistoryEntry) => {
        if (Boolean(run.blocked)) {
            return {
                label: 'Blocked',
                className: 'bg-red-900/20 text-red-400 border border-red-900/30',
            }
        }
        if (Boolean(run.served_from_cache)) {
            return {
                label: 'Delivered (Cached)',
                className: 'bg-amber-900/20 text-amber-400 border border-amber-900/30',
            }
        }
        return {
            label: 'Delivered (Live)',
            className: 'bg-emerald-900/20 text-emerald-400 border border-emerald-900/30',
        }
    }

    const formatBlockedReasonLabel = (reason?: string) => {
        switch ((reason || '').trim()) {
            case 'ip_block':
                return 'IP Block'
            case 'token_block':
                return 'Token Block'
            case 'rate_limit':
                return 'Rate Limit'
            case 'ua_browser':
            case 'ua_filter':
                return 'Browser UA'
            case 'ua_social_fetcher':
                return 'Social/Chat Fetcher'
            default:
                return (reason || '').trim()
        }
    }

    return (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 sm:gap-6 items-start pb-4 sm:pb-0">
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

            <div
                className="min-h-0 h-[480px] lg:h-auto"
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
                                subscriptionRequestHistory.map((run) => {
                                    const badge = getSubscriptionHistoryBadge(run)
                                    const isBlocked = Boolean(run.blocked)
                                    const blockedReason = formatBlockedReasonLabel(run.block_reason)
                                    const country = (() => {
                                        const value = (run.country || '').trim()
                                        return value && value !== '0' ? value : ''
                                    })()
                                    const hwidPrefix = (() => {
                                        const value = (run.hwid_prefix || '').trim()
                                        return value && value !== '0' ? value : ''
                                    })()
                                    const deviceDetails = formatDeviceDetails(run)
                                    const appVersion = formatAppVersion(run)
                                    const extraDetails = [deviceDetails, appVersion, country, hwidPrefix ? `HWID ${hwidPrefix}` : ''].filter(Boolean).join(' • ')
                                    return (
                                    <div key={run.id} className="py-2 border-b border-slate-800/50 last:border-0">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex items-start justify-between gap-3">
                                                <div className="flex min-w-0 flex-1 items-center gap-2">
                                                    <div className="truncate text-slate-200 text-xs font-medium" title={run.name}>{run.name}</div>
                                                    <span className={`shrink-0 text-[10px] px-1.5 py-0.5 rounded ${badge.className}`}>
                                                        {badge.label}
                                                    </span>
                                                    {isBlocked && blockedReason && (
                                                        <span className="min-w-0 truncate text-[10px] text-red-400" title={blockedReason}>
                                                            {blockedReason}
                                                        </span>
                                                    )}
                                                </div>
                                                <div className="shrink-0 text-right font-mono text-blue-400 text-xs">{run.request_ip || '-'}</div>
                                            </div>
                                            <div className="flex items-start justify-between gap-3">
                                                <div className="min-w-0 flex-1 truncate text-slate-500 text-[10px]" title={run.user_name || 'No users'}>
                                                    {run.user_name || 'No users'}
                                                </div>
                                                <div className="shrink-0 text-right text-slate-500 text-[10px]">{formatHistoryDateTime(run.requested_at)}</div>
                                            </div>
                                            <div className="truncate text-slate-400 text-[10px]" title={formatClientLabel(run)}>
                                                {formatClientLabel(run)}
                                            </div>
                                            {extraDetails && (
                                                <div className="truncate text-slate-500 text-[10px]" title={extraDetails}>
                                                    {extraDetails}
                                                </div>
                                            )}
                                        </div>
                                    </div>
                                )})
                            )}
                        </div>
                    </div>
                </Card>
            </div>

            <Card
                    title="Sampler History"
                    className="flex h-[480px] flex-col min-h-[312px] lg:col-start-1 lg:h-[416px]"
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
    )
}
