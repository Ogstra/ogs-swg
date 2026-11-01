import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Edit, Trash2 } from 'lucide-react'
import { api, type UserTemplate, type UserTemplateInput } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { Modal } from '../../components/ui/Modal'
import { Button } from '../../components/ui/Button'
import { ActionIconButton } from '../../components/ui/ActionIconButton'

const BYTES_PER_GB = 1024 * 1024 * 1024
const DEFAULT_FLOW = 'xtls-rprx-vision'

const toGb = (bytes: number) => (bytes > 0 ? String(Number((bytes / BYTES_PER_GB).toFixed(2))) : '')
const toBytes = (gb: string) => {
    const value = parseFloat(gb.replace(',', '.'))
    return Number.isFinite(value) && value > 0 ? Math.round(value * BYTES_PER_GB) : 0
}

const emptyDraft = (): UserTemplateInput => ({ name: '', inbound_tag: '', flow: '', quota_limit: 0, quota_period: 'monthly', route_tag_ids: [] })

const inputClass = 'w-full rounded-lg border border-slate-800 bg-slate-950 p-2.5 text-white outline-none transition-colors focus:border-blue-500/50'

export function describeTemplate(template: UserTemplateInput): string {
    const quota = template.quota_limit > 0
        ? `${toGb(template.quota_limit)} GB ${template.quota_period === 'total' ? 'total' : 'per month'}`
        : 'No quota'
    const tagCount = template.route_tag_ids?.length ?? 0
    return [template.inbound_tag || 'No inbound', quota, ...(tagCount > 0 ? [`${tagCount} tag${tagCount > 1 ? 's' : ''}`] : [])].join(' · ')
}

interface Props {
    inbounds: Array<{ tag: string; type?: string }>
    canWrite: boolean
    onClose: () => void
}

export function UserTemplatesModal({ inbounds, canWrite, onClose }: Props) {
    const { success, error } = useToast()
    const queryClient = useQueryClient()
    const templatesQuery = useQuery({ queryKey: ['user-templates'], queryFn: () => api.getUserTemplates() })
    const routeTagsQuery = useQuery({ queryKey: ['user-route-tags'], queryFn: () => api.getUserRouteTags() })
    const routeTags = routeTagsQuery.data || []
    const [editingId, setEditingId] = useState<number | null>(null)
    const [draft, setDraft] = useState<UserTemplateInput>(emptyDraft())
    const [quotaInput, setQuotaInput] = useState('')

    const inboundType = (tag: string) => String(inbounds.find(inb => inb.tag === tag)?.type || '').toLowerCase()
    const reset = () => {
        setEditingId(null)
        setDraft(emptyDraft())
        setQuotaInput('')
    }

    const save = useMutation({
        mutationFn: () => (editingId === null ? api.createUserTemplate(draft) : api.updateUserTemplate(editingId, draft)),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ['user-templates'] })
            success(editingId === null ? 'Template created' : 'Template saved')
            reset()
        },
        onError: err => error(err instanceof Error ? err.message : 'Failed to save template'),
    })

    const remove = useMutation({
        mutationFn: (id: number) => api.deleteUserTemplate(id),
        onSuccess: async (_data, id) => {
            await queryClient.invalidateQueries({ queryKey: ['user-templates'] })
            if (id === editingId) reset()
            success('Template deleted')
        },
        onError: err => error(err instanceof Error ? err.message : 'Failed to delete template'),
    })

    const startEdit = (template: UserTemplate) => {
        setEditingId(template.id)
        setDraft({ name: template.name, inbound_tag: template.inbound_tag, flow: template.flow, quota_limit: template.quota_limit, quota_period: template.quota_period, route_tag_ids: template.route_tag_ids || [] })
        setQuotaInput(toGb(template.quota_limit))
    }

    const templates = templatesQuery.data || []
    const showFlow = inboundType(draft.inbound_tag) === 'vless'

    return (
        <Modal isOpen onClose={onClose} title="User templates" size="lg" footer={<Button variant="secondary" onClick={onClose}>Close</Button>}>
            <div className="space-y-5">
                <div className="space-y-3 rounded-lg border border-slate-800 bg-slate-950/60 p-4">
                    <h3 className="text-sm font-semibold text-white">{editingId === null ? 'New template' : 'Edit template'}</h3>
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        <div>
                            <label className="mb-1 block text-sm font-medium text-slate-400">Name</label>
                            <input className={inputClass} value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} placeholder="e.g. Basic 50 GB" />
                        </div>
                        <div>
                            <label className="mb-1 block text-sm font-medium text-slate-400">Inbound</label>
                            <select
                                className={`select-field ${inputClass}`}
                                value={draft.inbound_tag}
                                onChange={e => {
                                    const tag = e.target.value
                                    setDraft({ ...draft, inbound_tag: tag, flow: inboundType(tag) === 'vless' ? (draft.flow || DEFAULT_FLOW) : '' })
                                }}
                            >
                                <option value="">None</option>
                                {inbounds.map(inb => <option key={inb.tag} value={inb.tag}>{inb.tag} ({inb.type})</option>)}
                            </select>
                        </div>
                        {showFlow && (
                            <div>
                                <label className="mb-1 block text-sm font-medium text-slate-400">Flow</label>
                                <select className={`select-field ${inputClass}`} value={draft.flow} onChange={e => setDraft({ ...draft, flow: e.target.value })}>
                                    <option value={DEFAULT_FLOW}>{DEFAULT_FLOW}</option>
                                    <option value="">none</option>
                                </select>
                            </div>
                        )}
                        <div>
                            <label className="mb-1 block text-sm font-medium text-slate-400">Quota (GB)</label>
                            <input
                                className={inputClass}
                                inputMode="decimal"
                                value={quotaInput}
                                onChange={e => {
                                    setQuotaInput(e.target.value)
                                    setDraft({ ...draft, quota_limit: toBytes(e.target.value) })
                                }}
                                placeholder="0 for unlimited"
                            />
                        </div>
                        <div>
                            <label className="mb-1 block text-sm font-medium text-slate-400">Quota period</label>
                            <select className={`select-field ${inputClass}`} value={draft.quota_period} onChange={e => setDraft({ ...draft, quota_period: e.target.value })}>
                                <option value="monthly">Monthly</option>
                                <option value="total">Total (One-time)</option>
                            </select>
                        </div>
                    </div>
                    {routeTags.length > 0 && (
                        <div className="space-y-1">
                            <label className="text-sm font-medium text-slate-400">Route tags</label>
                            <div className="flex flex-wrap gap-2">
                                {routeTags.map(tag => {
                                    const selected = draft.route_tag_ids.includes(tag.id)
                                    return (
                                        <label
                                            key={tag.id}
                                            className={`flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-1.5 text-sm transition-colors ${selected ? 'border-blue-500/60 bg-blue-500/10 text-white' : 'border-slate-800 bg-slate-950 text-slate-300 hover:border-slate-700'}`}
                                        >
                                            <input
                                                type="checkbox"
                                                className="h-4 w-4 rounded border-slate-700 bg-slate-900"
                                                checked={selected}
                                                disabled={!canWrite}
                                                onChange={event => setDraft({
                                                    ...draft,
                                                    route_tag_ids: event.target.checked
                                                        ? [...draft.route_tag_ids, tag.id]
                                                        : draft.route_tag_ids.filter(id => id !== tag.id),
                                                })}
                                            />
                                            <span className="truncate">{tag.name}</span>
                                        </label>
                                    )
                                })}
                            </div>
                        </div>
                    )}
                    <div className="flex gap-2">
                        <Button variant="primary" onClick={() => save.mutate()} disabled={!canWrite || !draft.name.trim() || save.isPending}>
                            {editingId === null ? 'Add template' : 'Save changes'}
                        </Button>
                        {editingId !== null && <Button variant="ghost" onClick={reset}>Cancel</Button>}
                    </div>
                </div>

                {templatesQuery.isPending ? (
                    <p className="py-4 text-center text-sm text-slate-400">Loading…</p>
                ) : templates.length === 0 ? (
                    <p className="py-4 text-center text-sm text-slate-400">No templates yet.</p>
                ) : (
                    <div className="divide-y divide-slate-800 rounded-lg border border-slate-800">
                        {templates.map(template => (
                            <div key={template.id} className="flex items-center justify-between gap-3 px-4 py-3">
                                <div className="min-w-0">
                                    <div className="truncate font-medium text-slate-100">{template.name}</div>
                                    <div className="truncate text-xs text-slate-400">{describeTemplate(template)}</div>
                                </div>
                                <div className="flex shrink-0 items-center gap-1.5">
                                    <ActionIconButton onClick={() => startEdit(template)} title="Edit" tone="primary" disabled={!canWrite}><Edit size={16} /></ActionIconButton>
                                    <ActionIconButton onClick={() => remove.mutate(template.id)} title="Delete" tone="danger" disabled={!canWrite || remove.isPending}><Trash2 size={16} /></ActionIconButton>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </Modal>
    )
}
