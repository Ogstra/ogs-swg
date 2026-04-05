import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, Subscription } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { useAuth } from '../../context/AuthContext'
import { Button } from '../../components/ui/Button'
import { Modal } from '../../components/ui/Modal'
import { ConfirmModal } from '../../components/ui/ConfirmModal'
import { ActionIconButton } from '../../components/ui/ActionIconButton'
import { Link as LinkIcon, Plus, Copy, Trash2, Edit, RefreshCw, QrCode as QrCodeIcon, Settings2 } from 'lucide-react'
import { QrLinkModal } from '../../components/ui/QrLinkModal'
import { formatTimeAgo } from '../../utils/traffic'

const formatBytes = (bytes: number): string => {
    if (!bytes || bytes === 0) return '0'
    if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + ' MB'
    return (bytes / 1024 ** 3).toFixed(2) + ' GB'
}

const parseGBInput = (value: string): number => {
    const n = parseFloat(value)
    return isNaN(n) ? 0 : Math.round(n * 1024 ** 3)
}

const toBase64 = (value: string): string => btoa(value)
const SUBSCRIPTION_DEFAULTS_STORAGE_KEY = 'subscription_create_defaults'
const DEFAULT_REFRESH_INTERVAL_HOURS = '24'

type RefreshPolicyDraft = {
    intervalEnabled: boolean
    intervalHours: string
    updateAlways: boolean
}

const defaultRefreshPolicyDraft = (): RefreshPolicyDraft => ({
    intervalEnabled: false,
    intervalHours: DEFAULT_REFRESH_INTERVAL_HOURS,
    updateAlways: false,
})

const parseIntervalHours = (value: string): number | null => {
    const trimmed = value.trim()
    if (!trimmed) return null

    const parsed = Number.parseInt(trimmed, 10)
    if (!Number.isFinite(parsed) || parsed <= 0) return null
    return parsed
}

const loadSubscriptionDefaults = (): RefreshPolicyDraft => {
    if (typeof window === 'undefined') return defaultRefreshPolicyDraft()

    try {
        const raw = window.localStorage.getItem(SUBSCRIPTION_DEFAULTS_STORAGE_KEY)
        if (!raw) return defaultRefreshPolicyDraft()

        const parsed = JSON.parse(raw) as { profile_update_interval_hours?: unknown; update_always?: unknown }
        const hasInterval = typeof parsed.profile_update_interval_hours === 'number' && parsed.profile_update_interval_hours > 0

        return {
            intervalEnabled: hasInterval,
            intervalHours: hasInterval ? String(Math.trunc(parsed.profile_update_interval_hours as number)) : DEFAULT_REFRESH_INTERVAL_HOURS,
            updateAlways: parsed.update_always === true,
        }
    } catch {
        return defaultRefreshPolicyDraft()
    }
}

export default function Subscriptions() {
    const { success, error: toastError } = useToast()
    const { permissions } = useAuth()
    const canWriteUsers = !!permissions?.can_write_users

    const [modalState, setModalState] = useState<{ type: 'create' | 'edit' | 'qr' | null, data?: Subscription }>({ type: null })
    const [confirmDelete, setConfirmDelete] = useState<Subscription | null>(null)
    const [confirmRegenerate, setConfirmRegenerate] = useState<Subscription | null>(null)
    const [defaultsModalOpen, setDefaultsModalOpen] = useState(false)

    const [nameInput, setNameInput] = useState('')
    const [quotaGB, setQuotaGB] = useState('0')
    const [selectedUsers, setSelectedUsers] = useState<string[]>([])
    const [profileUpdateIntervalEnabled, setProfileUpdateIntervalEnabled] = useState(false)
    const [profileUpdateIntervalHours, setProfileUpdateIntervalHours] = useState(DEFAULT_REFRESH_INTERVAL_HOURS)
    const [updateAlways, setUpdateAlways] = useState(false)
    const [subscriptionDefaults, setSubscriptionDefaults] = useState<RefreshPolicyDraft>(() => loadSubscriptionDefaults())
    const [defaultIntervalEnabled, setDefaultIntervalEnabled] = useState(false)
    const [defaultIntervalHours, setDefaultIntervalHours] = useState(DEFAULT_REFRESH_INTERVAL_HOURS)
    const [defaultUpdateAlways, setDefaultUpdateAlways] = useState(false)

    const subsQuery = useQuery({ queryKey: ['subscriptions'], queryFn: () => api.getSubscriptions() })
    const usersQuery = useQuery({ queryKey: ['users'], queryFn: () => api.getUsers() })
    const domainQuery = useQuery({
        queryKey: ['settings-subscription-domain'],
        queryFn: () => api.getSubscriptionDomain(),
        enabled: canWriteUsers,
    })

    const subs = subsQuery.data || []
    const usersInfo = usersQuery.data || []
    const subDomain = domainQuery.data || window.location.host

    const applyRefreshPolicyDraft = (draft: RefreshPolicyDraft) => {
        setProfileUpdateIntervalEnabled(draft.intervalEnabled)
        setProfileUpdateIntervalHours(draft.intervalHours)
        setUpdateAlways(draft.updateAlways)
    }

    const openCreate = () => {
        setNameInput('')
        setQuotaGB('0')
        setSelectedUsers([])
        applyRefreshPolicyDraft(subscriptionDefaults)
        setModalState({ type: 'create' })
    }

    const openEdit = (sub: Subscription) => {
        setNameInput(sub.name)
        setQuotaGB(sub.quota_limit ? (sub.quota_limit / 1024 ** 3).toFixed(2) : '0')
        setSelectedUsers(sub.users || [])
        setProfileUpdateIntervalEnabled(sub.profile_update_interval_hours != null)
        setProfileUpdateIntervalHours(
            sub.profile_update_interval_hours != null
                ? String(sub.profile_update_interval_hours)
                : subscriptionDefaults.intervalHours
        )
        setUpdateAlways(sub.update_always === true)
        setModalState({ type: 'edit', data: sub })
    }

    const openDefaults = () => {
        setDefaultIntervalEnabled(subscriptionDefaults.intervalEnabled)
        setDefaultIntervalHours(subscriptionDefaults.intervalHours)
        setDefaultUpdateAlways(subscriptionDefaults.updateAlways)
        setDefaultsModalOpen(true)
    }

    const handleSave = async () => {
        if (!nameInput.trim()) return toastError('Name is required')
        const quotaLimit = parseGBInput(quotaGB)
        const intervalHours = profileUpdateIntervalEnabled ? parseIntervalHours(profileUpdateIntervalHours) : null

        if (profileUpdateIntervalEnabled && intervalHours == null) {
            return toastError('Refresh interval must be a whole number greater than zero')
        }

        const payload = {
            name: nameInput.trim(),
            quota_limit: quotaLimit,
            quota_period: 'monthly' as const,
            users: selectedUsers,
            profile_update_interval_hours: intervalHours,
            update_always: updateAlways,
        }

        try {
            if (modalState.type === 'create') {
                await api.createSubscription(payload)
                success('Subscription created')
            } else if (modalState.type === 'edit' && modalState.data) {
                await api.updateSubscription(modalState.data.id, payload)
                success('Subscription updated')
            }
            setModalState({ type: null })
            await subsQuery.refetch()
        } catch (err) {
            toastError('Failed to save subscription: ' + err)
        }
    }

    const handleSaveDefaults = () => {
        const intervalHours = defaultIntervalEnabled ? parseIntervalHours(defaultIntervalHours) : null
        if (defaultIntervalEnabled && intervalHours == null) {
            return toastError('Default refresh interval must be a whole number greater than zero')
        }

        const nextDefaults: RefreshPolicyDraft = {
            intervalEnabled: defaultIntervalEnabled,
            intervalHours: defaultIntervalEnabled ? String(intervalHours) : DEFAULT_REFRESH_INTERVAL_HOURS,
            updateAlways: defaultUpdateAlways,
        }

        window.localStorage.setItem(
            SUBSCRIPTION_DEFAULTS_STORAGE_KEY,
            JSON.stringify({
                profile_update_interval_hours: intervalHours,
                update_always: defaultUpdateAlways,
            })
        )

        setSubscriptionDefaults(nextDefaults)
        setDefaultsModalOpen(false)
        success('Subscription defaults updated')
    }

    const handleDelete = async () => {
        if (!confirmDelete) return
        try {
            await api.deleteSubscription(confirmDelete.id)
            success('Subscription deleted')
            setConfirmDelete(null)
            await subsQuery.refetch()
        } catch (err) {
            toastError('Failed to delete subscription: ' + err)
        }
    }

    const handleRegenerate = async () => {
        if (!confirmRegenerate) return
        try {
            await api.regenerateSubscriptionToken(confirmRegenerate.id)
            success('Token regenerated')
            setConfirmRegenerate(null)
            await subsQuery.refetch()
        } catch (err) {
            toastError('Failed to regenerate token: ' + err)
        }
    }

    const copyLink = async (token: string) => {
        const protocol = window.location.protocol
        const link = `${protocol}//${subDomain}/s/${token}`
        try {
            if (navigator.clipboard?.writeText) {
                await navigator.clipboard.writeText(link)
            } else {
                const textarea = document.createElement('textarea')
                textarea.value = link
                textarea.setAttribute('readonly', '')
                textarea.style.position = 'absolute'
                textarea.style.left = '-9999px'
                document.body.appendChild(textarea)
                textarea.select()
                document.execCommand('copy')
                document.body.removeChild(textarea)
            }
            success('Link copied to clipboard')
        } catch {
            toastError('Failed to copy link')
        }
    }

    const openQr = (sub: Subscription) => {
        if (!canWriteUsers || !sub.token) return
        setModalState({ type: 'qr', data: sub })
    }
    const toggleUser = (userName: string) => {
        setSelectedUsers(prev => prev.includes(userName) ? prev.filter(u => u !== userName) : [...prev, userName])
    }

    const getQuotaPill = (sub: Subscription) => {
        const used = sub.used_bytes || 0
        const limit = sub.quota_limit || 0
        if (limit === 0) {
            // No sub-level quota — show usage as informational
            return <span className="text-slate-400 text-xs">{formatBytes(used)} used</span>
        }
        const pct = Math.min(100, Math.round((used / limit) * 100))
        const over = used >= limit
        const barColor = over ? 'bg-red-500' : pct > 80 ? 'bg-yellow-500' : 'bg-emerald-500'
        return (
            <div className="flex flex-col gap-1 min-w-[120px]">
                <div className="flex justify-between text-xs gap-2">
                    <span className={over ? 'text-red-400 font-semibold' : 'text-slate-300'}>
                        {formatBytes(used)}
                    </span>
                    <span className="text-slate-500">/ {formatBytes(limit)}</span>
                </div>
                <div className="h-1.5 rounded-full bg-slate-800">
                    <div className={`h-full rounded-full ${barColor} transition-all`} style={{ width: `${pct}%` }} />
                </div>
            </div>
        )
    }

    const getLastRequestMeta = (sub: Subscription) => {
        const lastRequestAt = sub.last_request_at || 0
        if (!lastRequestAt) {
            return {
                dotClass: 'bg-slate-700',
                textClass: 'text-slate-500',
                text: 'Never',
                isRecent: false,
            }
        }

        const diff = Math.floor(Date.now() / 1000) - lastRequestAt
        const isRecent = diff < 300
        return {
            dotClass: isRecent ? 'bg-emerald-500' : 'bg-slate-700',
            textClass: isRecent ? 'text-emerald-400' : 'text-slate-500',
            text: formatTimeAgo(lastRequestAt),
            isRecent,
        }
    }

    const subLink = (token: string) => `${window.location.protocol}//${subDomain}/s/${token}`
    const buildShadowrocketLink = (token: string, name: string) => `sub://${toBase64(subLink(token))}#${encodeURIComponent(name)}`
    // Product rules currently support only Direct and Shadowrocket in this modal.
    const getSubscriptionLinkVariants = (sub: Subscription) => (
        sub.token
            ? [
                { id: 'direct', label: 'Direct', link: subLink(sub.token) },
                { id: 'shadowrocket', label: 'Shadowrocket', link: buildShadowrocketLink(sub.token, sub.name) },
            ]
            : []
    )

    const renderRefreshPolicyFields = (
        intervalEnabled: boolean,
        setIntervalEnabled: (value: boolean) => void,
        intervalHours: string,
        setIntervalHours: (value: string) => void,
        updateAlwaysValue: boolean,
        setUpdateAlwaysValue: (value: boolean) => void,
        helperText?: string
    ) => (
        <div className="space-y-4 rounded-xl border border-slate-800 bg-slate-950/70 p-4">
            <div>
                <h3 className="text-sm font-semibold text-white">Refresh Policy</h3>
                {helperText && <p className="mt-1 text-xs text-slate-400">{helperText}</p>}
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-end">
                <label className="flex items-start gap-3 cursor-pointer min-h-[42px]">
                    <input
                        type="checkbox"
                        checked={intervalEnabled}
                        onChange={e => setIntervalEnabled(e.target.checked)}
                        className="mt-1 shrink-0"
                    />
                    <div className="space-y-1">
                        <div className="text-sm font-medium text-slate-200">Emit profile-update-interval</div>
                    </div>
                </label>

                <div>
                    <label className="block text-sm font-medium text-slate-300 mb-1">Refresh Interval (hours)</label>
                    <input
                        type="number"
                        min="1"
                        step="1"
                        value={intervalHours}
                        onChange={e => setIntervalHours(e.target.value)}
                        disabled={!intervalEnabled}
                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-white focus:outline-none focus:border-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                        placeholder="24"
                    />
                </div>
            </div>

            <label className="flex items-start gap-3 cursor-pointer">
                <input
                    type="checkbox"
                    checked={updateAlwaysValue}
                    onChange={e => setUpdateAlwaysValue(e.target.checked)}
                    className="mt-1 shrink-0"
                />
                <div className="space-y-1">
                    <div className="text-sm font-medium text-slate-200">update-always</div>
                </div>
            </label>
        </div>
    )

    return (
        <div className="space-y-4 sm:space-y-6 pb-4 sm:pb-0">
            <div className="flex flex-col sm:flex-row sm:items-center justify-end gap-4">
                <div className="flex items-center justify-end gap-2">
                    <ActionIconButton
                        onClick={openDefaults}
                        title="Subscription Defaults"
                        className="h-9 w-9"
                        disabled={!canWriteUsers}
                    >
                        <Settings2 size={16} />
                    </ActionIconButton>
                <Button onClick={openCreate} icon={<Plus size={16} />} variant="primary" disabled={!canWriteUsers}>
                    Create Subscription
                </Button>
                </div>
            </div>

            {/* Desktop table */}
            <div className="hidden sm:block bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm min-h-[220px]">
                <div className="overflow-x-auto">
                    <table className="w-full text-left border-collapse">
                        <thead>
                            <tr className="bg-slate-950/50 border-b border-slate-800 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="p-4 font-semibold">Name</th>
                                <th className="p-4 font-semibold">Last Request</th>
                                <th className="p-4 font-semibold">Users</th>
                                <th className="p-4 font-semibold">Quota</th>
                                <th className="p-4 font-semibold text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {subs.length === 0 ? (
                                <tr>
                                    <td colSpan={5} className="p-12 text-center text-slate-500">
                                        <LinkIcon size={48} className="mx-auto mb-4 opacity-20" />
                                        <p>No subscriptions found.</p>
                                    </td>
                                </tr>
                            ) : subs.map(sub => (
                                <tr key={sub.id} className="border-b last:border-0 border-slate-800/50 hover:bg-slate-800/20 transition-colors">
                                    <td className="p-4 text-white font-medium">{sub.name}</td>
                                    <td className="p-4">
                                        {(() => {
                                            const lastRequest = getLastRequestMeta(sub)
                                            return (
                                                <div className="flex items-center gap-2">
                                                    <div className={`w-2 h-2 rounded-full ${lastRequest.dotClass} ${lastRequest.isRecent ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                                    <span className={`text-xs ${lastRequest.textClass}`}>{lastRequest.text}</span>
                                                </div>
                                            )
                                        })()}
                                    </td>
                                    <td className="p-4 text-slate-400">{sub.users?.length || 0} users</td>
                                    <td className="p-4">{getQuotaPill(sub)}</td>
                                    <td className="p-4">
                                        <div className="flex items-center justify-end gap-2">
                                            {canWriteUsers && (
                                                <>
                                                    {sub.token && (
                                                        <>
                                                            <ActionIconButton onClick={() => openQr(sub)} title="Show QR Code" className="text-emerald-400 hover:text-emerald-300 hover:bg-emerald-500/10"><QrCodeIcon size={16} /></ActionIconButton>
                                                            <ActionIconButton onClick={() => copyLink(sub.token!)} title="Copy Link" className="text-blue-400 hover:text-blue-300 hover:bg-blue-500/10"><Copy size={16} /></ActionIconButton>
                                                        </>
                                                    )}
                                                    <ActionIconButton onClick={() => openEdit(sub)} title="Edit"><Edit size={16} /></ActionIconButton>
                                                    <ActionIconButton onClick={() => setConfirmRegenerate(sub)} title="Regenerate Token" className="text-yellow-400 hover:text-yellow-300 hover:bg-yellow-500/10"><RefreshCw size={16} /></ActionIconButton>
                                                    <ActionIconButton onClick={() => setConfirmDelete(sub)} title="Delete" className="text-red-400 hover:text-red-300 hover:bg-red-500/10"><Trash2 size={16} /></ActionIconButton>
                                                </>
                                            )}
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Mobile cards */}
            <div className="sm:hidden space-y-3">
                {subs.length === 0 ? (
                    <div className="bg-slate-900 border border-slate-800 rounded-xl p-12 text-center text-slate-500">
                        <LinkIcon size={48} className="mx-auto mb-4 opacity-20" />
                        <p>No subscriptions found.</p>
                    </div>
                ) : subs.map(sub => (
                    <div key={sub.id} className="bg-slate-900 border border-slate-800 rounded-xl">
                        {(() => {
                            const lastRequest = getLastRequestMeta(sub)
                            return (
                                <div className="p-4 space-y-4">
                                    <div className="flex items-start justify-between gap-3">
                                        <div className="min-w-0 flex-1 space-y-1">
                                            <p className="text-white font-semibold truncate">{sub.name}</p>
                                            <div className="flex items-center gap-2">
                                                <div className={`w-2 h-2 rounded-full ${lastRequest.dotClass} ${lastRequest.isRecent ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                                <span className={`text-xs ${lastRequest.textClass}`}>
                                                    {lastRequest.text}
                                                </span>
                                            </div>
                                        </div>
                                        <div className="flex gap-2">
                                            {canWriteUsers && (
                                                <>
                                                    {sub.token && (
                                                        <ActionIconButton onClick={() => openQr(sub)} title="QR Code">
                                                            <QrCodeIcon size={16} />
                                                        </ActionIconButton>
                                                    )}
                                                    <ActionIconButton onClick={() => openEdit(sub)} title="Edit" tone="primary">
                                                        <Edit size={16} />
                                                    </ActionIconButton>
                                                    <ActionIconButton onClick={() => setConfirmRegenerate(sub)} title="Regenerate Token" className="text-yellow-400 hover:text-yellow-300 hover:bg-yellow-500/10">
                                                        <RefreshCw size={16} />
                                                    </ActionIconButton>
                                                    <ActionIconButton onClick={() => setConfirmDelete(sub)} title="Delete" tone="danger">
                                                        <Trash2 size={16} />
                                                    </ActionIconButton>
                                                </>
                                            )}
                                        </div>
                                    </div>

                                    <div className="text-xs">
                                        <div className="text-slate-400">
                                            {sub.users?.length || 0} users
                                        </div>
                                    </div>

                                    <div className="bg-slate-950/50 rounded-lg p-3">
                                        {getQuotaPill(sub)}
                                    </div>
                                </div>
                            )
                        })()}
                    </div>
                ))}
            </div>

            {/* Create / Edit Modal */}
            <Modal
                title={modalState.type === 'create' ? 'Create Subscription' : 'Edit Subscription'}
                isOpen={modalState.type === 'create' || modalState.type === 'edit'}
                onClose={() => setModalState({ type: null })}
                footer={
                    <div className="flex gap-3 justify-end w-full">
                        <Button variant="secondary" onClick={() => setModalState({ type: null })}>Cancel</Button>
                        <Button variant="primary" onClick={handleSave}>Save</Button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                        <div className="md:col-span-2">
                            <label className="block text-sm font-medium text-slate-300 mb-1">Name</label>
                            <input type="text" value={nameInput} onChange={e => setNameInput(e.target.value)} className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-white focus:outline-none focus:border-blue-500" placeholder="" />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-300 mb-1">
                                Quota (GB)
                            </label>
                            <input
                                type="number"
                                min="0"
                                step="0.5"
                                value={quotaGB}
                                onChange={e => setQuotaGB(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-white focus:outline-none focus:border-blue-500"
                                placeholder="0 = unlimited"
                            />
                        </div>
                    </div>
                    {renderRefreshPolicyFields(
                        profileUpdateIntervalEnabled,
                        setProfileUpdateIntervalEnabled,
                        profileUpdateIntervalHours,
                        setProfileUpdateIntervalHours,
                        updateAlways,
                        setUpdateAlways
                    )}
                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">Select Users</label>
                        <div className="border border-slate-800 rounded bg-slate-950 max-h-[300px] overflow-y-auto">
                            {usersInfo.map(u => (
                                <label key={u.name} className="flex items-center p-3 hover:bg-slate-900 cursor-pointer border-b border-slate-800 last:border-0">
                                    <input type="checkbox" checked={selectedUsers.includes(u.name)} onChange={() => toggleUser(u.name)} className="mr-3 shrink-0" />
                                    <div className="flex-1 truncate text-sm text-slate-200">{u.name}</div>
                                </label>
                            ))}
                            {usersInfo.length === 0 && <div className="p-4 text-center text-slate-500 text-sm">No users available</div>}
                        </div>
                    </div>
                </div>
            </Modal>

            <Modal
                title="Subscription Defaults"
                isOpen={defaultsModalOpen}
                onClose={() => setDefaultsModalOpen(false)}
                footer={
                    <div className="flex gap-3 justify-end w-full">
                        <Button variant="secondary" onClick={() => setDefaultsModalOpen(false)}>Cancel</Button>
                        <Button variant="primary" onClick={handleSaveDefaults}>Save Defaults</Button>
                    </div>
                }
            >
                <div className="space-y-4">
                    {renderRefreshPolicyFields(
                        defaultIntervalEnabled,
                        setDefaultIntervalEnabled,
                        defaultIntervalHours,
                        setDefaultIntervalHours,
                        defaultUpdateAlways,
                        setDefaultUpdateAlways
                    )}
                </div>
            </Modal>

            {/* QR Modal */}
            <QrLinkModal
                isOpen={modalState.type === 'qr'}
                onClose={() => setModalState({ type: null })}
                title={`${modalState.data?.name || ''}`}
                link={modalState.data?.token ? subLink(modalState.data.token) : ''}
                linkVariants={modalState.data ? getSubscriptionLinkVariants(modalState.data) : undefined}
            />

            <ConfirmModal
                isOpen={!!confirmDelete}
                title="Delete Subscription"
                message={`Are you sure you want to delete "${confirmDelete?.name}"? Its users will not be deleted.`}
                confirmLabel="Delete"
                onConfirm={handleDelete}
                onClose={() => setConfirmDelete(null)}
                confirmTone="danger"
            />

            <ConfirmModal
                isOpen={!!confirmRegenerate}
                title="Regenerate Token"
                message={`Are you sure you want to regenerate the token for "${confirmRegenerate?.name}"? The old link will break.`}
                confirmLabel="Regenerate"
                onConfirm={handleRegenerate}
                onClose={() => setConfirmRegenerate(null)}
                confirmTone="danger"
            />
        </div>
    )
}
