import { useState, useEffect } from 'react'
import { Modal } from '../ui/Modal'
import { Button } from '../ui/Button'
import { Save } from 'lucide-react'

interface InboundModalProps {
    isOpen: boolean
    onClose: () => void
    initialData?: any
    onSave: (data: any) => void
}

const DEFAULT_VLESS = {
    type: 'vless',
    tag: 'vless-in',
    listen: '0.0.0.0',
    listen_port: 443,
    tls: {
        enabled: false,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: '',
        reality: {
            enabled: false,
            handshake: {
                server: '',
                server_port: 443
            },
            private_key: '',
            short_id: ['']
        }
    },
    transport: {
        enabled: false,
        type: 'http',
        path: '/',
        service_name: 'grpc-service'
    }
}

export default function InboundModal({ isOpen, onClose, initialData, onSave }: InboundModalProps) {
    const [formData, setFormData] = useState<any>(DEFAULT_VLESS)

    useEffect(() => {
        if (initialData) {
            // Deep copy to avoid mutating prop
            const data = JSON.parse(JSON.stringify(initialData))
            // Ensure nested objects exist
            if (!data.transport) data.transport = { ...DEFAULT_VLESS.transport }
            if (!data.tls) data.tls = { ...DEFAULT_VLESS.tls }
            if (!data.tls.reality) data.tls.reality = { ...DEFAULT_VLESS.tls.reality }
            if (!data.tls.alpn) data.tls.alpn = [...DEFAULT_VLESS.tls.alpn]
            setFormData(data)
        } else {
            setFormData(JSON.parse(JSON.stringify(DEFAULT_VLESS)))
        }
    }, [initialData, isOpen])

    const handleSubmit = () => {
        // Basic validation
        if (!formData.tag || !formData.listen_port) {
            alert('Tag and Port are required')
            return
        }

        // Build submission object
        const submission: any = {
            ...formData,
            listen_port: parseInt(formData.listen_port)
        }

        // Only include transport if enabled
        if (formData.transport?.enabled) {
            const cleanedTransport: any = {
                type: formData.transport?.type || 'http'
            }

            // Add type-specific fields
            const transportType = formData.transport?.type || 'http'
            if (transportType === 'http' || transportType === 'ws' || transportType === 'httpupgrade') {
                if (formData.transport?.path) {
                    cleanedTransport.path = formData.transport.path
                }
            } else if (transportType === 'grpc') {
                if (formData.transport?.service_name) {
                    cleanedTransport.service_name = formData.transport.service_name
                }
            }
            submission.transport = cleanedTransport
        } else {
            // Remove transport field entirely if disabled
            delete submission.transport
        }

        onSave(submission)
    }



    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={initialData ? 'Edit Inbound' : 'Add Inbound'}
            size="lg"
            footer={
                <>
                    <Button variant="secondary" onClick={onClose}>Cancel</Button>
                    <Button onClick={handleSubmit} icon={<Save size={16} />}>Save Inbound</Button>
                </>
            }
        >
            <div className="space-y-6">
                {/* Basic Info */}
                <div className="space-y-4">
                    <h3 className="text-sm font-medium text-slate-400 uppercase tracking-wider">Basic Settings</h3>
                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Tag (ID)</label>
                            <input
                                type="text"
                                value={formData.tag}
                                onChange={e => setFormData({ ...formData, tag: e.target.value })}
                                disabled={!!initialData} // Tag is ID, cannot change on edit generally unless we handle rename logic
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors disabled:opacity-50"
                                placeholder="e.g. vless-in"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Protocol</label>
                            <select
                                value={formData.type}
                                onChange={e => setFormData({ ...formData, type: e.target.value })}
                                disabled // Start with only VLESS support
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors disabled:opacity-50"
                            >
                                <option value="vless">VLESS</option>
                                <option value="vmess">VMess</option>
                                <option value="trojan">Trojan</option>
                            </select>
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Listen Address</label>
                            <input
                                type="text"
                                value={formData.listen}
                                onChange={e => setFormData({ ...formData, listen: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                placeholder="::"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Listen Port</label>
                            <input
                                type="number"
                                value={formData.listen_port}
                                onChange={e => setFormData({ ...formData, listen_port: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                            />
                        </div>
                    </div>
                </div>



                {/* TLS Settings */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-medium text-slate-400 uppercase tracking-wider">TLS Configuration</h3>
                        <label className="flex items-center gap-2 cursor-pointer">
                            <input
                                type="checkbox"
                                checked={formData.tls?.enabled}
                                onChange={e => setFormData({ ...formData, tls: { ...(formData.tls || {}), enabled: e.target.checked } })}
                                className="w-4 h-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <span className="text-xs font-medium text-white">Enable TLS</span>
                        </label>
                    </div>

                    {formData.tls?.enabled && (
                        <div className="grid grid-cols-1 gap-4 p-4 bg-slate-950/50 rounded-lg border border-slate-800/50">

                            {/* ALPN */}
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-300">ALPN (comma-separated)</label>
                                <input
                                    type="text"
                                    value={(formData.tls?.alpn || []).join(', ')}
                                    onChange={e => setFormData({
                                        ...formData,
                                        tls: {
                                            ...formData.tls,
                                            alpn: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                                        }
                                    })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono text-xs"
                                    placeholder="h2, http/1.1"
                                />
                            </div>

                            {/* Reality Toggle */}
                            <div className="flex items-center gap-2">
                                <input
                                    type="checkbox"
                                    checked={formData.tls?.reality?.enabled}
                                    onChange={e => setFormData({
                                        ...formData,
                                        tls: {
                                            ...formData.tls,
                                            reality: { ...(formData.tls?.reality || {}), enabled: e.target.checked }
                                        }
                                    })}
                                    className="w-4 h-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <label className="text-xs font-medium text-white">Enable Reality</label>
                            </div>

                            {/* Reality Configuration */}
                            {formData.tls?.reality?.enabled && (
                                <div className="space-y-3 p-3 bg-slate-900/50 rounded border border-slate-700">
                                    <div className="grid grid-cols-2 gap-3">
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-300">Server Name (SNI)</label>
                                            <input
                                                type="text"
                                                value={formData.tls?.reality?.handshake?.server || ''}
                                                onChange={e => setFormData({
                                                    ...formData,
                                                    tls: {
                                                        ...formData.tls,
                                                        reality: {
                                                            ...formData.tls.reality,
                                                            handshake: {
                                                                ...(formData.tls.reality.handshake || {}),
                                                                server: e.target.value
                                                            }
                                                        }
                                                    }
                                                })}
                                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500"
                                                placeholder="www.cloudflare.com"
                                            />
                                        </div>
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-300">Handshake Port</label>
                                            <input
                                                type="number"
                                                value={formData.tls?.reality?.handshake?.server_port || 443}
                                                onChange={e => setFormData({
                                                    ...formData,
                                                    tls: {
                                                        ...formData.tls,
                                                        reality: {
                                                            ...formData.tls.reality,
                                                            handshake: {
                                                                ...(formData.tls.reality.handshake || {}),
                                                                server_port: parseInt(e.target.value)
                                                            }
                                                        }
                                                    }
                                                })}
                                                className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500"
                                            />
                                        </div>
                                    </div>
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Private Key</label>
                                        <input
                                            type="text"
                                            value={formData.tls?.reality?.private_key || ''}
                                            onChange={e => setFormData({
                                                ...formData,
                                                tls: {
                                                    ...formData.tls,
                                                    reality: { ...formData.tls.reality, private_key: e.target.value }
                                                }
                                            })}
                                            className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 font-mono"
                                            placeholder="Base64 encoded private key"
                                        />
                                    </div>
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Short ID</label>
                                        <input
                                            type="text"
                                            value={Array.isArray(formData.tls?.reality?.short_id) ? formData.tls.reality.short_id[0] || '' : formData.tls?.reality?.short_id || ''}
                                            onChange={e => setFormData({
                                                ...formData,
                                                tls: {
                                                    ...formData.tls,
                                                    reality: { ...formData.tls.reality, short_id: [e.target.value] }
                                                }
                                            })}
                                            className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 font-mono"
                                            placeholder="Hex string"
                                        />
                                    </div>
                                </div>
                            )}

                            <div className="grid grid-cols-2 gap-4">
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Certificate Path</label>
                                    <input
                                        type="text"
                                        value={formData.tls?.certificate_path || ''}
                                        onChange={e => setFormData({ ...formData, tls: { ...formData.tls, certificate_path: e.target.value } })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                        placeholder="/path/to/cert.pem"
                                    />
                                </div>
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Key Path</label>
                                    <input
                                        type="text"
                                        value={formData.tls?.key_path || ''}
                                        onChange={e => setFormData({ ...formData, tls: { ...formData.tls, key_path: e.target.value } })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                        placeholder="/path/to/key.pem"
                                    />
                                </div>
                            </div>
                        </div>
                    )}
                </div>

                <div className="w-full h-px bg-slate-800/50" />


                {/* Transport Settings */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-medium text-slate-400 uppercase tracking-wider">Transport</h3>
                        <label className="flex items-center gap-2 cursor-pointer">
                            <input
                                type="checkbox"
                                checked={formData.transport?.enabled}
                                onChange={e => setFormData({ ...formData, transport: { ...(formData.transport || {}), enabled: e.target.checked } })}
                                className="w-4 h-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <span className="text-xs font-medium text-white">Enable Transport</span>
                        </label>
                    </div>

                    {formData.transport?.enabled && (
                        <div className="grid grid-cols-1 gap-4 p-4 bg-slate-950/50 rounded-lg border border-slate-800/50">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-300">Transport Type</label>
                                <select
                                    value={formData.transport?.type || 'http'}
                                    onChange={e => setFormData({ ...formData, transport: { ...formData.transport, type: e.target.value } })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                                >
                                    <option value="http">HTTP</option>
                                    <option value="ws">WebSocket</option>
                                    <option value="grpc">gRPC</option>
                                    <option value="httpupgrade">HTTP Upgrade</option>
                                </select>
                            </div>

                            {/* Path for HTTP/WS/HTTPUpgrade */}
                            {(formData.transport?.type === 'http' || formData.transport?.type === 'ws' || formData.transport?.type === 'httpupgrade') && (
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Path</label>
                                    <input
                                        type="text"
                                        value={formData.transport?.path || '/'}
                                        onChange={e => setFormData({ ...formData, transport: { ...formData.transport, path: e.target.value } })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                                        placeholder="/"
                                    />
                                </div>
                            )}

                            {/* Service Name for gRPC */}
                            {formData.transport?.type === 'grpc' && (
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Service Name</label>
                                    <input
                                        type="text"
                                        value={formData.transport?.service_name || ''}
                                        onChange={e => setFormData({ ...formData, transport: { ...formData.transport, service_name: e.target.value } })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                                        placeholder="grpc-service"
                                    />
                                </div>
                            )}
                        </div>
                    )}
                </div>

            </div>
        </Modal>
    )
}
