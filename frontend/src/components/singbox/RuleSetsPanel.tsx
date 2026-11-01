import { useEffect, useState } from 'react'
import { Edit, Plus, Trash2 } from 'lucide-react'
import { api } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { Button } from '../ui/Button'
import { Badge } from '../ui/Badge'
import { Modal } from '../ui/Modal'
import { ActionIconButton } from '../ui/ActionIconButton'
import {
    emptyRuleSetDraft,
    parseRuleSet,
    presetRuleSetDraft,
    ruleSetUsage,
    serializeRuleSet,
    validateRuleSet,
    type RuleSetDraft,
    type RuleSetItem,
    type RuleSetPreset,
} from './routingModel'

const inputClass = 'w-full rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white outline-none transition-colors focus:border-blue-500/50'

interface Props {
    canWrite: boolean
    outbounds: string[]
    onChanged: () => void
}

export function RuleSetsPanel({ canWrite, outbounds, onChanged }: Props) {
    const { success, error } = useToast()
    const [items, setItems] = useState<RuleSetItem[]>([])
    const [rules, setRules] = useState<unknown[]>([])
    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [dirty, setDirty] = useState(false)
    const [editor, setEditor] = useState<{ index: number | null; draft: RuleSetDraft } | null>(null)
    const [editorError, setEditorError] = useState('')
    const [presetKind, setPresetKind] = useState<RuleSetPreset>('geosite')
    const [presetName, setPresetName] = useState('')

    const load = async () => {
        setLoading(true)
        try {
            const parsed = JSON.parse(await api.getSingboxConfig())
            const entries = Array.isArray(parsed?.route?.rule_set) ? parsed.route.rule_set : []
            setItems(entries.map(parseRuleSet))
            setRules(Array.isArray(parsed?.route?.rules) ? parsed.route.rules : [])
            setDirty(false)
        } catch {
            error('Failed to load rule sets')
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        void load()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    const tagOf = (item: RuleSetItem): string => {
        if (item.kind === 'editable') return item.draft.tag
        const raw = item.raw as { tag?: unknown } | null
        return raw && typeof raw.tag === 'string' ? raw.tag : ''
    }
    const tags = items.map(tagOf)

    const openNew = (draft: RuleSetDraft = emptyRuleSetDraft()) => {
        setEditorError('')
        setEditor({ index: null, draft })
    }

    const openEdit = (index: number) => {
        const item = items[index]
        if (item.kind !== 'editable') return
        setEditorError('')
        setEditor({ index, draft: { ...item.draft } })
    }

    const confirmEditor = () => {
        if (!editor) return
        const others = tags.filter((_, i) => i !== editor.index)
        const problem = validateRuleSet(editor.draft, others)
        if (problem) {
            setEditorError(problem)
            return
        }
        const next: RuleSetItem = { kind: 'editable', draft: editor.draft }
        setItems(prev => (editor.index === null ? [...prev, next] : prev.map((item, i) => (i === editor.index ? next : item))))
        setDirty(true)
        setEditor(null)
    }

    const remove = (index: number) => {
        const tag = tagOf(items[index])
        const used = tag ? ruleSetUsage(rules, tag) : []
        if (used.length > 0) {
            error(`"${tag}" is used by ${used.length} rule${used.length > 1 ? 's' : ''}. Remove it from them first.`)
            return
        }
        setItems(prev => prev.filter((_, i) => i !== index))
        setDirty(true)
    }

    const addPreset = () => {
        if (!presetName.trim()) return
        openNew(presetRuleSetDraft(presetKind, presetName))
        setPresetName('')
    }

    const save = async () => {
        setSaving(true)
        try {
            const parsed = JSON.parse(await api.getSingboxConfig())
            const route = parsed.route && typeof parsed.route === 'object' ? parsed.route : {}
            const entries = items.map(item => (item.kind === 'editable' ? serializeRuleSet(item.draft) : item.raw))
            if (entries.length > 0) route.rule_set = entries
            else delete route.rule_set
            parsed.route = route
            await api.updateSingboxConfig(JSON.stringify(parsed, null, 2))
            success('Rule sets saved')
            await load()
            onChanged()
        } catch (err) {
            error(err instanceof Error ? `Failed to save rule sets: ${err.message}` : 'Failed to save rule sets')
        } finally {
            setSaving(false)
        }
    }

    const draft = editor?.draft

    return (
        <div className="space-y-3 rounded-xl border border-slate-800 bg-slate-900 p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                    <h3 className="text-sm font-semibold text-white">Rule sets</h3>
                    <p className="text-xs text-slate-400">Lists of domains or IPs that rules can match, downloaded by Sing-box.</p>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="secondary" size="sm" onClick={() => openNew()} disabled={!canWrite} icon={<Plus size={14} />}>Add</Button>
                    <Button variant="primary" size="sm" onClick={save} disabled={!canWrite || !dirty || saving} isLoading={saving}>Save rule sets</Button>
                </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
                <select
                    value={presetKind}
                    onChange={e => setPresetKind(e.target.value as RuleSetPreset)}
                    className={`select-field ${inputClass.replace('w-full', 'w-28')}`}
                    aria-label="Rule set source"
                    disabled={!canWrite}
                >
                    <option value="geosite">geosite</option>
                    <option value="geoip">geoip</option>
                </select>
                <input
                    value={presetName}
                    onChange={e => setPresetName(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter') addPreset() }}
                    placeholder={presetKind === 'geosite' ? 'netflix, category-ads-all…' : 'cn, us…'}
                    className={`${inputClass.replace('w-full', 'min-w-[10rem] flex-1 sm:max-w-xs')}`}
                    disabled={!canWrite}
                />
                <Button variant="ghost" size="sm" onClick={addPreset} disabled={!canWrite || !presetName.trim()}>Quick add</Button>
            </div>

            {loading ? (
                <p className="py-3 text-sm text-slate-400">Loading…</p>
            ) : items.length === 0 ? (
                <p className="py-3 text-sm text-slate-400">No rule sets yet.</p>
            ) : (
                <div className="divide-y divide-slate-800 rounded-lg border border-slate-800">
                    {items.map((item, index) => {
                        const editable = item.kind === 'editable'
                        const tag = tagOf(item) || '(no tag)'
                        const detail = editable
                            ? (item.draft.type === 'remote' ? item.draft.url : item.draft.path)
                            : 'Custom entry, kept as is'
                        const used = ruleSetUsage(rules, tagOf(item)).length
                        return (
                            <div key={`${tag}-${index}`} className="flex items-center justify-between gap-3 px-4 py-2.5">
                                <div className="min-w-0">
                                    <div className="flex items-center gap-2">
                                        <span className="truncate font-mono text-sm text-slate-100">{tag}</span>
                                        {editable && <Badge variant="neutral">{item.draft.type}</Badge>}
                                        {editable && <Badge variant="neutral">{item.draft.format}</Badge>}
                                        {used > 0 && <Badge variant="info">{used} rule{used > 1 ? 's' : ''}</Badge>}
                                    </div>
                                    <div className="truncate text-xs text-slate-400" title={detail}>{detail}</div>
                                </div>
                                <div className="flex shrink-0 items-center gap-1.5">
                                    {editable && <ActionIconButton onClick={() => openEdit(index)} title="Edit" tone="primary" disabled={!canWrite}><Edit size={16} /></ActionIconButton>}
                                    <ActionIconButton onClick={() => remove(index)} title="Delete" tone="danger" disabled={!canWrite}><Trash2 size={16} /></ActionIconButton>
                                </div>
                            </div>
                        )
                    })}
                </div>
            )}

            {editor && draft && (
                <Modal
                    isOpen
                    onClose={() => setEditor(null)}
                    title={editor.index === null ? 'Add rule set' : 'Edit rule set'}
                    footer={
                        <>
                            <Button variant="ghost" onClick={() => setEditor(null)}>Cancel</Button>
                            <Button variant="primary" onClick={confirmEditor}>{editor.index === null ? 'Add' : 'Apply'}</Button>
                        </>
                    }
                >
                    <div className="space-y-4">
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Type</label>
                                <select className={`select-field ${inputClass}`} value={draft.type} onChange={e => setEditor({ ...editor, draft: { ...draft, type: e.target.value as RuleSetDraft['type'] } })}>
                                    <option value="remote">remote (download)</option>
                                    <option value="local">local (file)</option>
                                </select>
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Tag</label>
                                <input className={inputClass} value={draft.tag} onChange={e => setEditor({ ...editor, draft: { ...draft, tag: e.target.value } })} placeholder="geosite-netflix" />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Format</label>
                                <select className={`select-field ${inputClass}`} value={draft.format} onChange={e => setEditor({ ...editor, draft: { ...draft, format: e.target.value as RuleSetDraft['format'] } })}>
                                    <option value="binary">binary (.srs)</option>
                                    <option value="source">source (.json)</option>
                                </select>
                            </div>
                        </div>
                        {draft.type === 'remote' ? (
                            <>
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-400">URL</label>
                                    <input className={inputClass} value={draft.url} onChange={e => setEditor({ ...editor, draft: { ...draft, url: e.target.value } })} placeholder="https://…/geosite-netflix.srs" />
                                </div>
                                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-400">Download through (optional)</label>
                                        <select className={`select-field ${inputClass}`} value={draft.downloadDetour} onChange={e => setEditor({ ...editor, draft: { ...draft, downloadDetour: e.target.value } })}>
                                            <option value="">Default</option>
                                            {outbounds.map(tag => <option key={tag} value={tag}>{tag}</option>)}
                                        </select>
                                    </div>
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-400">Update interval (optional)</label>
                                        <input className={inputClass} value={draft.updateInterval} onChange={e => setEditor({ ...editor, draft: { ...draft, updateInterval: e.target.value } })} placeholder="1d" />
                                    </div>
                                </div>
                            </>
                        ) : (
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Path</label>
                                <input className={inputClass} value={draft.path} onChange={e => setEditor({ ...editor, draft: { ...draft, path: e.target.value } })} placeholder="/etc/sing-box/my-rules.srs" />
                            </div>
                        )}
                        {editorError && <p className="text-xs text-red-400">{editorError}</p>}
                    </div>
                </Modal>
            )}
        </div>
    )
}
