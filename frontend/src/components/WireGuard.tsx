import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { Plus, Trash2, Copy, Check, Settings, Edit, ArrowUp, ArrowDown, Shield, ArrowUpDown } from 'lucide-react'

interface WireGuardPeer {
    public_key: string
    private_key?: string
    allowed_ips: string
    endpoint?: string
    alias?: string
    name?: string // legacy
    preshared_key?: string
    persistent_keepalive?: number
    stats?: {
        latest_handshake: number
        transfer_rx: number
        transfer_tx: number
        endpoint: string
    }
}

interface WireGuardInterface {
    address: string
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
    if (!timestamp) return 'Never'
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
    const [generatedPeer, setGeneratedPeer] = useState<WireGuardPeer | null>(null)
    const [copied, setCopied] = useState(false)

    // Interface Edit State
    const [editInterface, setEditInterface] = useState<WireGuardInterface | null>(null)
    const [sortKey, setSortKey] = useState<'alias' | 'ip' | 'endpoint' | 'traffic' | 'handshake'>('alias')
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

    const fetchData = () => {
        api.getWireGuardPeers()
            .then(data => setPeers(Array.isArray(data) ? data : []))
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
    }

    useEffect(() => {
        fetchData()
        const interval = setInterval(fetchData, 5000) // Refresh stats every 5s
        return () => clearInterval(interval)
    }, [])

    const handleCreatePeer = async () => {
        if (!newName.trim()) {
            alert('Alias is required')
            return
        }
        try {
            const peer = await api.createWireGuardPeer(newName)
            setGeneratedPeer(peer)
            setNewName('')
            fetchData()
        } catch (err) {
            alert('Failed to create peer: ' + err)
        }
    }

    const handleUpdatePeer = async () => {
        if (!editingPeer) return
        try {
            await api.updateWireGuardPeer(editingPeer.public_key, editingPeer)
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

    const generateConfigBlock = (peer: WireGuardPeer) => {
        return `[Interface]
PrivateKey = ${peer.private_key || '<YOUR_PRIVATE_KEY>'}
Address = ${peer.allowed_ips}
DNS = 1.1.1.1

[Peer]
PublicKey = ${interfaceConfig?.private_key ? '<SERVER_PUBLIC_KEY>' : '<SERVER_PUBLIC_KEY>'} 
Endpoint = <SERVER_IP>:${interfaceConfig?.listen_port || 51820}
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = ${peer.persistent_keepalive || 25}`
    }

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
                        const base = ip.split('/')[0]
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

    return (
        <div className="space-y-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white">WireGuard</h1>
                    <p className="text-slate-400 text-sm mt-1">Manage WireGuard peers and settings</p>
                </div>
                <div className="w-full grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(180px,1fr))] md:flex md:flex-wrap md:w-auto">
                    <button
                        onClick={() => {
                            setEditInterface(interfaceConfig ?? { address: '', private_key: '', listen_port: 51820, post_up: '', post_down: '', mtu: 1420, dns: '' })
                            setShowInterfaceModal(true)
                        }}
                        className="flex items-center justify-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-lg transition-colors w-full md:w-auto"
                    >
                        <Settings size={16} />
                        <span className="text-sm font-medium">Interface Settings</span>
                    </button>
                    <button
                        onClick={() => {
                            setEditingPeer(null)
                            setGeneratedPeer(null)
                            setShowPeerModal(true)
                        }}
                        className="flex items-center justify-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors w-full md:w-auto"
                    >
                        <Plus size={16} />
                        <span className="text-sm font-medium">Add Peer</span>
                    </button>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className="md:col-span-2 bg-slate-900 border border-slate-800 rounded-xl p-4 flex flex-col lg:flex-row items-start lg:items-center gap-6">
                    <div className="p-3 rounded-lg bg-emerald-500/10 text-emerald-400">
                        <Shield size={20} />
                    </div>
                    <div className="flex-1 grid grid-cols-1 sm:grid-cols-3 gap-4 items-start">
                        <div>
                            <p className="text-sm text-slate-400">Interface IP</p>
                            <p className="text-lg text-white font-semibold">{interfaceConfig?.address || 'Not configured'}</p>
                        </div>
                        <div>
                            <p className="text-sm text-slate-400">Listen Port</p>
                            <p className="text-lg text-white font-semibold">{interfaceConfig?.listen_port || 51820}</p>
                        </div>
                        <div>
                            <p className="text-sm text-slate-400">MTU</p>
                            <p className="text-lg text-white font-semibold">{interfaceConfig?.mtu || 1420}</p>
                        </div>
                    </div>
                </div>
            </div>

            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
                <div className="overflow-x-auto hidden md:block">
                    <table className="w-full text-left border-collapse">
                        <thead>
                            <tr className="bg-slate-950/50 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="p-4 font-medium cursor-pointer select-none" onClick={() => toggleSort('handshake')}>
                                    Status {renderSortIcon('handshake')}
                                </th>
                                <th className="p-4 font-medium cursor-pointer select-none" onClick={() => toggleSort('alias')}>
                                    Alias {renderSortIcon('alias')}
                                </th>
                                <th className="p-4 font-medium cursor-pointer select-none" onClick={() => toggleSort('ip')}>
                                    IP {renderSortIcon('ip')}
                                </th>
                                <th className="p-4 font-medium cursor-pointer select-none" onClick={() => toggleSort('endpoint')}>
                                    Endpoint {renderSortIcon('endpoint')}
                                </th>
                                <th className="p-4 font-medium cursor-pointer select-none" onClick={() => toggleSort('traffic')}>
                                    Traffic {renderSortIcon('traffic')}
                                </th>
                                <th className="p-4 font-medium text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-800">
                            {(sortedPeers || []).map(peer => {
                                const isOnline = peer.stats && (Date.now() / 1000 - peer.stats.latest_handshake) < 180 // 3 mins
                                return (
                                    <tr key={peer.public_key} className="hover:bg-slate-800/50 transition-colors">
                                        <td className="p-4">
                                            <div className="flex items-center gap-2">
                                                <div className={`w-2 h-2 rounded-full ${isOnline ? 'bg-emerald-500 animate-pulse' : 'bg-slate-600'}`}></div>
                                                <span className="text-xs text-slate-500">
                                                    {peer.stats?.latest_handshake ? formatTimeAgo(peer.stats.latest_handshake) : 'Never'}
                                                </span>
                                            </div>
                                        </td>
                                        <td className="p-4">
                                            <div className="font-medium text-slate-200">{peer.alias || peer.name || '-'}</div>
                                        </td>
                                        <td className="p-4 font-mono text-slate-400 text-xs">
                                            {peer.allowed_ips}
                                        </td>
                                        <td className="p-4 font-mono text-slate-400 text-xs">
                                            {peer.stats?.endpoint || peer.endpoint || '-'}
                                        </td>
                                        <td className="p-4">
                                            <div className="flex flex-col gap-1 text-xs">
                                                <div className="flex items-center gap-1 text-emerald-400">
                                                    <ArrowUp size={12} />
                                                    {formatBytes(peer.stats?.transfer_tx || 0)}
                                                </div>
                                                <div className="flex items-center gap-1 text-indigo-400">
                                                    <ArrowDown size={12} />
                                                    {formatBytes(peer.stats?.transfer_rx || 0)}
                                                </div>
                                            </div>
                                        </td>
                                        <td className="p-4 text-right">
                                            <div className="flex justify-end gap-2">
                                                <button
                                                    onClick={() => {
                                                        setEditingPeer(peer)
                                                        setShowPeerModal(true)
                                                    }}
                                                    className="p-2 text-slate-500 hover:text-blue-400 transition-colors"
                                                    title="Edit Peer"
                                                >
                                                    <Edit size={16} />
                                                </button>
                                                <button
                                                    onClick={() => handleDeletePeer(peer.public_key)}
                                                    className="p-2 text-slate-500 hover:text-red-400 transition-colors"
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
                                    <td colSpan={6} className="p-8 text-center text-slate-500">
                                        No WireGuard peers found.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>

                {/* Mobile cards */}
                <div className="md:hidden divide-y divide-slate-800">
                    {(sortedPeers || []).map(peer => {
                        const isOnline = peer.stats && (Date.now() / 1000 - peer.stats.latest_handshake) < 180
                        return (
                            <div key={peer.public_key} className="p-4 space-y-3">
                                <div className="flex items-center justify-between">
                                    <div className="flex items-center gap-2">
                                        <div className={`w-2 h-2 rounded-full ${isOnline ? 'bg-emerald-500 animate-pulse' : 'bg-slate-600'}`}></div>
                                        <span className="text-xs text-slate-500">
                                            {peer.stats?.latest_handshake ? formatTimeAgo(peer.stats.latest_handshake) : 'Never'}
                                        </span>
                                    </div>
                                    <div className="flex gap-2">
                                        <button
                                            onClick={() => {
                                                setEditingPeer(peer)
                                                setShowPeerModal(true)
                                            }}
                                            className="p-2 text-slate-500 hover:text-blue-400 transition-colors"
                                            title="Edit Peer"
                                        >
                                            <Edit size={16} />
                                        </button>
                                        <button
                                            onClick={() => handleDeletePeer(peer.public_key)}
                                            className="p-2 text-slate-500 hover:text-red-400 transition-colors"
                                            title="Delete Peer"
                                        >
                                            <Trash2 size={16} />
                                        </button>
                                    </div>
                                </div>
                                <div className="space-y-1">
                                    <div className="font-medium text-slate-200">{peer.alias || peer.name || '-'}</div>
                                    <div className="font-mono text-slate-400 text-xs">{peer.allowed_ips}</div>
                                    <div className="font-mono text-slate-400 text-xs">{peer.stats?.endpoint || peer.endpoint || '-'}</div>
                                </div>
                                <div className="flex items-center gap-4 text-xs">
                                    <div className="flex items-center gap-1 text-emerald-400">
                                        <ArrowUp size={12} />
                                        {formatBytes(peer.stats?.transfer_tx || 0)}
                                    </div>
                                    <div className="flex items-center gap-1 text-indigo-400">
                                        <ArrowDown size={12} />
                                        {formatBytes(peer.stats?.transfer_rx || 0)}
                                    </div>
                                </div>
                            </div>
                        )
                    })}
                    {peers.length === 0 && (
                        <div className="p-6 text-center text-slate-500">No WireGuard peers found.</div>
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
                                <div>
                                    <label className="block text-sm font-medium text-slate-400 mb-1">Persistent Keepalive (s)</label>
                                    <input
                                        type="number"
                                        value={editingPeer.persistent_keepalive || 0}
                                        onChange={e => setEditingPeer({ ...editingPeer, persistent_keepalive: parseInt(e.target.value) || 0 })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                                        placeholder="25"
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
                                        {generateConfigBlock(generatedPeer)}
                                    </pre>
                                </div>

                                <div className="flex justify-end mt-6">
                                    <button
                                        onClick={() => {
                                            setShowPeerModal(false)
                                            setGeneratedPeer(null)
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
