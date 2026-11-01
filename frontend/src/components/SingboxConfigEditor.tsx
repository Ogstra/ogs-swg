import { useEffect, useState } from 'react'
import { FileJson, List, GitBranch, Waypoints } from 'lucide-react'
import { api } from '../services/api'
import type { SingboxOutboundDomainStrategyUpdate, SingboxOutboundView } from '../services/api'
import InboundList from './singbox/InboundList'
import { RuleSetsPanel } from './singbox/RuleSetsPanel'
import { RoutingRulesPanel } from './singbox/RoutingRulesPanel'
import { Card } from './ui/Card'
import { RawEditorPanel } from './raw/RawEditorPanel'
import { useAuth } from '../context/AuthContext'
import { useToast } from '../context/ToastContext'
import { ConfirmModal } from './ui/ConfirmModal'

type TabId = 'inbounds' | 'rules' | 'outbounds' | 'raw'
export default function SingboxConfigEditor() {
    const { permissions } = useAuth()
    const { success, error: toastError } = useToast()
    const canWriteConfig = !!permissions?.can_write_config
    const [activeTab, setActiveTab] = useState<TabId>('inbounds')
    const [config, setConfig] = useState('')
    const [originalConfig, setOriginalConfig] = useState('')
    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [lastBackup, setLastBackup] = useState<string>('')
    const [routingOutbounds, setRoutingOutbounds] = useState<string[]>([])
    const [routingRefresh, setRoutingRefresh] = useState(0)
    const [outbounds, setOutbounds] = useState<SingboxOutboundView[]>([])
    const [outboundsLoading, setOutboundsLoading] = useState(false)
    const [outboundsSaving, setOutboundsSaving] = useState(false)
    const [restoreConfirmOpen, setRestoreConfirmOpen] = useState(false)

    // Load Config
    useEffect(() => {
        if (activeTab === 'raw') {
            loadConfig()
            loadBackupMeta()
        }
        if (activeTab === 'outbounds') {
            loadOutbounds()
        }
    }, [activeTab])

    const loadConfig = async () => {
        setLoading(true)
        try {
            const content = await api.getSingboxConfig()
            // Ensure pretty print
            try {
                const json = JSON.parse(content)
                const formatted = JSON.stringify(json, null, 2)
                setConfig(formatted)
                setOriginalConfig(formatted)
            } catch {
                // If not valid JSON, show as is
                setConfig(content)
                setOriginalConfig(content)
            }
        } catch (err: any) {
            console.error('Failed to load config:', err)
        } finally {
            setLoading(false)
        }
    }

    const loadBackupMeta = async () => {
        try {
            const meta = await api.getBackupMeta()
            if (meta.singbox_last_backup) {
                setLastBackup(new Date(meta.singbox_last_backup).toLocaleString())
            } else {
                setLastBackup('')
            }
        } catch (err) {
            console.error('Failed to load backup meta', err)
        }
    }

    const handleBackup = async () => {
        try {
            await api.backupConfig()
            success('Backup created (.bak)')
            loadBackupMeta()
        } catch (err: any) {
            toastError('Backup failed: ' + (err.message || err))
        }
    }

    const handleRestore = () => {
        setRestoreConfirmOpen(true)
    }

    const handleConfirmRestore = async () => {
        try {
            const cfg = await api.restoreConfig()
            const formatted = JSON.stringify(cfg, null, 2)
            setConfig(formatted)
            setOriginalConfig(formatted)
            success('Restored from backup')
            loadBackupMeta()
            setRestoreConfirmOpen(false)
        } catch (err: any) {
            toastError('Restore failed: ' + (err.message || err))
        }
    }

    const handleSave = async () => {
        setSaving(true)
        try {
            await api.updateSingboxConfig(config)
            setOriginalConfig(config)
            success('Configuration saved and service restarted!')
        } catch (err: any) {
            toastError(`Failed to save: ${err.message || err}`)
        } finally {
            setSaving(false)
        }
    }

    const loadOutbounds = async () => {
        setOutboundsLoading(true)
        try {
            const nextOutbounds = await api.getSingboxOutbounds()
            setOutbounds(Array.isArray(nextOutbounds) ? nextOutbounds : [])
        } catch (err: any) {
            console.error('Failed to load outbounds', err)
            toastError('Failed to load outbounds')
        } finally {
            setOutboundsLoading(false)
        }
    }

    const saveOutbounds = async () => {
        setOutboundsSaving(true)
        try {
            const updates: SingboxOutboundDomainStrategyUpdate[] = outbounds.map(outbound => ({
                tag: outbound.tag,
                domain_strategy: (outbound.domain_strategy || '').trim(),
            }))
            await api.updateSingboxOutboundDomainStrategies(updates)
            success('Outbound domain_strategy values saved')
            await loadOutbounds()
        } catch (err: any) {
            console.error('Failed to save outbounds', err)
            toastError('Failed to save outbounds')
        } finally {
            setOutboundsSaving(false)
        }
    }

    return (
        <div className="h-full min-h-0 flex flex-col">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-end gap-0 sm:gap-4">
                <div />
            </div>

            {/* Editor Container */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex-1 min-h-0 flex flex-col">
                {/* Tabs */}
                <div className="flex border-b border-slate-800 bg-slate-950/50">
                    <button
                        onClick={() => setActiveTab('inbounds')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 flex items-center gap-2 ${activeTab === 'inbounds' ? 'border-blue-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        <List size={16} />
                        Inbounds
                    </button>
                    <button
                        onClick={() => setActiveTab('outbounds')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 flex items-center gap-2 ${activeTab === 'outbounds' ? 'border-fuchsia-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        <Waypoints size={16} />
                        Outbounds
                    </button>
                    <button
                        onClick={() => setActiveTab('rules')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 flex items-center gap-2 ${activeTab === 'rules' ? 'border-emerald-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        <GitBranch size={16} />
                        Rules
                    </button>
                    <button
                        onClick={() => setActiveTab('raw')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 flex items-center gap-2 ${activeTab === 'raw' ? 'border-amber-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        <FileJson size={16} />
                        Raw Config (JSON)
                    </button>
                </div>

                {/* Content */}
                <div className="flex-1 bg-slate-950 overflow-hidden flex flex-col">
                    {activeTab === 'raw' ? (
                        <RawEditorPanel
                            value={config}
                            originalValue={originalConfig}
                            onChange={setConfig}
                            onRefresh={loadConfig}
                            onSave={handleSave}
                            onBackup={handleBackup}
                            onRestore={handleRestore}
                            loading={loading}
                            saving={saving}
                            canWrite={canWriteConfig}
                            lastBackupText={lastBackup}
                            language="json"
                            textareaId="singbox-raw-editor"
                        />
                    ) : (
                        <div className="flex-1 overflow-auto custom-scrollbar p-6">
                            {activeTab === 'inbounds' && <InboundList />}
                            {activeTab === 'rules' && (
                                <div className="space-y-6">
                                    <RuleSetsPanel
                                        canWrite={canWriteConfig}
                                        outbounds={routingOutbounds}
                                        onChanged={() => setRoutingRefresh(value => value + 1)}
                                    />
                                    <RoutingRulesPanel
                                        canWrite={canWriteConfig}
                                        refreshKey={routingRefresh}
                                        onOutbounds={setRoutingOutbounds}
                                    />
                                </div>
                            )}
                            {activeTab === 'outbounds' && (
                                <OutboundsTab
                                    outbounds={outbounds}
                                    setOutbounds={setOutbounds}
                                    loading={outboundsLoading}
                                    saving={outboundsSaving}
                                    canWrite={canWriteConfig}
                                    reload={loadOutbounds}
                                    save={saveOutbounds}
                                />
                            )}
                        </div>
                    )}
                </div>
            </div>
            <ConfirmModal
                isOpen={restoreConfirmOpen}
                onClose={() => setRestoreConfirmOpen(false)}
                onConfirm={handleConfirmRestore}
                title="Restore from backup?"
                message="This will overwrite the current config in the editor with the last backup."
                confirmLabel="Restore"
                confirmTone="danger"
            />
        </div>
    )
}

function OutboundsTab({
    outbounds,
    setOutbounds,
    loading,
    saving,
    canWrite,
    reload,
    save,
}: {
    outbounds: SingboxOutboundView[]
    setOutbounds: React.Dispatch<React.SetStateAction<SingboxOutboundView[]>>
    loading: boolean
    saving: boolean
    canWrite: boolean
    reload: () => void
    save: () => void
}) {
    const outboundTypeOptions = [
        'direct',
        'block',
        'dns',
        'http',
        'socks',
        'shadowsocks',
        'vmess',
        'vless',
        'trojan',
        'wireguard',
        'hysteria',
        'hysteria2',
        'tuic',
        'tor',
        'ssh',
        'shadowtls',
        'anytls',
        'selector',
        'urltest',
    ]
    const domainStrategyOptions = ['prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only']
    const outboundTagOptions = Array.from(new Set(outbounds.map(o => (o.tag || '').trim()).filter(Boolean))).sort()

    const updateOutbound = (idx: number, domainStrategy: string) => {
        if (!canWrite) return
        setOutbounds(prev => prev.map((outbound, i) => (i === idx ? { ...outbound, domain_strategy: domainStrategy } : outbound)))
    }

    return (
        <div className="space-y-4">
            <div className="flex justify-end mb-4 gap-2">
                <button
                    onClick={reload}
                    className="px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium disabled:opacity-60"
                    disabled={loading}
                >
                    {loading ? 'Refreshing...' : 'Refresh'}
                </button>
                <button
                    onClick={save}
                    disabled={!canWrite || saving}
                    className={`px-3 py-2 rounded-lg text-sm font-medium transition-colors ${!canWrite || saving
                        ? 'bg-slate-800 text-slate-500 cursor-not-allowed'
                        : 'bg-blue-600 hover:bg-blue-500 text-white'
                        }`}
                >
                    {saving ? 'Saving...' : 'Save Outbounds'}
                </button>
            </div>

            <div className="space-y-3">
                {outbounds.length === 0 && (
                    <div className="text-sm text-slate-400">No outbounds found.</div>
                )}
                {outbounds.map((outbound, idx) => (
                    <Card key={outbound.tag || `${outbound.type}-${idx}`} title={outbound.tag || `Outbound ${idx + 1}`} className="space-y-3">
                        <div className="grid grid-cols-1 md:grid-cols-[2fr_2fr_3fr] gap-3">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Type</label>
                                <select
                                    value={outbound.type || ''}
                                    disabled
                                    className="select-field w-full h-[38px] rounded-lg border border-slate-800 bg-slate-950 px-3 text-slate-300 outline-none disabled:opacity-80"
                                >
                                    <option value="">unknown</option>
                                    {outboundTypeOptions.map(option => (
                                        <option key={option} value={option}>
                                            {option}
                                        </option>
                                    ))}
                                    {outbound.type && !outboundTypeOptions.includes(outbound.type) && (
                                        <option value={outbound.type}>
                                            {outbound.type}
                                        </option>
                                    )}
                                </select>
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Tag</label>
                                <select
                                    value={outbound.tag || ''}
                                    disabled
                                    className="select-field w-full h-[38px] rounded-lg border border-slate-800 bg-slate-950 px-3 text-slate-300 outline-none disabled:opacity-80"
                                >
                                    <option value="">{outbound.tag ? outbound.tag : 'no-tag'}</option>
                                    {outboundTagOptions.map(tag => (
                                        <option key={tag} value={tag}>
                                            {tag}
                                        </option>
                                    ))}
                                </select>
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">domain_strategy</label>
                                <select
                                    value={outbound.domain_strategy || ''}
                                    disabled={!canWrite}
                                    onChange={(e) => updateOutbound(idx, e.target.value)}
                                    className="select-field w-full h-[38px] rounded-lg border border-slate-800 bg-slate-950 px-3 text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                                >
                                    <option value="">empty (remove field)</option>
                                    {domainStrategyOptions.map(option => (
                                        <option key={option} value={option}>
                                            {option}
                                        </option>
                                    ))}
                                    {outbound.domain_strategy && !domainStrategyOptions.includes(outbound.domain_strategy) && (
                                        <option value={outbound.domain_strategy}>
                                            {outbound.domain_strategy}
                                        </option>
                                    )}
                                </select>
                            </div>
                        </div>
                        {outbound.domain_resolver && (
                            <p className="text-xs text-amber-400">
                                `domain_resolver` present: {outbound.domain_resolver}. Prefer this over `domain_strategy` in newer Sing-box configs.
                            </p>
                        )}
                    </Card>
                ))}
            </div>
        </div>
    )
}
