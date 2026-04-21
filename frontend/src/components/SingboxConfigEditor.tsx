import { useEffect, useState } from 'react'
import { FileJson, List, GitBranch, Waypoints } from 'lucide-react'
import { api } from '../services/api'
import type { SingboxOutboundDomainStrategyUpdate, SingboxOutboundView } from '../services/api'
import InboundList from './singbox/InboundList'
import { Card } from './ui/Card'
import { RawEditorPanel } from './raw/RawEditorPanel'
import { useAuth } from '../context/AuthContext'
import { useToast } from '../context/ToastContext'
import { ConfirmModal } from './ui/ConfirmModal'
import { Modal } from './ui/Modal'
import { Button } from './ui/Button'

type TabId = 'inbounds' | 'rules' | 'outbounds' | 'raw'
type RouteRuleKind = 'inbound' | 'auth_user' | 'rule_set'
type EditableRouteRule = {
    kind: RouteRuleKind
    inbound: string
    authUsers: string[]
    ruleSets: string[]
    outbound: string
    ipVersion?: string
    actionRoute?: boolean
}

const emptyEditableRule = (): EditableRouteRule => ({
    kind: 'inbound',
    inbound: '',
    authUsers: [],
    ruleSets: [],
    outbound: '',
    ipVersion: '',
    actionRoute: false,
})

const normalizeStringArray = (value: unknown): string[] => {
    const rawValues = Array.isArray(value) ? value : value === undefined || value === null ? [] : [value]
    return rawValues.map(item => (typeof item === 'string' ? item.trim() : '')).filter(Boolean)
}

const uniqueSorted = (values: string[]) => Array.from(new Set(values.map(value => value.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b))

const getRuleSetTag = (ruleSet: any): string => {
    if (typeof ruleSet === 'string') return ruleSet.trim()
    if (ruleSet && typeof ruleSet === 'object') {
        if (typeof ruleSet.tag === 'string') return ruleSet.tag.trim()
        if (typeof ruleSet.name === 'string') return ruleSet.name.trim()
    }
    return ''
}

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
    const [rules, setRules] = useState<EditableRouteRule[]>([])
    const [rulesLoading, setRulesLoading] = useState(false)
    const [rulesSaving, setRulesSaving] = useState(false)
    const [availableInbounds, setAvailableInbounds] = useState<string[]>([])
    const [availableOutbounds, setAvailableOutbounds] = useState<string[]>([])
    const [availableAuthUsers, setAvailableAuthUsers] = useState<string[]>([])
    const [availableRuleSets, setAvailableRuleSets] = useState<string[]>([])
    const [preservedRulesCount, setPreservedRulesCount] = useState(0)
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
        if (activeTab === 'rules') {
            loadRules()
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
            // Validate JSON before sending
            try {
                JSON.parse(config)
            } catch (e: any) {
                toastError(`Invalid JSON: ${e.message}`)
                setSaving(false)
                return
            }

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

    const isEditableRule = (rule: any) => {
        if (!rule || typeof rule !== 'object') return false
        const keys = Object.keys(rule)
        const outboundOk = typeof rule.outbound === 'string' && rule.outbound.trim() !== ''
        const ipOk = rule.ip_version === undefined || rule.ip_version === 4 || rule.ip_version === 6
        if (!outboundOk || !ipOk) return false

        if ('inbound' in rule) {
            const allowed = new Set(['inbound', 'outbound', 'ip_version'])
            const inboundOk = Array.isArray(rule.inbound)
                ? rule.inbound.every((x: any) => typeof x === 'string')
                : typeof rule.inbound === 'string'
            return keys.every(k => allowed.has(k)) && inboundOk
        }

        if ('auth_user' in rule) {
            const allowed = new Set(['auth_user', 'rule_set', 'action', 'outbound', 'ip_version'])
            const actionOk = rule.action === undefined || rule.action === 'route'
            const authUsersOk = normalizeStringArray(rule.auth_user).length > 0
            const ruleSetsOk = !('rule_set' in rule) || normalizeStringArray(rule.rule_set).length > 0
            return keys.every(k => allowed.has(k)) && actionOk && authUsersOk && ruleSetsOk
        }

        if ('rule_set' in rule) {
            const allowed = new Set(['rule_set', 'outbound', 'ip_version'])
            return keys.every(k => allowed.has(k)) && normalizeStringArray(rule.rule_set).length > 0
        }

        return false
    }

    const toEditableRule = (rule: any): EditableRouteRule => {
        if ('auth_user' in rule) {
            const ruleSets = normalizeStringArray(rule.rule_set)
            return {
                kind: 'auth_user',
                inbound: '',
                authUsers: normalizeStringArray(rule.auth_user),
                ruleSets,
                outbound: String(rule.outbound || '').trim(),
                ipVersion: rule.ip_version === 4 || rule.ip_version === 6 ? String(rule.ip_version) : '',
                actionRoute: rule.action === 'route',
            }
        }
        if ('rule_set' in rule) {
            return {
                kind: 'rule_set',
                inbound: '',
                authUsers: [],
                ruleSets: normalizeStringArray(rule.rule_set),
                outbound: String(rule.outbound || '').trim(),
                ipVersion: rule.ip_version === 4 || rule.ip_version === 6 ? String(rule.ip_version) : '',
                actionRoute: false,
            }
        }
        return {
            kind: 'inbound',
            inbound: Array.isArray(rule.inbound) ? (rule.inbound[0] ?? '') : String(rule.inbound || ''),
            authUsers: [],
            ruleSets: [],
            outbound: String(rule.outbound || '').trim(),
            ipVersion: rule.ip_version === 4 || rule.ip_version === 6 ? String(rule.ip_version) : '',
            actionRoute: false,
        }
    }

    const fromEditableRule = (rule: EditableRouteRule) => {
        const obj: any = { outbound: rule.outbound.trim() }
        if (rule.kind === 'auth_user') {
            obj.auth_user = uniqueSorted(rule.authUsers)
            if (rule.actionRoute) {
                obj.action = 'route'
            }
        }
        if ((rule.kind === 'rule_set' || rule.kind === 'auth_user') && rule.ruleSets.length > 0) {
            obj.rule_set = uniqueSorted(rule.ruleSets)
        }
        if (rule.kind === 'inbound') {
            obj.inbound = [rule.inbound]
        }
        if (rule.ipVersion === '4' || rule.ipVersion === '6') {
            obj.ip_version = Number(rule.ipVersion)
        }
        return obj
    }

    const loadRules = async () => {
        setRulesLoading(true)
        try {
            const raw = await api.getSingboxConfig()
            const parsed = JSON.parse(raw)
            const inboundTags = Array.isArray(parsed?.inbounds)
                ? parsed.inbounds.map((i: any) => (i && typeof i.tag === 'string' ? i.tag.trim() : '')).filter(Boolean)
                : []
            const authUsers = Array.isArray(parsed?.inbounds)
                ? parsed.inbounds.flatMap((inbound: any) => {
                    if (!Array.isArray(inbound?.users)) return []
                    return inbound.users.map((user: any) => {
                        if (typeof user === 'string') return user.trim()
                        if (user && typeof user === 'object' && typeof user.name === 'string') return user.name.trim()
                        return ''
                    })
                }).filter(Boolean)
                : []
            const outboundTags = Array.isArray(parsed?.outbounds)
                ? parsed.outbounds.map((o: any) => (o && typeof o.tag === 'string' ? o.tag.trim() : '')).filter(Boolean)
                : []
            const configuredRuleSets = [
                ...(Array.isArray(parsed?.route?.rule_set) ? parsed.route.rule_set.map(getRuleSetTag) : []),
                ...(Array.isArray(parsed?.route?.ruleset) ? parsed.route.ruleset.map(getRuleSetTag) : []),
            ].filter(Boolean)
            setAvailableInbounds(inboundTags)
            setAvailableOutbounds(outboundTags)

            const rulesArr = parsed?.route?.rules
            if (Array.isArray(rulesArr)) {
                const simple = rulesArr
                    .filter(isEditableRule)
                    .map(toEditableRule)
                const preserved = rulesArr.filter((r: any) => !isEditableRule(r))
                setPreservedRulesCount(preserved.length)
                setAvailableAuthUsers(uniqueSorted([...authUsers, ...simple.flatMap(r => r.authUsers)]))
                setAvailableRuleSets(uniqueSorted([...configuredRuleSets, ...simple.flatMap(r => r.ruleSets)]))
                setRules(simple)
            } else {
                setRules([])
                setAvailableAuthUsers(uniqueSorted(authUsers))
                setAvailableRuleSets(uniqueSorted(configuredRuleSets))
                setPreservedRulesCount(0)
            }
        } catch (err: any) {
            console.error('Failed to load rules', err)
            toastError('Failed to load rules')
        } finally {
            setRulesLoading(false)
        }
    }

    const saveRules = async () => {
        const hasInvalid = rules.some(r => !r.outbound
            || (r.kind === 'inbound' && !r.inbound)
            || (r.kind === 'auth_user' && r.authUsers.length === 0)
            || (r.kind === 'rule_set' && r.ruleSets.length === 0))
        if (hasInvalid) {
            toastError('Please fix rules before saving')
            return
        }
        setRulesSaving(true)
        try {
            const raw = await api.getSingboxConfig()
            const parsed = JSON.parse(raw)
            const route = parsed.route || {}
            const existing = Array.isArray(route.rules) ? route.rules : []
            const preserved = existing.filter((r: any) => !isEditableRule(r))
            const nextRules = [
                ...preserved,
                ...rules.map(fromEditableRule)
            ]
            parsed.route = route
            parsed.route.rules = nextRules
            await api.updateSingboxConfig(JSON.stringify(parsed, null, 2))
            success('Rules saved')
            await loadRules()
        } catch (err: any) {
            console.error('Failed to save rules', err)
            toastError('Failed to save rules')
        } finally {
            setRulesSaving(false)
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
                                <RulesTab
                                    rules={rules}
                                    setRules={setRules}
                                    availableInbounds={availableInbounds}
                                    availableOutbounds={availableOutbounds}
                                    availableAuthUsers={availableAuthUsers}
                                    availableRuleSets={availableRuleSets}
                                    preservedCount={preservedRulesCount}
                                    loading={rulesLoading}
                                    saving={rulesSaving}
                                    canWrite={canWriteConfig}
                                    reload={loadRules}
                                    save={saveRules}
                                />
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

function RulesTab({
    rules,
    setRules,
    availableInbounds,
    availableOutbounds,
    availableAuthUsers,
    availableRuleSets,
    preservedCount: _preservedCount,
    loading,
    saving,
    canWrite,
    reload,
    save,
}: {
    rules: EditableRouteRule[]
    setRules: React.Dispatch<React.SetStateAction<EditableRouteRule[]>>
    availableInbounds: string[]
    availableOutbounds: string[]
    availableAuthUsers: string[]
    availableRuleSets: string[]
    preservedCount: number
    loading: boolean
    saving: boolean
    canWrite: boolean
    reload: () => void
    save: () => void
}) {
    const [selectionModal, setSelectionModal] = useState<{ idx: number; type: 'auth_user' | 'rule_set' } | null>(null)

    const addRule = () => {
        if (!canWrite) return
        setRules(prev => [...prev, emptyEditableRule()])
    }
    const updateRule = (idx: number, patch: Partial<EditableRouteRule>) => {
        if (!canWrite) return
        setRules(prev => prev.map((r, i) => (i === idx ? { ...r, ...patch } : r)))
    }
    const updateRuleKind = (idx: number, kind: RouteRuleKind) => {
        if (!canWrite) return
        setRules(prev => prev.map((r, i) => {
            if (i !== idx) return r
            return {
                ...r,
                kind,
                inbound: kind === 'inbound' ? r.inbound : '',
                authUsers: kind === 'auth_user' ? r.authUsers : [],
                ruleSets: kind === 'rule_set' || kind === 'auth_user' ? r.ruleSets : [],
                ipVersion: r.ipVersion || '',
            }
        }))
    }
    const removeRule = (idx: number) => {
        if (!canWrite) return
        setRules(prev => prev.filter((_, i) => i !== idx))
    }
    const applySelection = (idx: number, type: 'auth_user' | 'rule_set', selected: string[]) => {
        const nextValues = uniqueSorted(selected)
        if (type === 'auth_user') {
            updateRule(idx, { authUsers: nextValues })
        } else {
            updateRule(idx, { ruleSets: nextValues })
        }
        setSelectionModal(null)
    }

    const hasInvalid = rules.some(r => !r.outbound
        || (r.kind === 'inbound' && !r.inbound)
        || (r.kind === 'auth_user' && r.authUsers.length === 0)
        || (r.kind === 'rule_set' && r.ruleSets.length === 0))
    const activeSelectionRule = selectionModal ? rules[selectionModal.idx] : null

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
                    disabled={!canWrite || saving || hasInvalid}
                    className={`px-3 py-2 rounded-lg text-sm font-medium transition-colors ${!canWrite || saving || hasInvalid
                        ? 'bg-slate-800 text-slate-500 cursor-not-allowed'
                        : 'bg-blue-600 hover:bg-blue-500 text-white'
                        }`}
                >
                    {saving ? 'Saving...' : 'Save Rules'}
                </button>
            </div>

            <div className="space-y-3">
                {rules.length === 0 && (
                    <div className="text-sm text-slate-400">No editable rules. Add one below.</div>
                )}
                {rules.map((rule, idx) => (
                    <Card key={idx} title={`Rule ${idx + 1}`} className="space-y-3">
                        <div className="grid grid-cols-1 md:grid-cols-[2fr_5fr_5fr_2fr_auto] gap-3 items-end">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Type</label>
                                <select
                                    className="select-field w-full h-[38px] bg-slate-950 border border-slate-800 rounded-lg px-3 text-white outline-none focus:border-blue-500/50 transition-colors"
                                    value={rule.kind}
                                    disabled={!canWrite}
                                    onChange={e => updateRuleKind(idx, e.target.value as RouteRuleKind)}
                                >
                                    <option value="inbound">Inbound</option>
                                    <option value="auth_user">Auth user</option>
                                    <option value="rule_set">Rule set</option>
                                </select>
                            </div>
                            <div className="space-y-1">
                                {rule.kind === 'inbound' ? (
                                    <>
                                        <label className="text-xs font-medium text-slate-400">Inbound</label>
                                        <select
                                            className="select-field w-full h-[38px] bg-slate-950 border border-slate-800 rounded-lg px-3 text-white outline-none focus:border-blue-500/50 transition-colors"
                                            value={rule.inbound}
                                            disabled={!canWrite}
                                            onChange={e => updateRule(idx, { inbound: e.target.value })}
                                        >
                                            <option value="">Select inbound</option>
                                            {availableInbounds.map(tag => (
                                                <option key={tag} value={tag}>{tag}</option>
                                            ))}
                                        </select>
                                        {!rule.inbound && <p className="text-xs text-amber-400">Inbound is required.</p>}
                                    </>
                                ) : (
                                    <>
                                        <label className="text-xs font-medium text-slate-400">{rule.kind === 'auth_user' ? 'Auth users' : 'Rule sets'}</label>
                                        <button
                                            type="button"
                                            onClick={() => setSelectionModal({ idx, type: rule.kind === 'auth_user' ? 'auth_user' : 'rule_set' })}
                                            disabled={!canWrite}
                                            className="w-full min-h-[38px] rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-left text-sm text-white outline-none hover:border-blue-500/50 disabled:opacity-60"
                                        >
                                            {rule.kind === 'auth_user'
                                                ? rule.authUsers.length ? rule.authUsers.join(', ') : 'Select users'
                                                : rule.ruleSets.length ? rule.ruleSets.join(', ') : 'Select rule sets'}
                                        </button>
                                        {rule.kind === 'auth_user' && rule.authUsers.length === 0 && <p className="text-xs text-amber-400">At least one user is required.</p>}
                                        {rule.kind === 'rule_set' && rule.ruleSets.length === 0 && <p className="text-xs text-amber-400">At least one rule set is required.</p>}
                                        {rule.kind === 'auth_user' && rule.ruleSets.length > 0 && (
                                            <div className="space-y-1 pt-2">
                                                <div className="flex items-center justify-between gap-2">
                                                    <label className="text-xs font-medium text-slate-400">Rule sets</label>
                                                    <button
                                                        type="button"
                                                        onClick={() => updateRule(idx, { ruleSets: [] })}
                                                        disabled={!canWrite}
                                                        className="text-xs text-slate-400 hover:text-white disabled:opacity-60"
                                                    >
                                                        Remove
                                                    </button>
                                                </div>
                                                <button
                                                    type="button"
                                                    onClick={() => setSelectionModal({ idx, type: 'rule_set' })}
                                                    disabled={!canWrite}
                                                    className="w-full min-h-[38px] rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-left text-sm text-white outline-none hover:border-blue-500/50 disabled:opacity-60"
                                                >
                                                    {rule.ruleSets.length ? rule.ruleSets.join(', ') : 'Select rule sets'}
                                                </button>
                                            </div>
                                        )}
                                        {rule.kind === 'auth_user' && rule.ruleSets.length === 0 && (
                                            <button
                                                type="button"
                                                onClick={() => setSelectionModal({ idx, type: 'rule_set' })}
                                                disabled={!canWrite}
                                                className="mt-2 text-xs font-medium text-blue-400 hover:text-blue-300 disabled:opacity-60"
                                            >
                                                Add rule set condition
                                            </button>
                                        )}
                                    </>
                                )}
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Outbound</label>
                                <select
                                    className="select-field w-full h-[38px] bg-slate-950 border border-slate-800 rounded-lg px-3 text-white outline-none focus:border-blue-500/50 transition-colors"
                                    value={rule.outbound}
                                    disabled={!canWrite}
                                    onChange={e => updateRule(idx, { outbound: e.target.value })}
                                >
                                    <option value="">Select outbound</option>
                                    {availableOutbounds.map(tag => (
                                        <option key={tag} value={tag}>{tag}</option>
                                    ))}
                                </select>
                                {!rule.outbound && <p className="text-xs text-amber-400">Outbound is required.</p>}
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">IP Version (optional)</label>
                                <select
                                    className="select-field w-full h-[38px] bg-slate-950 border border-slate-800 rounded-lg px-3 text-white outline-none focus:border-blue-500/50 transition-colors"
                                    value={rule.ipVersion || ''}
                                    disabled={!canWrite}
                                    onChange={e => updateRule(idx, { ipVersion: e.target.value })}
                                >
                                    <option value="">Any</option>
                                    <option value="4">IPv4</option>
                                    <option value="6">IPv6</option>
                                </select>
                            </div>
                            <div className="flex md:justify-end">
                                <button
                                    onClick={() => removeRule(idx)}
                                    disabled={!canWrite}
                                    className="w-full md:w-auto h-[38px] px-3 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium"
                                >
                                    Remove
                                </button>
                            </div>
                        </div>
                    </Card>
                ))}
            </div>

            <button
                onClick={addRule}
                disabled={!canWrite}
                className="px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium"
            >
                Add Rule
            </button>

            {selectionModal && activeSelectionRule && (
                <RuleValueModal
                    isOpen
                    title={selectionModal.type === 'auth_user' ? 'Manage Auth Users' : 'Manage Rule Sets'}
                    options={selectionModal.type === 'auth_user' ? availableAuthUsers : availableRuleSets}
                    selected={selectionModal.type === 'auth_user' ? activeSelectionRule.authUsers : activeSelectionRule.ruleSets}
                    emptyText={selectionModal.type === 'auth_user' ? 'No users found.' : 'No rule sets found.'}
                    allowCustom={selectionModal.type === 'auth_user'}
                    customPlaceholder="Add user"
                    onClose={() => setSelectionModal(null)}
                    onApply={selected => applySelection(selectionModal.idx, selectionModal.type, selected)}
                />
            )}
        </div>
    )
}

function RuleValueModal({
    isOpen,
    title,
    options,
    selected,
    emptyText,
    allowCustom = false,
    customPlaceholder = 'Add value',
    onClose,
    onApply,
}: {
    isOpen: boolean
    title: string
    options: string[]
    selected: string[]
    emptyText: string
    allowCustom?: boolean
    customPlaceholder?: string
    onClose: () => void
    onApply: (selected: string[]) => void
}) {
    const [draft, setDraft] = useState<string[]>(selected)
    const [customValue, setCustomValue] = useState('')
    const allOptions = uniqueSorted([...options, ...draft])

    useEffect(() => {
        if (!isOpen) return
        setDraft(selected)
        setCustomValue('')
    }, [isOpen, selected])

    const toggle = (value: string) => {
        setDraft(prev => prev.includes(value) ? prev.filter(item => item !== value) : uniqueSorted([...prev, value]))
    }

    const addCustom = () => {
        const value = customValue.trim()
        if (!value) return
        setDraft(prev => uniqueSorted([...prev, value]))
        setCustomValue('')
    }

    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={title}
            size="md"
            footer={
                <>
                    <Button variant="ghost" onClick={onClose}>Cancel</Button>
                    <Button variant="primary" onClick={() => onApply(draft)}>Apply</Button>
                </>
            }
        >
            <div className="space-y-4">
                {allowCustom && (
                    <div className="flex gap-2">
                        <input
                            value={customValue}
                            onChange={e => setCustomValue(e.target.value)}
                            onKeyDown={e => {
                                if (e.key === 'Enter') {
                                    e.preventDefault()
                                    addCustom()
                                }
                            }}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                            placeholder={customPlaceholder}
                        />
                        <Button variant="secondary" onClick={addCustom}>Add</Button>
                    </div>
                )}

                {allOptions.length === 0 ? (
                    <div className="text-sm text-slate-400">{emptyText}</div>
                ) : (
                    <div className="max-h-[360px] overflow-y-auto custom-scrollbar space-y-2">
                        {allOptions.map(option => (
                            <label
                                key={option}
                                className="flex items-center gap-3 rounded-lg border border-slate-800 bg-slate-950 p-3 text-sm text-slate-200"
                            >
                                <input
                                    type="checkbox"
                                    checked={draft.includes(option)}
                                    onChange={() => toggle(option)}
                                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-blue-500"
                                />
                                <span className="min-w-0 flex-1 truncate" title={option}>{option}</span>
                            </label>
                        ))}
                    </div>
                )}
            </div>
        </Modal>
    )
}
