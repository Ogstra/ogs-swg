import { useEffect, useMemo, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '../../services/api'
import { Button } from '../ui/Button'
import { Modal } from '../ui/Modal'
import {
    buildInboundSubmission,
    computeInboundVisibility,
    getDefaultInbound,
    getPrimaryHysteria2Password,
    normalizeInboundForEditor,
    setPrimaryHysteria2Password,
} from './inboundVisibility'

interface InboundModalProps {
    isOpen: boolean
    onClose: () => void
    initialData?: any
    onSave: (data: any) => void
    canWrite?: boolean
}

export default function InboundModal({ isOpen, onClose, initialData, onSave, canWrite = true }: InboundModalProps) {
    const [formData, setFormData] = useState<any>(() => getDefaultInbound('vless'))
    const [validationError, setValidationError] = useState('')
    const [certLoading, setCertLoading] = useState(false)
    const [certError, setCertError] = useState('')

    useEffect(() => {
        setFormData(normalizeInboundForEditor(initialData || getDefaultInbound('vless')))
        setValidationError('')
        setCertError('')
    }, [initialData, isOpen])

    useEffect(() => {
        if (validationError) setValidationError('')
        if (certError) setCertError('')
    }, [formData, validationError, certError])

    const visibility = useMemo(() => computeInboundVisibility(formData), [formData])

    const updateForm = (updater: (current: any) => any) => {
        setFormData((prev: any) => normalizeInboundForEditor(updater(prev)))
    }

    const handleGenerateCert = async () => {
        setCertLoading(true)
        setCertError('')
        try {
            const commonName = (formData.tls?.server_name || formData.tag || '').trim()
            const res = await api.generateSelfSignedCert({
                tag: formData.tag || '',
                common_name: commonName || 'localhost',
            })
            updateForm((prev: any) => ({
                ...prev,
                tls: {
                    ...(prev.tls || {}),
                    certificate_path: res.cert_path,
                    key_path: res.key_path,
                },
            }))
        } catch (err: any) {
            setCertError(err?.message || 'Failed to generate certificate')
        } finally {
            setCertLoading(false)
        }
    }

    const handleSubmit = () => {
        if (!canWrite) return
        const result = buildInboundSubmission(formData)
        if (result.error || !result.submission) {
            setValidationError(result.error || 'Failed to build inbound payload.')
            return
        }
        setValidationError('')
        onSave(result.submission)
    }

    const hysteria2Password = getPrimaryHysteria2Password(formData)

    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={initialData ? 'Edit Inbound' : 'Add Inbound'}
            size="lg"
            footer={
                <>
                    <Button variant="secondary" onClick={onClose}>Cancel</Button>
                    <Button onClick={handleSubmit} icon={<Save size={16} />} disabled={!canWrite}>Save Inbound</Button>
                </>
            }
        >
            <fieldset disabled={!canWrite} className={`space-y-6 ${!canWrite ? 'opacity-80' : ''}`}>
                {validationError && (
                    <div className="rounded-lg border border-red-700/60 bg-red-900/30 px-3 py-2 text-xs text-red-200">
                        {validationError}
                    </div>
                )}

                <div className="space-y-4">
                    <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Basic Settings</h3>
                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Tag (ID)</label>
                            <input
                                type="text"
                                value={formData.tag || ''}
                                onChange={e => updateForm(prev => ({ ...prev, tag: e.target.value }))}
                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none disabled:opacity-50"
                                placeholder="e.g. vless-in"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Protocol</label>
                            <select
                                value={formData.type}
                                onChange={e => {
                                    const nextDefaults = getDefaultInbound(e.target.value)
                                    updateForm((prev: any) => ({
                                        ...nextDefaults,
                                        tag: prev.tag || nextDefaults.tag,
                                        listen: prev.listen || nextDefaults.listen,
                                        listen_port: prev.listen_port || nextDefaults.listen_port,
                                        external_port: prev.external_port || '',
                                    }))
                                }}
                                className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                            >
                                <option value="vless">VLESS</option>
                                <option value="vmess">VMess</option>
                                <option value="trojan">Trojan</option>
                                <option value="hysteria2">Hysteria2</option>
                            </select>
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Listen Address</label>
                            <input
                                type="text"
                                value={formData.listen || ''}
                                onChange={e => updateForm(prev => ({ ...prev, listen: e.target.value }))}
                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                placeholder="::"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Listen Port</label>
                            <input
                                type="number"
                                value={formData.listen_port ?? ''}
                                onChange={e => updateForm(prev => ({ ...prev, listen_port: e.target.value }))}
                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">External Port (optional)</label>
                            <input
                                type="number"
                                value={formData.external_port ?? ''}
                                onChange={e => updateForm(prev => ({ ...prev, external_port: e.target.value }))}
                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                placeholder="e.g. 443"
                            />
                        </div>
                    </div>
                </div>

                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">TLS Configuration</h3>
                        <label className="flex cursor-pointer items-center gap-2">
                            <input
                                type="checkbox"
                                checked={!!formData.tls?.enabled}
                                onChange={e => updateForm((prev: any) => ({
                                    ...prev,
                                    tls: {
                                        ...(prev.tls || {}),
                                        enabled: e.target.checked,
                                    },
                                }))}
                                className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <span className="text-xs font-medium text-white">Enable TLS</span>
                        </label>
                    </div>

                    {formData.tls?.enabled && (
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                            {visibility.showAlpn && (
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">ALPN (comma-separated)</label>
                                    <input
                                        type="text"
                                        value={(formData.tls?.alpn || []).join(', ')}
                                        onChange={e => updateForm((prev: any) => ({
                                            ...prev,
                                            tls: {
                                                ...prev.tls,
                                                alpn: e.target.value.split(',').map(item => item.trim()).filter(Boolean),
                                            },
                                        }))}
                                        className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 font-mono text-xs text-white transition-colors focus:border-blue-500 focus:outline-none"
                                        placeholder="h2, http/1.1"
                                    />
                                </div>
                            )}

                            {visibility.showRealityToggle && (
                                <>
                                    <div className="flex items-center gap-2">
                                        <input
                                            type="checkbox"
                                            checked={!!formData.tls?.reality?.enabled}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                tls: {
                                                    ...prev.tls,
                                                    reality: {
                                                        ...(prev.tls?.reality || {}),
                                                        enabled: e.target.checked,
                                                    },
                                                },
                                            }))}
                                            className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                        />
                                        <label className="text-xs font-medium text-white">Enable Reality</label>
                                    </div>

                                    {visibility.showRealitySection && (
                                        <div className="space-y-3 rounded border border-slate-700 bg-slate-900/50 p-3">
                                            <div className="grid grid-cols-2 gap-3">
                                                <div className="space-y-1">
                                                    <label className="text-xs font-medium text-slate-300">Server Name (SNI)</label>
                                                    <input
                                                        type="text"
                                                        value={formData.tls?.reality?.handshake?.server || ''}
                                                        onChange={e => updateForm((prev: any) => ({
                                                            ...prev,
                                                            tls: {
                                                                ...prev.tls,
                                                                reality: {
                                                                    ...prev.tls.reality,
                                                                    handshake: {
                                                                        ...(prev.tls.reality?.handshake || {}),
                                                                        server: e.target.value,
                                                                    },
                                                                },
                                                            },
                                                        }))}
                                                        className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                                                        placeholder="www.cloudflare.com"
                                                    />
                                                </div>
                                                <div className="space-y-1">
                                                    <label className="text-xs font-medium text-slate-300">Handshake Port</label>
                                                    <input
                                                        type="number"
                                                        value={formData.tls?.reality?.handshake?.server_port || 443}
                                                        onChange={e => updateForm((prev: any) => ({
                                                            ...prev,
                                                            tls: {
                                                                ...prev.tls,
                                                                reality: {
                                                                    ...prev.tls.reality,
                                                                    handshake: {
                                                                        ...(prev.tls.reality?.handshake || {}),
                                                                        server_port: e.target.value,
                                                                    },
                                                                },
                                                            },
                                                        }))}
                                                        className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                                                    />
                                                </div>
                                            </div>
                                            <div className="space-y-1">
                                                <label className="text-xs font-medium text-slate-300">Server Name for clients (optional)</label>
                                                <input
                                                    type="text"
                                                    value={formData.tls?.server_name || ''}
                                                    onChange={e => updateForm((prev: any) => ({
                                                        ...prev,
                                                        tls: {
                                                            ...prev.tls,
                                                            server_name: e.target.value || undefined,
                                                        },
                                                    }))}
                                                    className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                                                    placeholder="e.g. cdn.example.com"
                                                />
                                            </div>
                                            <div className="space-y-1">
                                                <label className="text-xs font-medium text-slate-300">Private Key</label>
                                                <input
                                                    type="text"
                                                    value={formData.tls?.reality?.private_key || ''}
                                                    onChange={e => updateForm((prev: any) => ({
                                                        ...prev,
                                                        tls: {
                                                            ...prev.tls,
                                                            reality: {
                                                                ...prev.tls.reality,
                                                                private_key: e.target.value,
                                                            },
                                                        },
                                                    }))}
                                                    className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 font-mono text-xs text-white focus:border-blue-500 focus:outline-none"
                                                    placeholder="Base64 encoded private key"
                                                />
                                            </div>
                                            <div className="space-y-1">
                                                <label className="text-xs font-medium text-slate-300">Short ID</label>
                                                <input
                                                    type="text"
                                                    value={formData.tls?.reality?.short_id?.[0] || ''}
                                                    onChange={e => updateForm((prev: any) => ({
                                                        ...prev,
                                                        tls: {
                                                            ...prev.tls,
                                                            reality: {
                                                                ...prev.tls.reality,
                                                                short_id: [e.target.value],
                                                            },
                                                        },
                                                    }))}
                                                    className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 font-mono text-xs text-white focus:border-blue-500 focus:outline-none"
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
                                        onChange={e => updateForm((prev: any) => ({
                                            ...prev,
                                            tls: {
                                                ...prev.tls,
                                                certificate_path: e.target.value,
                                            },
                                        }))}
                                        className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                        placeholder="/path/to/cert.pem"
                                    />
                                </div>
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Key Path</label>
                                    <input
                                        type="text"
                                        value={formData.tls?.key_path || ''}
                                        onChange={e => updateForm((prev: any) => ({
                                            ...prev,
                                            tls: {
                                                ...prev.tls,
                                                key_path: e.target.value,
                                            },
                                        }))}
                                        className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                        placeholder="/path/to/key.pem"
                                    />
                                </div>
                            </div>

                            {!visibility.showRealitySection && (
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
                                    {certError && <span className="text-[10px] text-red-400">{certError}</span>}
                                </div>
                            )}
                        </div>
                    )}
                </div>

                {visibility.showHysteria2Password && (
                    <div className="space-y-4">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Hysteria2</h3>
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-300">Password</label>
                                <input
                                    type="text"
                                    value={hysteria2Password}
                                    onChange={e => setFormData((prev: any) => setPrimaryHysteria2Password(prev, e.target.value))}
                                    className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                    placeholder="Enter Hysteria2 password"
                                />
                            </div>

                            {visibility.showHysteria2Bandwidth && (
                                <div className="grid grid-cols-2 gap-4">
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Upload (Mbps)</label>
                                        <input
                                            type="number"
                                            min={1}
                                            value={formData.up_mbps ?? ''}
                                            onChange={e => updateForm(prev => ({ ...prev, up_mbps: e.target.value }))}
                                            className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                            placeholder="Optional"
                                        />
                                    </div>
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Download (Mbps)</label>
                                        <input
                                            type="number"
                                            min={1}
                                            value={formData.down_mbps ?? ''}
                                            onChange={e => updateForm(prev => ({ ...prev, down_mbps: e.target.value }))}
                                            className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                            placeholder="Optional"
                                        />
                                    </div>
                                </div>
                            )}

                            {visibility.showHysteria2Obfs && (
                                <div className="grid grid-cols-2 gap-4">
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Obfs Type</label>
                                        <select
                                            value={formData.obfs?.type || ''}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                obfs: {
                                                    ...(prev.obfs || {}),
                                                    type: e.target.value,
                                                },
                                            }))}
                                            className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                        >
                                            <option value="">None</option>
                                            <option value="salamander">salamander</option>
                                        </select>
                                    </div>
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Obfs Password</label>
                                        <input
                                            type="text"
                                            value={formData.obfs?.password || ''}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                obfs: {
                                                    ...(prev.obfs || {}),
                                                    password: e.target.value,
                                                },
                                            }))}
                                            disabled={!formData.obfs?.type}
                                            className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none disabled:opacity-50"
                                            placeholder="Required when obfs is enabled"
                                        />
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>
                )}

                {visibility.showTransport && (
                    <div className="space-y-4">
                        <div className="flex items-center justify-between">
                            <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Transport</h3>
                            <label className="flex cursor-pointer items-center gap-2">
                                <input
                                    type="checkbox"
                                    checked={!!formData.transport?.enabled}
                                    onChange={e => updateForm((prev: any) => ({
                                        ...prev,
                                        transport: {
                                            ...(prev.transport || {}),
                                            enabled: e.target.checked,
                                        },
                                    }))}
                                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <span className="text-xs font-medium text-white">Enable Transport</span>
                            </label>
                        </div>

                        {formData.transport?.enabled && (
                            <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Transport Type</label>
                                    <select
                                        value={formData.transport?.type || 'http'}
                                        onChange={e => updateForm((prev: any) => ({
                                            ...prev,
                                            transport: {
                                                ...(prev.transport || {}),
                                                type: e.target.value,
                                            },
                                        }))}
                                        className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                    >
                                        <option value="http">HTTP</option>
                                        <option value="ws">WebSocket</option>
                                        <option value="grpc">gRPC</option>
                                        <option value="httpupgrade">HTTP Upgrade</option>
                                    </select>
                                </div>

                                {visibility.showTransportPath && (
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Path</label>
                                        <input
                                            type="text"
                                            value={formData.transport?.path || '/'}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                transport: {
                                                    ...(prev.transport || {}),
                                                    path: e.target.value,
                                                },
                                            }))}
                                            className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                            placeholder="/"
                                        />
                                    </div>
                                )}

                                {visibility.showTransportServiceName && (
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Service Name</label>
                                        <input
                                            type="text"
                                            value={formData.transport?.service_name || ''}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                transport: {
                                                    ...(prev.transport || {}),
                                                    service_name: e.target.value,
                                                },
                                            }))}
                                            className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                            placeholder="grpc-service"
                                        />
                                    </div>
                                )}

                                {visibility.showWsHeaders && (
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">headers (JSON)</label>
                                        <textarea
                                            value={formData.transport?.headers || ''}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                transport: {
                                                    ...(prev.transport || {}),
                                                    headers: e.target.value,
                                                },
                                            }))}
                                            rows={5}
                                            className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 font-mono text-xs text-white focus:border-blue-500 focus:outline-none"
                                            placeholder={`{\n  "Host": "cdn.example.com"\n}`}
                                        />
                                    </div>
                                )}

                                {visibility.showWsEarlyData && (
                                    <div className="grid grid-cols-2 gap-4">
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-300">max_early_data</label>
                                            <input
                                                type="number"
                                                min={1}
                                                value={formData.transport?.max_early_data ?? ''}
                                                onChange={e => updateForm((prev: any) => ({
                                                    ...prev,
                                                    transport: {
                                                        ...(prev.transport || {}),
                                                        max_early_data: e.target.value,
                                                    },
                                                }))}
                                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                                placeholder="Optional"
                                            />
                                        </div>
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-300">early_data_header_name</label>
                                            <input
                                                type="text"
                                                value={formData.transport?.early_data_header_name || ''}
                                                onChange={e => updateForm((prev: any) => ({
                                                    ...prev,
                                                    transport: {
                                                        ...(prev.transport || {}),
                                                        early_data_header_name: e.target.value,
                                                    },
                                                }))}
                                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                                placeholder="Sec-WebSocket-Protocol"
                                            />
                                        </div>
                                    </div>
                                )}
                            </div>
                        )}
                    </div>
                )}

                {visibility.showMultiplex && (
                    <div className="space-y-4">
                        <div className="flex items-center justify-between">
                            <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Multiplex</h3>
                            <label className="flex cursor-pointer items-center gap-2">
                                <input
                                    type="checkbox"
                                    checked={!!formData.multiplex?.enabled}
                                    onChange={e => updateForm((prev: any) => ({
                                        ...prev,
                                        multiplex: {
                                            ...(prev.multiplex || {}),
                                            enabled: e.target.checked,
                                            brutal: {
                                                ...(prev.multiplex?.brutal || {}),
                                                enabled: e.target.checked ? !!prev.multiplex?.brutal?.enabled : false,
                                            },
                                        },
                                    }))}
                                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <span className="text-xs font-medium text-white">Enable Multiplex</span>
                            </label>
                        </div>

                        {formData.multiplex?.enabled && (
                            <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                                <label className="flex cursor-pointer items-center gap-2">
                                    <input
                                        type="checkbox"
                                        checked={!!formData.multiplex?.padding}
                                        onChange={e => updateForm((prev: any) => ({
                                            ...prev,
                                            multiplex: {
                                                ...(prev.multiplex || {}),
                                                padding: e.target.checked,
                                            },
                                        }))}
                                        className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                    />
                                    <span className="text-xs font-medium text-white">Enable Padding</span>
                                </label>

                                <div className="space-y-3 rounded border border-slate-700 bg-slate-900/50 p-3">
                                    <label className="flex cursor-pointer items-center gap-2">
                                        <input
                                            type="checkbox"
                                            checked={!!formData.multiplex?.brutal?.enabled}
                                            onChange={e => updateForm((prev: any) => ({
                                                ...prev,
                                                multiplex: {
                                                    ...(prev.multiplex || {}),
                                                    brutal: {
                                                        ...(prev.multiplex?.brutal || {}),
                                                        enabled: e.target.checked,
                                                    },
                                                },
                                            }))}
                                            className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                        />
                                        <span className="text-xs font-medium text-white">Enable TCP Brutal</span>
                                    </label>

                                    {formData.multiplex?.brutal?.enabled && (
                                        <div className="grid grid-cols-2 gap-3">
                                            <div className="space-y-1">
                                                <label className="text-xs font-medium text-slate-300">Upload (Mbps)</label>
                                                <input
                                                    type="number"
                                                    min={1}
                                                    value={formData.multiplex?.brutal?.up_mbps ?? 100}
                                                    onChange={e => updateForm((prev: any) => ({
                                                        ...prev,
                                                        multiplex: {
                                                            ...(prev.multiplex || {}),
                                                            brutal: {
                                                                ...(prev.multiplex?.brutal || {}),
                                                                up_mbps: e.target.value,
                                                            },
                                                        },
                                                    }))}
                                                    className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                                                />
                                            </div>
                                            <div className="space-y-1">
                                                <label className="text-xs font-medium text-slate-300">Download (Mbps)</label>
                                                <input
                                                    type="number"
                                                    min={1}
                                                    value={formData.multiplex?.brutal?.down_mbps ?? 100}
                                                    onChange={e => updateForm((prev: any) => ({
                                                        ...prev,
                                                        multiplex: {
                                                            ...(prev.multiplex || {}),
                                                            brutal: {
                                                                ...(prev.multiplex?.brutal || {}),
                                                                down_mbps: e.target.value,
                                                            },
                                                        },
                                                    }))}
                                                    className="w-full rounded border border-slate-800 bg-slate-950 px-2 py-1.5 text-xs text-white focus:border-blue-500 focus:outline-none"
                                                />
                                            </div>
                                        </div>
                                    )}
                                </div>
                            </div>
                        )}
                    </div>
                )}
            </fieldset>
        </Modal>
    )
}
