import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, Subscription, SubscriptionDefaults } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { useAuth } from '../../context/AuthContext'
import { Button } from '../../components/ui/Button'
import { Badge } from '../../components/ui/Badge'
import { Modal } from '../../components/ui/Modal'
import { ConfirmModal } from '../../components/ui/ConfirmModal'
import { ActionIconButton } from '../../components/ui/ActionIconButton'
import { Link as LinkIcon, Plus, Copy, Trash2, Edit, RefreshCw, QrCode as QrCodeIcon, Settings2, Tag, ArrowUp, ArrowDown, ArrowUpDown } from 'lucide-react'
import { QrLinkModal } from '../../components/ui/QrLinkModal'
import { formatTimeAgo } from '../../utils/traffic'
import {
    getSubscriptionProfileDrafts,
    hydrateSubscriptionProfileAliases,
    serializeSubscriptionProfileAliases,
    type SelectedSubscriptionProfile,
} from './profileAliases'

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
const DEFAULT_REFRESH_INTERVAL_HOURS = '24'
const EMPTY_SUBSCRIPTION_DEFAULTS: SubscriptionDefaults = {
    profile_update_interval_hours: null,
    update_always: false,
    destinations: [],
}

type RefreshPolicyDraft = {
    intervalEnabled: boolean
    intervalHours: string
    updateAlways: boolean
}

const parseIntervalHours = (value: string): number | null => {
    const trimmed = value.trim()
    if (!trimmed) return null

    const parsed = Number.parseInt(trimmed, 10)
    if (!Number.isFinite(parsed) || parsed <= 0) return null
    return parsed
}

const subscriptionDefaultsToRefreshPolicyDraft = (defaults: SubscriptionDefaults): RefreshPolicyDraft => ({
    intervalEnabled: defaults.profile_update_interval_hours != null,
    intervalHours: defaults.profile_update_interval_hours != null
        ? String(defaults.profile_update_interval_hours)
        : DEFAULT_REFRESH_INTERVAL_HOURS,
    updateAlways: defaults.update_always === true,
})

export default function Subscriptions() {
    const { success, error: toastError } = useToast()
    const { permissions, token } = useAuth()
    const queryClient = useQueryClient()
    const canReadUsers = !!permissions?.can_read_users
    const canWriteUsers = !!permissions?.can_write_users
    const canManagePanelScopedDefaults = canWriteUsers && !!token

    const [modalState, setModalState] = useState<{ type: 'create' | 'edit' | 'qr' | null, data?: Subscription }>({ type: null })
    const [confirmDelete, setConfirmDelete] = useState<Subscription | null>(null)
    const [confirmRegenerate, setConfirmRegenerate] = useState<Subscription | null>(null)
    const [defaultsModalOpen, setDefaultsModalOpen] = useState(false)

    const [nameInput, setNameInput] = useState('')
    const [quotaGB, setQuotaGB] = useState('0')
    const [selectedProfiles, setSelectedProfiles] = useState<SelectedSubscriptionProfile[]>([])
    const [profileUpdateIntervalEnabled, setProfileUpdateIntervalEnabled] = useState(false)
    const [profileUpdateIntervalHours, setProfileUpdateIntervalHours] = useState(DEFAULT_REFRESH_INTERVAL_HOURS)
    const [updateAlways, setUpdateAlways] = useState(false)
    const [defaultIntervalEnabled, setDefaultIntervalEnabled] = useState(false)
    const [defaultIntervalHours, setDefaultIntervalHours] = useState(DEFAULT_REFRESH_INTERVAL_HOURS)
    const [defaultUpdateAlways, setDefaultUpdateAlways] = useState(false)
    const [userSearch, setUserSearch] = useState('')
    const [expandedAliasUsers, setExpandedAliasUsers] = useState<Set<string>>(new Set())
    const [sortKey, setSortKey] = useState<'name' | 'last_request' | 'users' | 'quota'>('name')
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

    const subsQuery = useQuery({ queryKey: ['subscriptions'], queryFn: () => api.getSubscriptions(), enabled: canReadUsers })
    const usersQuery = useQuery({ queryKey: ['users'], queryFn: () => api.getUsers(), enabled: canReadUsers })
    const domainQuery = useQuery({
        queryKey: ['settings-subscription-domain'],
        queryFn: () => api.getSubscriptionDomain(),
        enabled: canWriteUsers,
    })
    const cfWorkerURLQuery = useQuery({
        queryKey: ['settings-cf-worker-url'],
        queryFn: () => api.getCFWorkerURL(),
        enabled: canWriteUsers,
    })
    const defaultsQuery = useQuery({
        queryKey: ['subscription-defaults'],
        queryFn: () => api.getSubscriptionDefaults(),
        enabled: canManagePanelScopedDefaults,
    })

    const subs = subsQuery.data || []
    const usersInfo = usersQuery.data || []
    const sortedUsersInfo = [...usersInfo].sort((a, b) => a.name.localeCompare(b.name))
    const filteredUsers = userSearch.trim()
        ? sortedUsersInfo.filter(u => {
            const q = userSearch.toLowerCase()
            const alias = selectedProfiles.find(p => p.username === u.name)?.alias ?? ''
            return u.name.toLowerCase().includes(q)
                || alias.toLowerCase().includes(q)
                || (u.inbound_tags ?? []).some(t => t.toLowerCase().includes(q))
        })
        : sortedUsersInfo
    const subDomain = domainQuery.data || window.location.host
    const cfWorkerURL = cfWorkerURLQuery.data?.trim() || ''
    const subscriptionDefaults = defaultsQuery.data || EMPTY_SUBSCRIPTION_DEFAULTS
    const sortedSubs = [...subs].sort((a, b) => {
        const dir = sortDir === 'asc' ? 1 : -1
        switch (sortKey) {
            case 'last_request':
                return ((a.last_request_at || 0) - (b.last_request_at || 0)) * dir
            case 'users':
                return ((a.users?.length || 0) - (b.users?.length || 0)) * dir
            case 'quota': {
                const aLimit = a.quota_limit || 0
                const bLimit = b.quota_limit || 0
                const aRatio = aLimit ? ((a.used_bytes || 0) / aLimit) : 0
                const bRatio = bLimit ? ((b.used_bytes || 0) / bLimit) : 0
                if (Math.abs(aRatio - bRatio) < 0.0001) {
                    return ((a.used_bytes || 0) - (b.used_bytes || 0)) * dir
                }
                return (aRatio - bRatio) * dir
            }
            case 'name':
            default:
                return a.name.localeCompare(b.name) * dir
        }
    })

    const applyRefreshPolicyDraft = (draft: RefreshPolicyDraft) => {
        setProfileUpdateIntervalEnabled(draft.intervalEnabled)
        setProfileUpdateIntervalHours(draft.intervalHours)
        setUpdateAlways(draft.updateAlways)
    }

    const openCreate = () => {
        if (!canWriteUsers) {
            toastError('No write permission for subscriptions')
            return
        }
        setNameInput('')
        setQuotaGB('0')
        setSelectedProfiles(hydrateSubscriptionProfileAliases([]))
        setUserSearch('')
        setExpandedAliasUsers(new Set())
        applyRefreshPolicyDraft(subscriptionDefaultsToRefreshPolicyDraft(subscriptionDefaults))
        setModalState({ type: 'create' })
    }

    const openEdit = (sub: Subscription) => {
        if (!canWriteUsers) {
            toastError('No write permission for subscriptions')
            return
        }
        setNameInput(sub.name)
        setQuotaGB(sub.quota_limit ? (sub.quota_limit / 1024 ** 3).toFixed(2) : '0')
        setUserSearch('')
        const drafts = getSubscriptionProfileDrafts(sub)
        setSelectedProfiles(drafts)
        setExpandedAliasUsers(new Set(drafts.filter(p => p.alias).map(p => p.username)))
        setProfileUpdateIntervalEnabled(sub.profile_update_interval_hours != null)
        setProfileUpdateIntervalHours(
            sub.profile_update_interval_hours != null
                ? String(sub.profile_update_interval_hours)
                : (
                    subscriptionDefaults.profile_update_interval_hours != null
                        ? String(subscriptionDefaults.profile_update_interval_hours)
                        : DEFAULT_REFRESH_INTERVAL_HOURS
                )
        )
        setUpdateAlways(sub.update_always === true)
        setModalState({ type: 'edit', data: sub })
    }

    const openDefaults = () => {
        if (!canManagePanelScopedDefaults) {
            toastError('No permission to manage subscription defaults')
            return
        }
        const refreshDraft = subscriptionDefaultsToRefreshPolicyDraft(subscriptionDefaults)
        setDefaultIntervalEnabled(refreshDraft.intervalEnabled)
        setDefaultIntervalHours(refreshDraft.intervalHours)
        setDefaultUpdateAlways(refreshDraft.updateAlways)
        setDefaultsModalOpen(true)
    }

    const handleSave = async () => {
        if (!canWriteUsers) {
            toastError('No write permission for subscriptions')
            return
        }
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
            users: selectedProfiles.map(profile => profile.username),
            members: serializeSubscriptionProfileAliases(selectedProfiles),
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

    const handleSaveDefaults = async () => {
        if (!canManagePanelScopedDefaults) {
            toastError('No permission to manage subscription defaults')
            return
        }
        const intervalHours = defaultIntervalEnabled ? parseIntervalHours(defaultIntervalHours) : null
        if (defaultIntervalEnabled && intervalHours == null) {
            return toastError('Default refresh interval must be a whole number greater than zero')
        }

        try {
            await api.updateSubscriptionDefaults({
                profile_update_interval_hours: intervalHours,
                update_always: defaultUpdateAlways,
                destinations: subscriptionDefaults.destinations,
            })
            await queryClient.invalidateQueries({ queryKey: ['subscription-defaults'] })
            setDefaultsModalOpen(false)
            success('Subscription defaults updated')
        } catch (err) {
            toastError('Failed to update subscription defaults: ' + err)
        }
    }

    const handleDelete = async () => {
        if (!canWriteUsers) {
            toastError('No write permission for subscriptions')
            return
        }
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
        if (!canWriteUsers) {
            toastError('No write permission for subscriptions')
            return
        }
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
        if (!canWriteUsers) {
            toastError('No write permission for subscriptions')
            return
        }
        try {
            const link = subLink(token)
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
        setSelectedProfiles(prev => {
            const existing = prev.find(profile => profile.username === userName)
            if (existing) return prev.filter(profile => profile.username !== userName)
            // Preserve alias from current sub members so re-selecting restores it
            const savedAlias = modalState.data?.members?.find(m => m.username === userName)?.alias ?? ''
            return [...prev, { username: userName, alias: savedAlias }]
        })
    }

    const updateProfileAlias = (userName: string, alias: string) => {
        setSelectedProfiles(prev => prev.map(profile => (
            profile.username === userName
                ? { ...profile, alias }
                : profile
        )))
    }

    const toggleAlias = (userName: string) => {
        const isExpanded = expandedAliasUsers.has(userName)
        if (isExpanded) {
            updateProfileAlias(userName, '')
            setExpandedAliasUsers(prev => {
                const next = new Set(prev)
                next.delete(userName)
                return next
            })
        } else {
            setExpandedAliasUsers(prev => {
                const next = new Set(prev)
                next.add(userName)
                return next
            })
        }
    }

    const getQuotaPill = (sub: Subscription) => {
        const used = sub.used_bytes || 0
        const limit = sub.quota_limit || 0
        if (limit === 0) {
            return (
                <div className="flex flex-col gap-1 min-w-[120px]">
                    <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center text-[10px] mb-1 px-1.5 font-mono text-slate-400">
                        <span className="truncate whitespace-nowrap text-slate-300">{formatBytes(used)}</span>
                        <span className="text-center text-slate-300"></span>
                        <span className="text-right text-xl leading-none whitespace-nowrap">∞</span>
                    </div>
                    <div className="h-2.5 rounded-full bg-slate-800 overflow-hidden">
                        <div className="h-full w-full rounded-full bg-slate-700/50" />
                    </div>
                </div>
            )
        }
        const rawRatio = used / limit
        const pct = Math.min(100, Math.round(rawRatio * 100))
        const over = used >= limit
        const barColor = over ? 'bg-red-500' : pct > 80 ? 'bg-yellow-500' : 'bg-emerald-500'
        return (
            <div className="flex flex-col gap-1 min-w-[120px]">
                <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center text-[10px] mb-1 px-1.5 font-mono text-slate-400">
                    <span className={`truncate whitespace-nowrap ${over ? 'text-red-400 font-semibold' : 'text-slate-300'}`}>
                        {formatBytes(used)}
                    </span>
                    <span className="px-2 text-center text-slate-300 whitespace-nowrap">{pct}%</span>
                    <span className="truncate whitespace-nowrap text-right">{formatBytes(limit)}</span>
                </div>
                <div className="h-2.5 rounded-full bg-slate-800 overflow-hidden">
                    <div className={`h-full rounded-full ${barColor} transition-all duration-500`} style={{ width: `${Math.min(rawRatio * 100, 100)}%` }} />
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

    const toggleSort = (key: typeof sortKey) => {
        if (sortKey === key) {
            setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
        } else {
            setSortKey(key)
            setSortDir(['last_request', 'users', 'quota'].includes(key) ? 'desc' : 'asc')
        }
    }

    const renderSortIcon = (key: typeof sortKey) => {
        if (sortKey !== key) {
            return <ArrowUpDown size={12} className="inline ml-1 text-slate-500" />
        }
        return sortDir === 'asc'
            ? <ArrowUp size={12} className="inline ml-1 text-white" />
            : <ArrowDown size={12} className="inline ml-1 text-white" />
    }

    const subLink = (token: string) => {
        if (cfWorkerURL) {
            return `${cfWorkerURL}/s/${token}`
        }
        return `${window.location.protocol}//${subDomain}/s/${token}`
    }
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

            <label className="flex items-center gap-3 cursor-pointer">
                <input
                    type="checkbox"
                    checked={intervalEnabled}
                    onChange={e => setIntervalEnabled(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 shrink-0"
                />
                <div className="flex-1 text-sm font-medium text-slate-200">Emit profile-update-interval</div>
                <input
                    type="number"
                    min="1"
                    step="1"
                    value={intervalHours}
                    onChange={e => setIntervalHours(e.target.value)}
                    disabled={!intervalEnabled}
                    onClick={e => e.stopPropagation()}
                    className="w-20 bg-slate-950 border border-slate-800 rounded px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                    placeholder="Hours"
                />
            </label>

            <label className="flex items-center gap-3 cursor-pointer">
                <input
                    type="checkbox"
                    checked={updateAlwaysValue}
                    onChange={e => setUpdateAlwaysValue(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 shrink-0"
                />
                <div className="flex-1 text-sm font-medium text-slate-200">update-always</div>
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
                        disabled={!canManagePanelScopedDefaults}
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
                    <table className="w-full min-w-[980px] text-left border-collapse table-fixed">
                        <thead>
                            <tr className="bg-slate-950/50 border-b border-slate-800 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="w-[260px] p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('name')}>
                                    Name {renderSortIcon('name')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('last_request')}>
                                    Last Request {renderSortIcon('last_request')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('users')}>
                                    Users {renderSortIcon('users')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('quota')}>
                                    Quota {renderSortIcon('quota')}
                                </th>
                                <th className="p-4 font-semibold text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {sortedSubs.length === 0 ? (
                                <tr>
                                    <td colSpan={5} className="p-12 text-center text-slate-500">
                                        <LinkIcon size={48} className="mx-auto mb-4 opacity-20" />
                                        <p>No subscriptions found.</p>
                                    </td>
                                </tr>
                            ) : sortedSubs.map(sub => (
                                <tr key={sub.id} className="border-b last:border-0 border-slate-800/50 hover:bg-slate-800/20 transition-colors">
                                    <td className="w-[260px] p-4">
                                        <div className="max-w-[260px] truncate text-white font-medium" title={sub.name}>{sub.name}</div>
                                    </td>
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
                                    <td className="p-4">
                                        <div className="text-slate-400 text-sm">{sub.users?.length || 0} users</div>
                                    </td>
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
                {sortedSubs.length === 0 ? (
                    <div className="bg-slate-900 border border-slate-800 rounded-xl p-12 text-center text-slate-500">
                        <LinkIcon size={48} className="mx-auto mb-4 opacity-20" />
                        <p>No subscriptions found.</p>
                    </div>
                ) : sortedSubs.map(sub => (
                    <div key={sub.id} className="bg-slate-900 border border-slate-800 rounded-xl">
                        {(() => {
                            const lastRequest = getLastRequestMeta(sub)
                            return (
                                <div className="p-4 space-y-3">
                                    <div className="flex items-center justify-between gap-3">
                                        <p className="min-w-0 flex-1 text-white font-semibold truncate">{sub.name}</p>
                                        <div className="flex gap-2 shrink-0">
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

                                    <div className="flex items-center justify-between gap-2">
                                        <div className="flex items-center gap-2">
                                            <div className={`w-2 h-2 rounded-full shrink-0 ${lastRequest.dotClass} ${lastRequest.isRecent ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                            <span className={`text-xs ${lastRequest.textClass}`}>{lastRequest.text}</span>
                                        </div>
                                        <div className="text-xs text-slate-400 shrink-0">{sub.users?.length || 0} users</div>
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
                        <Button variant="primary" onClick={handleSave} disabled={!canWriteUsers}>Save</Button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <div className="grid grid-cols-3 gap-3">
                        <div className="col-span-2">
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
                        <input
                            type="text"
                            value={userSearch}
                            onChange={e => setUserSearch(e.target.value)}
                            placeholder="Select Users"
                            className="w-full bg-slate-950 border border-slate-800 rounded-t px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 placeholder:text-slate-400"
                        />
                        <div className="border border-t-0 border-slate-800 rounded-b bg-slate-950 h-[300px] overflow-y-auto">
                            {filteredUsers.map(u => {
                                const isSelected = selectedProfiles.some(p => p.username === u.name)
                                const alias = selectedProfiles.find(p => p.username === u.name)?.alias ?? ''
                                const aliasExpanded = expandedAliasUsers.has(u.name)
                                return (
                                    <div
                                        key={u.name}
                                        className="flex items-center gap-3 px-3 py-2 hover:bg-slate-900 cursor-pointer border-b border-slate-800 last:border-0"
                                        onClick={() => toggleUser(u.name)}
                                    >
                                        <input
                                            type="checkbox"
                                            checked={isSelected}
                                            readOnly
                                            className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 shrink-0 pointer-events-none self-start mt-1"
                                        />
                                        <div className="flex flex-1 min-w-0 items-start gap-2">
                                            <div className="w-1/2 min-w-0 space-y-1">
                                                <div className="truncate text-sm text-slate-200">{u.name}</div>
                                                {isSelected && aliasExpanded && (
                                                    <input
                                                        type="text"
                                                        value={alias}
                                                        onChange={e => updateProfileAlias(u.name, e.target.value)}
                                                        onClick={e => e.stopPropagation()}
                                                        onBlur={() => {
                                                            if (!alias.trim()) toggleAlias(u.name)
                                                        }}
                                                        placeholder="Alias"
                                                        className="w-full bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs text-white focus:outline-none focus:border-blue-500"
                                                    />
                                                )}
                                            </div>
                                            <div className="w-1/2 flex items-start gap-1">
                                                <div className="flex-1 flex flex-wrap justify-end gap-1.5">
                                                    {(u.inbound_tags && u.inbound_tags.length > 0) ? (
                                                        u.inbound_tags.map(tag => (
                                                            <Badge key={tag} variant="info" className="max-w-[150px]" title={tag}>{tag}</Badge>
                                                        ))
                                                    ) : (
                                                        <Badge variant="neutral">All</Badge>
                                                    )}
                                                </div>
                                                {isSelected && (
                                                    <button
                                                        onClick={e => { e.stopPropagation(); toggleAlias(u.name) }}
                                                        title={aliasExpanded ? 'Remove alias' : 'Set alias'}
                                                        className={`shrink-0 p-1 rounded border transition-all ${aliasExpanded ? 'bg-blue-500/15 border-blue-500/50 text-blue-400 hover:bg-blue-500/25' : 'bg-slate-800 border-slate-700 text-slate-400 hover:text-white hover:bg-slate-700'}`}
                                                    >
                                                        <Tag size={11} />
                                                    </button>
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                )
                            })}
                            {filteredUsers.length === 0 && (
                                <div className="p-4 text-center text-slate-500 text-sm">
                                    {userSearch.trim() ? 'No users match your search' : 'No users available'}
                                </div>
                            )}
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
                        <Button variant="primary" onClick={handleSaveDefaults} disabled={!canManagePanelScopedDefaults}>Save Defaults</Button>
                    </div>
                }
            >
                <div className="space-y-4">
                    {defaultsQuery.isLoading && (
                        <p className="text-sm text-slate-500">Loading your defaults for this panel account...</p>
                    )}
                    {renderRefreshPolicyFields(
                        defaultIntervalEnabled,
                        setDefaultIntervalEnabled,
                        defaultIntervalHours,
                        setDefaultIntervalHours,
                        defaultUpdateAlways,
                        setDefaultUpdateAlways,
                        'These are your defaults for the current panel account.'
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
