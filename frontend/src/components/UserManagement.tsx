import { useEffect, useState } from 'react'
import { api, UserStatus, CreateUserRequest } from '../api'
import { Users, Plus, Trash2, Upload, RefreshCw, AlertCircle, Pencil, QrCode, ArrowUp, ArrowDown, ArrowUpDown } from 'lucide-react'
import { v4 as uuidv4 } from 'uuid'
import QRCode from 'react-qr-code'

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
    const [users, setUsers] = useState<UserStatus[]>([])
    const [selectedUsers, setSelectedUsers] = useState<Set<string>>(new Set())
    const [showCreateModal, setShowCreateModal] = useState(false)
    const [showBulkModal, setShowBulkModal] = useState(false)
    const [showQRModal, setShowQRModal] = useState(false)
    const [selectedUserForQR, setSelectedUserForQR] = useState<UserStatus | null>(null)
    const [isEditing, setIsEditing] = useState(false)
    const [sortKey, setSortKey] = useState<'user' | 'quota' | 'usage' | 'status' | 'last_seen'>('user')
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
    const [originalName, setOriginalName] = useState<string>('')
    const [showUsageModal, setShowUsageModal] = useState(false)
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
        reset_day: 1
    })
    const [quotaInput, setQuotaInput] = useState<string>('')

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
        reset_day: 1
    })
    const [bulkQuotaInput, setBulkQuotaInput] = useState<string>('')

    const fetchUsers = () => {
        api.getUsers()
            .then(data => {
                setUsers(data)
                // Prune selected users that no longer exist to avoid stale "Delete (1)" badge
                setSelectedUsers(prev => {
                    const allowed = new Set(data.map(u => u.name))
                    const next = new Set<string>()
                    prev.forEach(name => {
                        if (allowed.has(name)) next.add(name)
                    })
                    return next
                })
            })
            .catch(console.error)
    }

    useEffect(() => {
        fetchUsers()
    }, [])

    const sortedUsers = [...users].sort((a, b) => {
        const dir = sortDir === 'asc' ? 1 : -1
        switch (sortKey) {
            case 'quota':
                return ((a.quota_limit || 0) - (b.quota_limit || 0)) * dir
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
            setSortDir('asc')
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
            console.error(err)
            alert('Failed to fetch usage')
        } finally {
            setLoadingUsage(false)
        }
    }

    const handleDeleteSelected = async () => {
        if (!confirm(`Are you sure you want to delete ${selectedUsers.size} users?`)) return

        for (const name of selectedUsers) {
            try {
                await api.deleteUser(name)
            } catch (err) {
                console.error(`Failed to delete ${name}`, err)
            }
        }
        setSelectedUsers(new Set())
        fetchUsers()
    }

    const handleEditClick = (user: UserStatus) => {
        setNewUser({
            name: user.name,
            uuid: user.uuid || '',
            flow: user.flow || '',
            quota_limit: user.quota_limit,
            quota_period: user.quota_period,
            reset_day: user.reset_day
        })
        setQuotaInput(bytesToGbString(user.quota_limit))
        setOriginalName(user.name)
        setIsEditing(true)
        setShowCreateModal(true)
    }

        const handleSaveUser = async () => {
        try {
            if (isEditing) {
                const payload = { ...newUser, original_name: originalName || newUser.name }
                if (!payload.uuid) payload.uuid = uuidv4()
                await api.updateUser(payload)
            } else {
                const payload = { ...newUser }
                if (!payload.uuid) payload.uuid = uuidv4()
                await api.createUser(payload)
            }
            setShowCreateModal(false)
            setNewUser({
                name: '',
                uuid: '',
                flow: 'xtls-rprx-vision',
                quota_limit: 0,
                quota_period: 'monthly',
                reset_day: 1
            })
            setQuotaInput('')
            setOriginalName('')
            setIsEditing(false)
            fetchUsers()
        } catch (err) {
            alert('Failed to save user: ' + err)
        }
    }

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
                    reset_day: bulkConfig.reset_day
                })
            }

            await api.bulkCreateUsers(usersToCreate)
            setShowBulkModal(false)
            fetchUsers()
        } catch (err) {
            alert('Failed to bulk create: ' + err)
        }
    }

    const generateVLESSLink = (user: UserStatus) => {
        // Prefer real UUID/flow coming from backend; fallback to placeholder if missing
        const uuid = user.uuid || "5e18b70f-bdaa-4b8a-8e50-67830e897bc5"
        const flow = user.flow || ""
        const flowParam = flow ? `&flow=${flow}` : ""
        const ip = "149.50.133.58"
        const port = "443"
        const pbk = "4aHK2h-F_LeS5FYsdUqipny0ae67oWcgmlcyfIofon8"
        const sni = "www.cloudflare.com"
        const sid = "0861c24f2c393938"
        const name = `VLESS-${user.name}`

        return `vless://${uuid}@${ip}:${port}?security=reality&encryption=none&pbk=${pbk}&headerType=none&fp=chrome&type=tcp${flowParam}&sni=${sni}&sid=${sid}#${name}`
    }

    return (
        <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white">User Management</h1>
                    <p className="text-slate-400 text-sm mt-1">Manage VLESS users and quotas</p>
                </div>
                <div className="w-full grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(180px,1fr))] md:flex md:flex-wrap md:w-auto">
                    {selectedUsers.size > 0 && (
                        <button
                            onClick={handleDeleteSelected}
                            className="flex items-center justify-center gap-2 px-4 py-2 bg-red-500/10 text-red-400 hover:bg-red-500/20 rounded-lg transition-colors w-full md:w-auto"
                        >
                            <Trash2 size={16} />
                            <span className="text-sm font-medium">Delete ({selectedUsers.size})</span>
                        </button>
                    )}
                    <button
                        onClick={() => setShowUsageModal(true)}
                        className="flex items-center justify-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg transition-colors w-full md:w-auto"
                    >
                        <RefreshCw size={16} />
                        <span className="text-sm font-medium">Usage by range</span>
                    </button>
                    <button
                        onClick={() => setShowBulkModal(true)}
                        className="flex items-center justify-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg transition-colors w-full md:w-auto"
                    >
                        <Upload size={16} />
                        <span className="text-sm font-medium">Bulk Generator</span>
                    </button>
                    <button
                        onClick={() => {
                            setIsEditing(false)
                            setNewUser({
                                name: '',
                                uuid: '',
                                flow: 'xtls-rprx-vision',
                                quota_limit: 0,
                                quota_period: 'monthly',
                                reset_day: 1
                            })
                            setQuotaInput('')
                            setShowCreateModal(true)
                        }}
                        className="flex items-center justify-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors w-full md:w-auto"
                    >
                        <Plus size={16} />
                        <span className="text-sm font-medium">Add User</span>
                    </button>
                </div>
            </div>

            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
                <div className="overflow-x-auto hidden md:block">
                    <table className="w-full text-left border-collapse">
                        <thead>
                            <tr className="bg-slate-950/50 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="p-4 w-10">
                                    <input
                                        type="checkbox"
                                        onChange={handleSelectAll}
                                        checked={users.length > 0 && selectedUsers.size === users.length}
                                        className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900"
                                    />
                                </th>
                                <th className="p-4 font-medium cursor-pointer select-none" onClick={() => toggleSort('user')}>
                                    User {renderSortIcon('user')}
                                </th>
                                <th className="p-4 font-medium text-right cursor-pointer select-none" onClick={() => toggleSort('quota')}>
                                    Quota {renderSortIcon('quota')}
                                </th>
                                <th className="p-4 font-medium text-right cursor-pointer select-none" onClick={() => toggleSort('usage')}>
                                    Usage {renderSortIcon('usage')}
                                </th>
                                <th className="p-4 font-medium text-right cursor-pointer select-none" onClick={() => toggleSort('last_seen')}>
                                    Last Seen {renderSortIcon('last_seen')}
                                </th>
                                <th className="p-4 font-medium text-center cursor-pointer select-none" onClick={() => toggleSort('status')}>
                                    Status {renderSortIcon('status')}
                                </th>
                                <th className="p-4 font-medium text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-800">
                            {sortedUsers.map(user => (
                                <tr key={user.name} className="hover:bg-slate-800/50 transition-colors group">
                                    <td className="p-4">
                                        <input
                                            type="checkbox"
                                            checked={selectedUsers.has(user.name)}
                                            onChange={() => handleSelectUser(user.name)}
                                            className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900"
                                        />
                                    </td>
                                    <td className="p-4">
                                        <div className="flex items-center gap-3">
                                            <div className="w-8 h-8 rounded-full bg-slate-800 flex items-center justify-center text-slate-400">
                                                <Users size={14} />
                                            </div>
                                            <div>
                                                <div className="font-medium text-slate-200">{user.name}</div>
                                                <div className="text-xs text-slate-500">Cycle Start: Day {user.reset_day}</div>
                                            </div>
                                        </div>
                                    </td>
                                    <td className="p-4 text-right font-mono text-slate-400 text-sm">
                                        {user.quota_limit > 0 ? formatBytes(user.quota_limit) : 'Unlimited'}
                                    </td>
                                    <td className="p-4 text-right">
                                        <div className="font-mono text-white text-sm">{formatBytes(user.total)}</div>
                                        {user.quota_limit > 0 && (
                                            <div className="w-24 ml-auto h-1 bg-slate-700 rounded-full mt-1 overflow-hidden">
                                                <div
                                                    className={`h-full ${user.total > user.quota_limit ? 'bg-red-500' : 'bg-blue-500'}`}
                                                    style={{ width: `${Math.min((user.total / user.quota_limit) * 100, 100)}%` }}
                                                />
                                            </div>
                                        )}
                                    </td>
                                    <td className="p-4 text-right text-xs text-slate-400 font-mono">
                                        {user.last_seen ? new Date(user.last_seen * 1000).toLocaleString() : '-'}
                                    </td>
                                    <td className="p-4 text-center">
                                        {user.quota_limit > 0 && user.total > user.quota_limit ? (
                                            <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-500/10 text-red-400 border border-red-500/20">
                                                <AlertCircle size={12} /> Exceeded
                                            </span>
                                        ) : (
                                            <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                                                Active
                                            </span>
                                        )}
                                    </td>
                                    <td className="p-4 text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <button
                                                onClick={() => handleEditClick(user)}
                                                className="p-2.5 rounded-lg bg-slate-800/70 text-slate-200 hover:text-blue-400"
                                                title="Edit User"
                                            >
                                                <Pencil size={16} />
                                            </button>
                                            <button
                                                onClick={() => {
                                                    setSelectedUserForQR(user)
                                                    setShowQRModal(true)
                                                }}
                                                className="p-2.5 rounded-lg bg-slate-800/70 text-slate-200 hover:text-blue-400"
                                                title="View QR Code"
                                            >
                                                <QrCode size={16} />
                                            </button>
                                            <button
                                                onClick={() => {
                                                    if (confirm('Delete user?')) api.deleteUser(user.name).then(fetchUsers)
                                                }}
                                                className="p-2.5 rounded-lg bg-slate-800/70 text-slate-200 hover:text-red-400"
                                                title="Delete User"
                                            >
                                                <Trash2 size={16} />
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>

                {/* Mobile cards */}
                <div className="md:hidden divide-y divide-slate-800">
                    {sortedUsers.map(user => {
                        const exceeded = user.quota_limit > 0 && user.total > user.quota_limit
                        return (
                            <div key={user.name} className="p-4 space-y-3">
                                <div className="flex items-center justify-between gap-3">
                                    <div className="flex items-center gap-3">
                                        <input
                                            type="checkbox"
                                            checked={selectedUsers.has(user.name)}
                                            onChange={() => handleSelectUser(user.name)}
                                            className="rounded border-slate-700 bg-slate-800 text-blue-600 focus:ring-offset-slate-900"
                                        />
                                        <div className="text-lg font-semibold text-white leading-tight break-words">{user.name}</div>
                                    </div>
                                    <div className="flex items-center gap-2">
                                    {exceeded ? (
                                        <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-[11px] font-semibold bg-red-500/10 text-red-300 border border-red-500/30">
                                            <AlertCircle size={12} /> Exceeded
                                        </span>
                                    ) : (
                                        <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-[11px] font-semibold bg-emerald-500/15 text-emerald-300 border border-emerald-500/30">
                                            Active
                                        </span>
                                    )}
                                        <button
                                            onClick={() => handleEditClick(user)}
                                            className="p-2.5 rounded-lg bg-slate-800/70 text-slate-200 hover:text-blue-400"
                                            title="Edit User"
                                        >
                                            <Pencil size={16} />
                                        </button>
                                        <button
                                            onClick={() => {
                                                setSelectedUserForQR(user)
                                                setShowQRModal(true)
                                            }}
                                            className="p-2.5 rounded-lg bg-slate-800/70 text-slate-200 hover:text-blue-400"
                                            title="View QR Code"
                                        >
                                            <QrCode size={16} />
                                        </button>
                                        <button
                                            onClick={() => {
                                                if (confirm('Delete user?')) api.deleteUser(user.name).then(fetchUsers)
                                            }}
                                            className="p-2.5 rounded-lg bg-slate-800/70 text-slate-200 hover:text-red-400"
                                            title="Delete User"
                                        >
                                            <Trash2 size={16} />
                                        </button>
                                    </div>
                                </div>

                                <div className="flex items-center justify-between text-xs text-slate-500">
                                    <span>Last Seen {user.last_seen ? new Date(user.last_seen * 1000).toLocaleString() : '-'}</span>
                                    <span className="text-slate-400">Cycle day: {user.reset_day}</span>
                                </div>

                                <div className="space-y-2">
                                    <div className="flex items-center justify-between text-sm text-slate-200">
                                        <span className="font-mono">{formatBytes(user.total)}</span>
                                        <span className="text-slate-400 text-xs">Usage</span>
                                        <span className="font-mono">{user.quota_limit > 0 ? formatBytes(user.quota_limit) : 'Unlimited'}</span>
                                    </div>
                                    {user.quota_limit > 0 && (
                                        <div className="w-full h-1.5 bg-slate-800 rounded-full overflow-hidden">
                                            <div
                                                className={`h-full ${exceeded ? 'bg-red-500' : 'bg-blue-500'}`}
                                                style={{ width: `${Math.min((user.total / user.quota_limit) * 100, 100)}%` }}
                                            />
                                        </div>
                                    )}
                                </div>
                            </div>
                        )
                    })}
                    {sortedUsers.length === 0 && (
                        <div className="p-6 text-center text-slate-500 text-sm">No users</div>
                    )}
                </div>
            </div>

            {/* Create/Edit Modal */}
            {showCreateModal && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-md p-6 shadow-xl">
                        <h2 className="text-xl font-bold text-white mb-4">{isEditing ? 'Edit User' : 'Add New User'}</h2>
                        <div className="space-y-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Name</label>
                                <input
                                    type="text"
                                    value={newUser.name}
                                    onChange={e => setNewUser({ ...newUser, name: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white focus:ring-2 focus:ring-blue-500 outline-none"
                                    placeholder="display name / identifier"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">UUID</label>
                                <div className="flex gap-2">
                                    <input
                                        type="text"
                                        value={newUser.uuid}
                                        onChange={e => setNewUser({ ...newUser, uuid: e.target.value })}
                                        className="flex-1 bg-slate-950 border border-slate-800 rounded-lg p-2 text-white font-mono text-sm focus:ring-2 focus:ring-blue-500 outline-none"
                                        placeholder="UUID"
                                    />
                                    {!isEditing && (
                                        <button
                                            onClick={() => setNewUser({ ...newUser, uuid: uuidv4() })}
                                            className="px-3 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg"
                                        >
                                            <RefreshCw size={16} />
                                        </button>
                                    )}
                                </div>
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Flow</label>
                                <select
                                    value={newUser.flow}
                                    onChange={e => setNewUser({ ...newUser, flow: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                >
                                    <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                                    <option value="">none</option>
                                </select>
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Quota (GB, admite . o ,)</label>
                                    <input
                                        type="text"
                                        inputMode="decimal"
                                        value={quotaInput}
                                        onChange={e => {
                                            const val = e.target.value
                                            setQuotaInput(val)
                                            setNewUser({ ...newUser, quota_limit: parseGbToBytes(val) })
                                        }}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                        placeholder="Ej: 10.5"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Traffic Reset Day</label>
                                    <input
                                        type="number"
                                        min="1" max="31"
                                        value={newUser.reset_day}
                                        onChange={e => setNewUser({ ...newUser, reset_day: parseInt(e.target.value) })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                    />
                                    <p className="text-xs text-slate-500 mt-1">Day of month when usage resets (1-31)</p>
                                </div>
                            </div>
                        </div>
                        <div className="flex justify-end gap-3 mt-6">
                            <button
                                onClick={() => setShowCreateModal(false)}
                                className="px-4 py-2 text-slate-400 hover:text-white"
                            >
                                Cancel
                            </button>
                            <button
                                onClick={handleSaveUser}
                                className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg"
                            >
                                {isEditing ? 'Save Changes' : 'Create User'}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Bulk Modal */}
            {showBulkModal && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-2xl p-6 shadow-xl">
                        <h2 className="text-xl font-bold text-white mb-4">Bulk Generate Users</h2>

                        <div className="grid grid-cols-2 gap-6 mb-4">
                            <div className="space-y-4">
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Username Prefix</label>
                                    <input
                                        type="text"
                                        value={bulkConfig.prefix}
                                        onChange={e => setBulkConfig({ ...bulkConfig, prefix: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                        placeholder="e.g. client"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Suffix (Optional)</label>
                                    <input
                                        type="text"
                                        value={bulkConfig.suffix}
                                        onChange={e => setBulkConfig({ ...bulkConfig, suffix: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                        placeholder="e.g. @example.com"
                                    />
                                </div>
                                <div className="grid grid-cols-2 gap-4">
                                    <div>
                                        <label className="block text-sm font-medium text-slate-400 mb-1">Count</label>
                                        <input
                                            type="number"
                                            min="1"
                                            value={bulkConfig.count}
                                            onChange={e => setBulkConfig({ ...bulkConfig, count: parseInt(e.target.value) })}
                                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-slate-400 mb-1">Start Index</label>
                                        <input
                                            type="number"
                                            min="0"
                                            value={bulkConfig.start_index}
                                            onChange={e => setBulkConfig({ ...bulkConfig, start_index: parseInt(e.target.value) })}
                                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                            disabled={bulkConfig.mode !== 'sequential'}
                                        />
                                    </div>
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Pattern</label>
                                    <select
                                        value={bulkConfig.mode}
                                        onChange={e => setBulkConfig({ ...bulkConfig, mode: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                    >
                                        <option value="sequential">Sequential (prefix-1, prefix-2...)</option>
                                        <option value="random">Random Suffix (prefix-x7z...)</option>
                                    </select>
                                </div>
                            </div>
                            <div className="space-y-4">
                                <h3 className="text-sm font-medium text-slate-300">Settings</h3>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Flow</label>
                                    <select
                                        value={bulkConfig.flow}
                                        onChange={e => setBulkConfig({ ...bulkConfig, flow: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                    >
                                        <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                                        <option value="">none</option>
                                    </select>
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Quota (GB, admite . o ,)</label>
                                    <input
                                        type="text"
                                        inputMode="decimal"
                                        value={bulkQuotaInput}
                                        onChange={e => {
                                            const val = e.target.value
                                            setBulkQuotaInput(val)
                                            setBulkConfig({ ...bulkConfig, quota_limit: parseGbToBytes(val) })
                                        }}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                        placeholder="Ej: 10.5"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Traffic Reset Day</label>
                                    <input
                                        type="number"
                                        min="1" max="31"
                                        value={bulkConfig.reset_day}
                                        onChange={e => setBulkConfig({ ...bulkConfig, reset_day: parseInt(e.target.value) })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none"
                                    />
                                </div>
                            </div>
                        </div>

                        <div className="flex justify-end gap-3 mt-6">
                            <button
                                onClick={() => setShowBulkModal(false)}
                                className="px-4 py-2 text-slate-400 hover:text-white"
                            >
                                Cancel
                            </button>
                            <button
                                onClick={handleBulkCreate}
                                className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg"
                            >
                                Generate Users
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Usage by Range Modal */}
            {showUsageModal && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-3xl p-6 shadow-xl space-y-4">
                        <div className="flex items-center justify-between">
                            <div>
                                <h2 className="text-xl font-bold text-white">Usage by range</h2>
                                <p className="text-slate-400 text-sm">Select a time range to see per-user traffic</p>
                            </div>
                            <button onClick={() => setShowUsageModal(false)} className="text-slate-400 hover:text-white">Close</button>
                        </div>
                        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                            <div className="md:col-span-2">
                                <label className="block text-sm text-slate-400 mb-1">Start (datetime)</label>
                                <input
                                    type="datetime-local"
                                    value={usageStart}
                                    onChange={e => setUsageStart(e.target.value)}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white"
                                />
                            </div>
                            <div className="md:col-span-2">
                                <label className="block text-sm text-slate-400 mb-1">End (datetime)</label>
                                <input
                                    type="datetime-local"
                                    value={usageEnd}
                                    onChange={e => setUsageEnd(e.target.value)}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white"
                                />
                            </div>
                            <div>
                                <label className="block text-sm text-slate-400 mb-1">Limit (GB, optional)</label>
                                <input
                                    type="text"
                                    value={usageLimitGb}
                                    onChange={e => setUsageLimitGb(e.target.value)}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white"
                                    placeholder="0 = no limit"
                                />
                            </div>
                            <div className="flex items-end">
                                <button
                                    onClick={fetchUsage}
                                    className="w-full px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg flex items-center justify-center gap-2 disabled:opacity-50"
                                    disabled={loadingUsage}
                                >
                                    <RefreshCw size={16} className={loadingUsage ? 'animate-spin' : ''} />
                                    Fetch
                                </button>
                            </div>
                        </div>
                        <div className="overflow-x-auto max-h-96">
                            <table className="min-w-full text-sm text-slate-200">
                                <thead className="text-xs text-slate-400 uppercase">
                                    <tr>
                                        <th className="text-left py-2 pr-4">User</th>
                                        <th className="text-right py-2 pr-4">Uplink</th>
                                        <th className="text-right py-2 pr-4">Downlink</th>
                                        <th className="text-right py-2 pr-4">Total</th>
                                        <th className="text-center py-2 pr-4">Status</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {usageData.map((row, idx) => (
                                        <tr key={idx} className="border-t border-slate-800">
                                            <td className="py-2 pr-4">{row.name}</td>
                                            <td className="py-2 pr-4 text-right font-mono">{formatBytes(row.uplink)}</td>
                                            <td className="py-2 pr-4 text-right font-mono">{formatBytes(row.downlink)}</td>
                                            <td className="py-2 pr-4 text-right font-mono">{formatBytes(row.total)}</td>
                                            <td className="py-2 pr-4 text-center">
                                                {row.exceeded ? <span className="text-red-400 text-xs">Exceeded</span> : <span className="text-emerald-400 text-xs">OK</span>}
                                            </td>
                                        </tr>
                                    ))}
                                    {usageData.length === 0 && (
                                        <tr><td className="py-3 text-center text-slate-500" colSpan={5}>No data</td></tr>
                                    )}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>
            )}

            {/* QR Modal */}
            {showQRModal && selectedUserForQR && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-sm p-6 shadow-xl flex flex-col items-center">
                        <h2 className="text-xl font-bold text-white mb-4">VLESS Config</h2>
                        <div className="w-full bg-white p-4 rounded-lg mb-4 flex items-center justify-center">
                            <QRCode value={generateVLESSLink(selectedUserForQR)} className="w-full h-auto" />
                        </div>
                        <p className="text-slate-400 text-xs text-center break-all font-mono bg-slate-950 p-2 rounded w-full mb-4">
                            {generateVLESSLink(selectedUserForQR)}
                        </p>
                        <button
                            onClick={() => setShowQRModal(false)}
                            className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg w-full"
                        >
                            Close
                        </button>
                    </div>
                </div>
            )}
        </div>
    )
}
