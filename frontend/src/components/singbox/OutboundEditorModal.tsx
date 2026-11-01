import { useEffect, useState } from 'react'
import { api } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { Modal } from '../ui/Modal'
import { Button } from '../ui/Button'
import { ChipInput } from '../ui/ChipInput'
import {
    emptyOutboundDraft,
    parseOutbound,
    serializeOutbound,
    uniqueTag,
    validateOutbound,
    type OutboundDraft,
    type OutboundKind,
} from './outboundModel'

const inputClass = 'w-full rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white outline-none transition-colors focus:border-blue-500/50'

const KIND_LABELS: Record<OutboundKind, string> = {
    link: 'From a share link',
    selector: 'Group: manual choice',
    urltest: 'Group: fastest (URL test)',
    socks: 'SOCKS proxy',
    http: 'HTTP proxy',
}

interface Props {
    // Tag of the outbound to edit, or null to add a new one.
    editTag: string | null
    onClose: () => void
    onSaved: () => void
}

export function OutboundEditorModal({ editTag, onClose, onSaved }: Props) {
    const { success, error } = useToast()
    const [draft, setDraft] = useState<OutboundDraft>(emptyOutboundDraft())
    const [tags, setTags] = useState<string[]>([])
    const [link, setLink] = useState('')
    const [converting, setConverting] = useState(false)
    const [saving, setSaving] = useState(false)
    const [problem, setProblem] = useState('')
    const [ready, setReady] = useState(false)

    useEffect(() => {
        let cancelled = false
        api.getSingboxConfig().then(raw => {
            if (cancelled) return
            const parsed = JSON.parse(raw)
            const outbounds: unknown[] = Array.isArray(parsed?.outbounds) ? parsed.outbounds : []
            setTags(outbounds.map(entry => (entry as { tag?: unknown })?.tag).filter((tag): tag is string => typeof tag === 'string'))
            if (editTag !== null) {
                const entry = outbounds.find(item => (item as { tag?: unknown })?.tag === editTag)
                const existing = parseOutbound(entry)
                if (!existing) {
                    error('This outbound can only be edited in the raw config.')
                    onClose()
                    return
                }
                setDraft(existing)
            }
            setReady(true)
        }).catch(() => {
            error('Failed to load the config')
            onClose()
        })
        return () => { cancelled = true }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    const otherTags = tags.filter(tag => tag !== editTag)
    const patch = (next: Partial<OutboundDraft>) => { setDraft(prev => ({ ...prev, ...next })); setProblem('') }

    const convert = async () => {
        setConverting(true)
        try {
            const converted = await api.outboundFromLink(link)
            const suggested = typeof converted.tag === 'string' ? converted.tag : ''
            patch({ converted, tag: draft.tag || uniqueTag(suggested.trim().replace(/\s+/g, '-'), tags) })
        } catch (err) {
            patch({ converted: null })
            setProblem(err instanceof Error ? err.message : 'Could not read that link')
        } finally {
            setConverting(false)
        }
    }

    const save = async () => {
        const message = validateOutbound(draft, otherTags)
        if (message) {
            setProblem(message)
            return
        }
        setSaving(true)
        try {
            const parsed = JSON.parse(await api.getSingboxConfig())
            const outbounds: unknown[] = Array.isArray(parsed.outbounds) ? parsed.outbounds : []
            const entry = serializeOutbound(draft)
            if (editTag === null) {
                outbounds.push(entry)
            } else {
                const index = outbounds.findIndex(item => (item as { tag?: unknown })?.tag === editTag)
                if (index < 0) throw new Error('The outbound no longer exists')
                outbounds[index] = entry
                // Renaming must follow every reference to the old tag.
                if (entry.tag !== editTag) renameReferences(parsed, editTag, String(entry.tag))
            }
            parsed.outbounds = outbounds
            await api.updateSingboxConfig(JSON.stringify(parsed, null, 2))
            success(editTag === null ? 'Outbound added' : 'Outbound saved')
            onSaved()
            onClose()
        } catch (err) {
            setProblem(err instanceof Error ? err.message : 'Failed to save the outbound')
        } finally {
            setSaving(false)
        }
    }

    const isGroup = draft.kind === 'selector' || draft.kind === 'urltest'

    return (
        <Modal
            isOpen
            onClose={onClose}
            title={editTag === null ? 'Add outbound' : `Edit outbound: ${editTag}`}
            footer={
                <>
                    <Button variant="ghost" onClick={onClose}>Cancel</Button>
                    <Button variant="primary" onClick={save} disabled={!ready || saving} isLoading={saving}>{editTag === null ? 'Add' : 'Save'}</Button>
                </>
            }
        >
            {!ready ? <p className="py-6 text-center text-sm text-slate-400">Loading…</p> : (
                <div className="space-y-4">
                    {editTag === null && (
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Type</label>
                            <select className={`select-field ${inputClass}`} value={draft.kind} onChange={event => { setLink(''); setDraft({ ...emptyOutboundDraft(event.target.value as OutboundKind), tag: '' }); setProblem('') }}>
                                {(Object.keys(KIND_LABELS) as OutboundKind[]).map(kind => <option key={kind} value={kind}>{KIND_LABELS[kind]}</option>)}
                            </select>
                        </div>
                    )}

                    {draft.kind === 'link' && (
                        <div className="space-y-2">
                            <label className="text-xs font-medium text-slate-400">Share link</label>
                            <textarea
                                className={`${inputClass} min-h-[88px] resize-y font-mono text-xs`}
                                value={link}
                                onChange={event => setLink(event.target.value)}
                                placeholder="vless://… vmess://… trojan://… hysteria2://… ss://… anytls://…"
                            />
                            <Button variant="secondary" size="sm" onClick={convert} disabled={!link.trim() || converting} isLoading={converting}>Read link</Button>
                            {draft.converted && (
                                <p className="text-xs text-emerald-400">
                                    {String(draft.converted.type)} → {String(draft.converted.server)}:{String(draft.converted.server_port)}
                                </p>
                            )}
                        </div>
                    )}

                    <div className="space-y-1">
                        <label className="text-xs font-medium text-slate-400">Tag</label>
                        <input className={inputClass} value={draft.tag} onChange={event => patch({ tag: event.target.value })} placeholder="proxy-eu" />
                    </div>

                    {isGroup && (
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Outbounds in the group</label>
                            <ChipInput
                                value={draft.members}
                                suggestions={otherTags}
                                placeholder="Pick or type a tag"
                                onChange={members => patch({ members })}
                                ariaLabel="Group outbounds"
                            />
                        </div>
                    )}

                    {draft.kind === 'selector' && (
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-400">Default (optional)</label>
                            <select className={`select-field ${inputClass}`} value={draft.defaultMember} onChange={event => patch({ defaultMember: event.target.value })}>
                                <option value="">First one</option>
                                {draft.members.map(tag => <option key={tag} value={tag}>{tag}</option>)}
                            </select>
                        </div>
                    )}

                    {draft.kind === 'urltest' && (
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                            <div className="space-y-1 sm:col-span-3">
                                <label className="text-xs font-medium text-slate-400">Test URL</label>
                                <input className={inputClass} value={draft.testUrl} onChange={event => patch({ testUrl: event.target.value })} />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Interval</label>
                                <input className={inputClass} value={draft.interval} onChange={event => patch({ interval: event.target.value })} placeholder="3m" />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Tolerance (ms)</label>
                                <input className={inputClass} inputMode="numeric" value={draft.tolerance} onChange={event => patch({ tolerance: event.target.value })} placeholder="50" />
                            </div>
                        </div>
                    )}

                    {(draft.kind === 'socks' || draft.kind === 'http') && (
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Server</label>
                                <input className={inputClass} value={draft.server} onChange={event => patch({ server: event.target.value })} placeholder="proxy.example.com" />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Port</label>
                                <input className={inputClass} inputMode="numeric" value={draft.port} onChange={event => patch({ port: event.target.value })} placeholder="1080" />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Username (optional)</label>
                                <input className={inputClass} autoComplete="off" value={draft.username} onChange={event => patch({ username: event.target.value })} />
                            </div>
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-400">Password (optional)</label>
                                <input className={inputClass} type="password" autoComplete="new-password" value={draft.password} onChange={event => patch({ password: event.target.value })} />
                            </div>
                        </div>
                    )}

                    {problem && <p className="text-xs text-amber-400">{problem}</p>}
                </div>
            )}
        </Modal>
    )
}

// renameReferences points every reference to oldTag at newTag.
function renameReferences(config: Record<string, any>, oldTag: string, newTag: string) {
    for (const outbound of Array.isArray(config.outbounds) ? config.outbounds : []) {
        if (Array.isArray(outbound?.outbounds)) outbound.outbounds = outbound.outbounds.map((tag: string) => (tag === oldTag ? newTag : tag))
        if (outbound?.default === oldTag) outbound.default = newTag
        if (outbound?.detour === oldTag) outbound.detour = newTag
    }
    const route = config.route
    if (!route || typeof route !== 'object') return
    if (route.final === oldTag) route.final = newTag
    for (const rule of Array.isArray(route.rules) ? route.rules : []) {
        if (rule?.outbound === oldTag) rule.outbound = newTag
    }
    for (const set of Array.isArray(route.rule_set) ? route.rule_set : []) {
        if (set?.download_detour === oldTag) set.download_detour = newTag
    }
}
