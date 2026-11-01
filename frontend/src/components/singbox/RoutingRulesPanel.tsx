import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Plus, Trash2 } from 'lucide-react'
import { api } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { Button } from '../ui/Button'
import { Badge } from '../ui/Badge'
import { ChipInput } from '../ui/ChipInput'
import { ActionIconButton } from '../ui/ActionIconButton'
import {
    emptyRuleDraft,
    moveItem,
    parseRule,
    serializeRule,
    splitListInput,
    summarizeRule,
    validateRule,
    type KnownTags,
    type RuleDraft,
    type RuleItem,
    type RuleMatch,
} from './routingModel'

const inputClass = 'w-full rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white outline-none transition-colors focus:border-blue-500/50'

interface Props {
    canWrite: boolean
    // Bumped by the parent when something outside this panel changed the config.
    refreshKey: number
    onOutbounds: (tags: string[]) => void
}

type ListField = {
    key: keyof Pick<RuleMatch, 'inbound' | 'auth_user' | 'rule_set' | 'domain' | 'domain_suffix' | 'domain_keyword' | 'ip_cidr' | 'port' | 'port_range' | 'protocol'>
    label: string
    placeholder: string
    suggest?: 'inbounds' | 'users' | 'ruleSets'
}

const FIELDS: ListField[] = [
    { key: 'domain_suffix', label: 'Domain suffix', placeholder: 'example.com' },
    { key: 'domain', label: 'Exact domain', placeholder: 'www.example.com' },
    { key: 'domain_keyword', label: 'Domain keyword', placeholder: 'ads' },
    { key: 'ip_cidr', label: 'IP / CIDR', placeholder: '10.0.0.0/8' },
    { key: 'rule_set', label: 'Rule sets', placeholder: 'geosite-cn', suggest: 'ruleSets' },
    { key: 'inbound', label: 'Inbound', placeholder: 'in-vless', suggest: 'inbounds' },
    { key: 'auth_user', label: 'Users', placeholder: 'alice', suggest: 'users' },
    { key: 'port', label: 'Port', placeholder: '443' },
    { key: 'port_range', label: 'Port range', placeholder: '1000:2000' },
    { key: 'protocol', label: 'Protocol', placeholder: 'tls, http, quic' },
]

export function RoutingRulesPanel({ canWrite, refreshKey, onOutbounds }: Props) {
    const { success, error } = useToast()
    const [items, setItems] = useState<RuleItem[]>([])
    const [open, setOpen] = useState<Set<number>>(new Set())
    const [known, setKnown] = useState<KnownTags>({ inbounds: [], outbounds: [], ruleSets: [] })
    const [users, setUsers] = useState<string[]>([])
    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [dirty, setDirty] = useState(false)
    const [pendingRestart, setPendingRestart] = useState(false)
    const [restarting, setRestarting] = useState(false)

    const load = async () => {
        setLoading(true)
        try {
            const parsed = JSON.parse(await api.getSingboxConfig())
            const tagsOf = (list: unknown): string[] => (Array.isArray(list)
                ? list.map(entry => (entry && typeof entry.tag === 'string' ? entry.tag.trim() : '')).filter(Boolean)
                : [])
            const inbounds = Array.isArray(parsed?.inbounds) ? parsed.inbounds : []
            const next: KnownTags = {
                inbounds: tagsOf(inbounds),
                outbounds: tagsOf(parsed?.outbounds),
                ruleSets: tagsOf(parsed?.route?.rule_set),
            }
            setKnown(next)
            onOutbounds(next.outbounds)
            setUsers(Array.from(new Set(inbounds.flatMap((inbound: { users?: unknown }) => (Array.isArray(inbound.users)
                ? inbound.users.map((user: unknown) => (typeof user === 'string' ? user : (user as { name?: unknown })?.name))
                : [])).filter((name: unknown): name is string => typeof name === 'string' && name.trim() !== ''))) as string[])
            const rules = Array.isArray(parsed?.route?.rules) ? parsed.route.rules : []
            setItems(rules.map(parseRule))
            setOpen(new Set())
            setDirty(false)
        } catch {
            error('Failed to load routing rules')
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        void load()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [refreshKey])

    const problems = useMemo(() => items.map(item => (item.kind === 'editable' ? validateRule(item.draft, known) : '')), [items, known])
    const hasProblem = problems.some(Boolean)

    const touch = (updater: (current: RuleItem[]) => RuleItem[]) => {
        setItems(updater)
        setDirty(true)
    }

    const updateDraft = (index: number, patch: (draft: RuleDraft) => RuleDraft) => {
        touch(current => current.map((item, i) => (i === index && item.kind === 'editable' ? { ...item, draft: patch(item.draft) } : item)))
    }

    const addRule = () => {
        touch(current => [...current, { kind: 'editable', draft: emptyRuleDraft() }])
        setOpen(prev => new Set(prev).add(items.length))
    }

    const move = (from: number, to: number) => {
        touch(current => moveItem(current, from, to))
        setOpen(prev => {
            const next = new Set<number>()
            prev.forEach(index => next.add(index === from ? to : index === to ? from : index))
            return next
        })
    }

    const remove = (index: number) => {
        touch(current => current.filter((_, i) => i !== index))
        setOpen(prev => {
            const next = new Set<number>()
            prev.forEach(i => { if (i < index) next.add(i); else if (i > index) next.add(i - 1) })
            return next
        })
    }

    const toggle = (index: number) => setOpen(prev => {
        const next = new Set(prev)
        if (next.has(index)) next.delete(index)
        else next.add(index)
        return next
    })

    const save = async () => {
        setSaving(true)
        try {
            const parsed = JSON.parse(await api.getSingboxConfig())
            const route = parsed.route && typeof parsed.route === 'object' ? parsed.route : {}
            route.rules = items.map(serializeRule)
            parsed.route = route
            await api.updateSingboxConfig(JSON.stringify(parsed, null, 2))
            const dashboard = await api.getDashboardData()
            if (dashboard.singbox_pending_changes) setPendingRestart(true)
            else {
                setPendingRestart(false)
                success('Rules saved')
            }
            await load()
        } catch (err) {
            error(err instanceof Error ? `Failed to save rules: ${err.message}` : 'Failed to save rules')
        } finally {
            setSaving(false)
        }
    }

    const restart = async () => {
        setRestarting(true)
        try {
            await api.restartService('sing-box')
            await new Promise(resolve => setTimeout(resolve, 2000))
            setPendingRestart(false)
            success('Sing-box restart started')
        } catch (err) {
            error('Failed to restart Sing-box: ' + err)
        } finally {
            setRestarting(false)
        }
    }

    const suggestionsFor = (field: ListField): string[] => (field.suggest === 'inbounds' ? known.inbounds : field.suggest === 'users' ? users : field.suggest === 'ruleSets' ? known.ruleSets : [])

    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                    <h3 className="text-sm font-semibold text-white">Rules</h3>
                    <p className="text-xs text-slate-400">The first rule that matches decides. Order matters.</p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                    <Button variant="secondary" size="sm" onClick={load} disabled={loading}>{loading ? 'Refreshing…' : 'Refresh'}</Button>
                    <Button variant="secondary" size="sm" onClick={addRule} disabled={!canWrite} icon={<Plus size={14} />}>Add rule</Button>
                    {pendingRestart && (
                        <Button variant="secondary" size="sm" onClick={restart} disabled={restarting}>{restarting ? 'Restarting…' : 'Pending restart'}</Button>
                    )}
                    <Button variant="primary" size="sm" onClick={save} disabled={!canWrite || !dirty || saving || hasProblem} isLoading={saving}>Save rules</Button>
                </div>
            </div>

            {items.length === 0 && !loading && <p className="rounded-lg border border-slate-800 p-4 text-sm text-slate-400">No rules yet.</p>}

            <div className="space-y-2">
                {items.map((item, index) => {
                    const isOpen = open.has(index)
                    const problem = problems[index]
                    const summary = item.kind === 'editable' ? summarizeRule(item.draft) : 'Custom rule, kept as is'
                    return (
                        <div key={index} className={`rounded-xl border bg-slate-900 ${problem ? 'border-amber-500/40' : 'border-slate-800'}`}>
                            <div className="flex flex-wrap items-center gap-2 px-3 py-2.5">
                                <button
                                    type="button"
                                    onClick={() => toggle(index)}
                                    className="flex min-w-0 flex-1 basis-48 items-center gap-2 text-left"
                                    aria-expanded={isOpen}
                                >
                                    {isOpen ? <ChevronDown size={16} className="shrink-0 text-slate-400" /> : <ChevronRight size={16} className="shrink-0 text-slate-400" />}
                                    <span className="w-6 shrink-0 text-xs text-slate-500">{index + 1}</span>
                                    <span className="break-words text-sm text-slate-100 sm:truncate" title={summary}>{summary}</span>
                                </button>
                                <div className="flex shrink-0 items-center gap-1.5">
                                    {item.kind === 'raw' && <Badge variant="neutral">custom</Badge>}
                                    {item.kind === 'editable' && item.draft.action === 'reject' && <Badge variant="error">reject</Badge>}
                                    <ActionIconButton onClick={() => move(index, index - 1)} title="Move up" disabled={!canWrite || index === 0}><ArrowUp size={16} /></ActionIconButton>
                                    <ActionIconButton onClick={() => move(index, index + 1)} title="Move down" disabled={!canWrite || index === items.length - 1}><ArrowDown size={16} /></ActionIconButton>
                                    <ActionIconButton onClick={() => remove(index)} title="Delete" tone="danger" disabled={!canWrite}><Trash2 size={16} /></ActionIconButton>
                                </div>
                            </div>

                            {problem && <p className="px-4 pb-2 text-xs text-amber-400">{problem}</p>}

                            {isOpen && item.kind === 'raw' && (
                                <pre className="mx-3 mb-3 max-h-64 overflow-auto rounded-lg bg-slate-950 p-3 text-xs text-slate-300">{JSON.stringify(item.raw, null, 2)}</pre>
                            )}

                            {isOpen && item.kind === 'editable' && (
                                <div className="space-y-3 border-t border-slate-800 px-4 py-3">
                                    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                                        {FIELDS.map(field => (
                                            <div key={field.key} className="space-y-1">
                                                <label className="text-xs font-medium text-slate-400">{field.label}</label>
                                                <ChipInput
                                                    value={item.draft.match[field.key]}
                                                    suggestions={suggestionsFor(field)}
                                                    placeholder={field.placeholder}
                                                    disabled={!canWrite}
                                                    parse={splitListInput}
                                                    ariaLabel={field.label}
                                                    onChange={next => updateDraft(index, draft => ({ ...draft, match: { ...draft.match, [field.key]: next } }))}
                                                />
                                            </div>
                                        ))}
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-400">IP version</label>
                                            <select
                                                className={`select-field ${inputClass}`}
                                                value={item.draft.match.ip_version}
                                                disabled={!canWrite}
                                                onChange={event => updateDraft(index, draft => ({ ...draft, match: { ...draft.match, ip_version: event.target.value as RuleMatch['ip_version'] } }))}
                                            >
                                                <option value="">Any</option>
                                                <option value="4">IPv4</option>
                                                <option value="6">IPv6</option>
                                            </select>
                                        </div>
                                    </div>
                                    <div className="grid grid-cols-1 gap-3 border-t border-slate-800 pt-3 sm:grid-cols-2">
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-400">Then</label>
                                            <select
                                                className={`select-field ${inputClass}`}
                                                value={item.draft.action}
                                                disabled={!canWrite}
                                                onChange={event => updateDraft(index, draft => ({ ...draft, action: event.target.value as RuleDraft['action'] }))}
                                            >
                                                <option value="route">Send through an outbound</option>
                                                <option value="reject">Reject (block)</option>
                                            </select>
                                        </div>
                                        {item.draft.action === 'route' && (
                                            <div className="space-y-1">
                                                <label className="text-xs font-medium text-slate-400">Outbound</label>
                                                <select
                                                    className={`select-field ${inputClass}`}
                                                    value={item.draft.outbound}
                                                    disabled={!canWrite}
                                                    onChange={event => updateDraft(index, draft => ({ ...draft, outbound: event.target.value }))}
                                                >
                                                    <option value="">Select…</option>
                                                    {known.outbounds.map(tag => <option key={tag} value={tag}>{tag}</option>)}
                                                    {item.draft.outbound && !known.outbounds.includes(item.draft.outbound) && (
                                                        <option value={item.draft.outbound}>{item.draft.outbound} (missing)</option>
                                                    )}
                                                </select>
                                            </div>
                                        )}
                                    </div>
                                </div>
                            )}
                        </div>
                    )
                })}
            </div>
        </div>
    )
}
