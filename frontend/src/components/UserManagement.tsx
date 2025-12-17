import { useEffect, useState } from 'react'
import { api, UserStatus, CreateUserRequest } from '../api'
import { Users, Plus, Trash2, RefreshCw, Edit, QrCode, ArrowUp, ArrowDown, ArrowUpDown, Copy, Check } from 'lucide-react'
import { v4 as uuidv4 } from 'uuid'
import QRCode from 'react-qr-code'
import { useToast } from '../context/ToastContext'
import { Button } from './ui/Button'
import { Badge } from './ui/Badge'
import { Modal } from './ui/Modal'

const BYTES_PER_GB = 1024 * 1024 * 1024

function formatBytes(bytes: number, decimals = 2) {
    if (!+bytes) return '0 B'
    const k = 1024
    const dm = decimals < 0 ? 0 : decimals
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

function bytesToGbString(bytes?: number) {
    return bytes && bytes > 0 ? (bytes / BYTES_PER_GB).toFixed(2) : ''
}

function parseGbToBytes(input: string) {
    const normalized = input.replace(',', '.').trim()
    if (!normalized) return 0
    const val = parseFloat(normalized)
    if (isNaN(val) || val <= 0) return 0
    return Math.round(val * BYTES_PER_GB)
}

export default function UserManagement() {
    const { success, error: toastError } = useToast()

    function formatTimeAgo(timestamp: number) {
        if (!timestamp || timestamp <= 0) return 'Never'
        const seconds = Math.floor((Date.now() / 1000) - timestamp)
        if (seconds < 60) return 'Just now'
        const minutes = Math.floor(seconds / 60)
        if (minutes < 60) return `${minutes}m ago`
        const hours = Math.floor(minutes / 60)
        if (hours < 24) return `${hours}h ago`
        return `${Math.floor(hours / 24)}d ago`
    }

    const [users, setUsers] = useState<UserStatus[]>([])
    const [selectedUsers, setSelectedUsers] = useState<Set<string>>(new Set())
    const [inbounds, setInbounds] = useState<any[]>([])
    const [singboxPendingChanges, setSingboxPendingChanges] = useState(false)

    // Modals state
    const [modalState, setModalState] = useState<{
        type: 'create' | 'bulk' | 'qr' | 'usage' | 'delete_confirm' | 'select_inbounds' | null,
        data?: any
    }>({ type: null })
    const [selectedInboundsToRemove, setSelectedInboundsToRemove] = useState<Set<string>>(new Set())

    const [isEditing, setIsEditing] = useState(false)
    const [sortKey, setSortKey] = useState<'user' | 'quota' | 'usage' | 'status' | 'last_seen'>('user')
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
    const [originalName, setOriginalName] = useState<string>('')
    const [filterInbound, setFilterInbound] = useState<string>('')

    // Usage Report State
    const [usageStart, setUsageStart] = useState<string>('')
    const [usageEnd, setUsageEnd] = useState<string>('')
    const [usageLimitGb, setUsageLimitGb] = useState<string>('0')
    const [usageData, setUsageData] = useState<any[]>([])
    const [loadingUsage, setLoadingUsage] = useState(false)

    // Create/Edit Form State
    const [newUser, setNewUser] = useState<CreateUserRequest>({
        name: '',
        uuid: '',
        flow: 'xtls-rprx-vision',
        quota_limit: 0,
        quota_period: 'monthly',
        reset_day: 1,
        inbound_tag: ''
    })
    const [quotaInput, setQuotaInput] = useState<string>('')
    const [inboundRows, setInboundRows] = useState<{ tag: string; flow: string }[]>([])
    const [originalInboundTags, setOriginalInboundTags] = useState<string[]>([])

    // Bulk Form State
    const [bulkConfig, setBulkConfig] = useState({
        prefix: 'user',
        count: 1,
        start_index: 1,
        mode: 'sequential', // 'sequential' | 'random'
        suffix: '', // e.g. @example.com
        flow: 'xtls-rprx-vision',
        quota_limit: 0,
        quota_period: 'monthly',
        reset_day: 1,
        inbound_tag: ''
    })
    const [bulkQuotaInput, setBulkQuotaInput] = useState<string>('')
    const [copied, setCopied] = useState(false)
    const [publicIP, setPublicIP] = useState<string>('')
    const [inboundConfigs, setInboundConfigs] = useState<Map<string, any>>(new Map())
    const [selectedQrInbound, setSelectedQrInbound] = useState<string>('')

    const fetchUsers = () => {
        api.getUsers()
            .then(data => {
                setUsers(data)
                // Prune selected users that no longer exist
                setSelectedUsers(prev => {
                    const allowed = new Set(data.map(u => u.name))
                    const next = new Set<string>()
                    prev.forEach(name => {
                        if (allowed.has(name)) next.add(name)
                    })
                    return next
                })
            })
            .catch(err => toastError(`Failed to fetch users: ${err}`))
    }

    const fetchInbounds = () => {
        api.getSingboxInbounds()
            .then(data => {
                // Filter only VLESS inbounds usually, but API returns all.
                setInbounds(data)
                // Build a map of inbound configs by tag
                const configMap = new Map()
                data.forEach((inb: any) => {
                    if (inb.tag) {
                        configMap.set(inb.tag, inb)
                    }
                })
                setInboundConfigs(configMap)
            })
            .catch(err => toastError(`Failed to fetch inbounds: ${err}`))
    }

    useEffect(() => {
        fetchUsers()
        fetchInbounds()

        // Fetch public IP from dashboard
        api.getDashboardData().then(data => {
            if (data.public_ip) {
                setPublicIP(data.public_ip)
            }
        }).catch(err => console.error('Failed to fetch public IP:', err))

        // Fetch singbox_pending_changes status
        api.getDashboardData().then(data => {
            setSingboxPendingChanges(data.singbox_pending_changes || false)
        }).catch(err => console.error('Failed to fetch pending changes:', err))

        // Poll for pending changes every 10 seconds
        const interval = setInterval(() => {
            api.getDashboardData().then(data => {
                setSingboxPendingChanges(data.singbox_pending_changes || false)
            }).catch(() => { })
        }, 10000)

        return () => clearInterval(interval)
    }, [])

    useEffect(() => {
        if (modalState.type === 'qr' && modalState.data?.inbound_tags?.length > 0) {
            setSelectedQrInbound(modalState.data.inbound_tags[0])
        }
    }, [modalState.type, modalState.data])

    const sortedUsers = users
        .filter(u => !filterInbound || (u.inbound_tags && u.inbound_tags.includes(filterInbound)) || (!u.inbound_tags && !filterInbound))
        .sort((a, b) => {
            const dir = sortDir === 'asc' ? 1 : -1
            switch (sortKey) {
                case 'quota':
                    // Sort by percentage used (Current Consumption / Limit)
                    const aRatio = a.quota_limit ? ((a.total || 0) / a.quota_limit) : 0
                    const bRatio = b.quota_limit ? ((b.total || 0) / b.quota_limit) : 0
                    if (Math.abs(aRatio - bRatio) < 0.0001) {
                        return ((a.total || 0) - (b.total || 0)) * dir
                    }
                    return (aRatio - bRatio) * dir
                case 'usage':
                    return ((a.total || 0) - (b.total || 0)) * dir
                case 'status':
                    return (a.total > (a.quota_limit || Infinity) ? 1 : 0 - (b.total > (b.quota_limit || Infinity) ? 1 : 0)) * dir
                case 'last_seen': {
                    const la = a.last_seen || 0
                    const lb = b.last_seen || 0
                    return (la - lb) * dir
                }
                case 'user':
                default:
                    return a.name.localeCompare(b.name) * dir
            }
        })

    const toggleSort = (key: typeof sortKey) => {
        if (sortKey === key) {
            setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
        } else {
            setSortKey(key)
            // Default to descending for numeric/time metrics
            if (['quota', 'usage', 'last_seen', 'status'].includes(key)) {
                setSortDir('desc')
            } else {
                setSortDir('asc')
            }
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

    const handleSelectAll = (e: React.ChangeEvent<HTMLInputElement>) => {
        if (e.target.checked) {
            setSelectedUsers(new Set(users.map(u => u.name)))
        } else {
            setSelectedUsers(new Set())
        }
    }

    const handleSelectUser = (name: string) => {
        const newSelected = new Set(selectedUsers)
        if (newSelected.has(name)) {
            newSelected.delete(name)
        } else {
            newSelected.add(name)
        }
        setSelectedUsers(newSelected)
    }

    const fetchUsage = async () => {
        setLoadingUsage(true)
        try {
            const start = usageStart ? Math.floor(new Date(usageStart).getTime() / 1000).toString() : ''
            const end = usageEnd ? Math.floor(new Date(usageEnd).getTime() / 1000).toString() : ''
            const limitBytes = parseGbToBytes(usageLimitGb)
            const data = await api.getReportSummary(start, end, limitBytes > 0 ? limitBytes : undefined)
            setUsageData(Array.isArray(data) ? data : [])
        } catch (err) {
            toastError('Failed to fetch usage: ' + err)
        } finally {
            setLoadingUsage(false)
        }
    }

    const handleDeleteSelected = async () => {
        setModalState({ type: 'delete_confirm' }) // Keep modal open until confirmed
    }

    const confirmDeleteSelected = async () => {
        // Check if any selected user is in multiple inbounds
        const multiInboundUsers = Array.from(selectedUsers).filter(name => {
            const user = users.find(u => u.name === name)
            return user && user.inbound_tags && user.inbound_tags.length > 1
        })

        if (multiInboundUsers.length > 0) {
            setModalState({ type: null })
            toastError(`User "${multiInboundUsers[0]}" exists in multiple inbounds. Please delete manually.`)
            return
        }

        setModalState({ type: null })
        let failed = 0;
        for (const name of selectedUsers) {
            try {
                await api.deleteUser(name)
            } catch (err) {
                console.error(`Failed to delete ${name}`, err)
                failed++;
            }
        }
        if (failed > 0) {
            toastError(`Failed to delete ${failed} users.`)
        } else {
            success(`Deleted ${selectedUsers.size} users successfully.`)
        }
        setSelectedUsers(new Set())
        fetchUsers()
    }

    const handleRemoveFromSelectedInbounds = async () => {
        if (!modalState.data) return

        const user = modalState.data as UserStatus
        const inboundsToRemove = Array.from(selectedInboundsToRemove)

        setModalState({ type: null })
        setSelectedInboundsToRemove(new Set())

        try {
            // If all inbounds selected, delete user completely
            if (inboundsToRemove.length === (user.inbound_tags?.length || 0)) {
                await api.deleteUser(user.name)
                success('User deleted successfully')
            } else {
                // Remove from selected inbounds only
                for (const tag of inboundsToRemove) {
                    await api.removeUserFromInbound(user.name, tag)
                }
                success(`User removed from ${inboundsToRemove.length} inbound(s)`)
            }
            fetchUsers()
        } catch (err) {
            toastError('Failed to remove user: ' + err)
        }
    }

    const handleEditClick = (user: UserStatus) => {
        const inboundTags = user.inbound_tags && user.inbound_tags.length > 0 ? user.inbound_tags : []
        setNewUser({
            name: user.name,
            uuid: user.uuid || '',
            flow: user.flow || '',
            quota_limit: user.quota_limit,
            quota_period: user.quota_period,
            reset_day: user.reset_day,
            enabled: user.enabled,
            inbound_tag: inboundTags[0] || ''
        })
        setOriginalInboundTags(inboundTags)
        setInboundRows(
            inboundTags.length > 0
                ? inboundTags.map(tag => ({ tag, flow: '' }))
                : [{ tag: inbounds[0]?.tag || '', flow: 'xtls-rprx-vision' }]
        )
        setQuotaInput(bytesToGbString(user.quota_limit))
        setOriginalName(user.name)
        setIsEditing(true)
        setModalState({ type: 'create' })
        api.getUserInbounds(user.name)
            .then(list => {
                if (!Array.isArray(list) || list.length === 0) return
                setInboundRows(prev => prev.map(row => {
                    const match = list.find(i => i.tag === row.tag)
                    if (!match) return row
                    return { ...row, flow: match.flow || '' }
                }))
            })
            .catch(err => {
                console.error('Failed to load user inbounds', err)
            })
    }

    const handleSaveUser = async () => {
        try {
            const normalizedRows = inboundRows.map(row => ({
                tag: row.tag.trim(),
                flow: row.flow
            }))
            const emptyInbound = normalizedRows.some(row => !row.tag)
            const inboundTags = normalizedRows.map(row => row.tag)
            const hasDuplicateInbound = new Set(inboundTags).size !== inboundTags.length

            if (normalizedRows.length === 0 || emptyInbound || hasDuplicateInbound) {
                toastError('Please fix inbound entries before saving')
                return
            }

            if (isEditing) {
                const nameChanged = originalName && originalName !== newUser.name
                if (nameChanged && normalizedRows.length > 1) {
                    toastError('Renaming users with multiple inbounds is not supported yet')
                    return
                }

                const payload = {
                    ...newUser,
                    original_name: originalName || newUser.name,
                    inbound_tag: normalizedRows[0].tag,
                    flow: nameChanged && normalizedRows.length === 1 ? normalizedRows[0].flow : '',
                }
                if (!payload.uuid) payload.uuid = uuidv4()

                await api.updateUser(payload)

                if (newUser.enabled !== false && !nameChanged) {
                    const originalTags = originalInboundTags.length > 0
                        ? originalInboundTags
                        : ((modalState.data?.inbound_tags || []) as string[])
                    const originalSet = new Set(originalTags)
                    const desiredSet = new Set(inboundTags)

                    for (const tag of originalTags) {
                        if (!desiredSet.has(tag)) {
                            await api.removeUserFromInbound(newUser.name, tag)
                        }
                    }

                    for (const row of normalizedRows) {
                        if (originalSet.has(row.tag)) {
                            await api.updateUserInbound(newUser.name, row.tag, {
                                uuid: payload.uuid,
                                flow: row.flow,
                            })
                        } else {
                            await api.createUser({
                                ...newUser,
                                uuid: payload.uuid,
                                inbound_tag: row.tag,
                                flow: row.flow,
                            })
                        }
                    }
                }

                success(`User updated successfully`)
            } else {
                const payload = { ...newUser }
                if (!payload.uuid) payload.uuid = uuidv4()
                for (const row of normalizedRows) {
                    await api.createUser({
                        ...payload,
                        inbound_tag: row.tag,
                        flow: row.flow,
                    })
                }
                success(`User created successfully`)
            }
            setModalState({ type: null }) // Close modal
            // Reset form
            setNewUser({
                name: '',
                uuid: '',
                flow: 'xtls-rprx-vision',
                quota_limit: 0,
                quota_period: 'monthly',
                reset_day: 1,
                inbound_tag: inbounds.length > 0 ? inbounds[0].tag : ''
            })
            setQuotaInput('')
            setOriginalName('')
            setIsEditing(false)
            setInboundRows([])
            setOriginalInboundTags([])
            fetchUsers()
        } catch (err) {
            toastError('Failed to save user: ' + err)
        }
    }

    const inboundTags = inboundRows.map(row => row.tag.trim()).filter(Boolean)
    const hasEmptyInbound = inboundRows.some(row => !row.tag.trim())
    const hasDuplicateInbound = new Set(inboundTags).size !== inboundTags.length
    const inboundValid = inboundRows.length > 0 && !hasEmptyInbound && !hasDuplicateInbound

    const handleBulkCreate = async () => {
        try {
            const usersToCreate: CreateUserRequest[] = []

            for (let i = 0; i < bulkConfig.count; i++) {
                let username = ''
                if (bulkConfig.mode === 'sequential') {
                    username = `${bulkConfig.prefix}-${bulkConfig.start_index + i}`
                } else {
                    const randomSuffix = Math.random().toString(36).substring(2, 6)
                    username = `${bulkConfig.prefix}-${randomSuffix}`
                }

                const fullName = `${username}${bulkConfig.suffix}`

                usersToCreate.push({
                    name: fullName,
                    uuid: uuidv4(),
                    flow: bulkConfig.flow,
                    quota_limit: bulkConfig.quota_limit,
                    quota_period: bulkConfig.quota_period,
                    reset_day: bulkConfig.reset_day,
                    inbound_tag: bulkConfig.inbound_tag
                })
            }

            await api.bulkCreateUsers(usersToCreate)
            success(`Bulk created ${usersToCreate.length} users successfully`)
            setModalState({ type: null })
            fetchUsers()
        } catch (err) {
            toastError('Failed to bulk create: ' + err)
        }
    }

    const generateVLESSLink = (user: UserStatus, inboundTagOverride?: string) => {
        const uuid = user.uuid || "5e18b70f-bdaa-4b8a-8e50-67830e897bc5"
        const inboundTag = inboundTagOverride || (user.inbound_tags && user.inbound_tags.length > 0 ? user.inbound_tags[0] : '')
        const inboundConfig = inboundConfigs.get(inboundTag)
        const inboundUsers = inboundConfig?.users || inboundConfig?.["users"]
        const inboundUser = Array.isArray(inboundUsers) ? inboundUsers.find((u: any) => u && u.name === user.name) : null
        const flow = typeof inboundUser?.flow === 'string' ? inboundUser.flow : (inboundUser?.flow ? String(inboundUser.flow) : '')
        const flowParam = flow ? `&flow=${flow}` : ""

        // Get IP from config or fallback
        const ip = publicIP || window.location.hostname || "127.0.0.1"

        // Get inbound config for the first inbound tag
        // Extract port from inbound config
        const port = inboundConfig?.listen_port || "443"

        // Extract Reality config if available
        const reality = inboundConfig?.tls?.reality
        console.log('Reality config:', reality)
        console.log('Short ID:', reality?.short_id)

        const pbk = reality?.public_key || "4aHK2h-F_LeS5FYsdUqipny0ae67oWcgmlcyfIofon8"
        const sni = reality?.handshake?.server || "www.cloudflare.com"

        // Handle short_id as both array and string
        let sid = "0861c24f2c393938"
        if (reality?.short_id) {
            if (Array.isArray(reality.short_id) && reality.short_id.length > 0) {
                sid = reality.short_id[0]
            } else if (typeof reality.short_id === 'string') {
                sid = reality.short_id
            }
        }

        const name = `VLESS-${user.name}`

        return `vless://${uuid}@${ip}:${port}?security=reality&encryption=none&pbk=${pbk}&headerType=none&fp=chrome&type=tcp${flowParam}&sni=${sni}&sid=${sid}#${name}`
    }

    const handleCopyLink = async (link: string) => {
        try {
            await navigator.clipboard.writeText(link)
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
            success('Link copied to clipboard')
        } catch (err) {
            toastError('Failed to copy link')
        }
    }


    const handleApplySingboxChanges = async () => {
        try {
            await api.applySingboxChanges()
            setSingboxPendingChanges(false)
            success('Sing-box configuration applied successfully')
        } catch (err) {
            toastError('Failed to apply changes. Please try again.')
        }
    }


    return (
        <div className="space-y-6">
            {/* Pending Changes Banner */}
            {singboxPendingChanges && (
                <div className="bg-yellow-900/20 border border-yellow-600/50 rounded-lg p-4 flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <Users className="text-yellow-500" size={20} />
                        <div>
                            <p className="text-sm font-medium text-yellow-200">Sing-box Configuration Changes Pending</p>
                            <p className="text-xs text-yellow-300/70 mt-0.5">User changes have been saved but not yet applied. Click "Apply Changes" to restart the service.</p>
                        </div>
                    </div>
                    <Button
                        onClick={handleApplySingboxChanges}
                        variant="primary"
                        size="sm"
                        className="whitespace-nowrap bg-yellow-600 hover:bg-yellow-700 text-white"
                    >
                        Apply Changes
                    </Button>
                </div>
            )}

            <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white">User Management</h1>
                    <p className="text-slate-400 text-sm mt-1">Manage VLESS users and quotas</p>
                </div>
                <div className="flex flex-wrap gap-2">
                    {selectedUsers.size > 0 && (
                        <Button
                            onClick={handleDeleteSelected}
                            variant="danger"
                            icon={<Trash2 size={16} />}
                        >
                            Delete ({selectedUsers.size})
                        </Button>
                    )}
                    <Button
                        onClick={() => {
                            setIsEditing(false)
                            setInboundRows([{ tag: inbounds.length > 0 ? inbounds[0].tag : '', flow: 'xtls-rprx-vision' }])
                            setOriginalInboundTags([])
                            setNewUser({
                                name: '',
                                uuid: '',
                                flow: 'xtls-rprx-vision',
                                quota_limit: 0,
                                quota_period: 'monthly',
                                reset_day: 1,
                                enabled: true,
                                inbound_tag: inbounds.length > 0 ? inbounds[0].tag : ''
                            })
                            setQuotaInput('')
                            setModalState({ type: 'create' })
                        }}
                        icon={<Plus size={16} />}
                        variant="primary"
                    >
                        Create User
                    </Button>
                    <Button
                        onClick={() => setModalState({ type: 'bulk' })}
                        variant="secondary"
                        icon={<Users size={16} />}
                    >
                        Bulk Create
                    </Button>
                    <div className="flex bg-slate-800 rounded-lg p-1 border border-slate-700">
                        <select
                            value={filterInbound}
                            onChange={(e) => setFilterInbound(e.target.value)}
                            className="bg-transparent text-slate-300 text-sm px-3 py-1 outline-none"
                        >
                            <option value="">All Inbounds</option>
                            {inbounds.map((inb) => (
                                <option key={inb.tag} value={inb.tag}>{inb.tag}</option>
                            ))}
                        </select>
                    </div>
                    <Button
                        onClick={() => setModalState({ type: 'usage' })}
                        variant="secondary"
                        icon={<RefreshCw size={16} />}
                    >
                        Usage Report
                    </Button>
                </div>
            </div>

            {/* Users Table Container - Matching WireGuard Style */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">

                {/* Checkbox Header (Custom addition to match WireGuard container but keep bulk actions) */}
                {users.length > 0 && (
                    <div className="p-3 bg-slate-950/30 border-b border-slate-800 flex items-center gap-3 md:hidden">
                        <input
                            type="checkbox"
                            onChange={handleSelectAll}
                            checked={users.length > 0 && selectedUsers.size === users.length}
                            className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900 cursor-pointer h-4 w-4"
                        />
                        <span className="text-xs text-slate-400 font-medium">Select All</span>
                    </div>
                )}

                <div className="overflow-x-auto hidden md:block">
                    <table className="w-full text-left border-collapse">
                        <thead>
                            <tr className="bg-slate-950/50 border-b border-slate-800 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="p-4 w-10">
                                    <input
                                        type="checkbox"
                                        onChange={handleSelectAll}
                                        checked={users.length > 0 && selectedUsers.size === users.length}
                                        className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900 cursor-pointer"
                                    />
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('status')}>
                                    Last Seen {renderSortIcon('status')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('user')}>
                                    Name/Alias {renderSortIcon('user')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('quota')}>
                                    Quota {renderSortIcon('quota')}
                                </th>

                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('usage')}>
                                    Data Usage {renderSortIcon('usage')}
                                </th>
                                <th className="p-4 font-semibold text-left">Inbound</th>
                                <th className="p-4 font-semibold text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-800">
                            {users.length === 0 ? (
                                <tr>
                                    <td colSpan={8} className="p-12 text-center text-slate-500">
                                        <Users size={48} className="mx-auto mb-4 opacity-20" />
                                        <p>No users found.</p>
                                    </td>
                                </tr>
                            ) : (
                                sortedUsers.map(user => {
                                    const isExceeded = user.quota_limit ? user.total > user.quota_limit : false
                                    const isSelected = selectedUsers.has(user.name)

                                    // Status Logic
                                    let statusColor = "bg-slate-600" // Default offline
                                    let statusText = "Never"
                                    let isOnline = false

                                    if (user.enabled) {
                                        if (user.last_seen) {
                                            const diff = Math.floor(Date.now() / 1000) - user.last_seen
                                            statusText = formatTimeAgo(user.last_seen)
                                            if (diff < 300) { // 5 mins
                                                statusColor = "bg-emerald-500"
                                                isOnline = true
                                            } else {
                                                statusColor = "bg-slate-700"
                                            }
                                        } else {
                                            statusColor = "bg-slate-700"
                                            statusText = "Never"
                                        }
                                    } else {
                                        statusColor = "bg-red-500"
                                        statusText = "Disabled"
                                    }

                                    if (isExceeded) {
                                        statusColor = "bg-amber-500"
                                        statusText = "Exceeded"
                                    }

                                    return (
                                        <tr
                                            key={user.name}
                                            className={`hover:bg-slate-800/30 transition-colors ${isSelected ? 'bg-blue-900/10' : ''}`}
                                        >
                                            <td className="p-4">
                                                <input
                                                    type="checkbox"
                                                    checked={isSelected}
                                                    onChange={() => handleSelectUser(user.name)}
                                                    className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900 cursor-pointer"
                                                />
                                            </td>
                                            <td className="p-4">
                                                <div className="flex items-center gap-3">
                                                    <div className={`w-2.5 h-2.5 rounded-full ring-2 ring-slate-900 ${statusColor} ${isOnline ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                                    <span className={`text-xs font-medium ${isOnline ? 'text-emerald-400' : 'text-slate-500'}`}>
                                                        {statusText}
                                                    </span>
                                                </div>
                                            </td>
                                            <td className="p-4">
                                                <div className="font-semibold text-slate-200">{user.name}</div>
                                            </td>
                                            <td className="p-4 align-middle">
                                                {user.quota_limit ? (
                                                    <div className="w-1/2 min-w-[140px]">
                                                        <div className="flex justify-between text-[10px] mb-1 font-mono text-slate-400">
                                                            <span>{formatBytes(user.total)}</span>
                                                            <span>{formatBytes(user.quota_limit)}</span>
                                                        </div>
                                                        <div className="h-2.5 bg-slate-800 rounded-full overflow-hidden">
                                                            <div
                                                                className={`h-full rounded-full transition-all duration-500 ${(user.total / user.quota_limit) > 1 ? 'bg-red-500' :
                                                                    (user.total / user.quota_limit) > 0.8 ? 'bg-amber-500' :
                                                                        'bg-blue-500'
                                                                    }`}
                                                                style={{ width: `${Math.min((user.total / user.quota_limit) * 100, 100)}%` }}
                                                            />
                                                        </div>
                                                    </div>
                                                ) : (
                                                    <div className="w-1/2 min-w-[140px]">
                                                        <div className="flex justify-between text-[10px] mb-1 font-mono text-slate-400">
                                                            <span>{formatBytes(user.total)}</span>
                                                            <span className="text-xl leading-none">∞</span>
                                                        </div>
                                                        <div className="h-2.5 bg-slate-800 rounded-full overflow-hidden">
                                                            <div className="h-full bg-slate-700/50 w-full rounded-full" />
                                                        </div>
                                                    </div>
                                                )}
                                            </td>

                                            <td className="p-4">
                                                <div className="flex flex-col gap-1 text-[11px] font-mono">
                                                    <div className="flex items-center gap-1.5 text-emerald-400">
                                                        <ArrowUp size={12} strokeWidth={3} />
                                                        {formatBytes(user.uplink)}
                                                    </div>
                                                    <div className="flex items-center gap-1.5 text-blue-400">
                                                        <ArrowDown size={12} strokeWidth={3} />
                                                        {formatBytes(user.downlink)}
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="p-4">
                                                <div className="flex flex-wrap gap-1">
                                                    {(user.inbound_tags && user.inbound_tags.length > 0) ? (
                                                        user.inbound_tags.map(tag => (
                                                            <Badge key={tag} variant="info">
                                                                {tag}
                                                            </Badge>
                                                        ))
                                                    ) : (
                                                        <Badge variant="neutral">All</Badge>
                                                    )}
                                                </div>
                                            </td>
                                            <td className="p-4 text-right">
                                                <div className="flex items-center justify-end gap-2">
                                                    <button
                                                        onClick={() => handleEditClick(user)}
                                                        className="p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-blue-400 hover:bg-slate-700 border border-slate-700 transition-all"
                                                        title="Edit User"
                                                    >
                                                        <Edit size={16} />
                                                    </button>
                                                    <button
                                                        onClick={() => setModalState({ type: 'qr', data: user })}
                                                        className="p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 border border-slate-700 transition-all"
                                                        title="QR Code"
                                                    >
                                                        <QrCode size={16} />
                                                    </button>
                                                    <button
                                                        onClick={() => {
                                                            if (user.inbound_tags && user.inbound_tags.length > 1) {
                                                                // Show inbound selection modal
                                                                setSelectedInboundsToRemove(new Set(user.inbound_tags))
                                                                setModalState({ type: 'select_inbounds', data: user })
                                                            } else {
                                                                // Delete immediately
                                                                if (confirm(`Delete user "${user.name}"?`)) {
                                                                    api.deleteUser(user.name)
                                                                        .then(() => {
                                                                            success('User deleted')
                                                                            fetchUsers()
                                                                        })
                                                                        .catch(err => toastError('Failed to delete: ' + err))
                                                                }
                                                            }
                                                        }}
                                                        className="p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-red-400 hover:bg-slate-700 border border-slate-700 transition-all"
                                                        title="Delete User"
                                                    >
                                                        <Trash2 size={16} />
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    )
                                })
                            )}
                        </tbody>
                    </table>
                </div>

                {/* Mobile Cards (Matching WireGuard) */}
                <div className="md:hidden divide-y divide-slate-800">
                    {(sortedUsers || []).map(user => {
                        const isExceeded = user.quota_limit ? user.total > user.quota_limit : false
                        const isSelected = selectedUsers.has(user.name)

                        let statusColor = "bg-slate-600"
                        let statusText = "Never"
                        let isOnline = false

                        if (user.enabled) {
                            if (user.last_seen) {
                                const diff = Math.floor(Date.now() / 1000) - user.last_seen
                                statusText = formatTimeAgo(user.last_seen)
                                if (diff < 300) {
                                    statusColor = "bg-emerald-500"
                                    isOnline = true
                                } else {
                                    statusColor = "bg-slate-700"
                                }
                            } else {
                                statusColor = "bg-slate-700"
                            }
                        } else {
                            statusColor = "bg-red-500"
                            statusText = "Disabled"
                        }

                        if (isExceeded) {
                            statusColor = "bg-amber-500"
                            statusText = "Exceeded"
                        }

                        return (
                            <div key={user.name} className={`p-4 space-y-4 ${isSelected ? 'bg-blue-900/10' : ''}`}>
                                <div className="flex items-start justify-between">
                                    <div className="flex items-start gap-3">
                                        <input
                                            type="checkbox"
                                            checked={isSelected}
                                            onChange={() => handleSelectUser(user.name)}
                                            className="mt-1 rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900 cursor-pointer h-4 w-4"
                                        />
                                        <div className="space-y-1">
                                            <div className="font-bold text-slate-200">{user.name}</div>
                                            <div className="flex items-center gap-2">
                                                <div className={`w-2 h-2 rounded-full ${statusColor} ${isOnline ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                                <span className={`text-xs ${isOnline ? 'text-emerald-400' : 'text-slate-500'}`}>
                                                    {statusText}
                                                </span>
                                            </div>
                                        </div>
                                    </div>
                                    <div className="flex gap-2">
                                        <button
                                            onClick={() => handleEditClick(user)}
                                            className="p-2 rounded-lg bg-slate-800 text-slate-300 border border-slate-700"
                                        >
                                            <Edit size={16} />
                                        </button>
                                        <button
                                            onClick={() => setModalState({ type: 'qr', data: user })}
                                            className="p-2 rounded-lg bg-slate-800 text-slate-300 border border-slate-700"
                                        >
                                            <QrCode size={16} />
                                        </button>
                                        <button
                                            onClick={async () => {
                                                if (confirm(`Delete user ${user.name}?`)) {
                                                    try {
                                                        await api.deleteUser(user.name)
                                                        success('User deleted')
                                                        fetchUsers()
                                                    } catch (e) { toastError(String(e)) }
                                                }
                                            }}
                                            className="p-2 rounded-lg bg-slate-800 text-red-400 border border-slate-700"
                                        >
                                            <Trash2 size={16} />
                                        </button>
                                    </div>
                                </div>

                                <div className="grid grid-cols-2 gap-4 text-xs">
                                    <div className="col-span-2">
                                        <div className="col-span-2">
                                            {user.quota_limit ? (
                                                <div className="w-full">
                                                    <div className="flex justify-between text-[10px] mb-1 font-mono text-slate-400">
                                                        <span>{formatBytes(user.total)}</span>
                                                        <span>{formatBytes(user.quota_limit)}</span>
                                                    </div>
                                                    <div className="h-2.5 bg-slate-800 rounded-full overflow-hidden">
                                                        <div
                                                            className={`h-full rounded-full transition-all duration-500 ${(user.total / user.quota_limit) > 1 ? 'bg-red-500' :
                                                                (user.total / user.quota_limit) > 0.8 ? 'bg-amber-500' :
                                                                    'bg-blue-500'
                                                                }`}
                                                            style={{ width: `${Math.min((user.total / user.quota_limit) * 100, 100)}%` }}
                                                        />
                                                    </div>
                                                </div>
                                            ) : (
                                                <div className="w-full">
                                                    <div className="flex justify-between text-[10px] mb-1 font-mono text-slate-400">
                                                        <span>{formatBytes(user.total)}</span>
                                                        <span className="text-xl leading-none">∞</span>
                                                    </div>
                                                    <div className="h-2.5 bg-slate-800 rounded-full overflow-hidden">
                                                        <div className="h-full bg-slate-700/50 w-full rounded-full" />
                                                    </div>
                                                </div>
                                            )}
                                        </div>
                                    </div>

                                </div>

                                <div className="bg-slate-950/50 rounded-lg p-3 grid grid-cols-2 gap-2">
                                    <div className="flex items-center gap-2 text-emerald-400 text-xs font-mono">
                                        <ArrowUp size={14} />
                                        {formatBytes(user.uplink)}
                                    </div>
                                    <div className="flex items-center gap-2 text-blue-400 text-xs font-mono">
                                        <ArrowDown size={14} />
                                        {formatBytes(user.downlink)}
                                    </div>
                                </div>
                            </div>
                        )
                    })}
                    {users.length === 0 && (
                        <div className="p-8 text-center text-slate-500">
                            <Users size={48} className="mx-auto mb-4 opacity-20" />
                            <p>No users found.</p>
                        </div>
                    )}
                </div>
            </div>

            {/* Create/Edit User Modal */}
            <Modal
                isOpen={modalState.type === 'create'}
                onClose={() => setModalState({ type: null })}
                title={isEditing ? 'Edit User' : 'Create New User'}
                footer={
                    <>
                        <Button variant="ghost" onClick={() => setModalState({ type: null })}>Cancel</Button>
                        <Button
                            variant="primary"
                            onClick={handleSaveUser}
                            disabled={!inboundValid}
                        >
                            {isEditing ? 'Save Changes' : 'Create User'}
                        </Button>
                    </>
                }
            >
                <div className="space-y-6">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Username</label>
                            <input
                                type="text"
                                value={newUser.name}
                                onChange={e => setNewUser({ ...newUser, name: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                                placeholder="e.g. john_doe"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">UUID (shared across inbounds)</label>
                            <div className="flex gap-2">
                                <input
                                    type="text"
                                    value={newUser.uuid || ''}
                                    onChange={e => setNewUser({ ...newUser, uuid: e.target.value })}
                                    className="flex-1 min-w-0 bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                                    placeholder="Auto-generated if empty"
                                />
                                <Button
                                    type="button"
                                    variant="icon"
                                    size="icon"
                                    className="aspect-square"
                                    onClick={() => {
                                        const uuid = crypto.randomUUID ? crypto.randomUUID() :
                                            'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, c => {
                                                const r = Math.random() * 16 | 0
                                                const v = c === 'x' ? r : (r & 0x3 | 0x8)
                                                return v.toString(16)
                                            })
                                        setNewUser({ ...newUser, uuid })
                                    }}
                                    title="Generate Random UUID"
                                >
                                    <RefreshCw size={16} />
                                </Button>
                            </div>
                        </div>
                    </div>
                    {/* Inbound List */}
                    <div className="space-y-3">
                        <div className="space-y-2">
                            {inboundRows.map((row, idx) => {
                                const tagValue = row.tag
                                return (
                                    <div key={`${tagValue}-${idx}`} className="grid grid-cols-[1fr_1fr_auto] gap-3 items-end">
                                        <div className="space-y-1">
                                            <label className="block text-sm font-medium text-slate-400">Inbound</label>
                                            <select
                                                value={row.tag}
                                                onChange={e => {
                                                    const value = e.target.value
                                                    setInboundRows(prev => prev.map((r, rIdx) => rIdx === idx ? { ...r, tag: value } : r))
                                                }}
                                                className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                                            >
                                                <option value="" disabled>Select an Inbound</option>
                                                {inbounds.map(inb => (
                                                    <option key={inb.tag} value={inb.tag}>{inb.tag} ({inb.type})</option>
                                                ))}
                                            </select>
                                        </div>
                                        <div className="space-y-1">
                                            <label className="block text-sm font-medium text-slate-400">Flow</label>
                                            <select
                                                value={row.flow}
                                                onChange={e => {
                                                    const value = e.target.value
                                                    setInboundRows(prev => prev.map((r, rIdx) => rIdx === idx ? { ...r, flow: value } : r))
                                                }}
                                                className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                                            >
                                                <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                                                <option value="">none</option>
                                            </select>
                                        </div>
                                        <div className="flex justify-end">
                                            <button
                                                type="button"
                                                onClick={() => {
                                                    if (inboundRows.length === 1) return
                                                    setInboundRows(prev => prev.filter((_, rIdx) => rIdx !== idx))
                                                }}
                                                disabled={inboundRows.length === 1}
                                                className="px-3 py-2 rounded-lg bg-slate-800 text-slate-300 border border-slate-700 hover:bg-slate-700 hover:text-white disabled:opacity-50 disabled:cursor-not-allowed text-sm"
                                                title="Remove inbound"
                                            >
                                                Remove
                                            </button>
                                        </div>
                                    </div>
                                )
                            })}
                        </div>
                        <div className="flex justify-start">
                            <button
                                type="button"
                                onClick={() => setInboundRows(prev => [...prev, { tag: '', flow: 'xtls-rprx-vision' }])}
                                className="px-3 py-2 rounded-lg bg-slate-800 text-slate-300 border border-slate-700 hover:bg-slate-700 hover:text-white text-sm"
                            >
                                Add Inbound
                            </button>
                        </div>
                        {hasEmptyInbound && (
                            <p className="text-xs text-amber-400">Each inbound row must have a selected inbound.</p>
                        )}
                        {hasDuplicateInbound && (
                            <p className="text-xs text-amber-400">Duplicate inbounds are not allowed.</p>
                        )}
                    </div>

                    {isEditing && (
                        <div className="flex items-center gap-2">
                            <input
                                type="checkbox"
                                checked={newUser.enabled !== false}
                                onChange={e => setNewUser({ ...newUser, enabled: e.target.checked })}
                                className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900 cursor-pointer h-4 w-4"
                                id="user-enabled"
                            />
                            <label htmlFor="user-enabled" className="text-sm font-medium text-slate-400 cursor-pointer select-none">
                                Account Enabled
                            </label>
                        </div>
                    )}
                    <div className="grid grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Quota Period</label>
                            <select
                                value={newUser.quota_period}
                                onChange={e => setNewUser({ ...newUser, quota_period: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            >
                                <option value="monthly">Monthly</option>
                                <option value="total">Total (One-time)</option>
                            </select>
                        </div>
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Quota (GB)</label>
                            <input
                                type="text"
                                inputMode="decimal"
                                value={quotaInput}
                                onChange={e => {
                                    const val = e.target.value
                                    setQuotaInput(val)
                                    setNewUser({ ...newUser, quota_limit: parseGbToBytes(val) })
                                }}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                placeholder="0 for unlimited"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Traffic Reset Day</label>
                            <input
                                type="number"
                                min="1" max="31"
                                value={newUser.reset_day}
                                onChange={e => setNewUser({ ...newUser, reset_day: parseInt(e.target.value) })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            />
                            <p className="text-[10px] text-slate-500 mt-1">Day of month (1-31)</p>
                        </div>
                    </div>
                </div>
            </Modal>

            {/* Bulk Create Modal */}
            <Modal
                isOpen={modalState.type === 'bulk'}
                onClose={() => setModalState({ type: null })}
                title="Bulk Generate Users"
                footer={
                    <>
                        <Button variant="ghost" onClick={() => setModalState({ type: null })}>Cancel</Button>
                        <Button variant="primary" onClick={handleBulkCreate}>Generate Users</Button>
                    </>
                }
            >
                <div className="space-y-6">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                        {/* Column 1: Naming & Count */}
                        <div className="space-y-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Inbound (Required)</label>
                                <select
                                    value={bulkConfig.inbound_tag || ''}
                                    onChange={e => setBulkConfig({ ...bulkConfig, inbound_tag: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                >
                                    <option value="">Select Inbound</option>
                                    {inbounds.map((inb: any) => (
                                        <option key={inb.tag} value={inb.tag}>{inb.tag} ({inb.type})</option>
                                    ))}
                                </select>
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Prefix</label>
                                    <input
                                        type="text"
                                        value={bulkConfig.prefix}
                                        onChange={e => setBulkConfig({ ...bulkConfig, prefix: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                        placeholder="user"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Suffix (Optional)</label>
                                    <input
                                        type="text"
                                        value={bulkConfig.suffix}
                                        onChange={e => setBulkConfig({ ...bulkConfig, suffix: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                        placeholder="@example.com"
                                    />
                                </div>
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Count</label>
                                    <input
                                        type="number"
                                        min="1"
                                        value={bulkConfig.count}
                                        onChange={e => setBulkConfig({ ...bulkConfig, count: parseInt(e.target.value) })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Start Index</label>
                                    <input
                                        type="number"
                                        min="0"
                                        value={bulkConfig.start_index}
                                        onChange={e => setBulkConfig({ ...bulkConfig, start_index: parseInt(e.target.value) })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                        disabled={bulkConfig.mode !== 'sequential'}
                                    />
                                </div>
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Pattern</label>
                                <select
                                    value={bulkConfig.mode}
                                    onChange={e => setBulkConfig({ ...bulkConfig, mode: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                >
                                    <option value="sequential">Sequential (prefix-1...)</option>
                                    <option value="random">Random Suffix (prefix-xyz...)</option>
                                </select>
                            </div>
                        </div>

                        {/* Column 2: User Settings */}
                        <div className="space-y-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Flow</label>
                                <select
                                    value={bulkConfig.flow}
                                    onChange={e => setBulkConfig({ ...bulkConfig, flow: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                >
                                    <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                                    <option value="">none</option>
                                </select>
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Quota (GB)</label>
                                <input
                                    type="text"
                                    inputMode="decimal"
                                    value={bulkQuotaInput}
                                    onChange={e => {
                                        const val = e.target.value
                                        setBulkQuotaInput(val)
                                        setBulkConfig({ ...bulkConfig, quota_limit: parseGbToBytes(val) })
                                    }}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                    placeholder="0 for unlimited"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Traffic Reset Day</label>
                                <input
                                    type="number"
                                    min="1" max="31"
                                    value={bulkConfig.reset_day}
                                    onChange={e => setBulkConfig({ ...bulkConfig, reset_day: parseInt(e.target.value) })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                />
                                <p className="text-[10px] text-slate-500 mt-1">Day of month (1-31)</p>
                            </div>
                        </div>
                    </div>
                </div>
            </Modal >

            {/* Usage Report Modal */}
            < Modal
                isOpen={modalState.type === 'usage'}
                onClose={() => setModalState({ type: null })
                }
                title="Usage Report"
                size="lg"
                footer={
                    < Button variant="ghost" onClick={() => setModalState({ type: null })}> Close</Button >
                }
            >
                <div className="space-y-6">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm text-slate-400 mb-1">Start Date</label>
                            <input
                                type="datetime-local"
                                value={usageStart}
                                onChange={e => setUsageStart(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white text-sm"
                            />
                        </div>
                        <div>
                            <label className="block text-sm text-slate-400 mb-1">End Date</label>
                            <input
                                type="datetime-local"
                                value={usageEnd}
                                onChange={e => setUsageEnd(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white text-sm"
                            />
                        </div>
                        <div>
                            <label className="block text-sm text-slate-400 mb-1">Filter Limit (GB)</label>
                            <input
                                type="text"
                                value={usageLimitGb}
                                onChange={e => setUsageLimitGb(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white text-sm"
                                placeholder="0 = show all"
                            />
                        </div>
                        <div className="flex items-end">
                            <Button
                                onClick={fetchUsage}
                                isLoading={loadingUsage}
                                variant="primary"
                                className="w-full"
                                icon={<RefreshCw size={16} />}
                            >
                                Generate Report
                            </Button>
                        </div>
                    </div>

                    <div className="border border-slate-800 rounded-lg overflow-hidden">
                        <table className="w-full text-sm text-left">
                            <thead className="bg-slate-900 text-slate-400 font-semibold text-xs uppercase">
                                <tr>
                                    <th className="p-3">User</th>
                                    <th className="p-3 text-right">Uplink</th>
                                    <th className="p-3 text-right">Downlink</th>
                                    <th className="p-3 text-right">Total</th>
                                    <th className="p-3 text-center">Status</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-slate-800 bg-slate-950/50">
                                {usageData.length === 0 ? (
                                    <tr><td className="p-4 text-center text-slate-500 italic" colSpan={5}>No data available</td></tr>
                                ) : (
                                    usageData.map((row, idx) => (
                                        <tr key={idx} className="hover:bg-slate-800/20">
                                            <td className="p-3 font-medium text-white">{row.name}</td>
                                            <td className="p-3 text-right font-mono text-slate-300">{formatBytes(row.uplink)}</td>
                                            <td className="p-3 text-right font-mono text-slate-300">{formatBytes(row.downlink)}</td>
                                            <td className="p-3 text-right font-mono text-blue-300">{formatBytes(row.total)}</td>
                                            <td className="p-3 text-center">
                                                {row.exceeded ? (
                                                    <Badge variant="error">Exceeded</Badge>
                                                ) : (
                                                    <Badge variant="success">OK</Badge>
                                                )}
                                            </td>
                                        </tr>
                                    ))
                                )}
                            </tbody>
                        </table>
                    </div>
                </div>
            </Modal >

            {/* QR Code Modal */}
            < Modal
                isOpen={modalState.type === 'qr'}
                onClose={() => setModalState({ type: null })}
                title="VLESS Configuration"
                size="sm"
                footer={
                    < Button variant="ghost" className="w-full" onClick={() => setModalState({ type: null })}> Close</Button >
                }
            >
                <div className="flex flex-col items-center space-y-4">
                    {modalState.data && (
                        <>
                            {modalState.data.inbound_tags && modalState.data.inbound_tags.length > 1 && (
                                <div className="w-full flex flex-wrap gap-2">
                                    {modalState.data.inbound_tags.map((tag: string) => (
                                        <button
                                            key={tag}
                                            type="button"
                                            onClick={() => setSelectedQrInbound(tag)}
                                            className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors ${selectedQrInbound === tag
                                                ? 'bg-slate-800 text-white border-slate-700'
                                                : 'bg-slate-900 text-slate-400 border-slate-800 hover:text-slate-200 hover:border-slate-700'
                                                }`}
                                        >
                                            {tag}
                                        </button>
                                    ))}
                                </div>
                            )}
                            <div className="p-4 bg-white rounded-xl shadow-lg w-full">
                                <QRCode
                                    size={256}
                                    style={{ height: "auto", maxWidth: "100%", width: "100%" }}
                                    value={generateVLESSLink(modalState.data, selectedQrInbound)}
                                    viewBox={`0 0 256 256`}
                                />
                            </div>
                            <div className="w-full space-y-2">
                                <label className="text-xs font-semibold text-slate-500 uppercase tracking-wider">Link</label>
                                <div className="flex gap-2">
                                    <input
                                        readOnly
                                        value={generateVLESSLink(modalState.data, selectedQrInbound)}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-xs text-slate-400 font-mono focus:outline-none"
                                    />
                                    <Button
                                        size="sm"
                                        variant="secondary"
                                        onClick={() => handleCopyLink(generateVLESSLink(modalState.data, selectedQrInbound))}
                                        icon={copied ? <Check size={14} /> : <Copy size={14} />}
                                    >
                                        {copied ? 'Copied' : 'Copy'}
                                    </Button>
                                </div>
                            </div>
                        </>
                    )}
                </div>
            </Modal >

            {/* Delete Confirmation Modal */}
            < Modal
                isOpen={modalState.type === 'delete_confirm'}
                onClose={() => setModalState({ type: null })}
                title="Confirm Deletion"
                size="sm"
                footer={
                    <>
                        <Button variant="ghost" onClick={() => setModalState({ type: null })}>Cancel</Button>
                        <Button variant="danger" onClick={confirmDeleteSelected}>Delete Users</Button>
                    </>
                }
            >
                <div className="flex flex-col items-center text-center p-4">
                    <div className="w-12 h-12 rounded-full bg-red-900/20 text-red-500 flex items-center justify-center mb-4">
                        <Trash2 size={24} />
                    </div>
                    <h3 className="text-lg font-semibold text-white mb-2">Delete {selectedUsers.size} Users?</h3>
                    <p className="text-slate-400 text-sm">
                        This action cannot be undone. All selected users will be permanently removed from the system.
                    </p>
                </div>
            </Modal >

            {/* Inbound Selection Modal */}
            <Modal
                isOpen={modalState.type === 'select_inbounds'}
                onClose={() => {
                    setModalState({ type: null })
                    setSelectedInboundsToRemove(new Set())
                }}
                title="Remove User from Inbounds"
                size="sm"
                footer={
                    <>
                        <Button variant="ghost" onClick={() => {
                            setModalState({ type: null })
                            setSelectedInboundsToRemove(new Set())
                        }}>Cancel</Button>
                        <Button
                            variant="danger"
                            onClick={handleRemoveFromSelectedInbounds}
                            disabled={selectedInboundsToRemove.size === 0}
                        >
                            Remove from {selectedInboundsToRemove.size} Inbound(s)
                        </Button>
                    </>
                }
            >
                <div className="space-y-4">
                    <p className="text-slate-400 text-sm">
                        Select which inbound(s) to remove user <span className="text-white font-mono">{modalState.data?.name}</span> from:
                    </p>
                    <div className="space-y-2">
                        {modalState.data?.inbound_tags?.map((tag: string) => (
                            <label key={tag} className="flex items-center gap-3 p-3 bg-slate-950 border border-slate-800 rounded-lg cursor-pointer hover:border-slate-700 transition-colors">
                                <input
                                    type="checkbox"
                                    checked={selectedInboundsToRemove.has(tag)}
                                    onChange={e => {
                                        const newSet = new Set(selectedInboundsToRemove)
                                        if (e.target.checked) {
                                            newSet.add(tag)
                                        } else {
                                            newSet.delete(tag)
                                        }
                                        setSelectedInboundsToRemove(newSet)
                                    }}
                                    className="w-4 h-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <span className="text-white font-mono text-sm">{tag}</span>
                            </label>
                        ))}
                    </div>
                    {selectedInboundsToRemove.size === modalState.data?.inbound_tags?.length && (
                        <div className="bg-amber-500/10 border border-amber-500/20 rounded-lg p-3 text-amber-400 text-xs">
                            ⚠️ All inbounds selected. User will be completely deleted.
                        </div>
                    )}
                </div>
            </Modal>
        </div >
    )
}
