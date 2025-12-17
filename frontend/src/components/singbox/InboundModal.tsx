import { useState, useEffect } from 'react'
import { Modal } from '../ui/Modal'
import { Button } from '../ui/Button'
import { Save } from 'lucide-react'
import { api } from '../../api'

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
    external_port: '',
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

const DEFAULT_VMESS = {
    type: 'vmess',
    tag: 'vmess-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    tls: {
        enabled: false,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: ''
    },
    transport: {
        enabled: false,
        type: 'ws',
        path: '/',
        service_name: 'grpc-service'
    }
}

const DEFAULT_TROJAN = {
    type: 'trojan',
    tag: 'trojan-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    tls: {
        enabled: false,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: ''
    },
    transport: {
        enabled: false,
        type: 'ws',
        path: '/',
        service_name: 'grpc-service'
    }
}

const DEFAULT_BY_TYPE: Record<string, any> = {
    vless: DEFAULT_VLESS,
    vmess: DEFAULT_VMESS,
    trojan: DEFAULT_TROJAN
}

export default function InboundModal({ isOpen, onClose, initialData, onSave }: InboundModalProps) {
    const [formData, setFormData] = useState<any>(DEFAULT_VLESS)
    const [validationError, setValidationError] = useState<string>('')
    const [certLoading, setCertLoading] = useState(false)
    const [certError, setCertError] = useState('')

    useEffect(() => {
        if (initialData) {
            // Deep copy to avoid mutating prop
            const data = JSON.parse(JSON.stringify(initialData))
            // Ensure nested objects exist
            const fallback = DEFAULT_BY_TYPE[data.type] || DEFAULT_VLESS
            if (!data.transport) data.transport = { ...fallback.transport }
            if (!data.tls) data.tls = { ...fallback.tls }
            if (!data.tls.reality && data.type === 'vless') data.tls.reality = { ...DEFAULT_VLESS.tls.reality }
            if (!data.tls.alpn) data.tls.alpn = [...fallback.tls.alpn]
            data.external_port = data.external_port ? String(data.external_port) : ''
            setFormData(data)
        } else {
            setFormData(JSON.parse(JSON.stringify(DEFAULT_VLESS)))
        }
        setValidationError('')
        setCertError('')
    }, [initialData, isOpen])

    useEffect(() => {
        if (validationError) {
            setValidationError('')
        }
        if (certError) {
            setCertError('')
        }
    }, [formData])

    const handleGenerateCert = async () => {
        setCertLoading(true)
        setCertError('')
        try {
            const commonName = (formData.tls?.server_name || formData.tag || '').trim()
            const res = await api.generateSelfSignedCert({
                tag: formData.tag || '',
                common_name: commonName || 'localhost'
            })
            setFormData((prev: any) => ({
                ...prev,
                tls: {
                    ...(prev.tls || {}),
                    certificate_path: res.cert_path,
                    key_path: res.key_path
                }
            }))
        } catch (err: any) {
            setCertError(err?.message || 'Failed to generate certificate')
        } finally {
            setCertLoading(false)
        }
    }

    const handleSubmit = () => {
        const tlsEnabled = !!formData.tls?.enabled
        const realityEnabled = !!formData.tls?.reality?.enabled
        const hasCert = !!formData.tls?.certificate_path && !!formData.tls?.key_path
        const requiresCert = tlsEnabled && !(formData.type === 'vless' && realityEnabled)
        if (requiresCert && !hasCert) {
            setValidationError('TLS is enabled but certificate/key paths are missing.')
            return
        }
        if (formData.type === 'vless' && realityEnabled) {
            const handshake = formData.tls?.reality?.handshake || {}
            if (!handshake.server || !formData.tls?.reality?.private_key || !formData.tls?.reality?.short_id?.[0]) {
                setValidationError('Reality is enabled but server, private key, or short ID is missing.')
                return
            }
        }
        setValidationError('')
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
        const externalPortRaw = String(formData.external_port ?? '').trim()
        if (externalPortRaw) {
            const externalPort = parseInt(externalPortRaw, 10)
            if (Number.isNaN(externalPort) || externalPort <= 0) {
                alert('External Port must be a valid number')
                return
            }
            submission.external_port = externalPort
        } else {
            delete submission.external_port
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

        // Ensure Reality is only present for VLESS
        if (submission.type !== 'vless' && submission.tls && submission.tls.reality) {
            delete submission.tls.reality
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
                {validationError && (
                    <div className="bg-red-900/30 border border-red-700/60 text-red-200 text-xs rounded-lg px-3 py-2">
                        {validationError}
                    </div>
                )}
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
                                onChange={e => {
                                    const nextType = e.target.value
                                    const nextDefaults = DEFAULT_BY_TYPE[nextType] || DEFAULT_VLESS
                                    setFormData((prev: any) => ({
                                        ...JSON.parse(JSON.stringify(nextDefaults)),
                                        tag: prev.tag || nextDefaults.tag,
                                        listen: prev.listen || nextDefaults.listen,
                                        listen_port: prev.listen_port || nextDefaults.listen_port,
                                        external_port: prev.external_port || ''
                                    }))
                                }}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
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
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">External Port (optional)</label>
                            <input
                                type="number"
                                value={formData.external_port ?? ''}
                                onChange={e => setFormData({ ...formData, external_port: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                placeholder="e.g. 443"
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

                            {formData.type === 'vless' && (
                                <>
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
                                </>
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
                            {formData.type !== 'vless' || !formData.tls?.reality?.enabled ? (
                                <div className="flex flex-wrap items-center gap-3">
                                    <Button
                                        type="button"
                                        variant="secondary"
                                        size="sm"
                                        onClick={handleGenerateCert}
                                        disabled={certLoading}
                                    >
                                        {certLoading ? 'Generating...' : 'Generate Self-Signed'}
                                    </Button>
                                    <span className="text-[10px] text-slate-400">Writes cert/key next to the sing-box config.</span>
                                    {certError && (
                                        <span className="text-[10px] text-red-400">{certError}</span>
                                    )}
                                </div>
                            ) : null}
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
                                    className="select-field w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
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
