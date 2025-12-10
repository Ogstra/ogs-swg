import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { Plus, Trash2, Copy, Check, Settings, Edit, ArrowUp, ArrowDown, Shield, ArrowUpDown, QrCode } from 'lucide-react'
import QRCode from 'react-qr-code'

interface WireGuardPeer {
    public_key: string
    private_key?: string
    allowed_ips: string
    endpoint?: string
    alias?: string
    name?: string // legacy
    preshared_key?: string
    persistent_keepalive?: number
    qr_available?: boolean
    traffic?: { rx: number; tx: number }
    stats?: {
        latest_handshake: number
        transfer_rx: number
        transfer_tx: number
        endpoint: string
    }
}

interface WireGuardInterface {
    address: string
    bind_address?: string
    private_key: string
    listen_port: number
    post_up?: string
    post_down?: string
    mtu?: number
    dns?: string
}

function formatBytes(bytes: number, decimals = 2) {
    if (!+bytes) return '0 B'
    const k = 1024
    const dm = decimals < 0 ? 0 : decimals
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

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

export default function WireGuard() {
    const [peers, setPeers] = useState<WireGuardPeer[]>([])
    const [interfaceConfig, setInterfaceConfig] = useState<WireGuardInterface | null>(null)
    const [showPeerModal, setShowPeerModal] = useState(false)
    const [showInterfaceModal, setShowInterfaceModal] = useState(false)

    // Edit/Create State
    const [editingPeer, setEditingPeer] = useState<WireGuardPeer | null>(null)
    const [newName, setNewName] = useState('')
    const [newIp, setNewIp] = useState('')
    const [newEndpoint, setNewEndpoint] = useState('')
    const [generatedPeer, setGeneratedPeer] = useState<WireGuardPeer | null>(null)
    const [generatedConfig, setGeneratedConfig] = useState('')
    const [copied, setCopied] = useState(false)

    // Interface Edit State
    const [editInterface, setEditInterface] = useState<WireGuardInterface | null>(null)
    const [sortKey, setSortKey] = useState<'alias' | 'ip' | 'endpoint' | 'traffic' | 'handshake'>('alias')
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

    // Config modal state
    const [configModalPeer, setConfigModalPeer] = useState<WireGuardPeer | null>(null)
    const [configText, setConfigText] = useState('')
    const [configLoading, setConfigLoading] = useState(false)
    const [copiedConfig, setCopiedConfig] = useState(false)
    const [pendingRestart, setPendingRestart] = useState(false)
    const [systemctlAvailable, setSystemctlAvailable] = useState<boolean | undefined>(undefined)
    const [manualPrivateKey, setManualPrivateKey] = useState('')

    const fetchData = useCallback(() => {
        api.getWireGuardPeers()
            .then(data => {
                setPeers(Array.isArray(data) ? data : [])
            })
            .catch(err => {
                console.error(err)
                setPeers([])
            })

        api.getWireGuardInterface()
            .then(cfg => setInterfaceConfig(cfg || null))
            .catch(err => {
                console.error(err)
                setInterfaceConfig(null)
            })

        api.getSystemStatus()
            .then(status => {
                setPendingRestart(!!status.wireguard_pending_restart)
                setSystemctlAvailable(status.systemctl_available)
            })
            .catch(err => {
                console.error(err)
                setPendingRestart(false)
            })
    }, [])

    useEffect(() => {
        fetchData()
        const interval = setInterval(fetchData, 5000) // Refresh stats every 5s
        return () => clearInterval(interval)
    }, [fetchData])

    const handleCreatePeer = async () => {
        if (!newName.trim()) {
            alert('Alias is required')
            return
        }
        try {
            const peer = await api.createWireGuardPeer({ alias: newName, ip: newIp, endpoint: newEndpoint })
            setGeneratedPeer(peer)
            setNewName('')
            setNewIp('')
            setNewEndpoint('')
            try {
                const cfg = await api.getWireGuardPeerConfig(peer.public_key)
                setGeneratedConfig(cfg.config)
            } catch (err) {
                console.error(err)
                setGeneratedConfig('')
            }
            fetchData()
        } catch (err) {
            alert('Failed to create peer: ' + err)
        }
    }

    const handleUpdatePeer = async () => {
        if (!editingPeer) return
        try {
            const { persistent_keepalive, private_key, ...rest } = editingPeer as any
            await api.updateWireGuardPeer(editingPeer.public_key, rest)
            setEditingPeer(null)
            setShowPeerModal(false)
            fetchData()
        } catch (err) {
            alert('Failed to update peer: ' + err)
        }
    }

    const handleDeletePeer = async (publicKey: string) => {
        if (!confirm('Delete this peer?')) return
        try {
            await api.deleteWireGuardPeer(publicKey)
            fetchData()
        } catch (err) {
            alert('Failed to delete peer: ' + err)
        }
    }

    const handleUpdateInterface = async () => {
        if (!editInterface) return
        try {
            await api.updateWireGuardInterface(editInterface)
            setInterfaceConfig(editInterface)
            setShowInterfaceModal(false)
        } catch (err) {
            alert('Failed to update interface: ' + err)
        }
    }

    const copyToClipboard = (text: string) => {
        navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
    }

    const primaryAllowedIp = (allowed: string) => (allowed.split(',')[0] || '').trim()

    const sortedPeers = useMemo(() => {
        const dir = sortDir === 'asc' ? 1 : -1
        return [...peers].sort((a, b) => {
            switch (sortKey) {
                case 'endpoint': {
                    const ea = (a.stats?.endpoint || a.endpoint || '').toLowerCase()
                    const eb = (b.stats?.endpoint || b.endpoint || '').toLowerCase()
                    return ea.localeCompare(eb) * dir
                }
                case 'traffic': {
                    const ta = (a.stats?.transfer_rx || 0) + (a.stats?.transfer_tx || 0)
                    const tb = (b.stats?.transfer_rx || 0) + (b.stats?.transfer_tx || 0)
                    return (ta - tb) * dir
                }
                case 'handshake':
                    return ((a.stats?.latest_handshake || 0) - (b.stats?.latest_handshake || 0)) * dir
                case 'ip': {
                    const ipToNum = (ip: string) => {
                        const base = primaryAllowedIp(ip).split('/')[0]
                        const parts = base.split('.').map(p => parseInt(p, 10))
                        if (parts.length !== 4 || parts.some(isNaN)) return 0
                        return (parts[0] << 24) + (parts[1] << 16) + (parts[2] << 8) + parts[3]
                    }
                    return (ipToNum(a.allowed_ips) - ipToNum(b.allowed_ips)) * dir
                }
                case 'alias':
                default: {
                    const na = (a.alias || a.name || '').toLowerCase()
                    const nb = (b.alias || b.name || '').toLowerCase()
                    return na.localeCompare(nb) * dir
                }
            }
        })
    }, [peers, sortDir, sortKey])

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

    const handleRestartWireGuard = async () => {
        try {
            await api.restartService('wireguard')
            setPendingRestart(false)
            fetchData()
        } catch (err) {
            alert('Failed to restart WireGuard: ' + err)
        }
    }

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white">WireGuard</h1>
                    <p className="text-slate-400 text-sm mt-1">Manage WireGuard peers and settings</p>
                </div>
                {pendingRestart && (
                    <div className="flex items-center gap-3 bg-amber-500/10 border border-amber-500/20 text-amber-200 px-4 py-2 rounded-lg">
                        <span className="text-sm font-medium">Changes pending restart</span>
                        <button
                            onClick={handleRestartWireGuard}
                            disabled={systemctlAvailable === false}
                            className="px-3 py-1.5 bg-amber-500 text-black rounded font-medium text-sm hover:bg-amber-400 transition-colors disabled:opacity-50"
                        >
                            Restart WireGuard
                        </button>
                    </div>
                )}
                <div className="flex flex-wrap gap-3">
                    <button
                        onClick={() => {
                            setEditInterface(interfaceConfig ?? { address: '', private_key: '', listen_port: 51820, post_up: '', post_down: '', mtu: 1420, dns: '' })
                            setShowInterfaceModal(true)
                        }}
                        className="flex items-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg transition-colors font-medium text-sm border border-slate-700"
                    >
                        <Settings size={16} />
                        Interface Config
                    </button>
                    <button
                        onClick={() => {
                            setEditingPeer(null)
                            setGeneratedPeer(null)
                            setGeneratedConfig('')
                            setNewIp('')
                            setNewEndpoint('')
                            setShowPeerModal(true)
                        }}
                        className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors font-medium text-sm shadow-lg shadow-blue-500/20"
                    >
                        <Plus size={16} />
                        Add Peer
                    </button>
                </div>
            </div>

            {/* Interface Info Cards */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex flex-col justify-between">
                    <div className="flex items-center gap-3 mb-2">
                        <div className="p-2 bg-slate-950 border border-slate-800 rounded-lg text-slate-400">
                            <Shield size={20} />
                        </div>
                        <p className="text-sm font-medium text-slate-400">Interface Address</p>
                    </div>
                    <div>
                        <p className="text-xl text-white font-bold font-mono tracking-tight">{interfaceConfig?.address || '-'}</p>
                        {interfaceConfig?.bind_address && <p className="text-xs text-slate-500 mt-1 font-mono">Bind: {interfaceConfig.bind_address}</p>}
                    </div>
                </div>
                <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex flex-col justify-between">
                    <div className="flex items-center gap-3 mb-2">
                        <div className="p-2 bg-slate-950 border border-slate-800 rounded-lg text-slate-400">
                            <Settings size={20} />
                        </div>
                        <p className="text-sm font-medium text-slate-400">Port</p>
                    </div>
                    <p className="text-xl text-white font-bold font-mono tracking-tight">{interfaceConfig?.listen_port || 51820}</p>
                </div>
                <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex flex-col justify-between">
                    <div className="flex items-center gap-3 mb-2">
                        <div className="p-2 bg-slate-950 border border-slate-800 rounded-lg text-slate-400">
                            <ArrowUpDown size={20} />
                        </div>
                        <p className="text-sm font-medium text-slate-400">MTU</p>
                    </div>
                    <p className="text-xl text-white font-bold font-mono tracking-tight">{interfaceConfig?.mtu || 1420}</p>
                </div>
            </div>

            {/* Peers Table */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
                <div className="overflow-x-auto hidden md:block">
                    <table className="w-full text-left border-collapse">
                        <thead>
                            <tr className="bg-slate-950/50 border-b border-slate-800 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('handshake')}>
                                    Last Seen {renderSortIcon('handshake')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('alias')}>
                                    Name/Alias {renderSortIcon('alias')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('ip')}>
                                    Allowed IPs {renderSortIcon('ip')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('endpoint')}>
                                    Endpoint {renderSortIcon('endpoint')}
                                </th>
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('traffic')}>
                                    Data Usage {renderSortIcon('traffic')}
                                </th>
                                <th className="p-4 font-semibold text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-800">
                            {(sortedPeers || []).map(peer => {
                                const isOnline = peer.stats && (Date.now() / 1000 - peer.stats.latest_handshake) < 180 // 3 mins
                                return (
                                    <tr key={peer.public_key} className="hover:bg-slate-800/30 transition-colors">
                                        <td className="p-4">
                                            <div className="flex items-center gap-3">
                                                <div className={`w-2.5 h-2.5 rounded-full ring-2 ring-slate-900 ${isOnline ? 'bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.4)]' : 'bg-slate-700'}`}></div>
                                                <span className={`text-xs font-medium ${isOnline ? 'text-emerald-400' : 'text-slate-500'}`}>
                                                    {peer.stats?.latest_handshake ? formatTimeAgo(peer.stats.latest_handshake) : 'Never'}
                                                </span>
                                            </div>
                                        </td>
                                        <td className="p-4">
                                            <div className="font-semibold text-slate-200">{peer.alias || peer.name || '-'}</div>
                                        </td>
                                        <td className="p-4 font-mono text-slate-400 text-xs">
                                            {peer.allowed_ips}
                                        </td>
                                        <td className="p-4 font-mono text-slate-400 text-xs">
                                            {peer.stats?.endpoint || peer.endpoint || '-'}
                                        </td>
                                        <td className="p-4">
                                            <div className="flex flex-col gap-1 text-[11px] font-mono">
                                                <div className="flex items-center gap-1.5 text-emerald-400">
                                                    <ArrowUp size={12} strokeWidth={3} />
                                                    {formatBytes(peer.stats?.transfer_tx || 0)}
                                                </div>
                                                <div className="flex items-center gap-1.5 text-blue-400">
                                                    <ArrowDown size={12} strokeWidth={3} />
                                                    {formatBytes(peer.stats?.transfer_rx || 0)}
                                                </div>
                                            </div>
                                        </td>
                                        <td className="p-4 text-right">
                                            <div className="flex items-center justify-end gap-2">
                                                <button
                                                    onClick={() => {
                                                        setEditingPeer(peer)
                                                        setShowPeerModal(true)
                                                    }}
                                                    className="p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-blue-400 hover:bg-slate-700 border border-slate-700 transition-all"
                                                    title="Edit Peer"
                                                >
                                                    <Edit size={16} />
                                                </button>
                                                <button
                                                    onClick={() => {
                                                        setConfigModalPeer(peer)
                                                        setConfigText('')
                                                        setManualPrivateKey('')
                                                        setConfigLoading(true)
                                                        api.getWireGuardPeerConfig(peer.public_key)
                                                            .then(res => setConfigText(res.config))
                                                            .catch(err => {
                                                                console.error(err)
                                                                setConfigText('')
                                                            })
                                                            .finally(() => setConfigLoading(false))
                                                    }}
                                                    className={`p-2 rounded-lg border transition-all ${peer.qr_available ? 'bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 border-slate-700' : 'bg-slate-900 text-slate-600 border-slate-800 cursor-not-allowed'}`}
                                                    title="View config / QR"
                                                    disabled={!peer.qr_available}
                                                >
                                                    <QrCode size={16} />
                                                </button>
                                                <button
                                                    onClick={() => handleDeletePeer(peer.public_key)}
                                                    className="p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-red-400 hover:bg-slate-700 border border-slate-700 transition-all"
                                                    title="Delete Peer"
                                                >
                                                    <Trash2 size={16} />
                                                </button>
                                            </div>
                                        </td>
                                    </tr>
                                )
                            })}
                            {peers.length === 0 && (
                                <tr>
                                    <td colSpan={6} className="p-12 text-center text-slate-500">
                                        <Shield size={48} className="mx-auto mb-4 opacity-20" />
                                        <p>No WireGuard peers found.</p>
                                        <button
                                            onClick={() => setShowPeerModal(true)}
                                            className="mt-4 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-sm transition-colors"
                                        >
                                            Create your first peer
                                        </button>
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>

                {/* Mobile Cards */}
                <div className="md:hidden divide-y divide-slate-800">
                    {(sortedPeers || []).map(peer => {
                        const isOnline = peer.stats && (Date.now() / 1000 - peer.stats.latest_handshake) < 180
                        return (
                            <div key={peer.public_key} className="p-4 space-y-4">
                                <div className="flex items-start justify-between">
                                    <div className="space-y-1">
                                        <div className="font-bold text-slate-200">{peer.alias || peer.name || 'Unnamed Peer'}</div>
                                        <div className="flex items-center gap-2">
                                            <div className={`w-2 h-2 rounded-full ${isOnline ? 'bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.4)]' : 'bg-slate-700'}`}></div>
                                            <span className="text-xs text-slate-500">
                                                {peer.stats?.latest_handshake ? formatTimeAgo(peer.stats.latest_handshake) : 'Never'}
                                            </span>
                                        </div>
                                    </div>
                                    <div className="flex gap-2">
                                        <button
                                            onClick={() => {
                                                setEditingPeer(peer)
                                                setShowPeerModal(true)
                                            }}
                                            className="p-2 rounded-lg bg-slate-800 text-slate-300 border border-slate-700"
                                        >
                                            <Edit size={16} />
                                        </button>
                                        <button
                                            onClick={() => {
                                                setConfigModalPeer(peer)
                                                setConfigText('')
                                                setManualPrivateKey('')
                                                setConfigLoading(true)
                                                api.getWireGuardPeerConfig(peer.public_key)
                                                    .then(res => setConfigText(res.config))
                                                    .catch(err => {
                                                        console.error(err)
                                                        setConfigText('')
                                                    })
                                                    .finally(() => setConfigLoading(false))
                                            }}
                                            className="p-2 rounded-lg bg-slate-800 text-slate-300 border border-slate-700"
                                            disabled={!peer.qr_available}
                                        >
                                            <QrCode size={16} />
                                        </button>
                                        <button
                                            onClick={() => handleDeletePeer(peer.public_key)}
                                            className="p-2 rounded-lg bg-slate-800 text-red-400 border border-slate-700"
                                        >
                                            <Trash2 size={16} />
                                        </button>
                                    </div>
                                </div>

                                <div className="grid grid-cols-2 gap-4 text-xs">
                                    <div>
                                        <p className="text-slate-500 mb-1">Allowed IPs</p>
                                        <p className="font-mono text-slate-300 break-all">{peer.allowed_ips}</p>
                                    </div>
                                    <div>
                                        <p className="text-slate-500 mb-1">Endpoint</p>
                                        <p className="font-mono text-slate-300 break-all">{peer.stats?.endpoint || peer.endpoint || '-'}</p>
                                    </div>
                                </div>

                                <div className="bg-slate-950/50 rounded-lg p-3 grid grid-cols-2 gap-2">
                                    <div className="flex items-center gap-2 text-emerald-400 text-xs font-mono">
                                        <ArrowUp size={14} />
                                        {formatBytes(peer.stats?.transfer_tx || 0)}
                                    </div>
                                    <div className="flex items-center gap-2 text-blue-400 text-xs font-mono">
                                        <ArrowDown size={14} />
                                        {formatBytes(peer.stats?.transfer_rx || 0)}
                                    </div>
                                </div>
                            </div>
                        )
                    })}
                    {peers.length === 0 && (
                        <div className="p-8 text-center text-slate-500">
                            <p>No WireGuard peers found.</p>
                            <button
                                onClick={() => setShowPeerModal(true)}
                                className="mt-4 px-4 py-2 bg-slate-800 text-slate-200 rounded-lg text-sm"
                            >
                                Add Peer
                            </button>
                        </div>
                    )}
                </div>
            </div>

            {/* Peer Modal (Create/Edit) */}
            {showPeerModal && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-md p-6 shadow-xl">
                        <h2 className="text-xl font-bold text-white mb-4">
                            {editingPeer ? 'Edit Peer' : 'Add WireGuard Peer'}
                        </h2>

                        {editingPeer ? (
                            <div className="space-y-4">
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Public Key</label>
                                    <input
                                        type="text"
                                        value={editingPeer.public_key}
                                        readOnly
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-slate-400 outline-none font-mono text-xs cursor-not-allowed"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Alias</label>
                                    <input
                                        type="text"
                                        value={editingPeer.alias || ''}
                                        onChange={e => setEditingPeer({ ...editingPeer, alias: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="client-alias"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Allowed IPs</label>
                                    <input
                                        type="text"
                                        value={editingPeer.allowed_ips}
                                        onChange={e => setEditingPeer({ ...editingPeer, allowed_ips: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="10.100.0.2/32, 10.0.0.0/24"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Endpoint (Optional)</label>
                                    <input
                                        type="text"
                                        value={editingPeer.endpoint || ''}
                                        onChange={e => setEditingPeer({ ...editingPeer, endpoint: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="x.x.x.x:51820"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Preshared Key (Optional)</label>
                                    <input
                                        type="text"
                                        value={editingPeer.preshared_key || ''}
                                        onChange={e => setEditingPeer({ ...editingPeer, preshared_key: e.target.value })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="Base64 key"
                                    />
                                </div>
                                <div className="flex justify-end gap-3 mt-6">
                                    <button
                                        onClick={() => {
                                            setShowPeerModal(false)
                                            setEditingPeer(null)
                                        }}
                                        className="px-4 py-2 text-slate-400 hover:text-white"
                                    >
                                        Cancel
                                    </button>
                                    <button
                                        onClick={handleUpdatePeer}
                                        className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg"
                                    >
                                        Save Changes
                                    </button>
                                </div>
                            </div>
                        ) : !generatedPeer ? (
                            <div className="space-y-4">
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Alias</label>
                                    <input
                                        type="text"
                                        value={newName}
                                        onChange={e => setNewName(e.target.value)}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500"
                                        placeholder="client-alias"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">IP / Allowed IPs (opcional)</label>
                                    <input
                                        type="text"
                                        value={newIp}
                                        onChange={e => setNewIp(e.target.value)}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="10.100.0.2/32, 10.0.0.0/24 (auto-assign si vacío)"
                                    />
                                    <p className="text-xs text-slate-500 mt-1">Si lo dejas vacío se asigna la próxima IP libre de la subred de la interfaz.</p>
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">IP / Allowed IPs</label>
                                    <input
                                        type="text"
                                        value={newIp}
                                        onChange={e => setNewIp(e.target.value)}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="10.100.0.2/32, 10.0.0.0/24"
                                    />
                                    <p className="text-xs text-slate-500 mt-1">Acepta varias entradas separadas por coma.</p>
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Endpoint (Optional, server public host:port)</label>
                                    <input
                                        type="text"
                                        value={newEndpoint}
                                        onChange={e => setNewEndpoint(e.target.value)}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="vpn.example.com:51820"
                                    />
                                </div>
                                <div className="flex justify-end gap-3 mt-6">
                                    <button
                                        onClick={() => setShowPeerModal(false)}
                                        className="px-4 py-2 text-slate-400 hover:text-white"
                                    >
                                        Cancel
                                    </button>
                                    <button
                                        onClick={handleCreatePeer}
                                        className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg"
                                    >
                                        Create
                                    </button>
                                </div>
                            </div>
                        ) : (
                            <div className="space-y-4">
                                <div className="p-4 bg-emerald-500/10 border border-emerald-500/20 rounded-lg text-emerald-400 text-sm">
                                    Peer created successfully!
                                </div>

                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Private Key (Save this!)</label>
                                    <div className="flex gap-2">
                                        <code className="flex-1 bg-slate-950 border border-slate-800 rounded-lg p-2 text-emerald-400 font-mono text-xs break-all">
                                            {generatedPeer.private_key}
                                        </code>
                                        <button
                                            onClick={() => copyToClipboard(generatedPeer.private_key || '')}
                                            className="p-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg"
                                        >
                                            {copied ? <Check size={16} /> : <Copy size={16} />}
                                        </button>
                                    </div>
                                </div>

                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Client Config Preview</label>
                                    <pre className="bg-slate-950 border border-slate-800 rounded-lg p-3 text-slate-300 font-mono text-xs overflow-x-auto">
                                        {generatedConfig || 'No config available yet.'}
                                    </pre>
                                </div>

                                {generatedConfig && (
                                    <div>
                                        <label className="block text-sm font-medium text-slate-400 mb-2">QR para app WireGuard</label>
                                        <div className="bg-white rounded-lg p-4 w-full">
                                            <QRCode
                                                size={256}
                                                style={{ height: "auto", maxWidth: "100%", width: "100%" }}
                                                value={generatedConfig}
                                                viewBox={`0 0 256 256`}
                                            />
                                        </div>
                                    </div>
                                )}

                                <div className="flex justify-end mt-6">
                                    <button
                                        onClick={() => {
                                            setShowPeerModal(false)
                                            setGeneratedPeer(null)
                                            setGeneratedConfig('')
                                        }}
                                        className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg"
                                    >
                                        Done
                                    </button>
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}

            {/* Peer Config Modal */}
            {configModalPeer && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-lg p-6 shadow-xl">
                        <div className="flex items-start justify-between gap-4 mb-4">
                            <div>
                                <h2 className="text-xl font-bold text-white">Peer Config / QR</h2>
                                <p className="text-slate-400 text-sm mt-1">{configModalPeer.alias || primaryAllowedIp(configModalPeer.allowed_ips) || 'Peer'}</p>
                            </div>
                            <button
                                onClick={() => {
                                    setConfigModalPeer(null)
                                    setConfigText('')
                                    setConfigLoading(false)
                                    setCopiedConfig(false)
                                }}
                                className="text-slate-400 hover:text-white"
                            >
                                Close
                            </button>
                        </div>

                        {configLoading ? (
                            <div className="text-slate-400 text-sm">Loading config...</div>
                        ) : (
                            <div className="space-y-4">
                                <div className="space-y-2">
                                    <label className="text-sm text-slate-400">Private key (only for regenerating QR if not cached)</label>
                                    <div className="flex gap-2">
                                        <input
                                            type="text"
                                            value={manualPrivateKey}
                                            onChange={e => setManualPrivateKey(e.target.value)}
                                            className="flex-1 bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-white text-sm font-mono"
                                            placeholder="Paste client private key to generate config"
                                        />
                                        <button
                                            onClick={() => {
                                                if (!configModalPeer) return
                                                setConfigLoading(true)
                                                api.getWireGuardPeerConfig(configModalPeer.public_key, manualPrivateKey || undefined)
                                                    .then(res => setConfigText(res.config))
                                                    .catch(err => {
                                                        console.error(err)
                                                        setConfigText(`Error: ${err}`)
                                                    })
                                                    .finally(() => setConfigLoading(false))
                                            }}
                                            className="px-3 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg text-sm"
                                        >
                                            Generate
                                        </button>
                                    </div>
                                    <p className="text-xs text-slate-500">No se almacena la private key; el QR se cachea 1h.</p>
                                </div>
                                <div className="flex items-center gap-2">
                                    <button
                                        onClick={() => {
                                            if (!configText) return
                                            navigator.clipboard.writeText(configText)
                                            setCopiedConfig(true)
                                            setTimeout(() => setCopiedConfig(false), 2000)
                                        }}
                                        className="px-3 py-1 bg-slate-800 hover:bg-slate-700 text-white rounded-md text-sm"
                                    >
                                        {copiedConfig ? 'Copied!' : 'Copy config'}
                                    </button>
                                    <span className="text-xs text-slate-500">Importable en la app WireGuard</span>
                                </div>
                                <pre className="bg-slate-950 border border-slate-800 rounded-lg p-3 text-slate-300 font-mono text-xs overflow-x-auto max-h-64">
                                    {configText || 'No config available.'}
                                </pre>
                                {configText && !configText.startsWith('Error:') && (
                                    <div>
                                        <label className="block text-sm font-medium text-slate-400 mb-2">QR</label>
                                        <div className="bg-white rounded-lg p-4 w-full">
                                            <QRCode
                                                size={256}
                                                style={{ height: "auto", maxWidth: "100%", width: "100%" }}
                                                value={configText}
                                                viewBox={`0 0 256 256`}
                                            />
                                        </div>
                                    </div>
                                )}
                                <div className="flex justify-end">
                                    <button
                                        onClick={() => {
                                            setConfigModalPeer(null)
                                            setConfigText('')
                                            setConfigLoading(false)
                                            setCopiedConfig(false)
                                        }}
                                        className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg"
                                    >
                                        Close
                                    </button>
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}

            {/* Interface Modal */}
            {showInterfaceModal && editInterface && (
                <div className="fixed inset-0 bg-black/50 backdrop-blur-sm z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-md p-6 shadow-xl">
                        <h2 className="text-xl font-bold text-white mb-4">Interface Settings</h2>
                        <div className="space-y-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Address</label>
                                <input
                                    type="text"
                                    value={editInterface.address}
                                    onChange={e => setEditInterface({ ...editInterface, address: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Bind Address (opcional)</label>
                                <input
                                    type="text"
                                    value={editInterface.bind_address || ''}
                                    onChange={e => setEditInterface({ ...editInterface, bind_address: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                    placeholder="149.50.133.58 (IP pública para Endpoint)"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Listen Port</label>
                                <input
                                    type="number"
                                    value={editInterface.listen_port}
                                    onChange={e => setEditInterface({ ...editInterface, listen_port: parseInt(e.target.value) })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">MTU</label>
                                <input
                                    type="number"
                                    value={editInterface.mtu || 1420}
                                    onChange={e => setEditInterface({ ...editInterface, mtu: parseInt(e.target.value) || 0 })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Post Up Script</label>
                                <input
                                    type="text"
                                    value={editInterface.post_up || ''}
                                    onChange={e => setEditInterface({ ...editInterface, post_up: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                    placeholder="iptables -A FORWARD..."
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Post Down Script</label>
                                <input
                                    type="text"
                                    value={editInterface.post_down || ''}
                                    onChange={e => setEditInterface({ ...editInterface, post_down: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                    placeholder="iptables -D FORWARD..."
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">DNS (optional)</label>
                                <input
                                    type="text"
                                    value={editInterface.dns || ''}
                                    onChange={e => setEditInterface({ ...editInterface, dns: e.target.value })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                    placeholder="1.1.1.1, 8.8.8.8"
                                />
                            </div>
                            <div className="flex justify-end gap-3 mt-6">
                                <button
                                    onClick={() => setShowInterfaceModal(false)}
                                    className="px-4 py-2 text-slate-400 hover:text-white"
                                >
                                    Cancel
                                </button>
                                <button
                                    onClick={handleUpdateInterface}
                                    className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg"
                                >
                                    Save Changes
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}
