import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, RefreshCw, Shield, Trash2 } from 'lucide-react'
import { Card } from '../../../components/ui/Card'
import { Button } from '../../../components/ui/Button'
import { Badge } from '../../../components/ui/Badge'
import {
    api,
    type BlockedSubscriptionRequestEntry,
    type CreateProtectionRuleRequest,
    type ProtectionRuleType,
} from '../../../services/api'

interface SecurityTabProps {
    canWriteSettings: boolean
    success: (msg: string) => void
    toastError: (msg: string) => void
}

const RULE_TYPE_OPTIONS: Array<{ value: ProtectionRuleType; label: string }> = [
    { value: 'ip_block', label: 'IP Block' },
    { value: 'token_block', label: 'Token Block' },
    { value: 'ip_allow', label: 'IP Allow' },
]

const PAGE_SIZE = 50

const formatTimestamp = (value?: number) => {
    if (!value) return '—'
    return new Date(value * 1000).toLocaleString()
}

const ruleVariant = (ruleType: ProtectionRuleType) => {
    switch (ruleType) {
        case 'ip_block':
            return 'error'
        case 'token_block':
            return 'warning'
        case 'ip_allow':
            return 'success'
        default:
            return 'neutral'
    }
}

const reasonVariant = (reason: string) => {
    switch (reason) {
        case 'ip_block':
        case 'token_block':
            return 'error'
        case 'rate_limit':
            return 'warning'
        case 'ua_filter':
            return 'info'
        default:
            return 'neutral'
    }
}

export default function SecurityTab({ canWriteSettings, success, toastError }: SecurityTabProps) {
    const queryClient = useQueryClient()
    const [maxRequests, setMaxRequests] = useState(60)
    const [windowSeconds, setWindowSeconds] = useState(60)
    const [uaFilterEnabled, setUAFilterEnabled] = useState(false)
    const [newRule, setNewRule] = useState<CreateProtectionRuleRequest>({
        rule_type: 'ip_block',
        value: '',
        note: '',
    })
    const [offset, setOffset] = useState(0)

    const settingsQuery = useQuery({
        queryKey: ['subscription-protection'],
        queryFn: () => api.getSubscriptionProtection(),
        placeholderData: previousData => previousData,
    })

    const rulesQuery = useQuery({
        queryKey: ['protection-rules'],
        queryFn: () => api.getProtectionRules(),
        placeholderData: previousData => previousData,
    })

    const blockedLogQuery = useQuery({
        queryKey: ['blocked-log', offset],
        queryFn: () => api.getBlockedSubscriptionRequestLog(PAGE_SIZE, offset),
        placeholderData: previousData => previousData,
        refetchInterval: 30000,
    })

    useEffect(() => {
        if (!settingsQuery.data) return
        setMaxRequests(Math.max(1, settingsQuery.data.max_requests || 60))
        setWindowSeconds(Math.max(1, settingsQuery.data.window_seconds || 60))
        setUAFilterEnabled(!!settingsQuery.data.ua_filter_enabled)
    }, [settingsQuery.data])

    const saveSettingsMutation = useMutation({
        mutationFn: async () => api.updateSubscriptionProtection({
            max_requests: Math.max(1, Number(maxRequests) || 1),
            window_seconds: Math.max(1, Number(windowSeconds) || 1),
            ua_filter_enabled: uaFilterEnabled,
        }),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ['subscription-protection'] })
            success('Subscription protection settings saved')
        },
        onError: err => {
            toastError('Failed to save subscription protection settings: ' + err)
        },
    })

    const createRuleMutation = useMutation({
        mutationFn: async () => api.createProtectionRule({
            rule_type: newRule.rule_type,
            value: newRule.value.trim(),
            note: newRule.note.trim(),
        }),
        onSuccess: async () => {
            setNewRule({ rule_type: newRule.rule_type, value: '', note: '' })
            await queryClient.invalidateQueries({ queryKey: ['protection-rules'] })
            success('Protection rule added')
        },
        onError: err => {
            toastError('Failed to add protection rule: ' + err)
        },
    })

    const deleteRuleMutation = useMutation({
        mutationFn: async (id: number) => api.deleteProtectionRule(id),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ['protection-rules'] })
            success('Protection rule deleted')
        },
        onError: err => {
            toastError('Failed to delete protection rule: ' + err)
        },
    })

    const settingsLoading = settingsQuery.isLoading && !settingsQuery.data
    const rulesLoading = rulesQuery.isLoading && !rulesQuery.data
    const blockedLogLoading = blockedLogQuery.isLoading && !blockedLogQuery.data
    const rules = rulesQuery.data ?? []
    const blockedRows = blockedLogQuery.data ?? []

    return (
        <div className="space-y-4 sm:space-y-6">
            <Card
                title="Protection Settings"
                action={settingsQuery.isFetching ? <RefreshCw size={16} className="animate-spin text-slate-400" /> : undefined}
            >
                {settingsLoading ? (
                    <div className="flex items-center gap-2 text-sm text-slate-400">
                        <RefreshCw size={16} className="animate-spin" />
                        Loading protection settings...
                    </div>
                ) : (
                    <>
                        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Max Requests</label>
                                <input
                                    type="number"
                                    min={1}
                                    value={maxRequests}
                                    disabled={!canWriteSettings}
                                    onChange={e => setMaxRequests(Number(e.target.value) || 1)}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                                />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Window Seconds</label>
                                <input
                                    type="number"
                                    min={1}
                                    value={windowSeconds}
                                    disabled={!canWriteSettings}
                                    onChange={e => setWindowSeconds(Number(e.target.value) || 1)}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                                />
                            </div>
                            <div className="flex items-end">
                                <label className="flex items-center gap-3 rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-slate-200 w-full">
                                    <input
                                        type="checkbox"
                                        checked={uaFilterEnabled}
                                        disabled={!canWriteSettings}
                                        onChange={e => setUAFilterEnabled(e.target.checked)}
                                        className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-500 focus:ring-blue-500/40"
                                    />
                                    Block browser-like User-Agents
                                </label>
                            </div>
                        </div>
                        <div className="flex justify-end mt-4">
                            <Button
                                onClick={() => saveSettingsMutation.mutate()}
                                isLoading={saveSettingsMutation.isPending}
                                disabled={!canWriteSettings}
                                icon={<Shield size={16} />}
                            >
                                Save Protection Settings
                            </Button>
                        </div>
                    </>
                )}
            </Card>

            <Card
                title="Block Rules"
                action={rulesQuery.isFetching ? <RefreshCw size={16} className="animate-spin text-slate-400" /> : undefined}
            >
                <div className="grid grid-cols-1 md:grid-cols-[180px_minmax(0,1fr)_minmax(0,1fr)_auto] gap-3 mb-4">
                    <select
                        value={newRule.rule_type}
                        disabled={!canWriteSettings}
                        onChange={e => setNewRule(prev => ({ ...prev, rule_type: e.target.value as ProtectionRuleType }))}
                        className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                    >
                        {RULE_TYPE_OPTIONS.map(option => (
                            <option key={option.value} value={option.value}>{option.label}</option>
                        ))}
                    </select>
                    <input
                        type="text"
                        value={newRule.value}
                        disabled={!canWriteSettings}
                        onChange={e => setNewRule(prev => ({ ...prev, value: e.target.value }))}
                        placeholder="Value"
                        className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                    />
                    <input
                        type="text"
                        value={newRule.note}
                        disabled={!canWriteSettings}
                        onChange={e => setNewRule(prev => ({ ...prev, note: e.target.value }))}
                        placeholder="Optional note"
                        className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                    />
                    <Button
                        onClick={() => createRuleMutation.mutate()}
                        isLoading={createRuleMutation.isPending}
                        disabled={!canWriteSettings || !newRule.value.trim()}
                        icon={<Plus size={16} />}
                    >
                        Add Rule
                    </Button>
                </div>

                {rulesLoading ? (
                    <div className="flex items-center gap-2 text-sm text-slate-400">
                        <RefreshCw size={16} className="animate-spin" />
                        Loading rules...
                    </div>
                ) : rules.length === 0 ? (
                    <div className="rounded-lg border border-dashed border-slate-800 px-4 py-6 text-sm text-slate-400">
                        No protection rules configured.
                    </div>
                ) : (
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead className="text-left text-slate-400">
                                <tr className="border-b border-slate-800">
                                    <th className="py-2 pr-4 font-medium">Type</th>
                                    <th className="py-2 pr-4 font-medium">Value</th>
                                    <th className="py-2 pr-4 font-medium">Note</th>
                                    <th className="py-2 pr-4 font-medium">Created</th>
                                    <th className="py-2 font-medium text-right">Actions</th>
                                </tr>
                            </thead>
                            <tbody>
                                {rules.map(rule => (
                                    <tr key={rule.id} className="border-b border-slate-900/80 text-slate-200">
                                        <td className="py-3 pr-4">
                                            <Badge variant={ruleVariant(rule.rule_type)}>
                                                {rule.rule_type}
                                            </Badge>
                                        </td>
                                        <td className="py-3 pr-4 font-mono text-xs text-slate-300">{rule.value}</td>
                                        <td className="py-3 pr-4 text-slate-400">{rule.note || '—'}</td>
                                        <td className="py-3 pr-4 text-slate-400">{formatTimestamp(rule.created_at)}</td>
                                        <td className="py-3 text-right">
                                            <Button
                                                variant="danger"
                                                size="sm"
                                                disabled={!canWriteSettings}
                                                isLoading={deleteRuleMutation.isPending && deleteRuleMutation.variables === rule.id}
                                                onClick={() => deleteRuleMutation.mutate(rule.id)}
                                                icon={<Trash2 size={14} />}
                                            >
                                                Delete
                                            </Button>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </Card>

            <Card
                title="Blocked Request Log"
                action={blockedLogQuery.isFetching ? <RefreshCw size={16} className="animate-spin text-slate-400" /> : undefined}
            >
                {blockedLogLoading ? (
                    <div className="flex items-center gap-2 text-sm text-slate-400">
                        <RefreshCw size={16} className="animate-spin" />
                        Loading blocked requests...
                    </div>
                ) : blockedRows.length === 0 ? (
                    <div className="rounded-lg border border-dashed border-slate-800 px-4 py-6 text-sm text-slate-400">
                        No blocked requests recorded yet.
                    </div>
                ) : (
                    <>
                        <div className="overflow-x-auto">
                            <table className="w-full text-sm">
                                <thead className="text-left text-slate-400">
                                    <tr className="border-b border-slate-800">
                                        <th className="py-2 pr-4 font-medium">Subscription</th>
                                        <th className="py-2 pr-4 font-medium">Source IP</th>
                                        <th className="py-2 pr-4 font-medium">Reason</th>
                                        <th className="py-2 pr-4 font-medium">User Agent</th>
                                        <th className="py-2 font-medium">Time</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {blockedRows.map((entry: BlockedSubscriptionRequestEntry) => (
                                        <tr key={entry.id} className="border-b border-slate-900/80 text-slate-200 align-top">
                                            <td className="py-3 pr-4">{entry.sub_name || `#${entry.sub_id}`}</td>
                                            <td className="py-3 pr-4 font-mono text-xs text-slate-300">{entry.request_ip || '—'}</td>
                                            <td className="py-3 pr-4">
                                                <Badge variant={reasonVariant(entry.block_reason)}>
                                                    {entry.block_reason || 'unknown'}
                                                </Badge>
                                            </td>
                                            <td className="py-3 pr-4 max-w-[260px]">
                                                <div className="truncate text-slate-400" title={entry.user_agent}>
                                                    {entry.user_agent || '—'}
                                                </div>
                                            </td>
                                            <td className="py-3 text-slate-400">{formatTimestamp(entry.requested_at)}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                        <div className="flex items-center justify-between mt-4">
                            <p className="text-xs text-slate-500">Showing {blockedRows.length} request{blockedRows.length === 1 ? '' : 's'} at offset {offset}.</p>
                            <div className="flex gap-2">
                                <Button
                                    variant="secondary"
                                    size="sm"
                                    disabled={offset === 0}
                                    onClick={() => setOffset(prev => Math.max(0, prev - PAGE_SIZE))}
                                >
                                    Previous
                                </Button>
                                <Button
                                    variant="secondary"
                                    size="sm"
                                    disabled={blockedRows.length < PAGE_SIZE}
                                    onClick={() => setOffset(prev => prev + PAGE_SIZE)}
                                >
                                    Next
                                </Button>
                            </div>
                        </div>
                    </>
                )}
            </Card>
        </div>
    )
}
