import { useEffect, useMemo, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { api } from '../../services/api'
import { Button } from '../ui/Button'
import { ConfirmModal } from '../ui/ConfirmModal'
import { Modal } from '../ui/Modal'
import { keyLengthForShadowsocksMethod, SUPPORTED_SHADOWSOCKS_METHODS } from '../../utils/shadowsocks'
import {
    buildInboundSubmission,
    computeInboundVisibility,
    getDefaultInbound,
    normalizeInboundForEditor,
} from './inboundVisibility'

interface InboundModalProps {
    isOpen: boolean
    onClose: () => void
    initialData?: any
    onSave: (data: any) => void | Promise<void>
    canWrite?: boolean
}

export default function InboundModal({ isOpen, onClose, initialData, onSave, canWrite = true }: InboundModalProps) {
    const [formData, setFormData] = useState<any>(() => getDefaultInbound('vless'))
    const [validationError, setValidationError] = useState('')
    const [certLoading, setCertLoading] = useState(false)
    const [certError, setCertError] = useState('')
    const [shadowsocksServerPasswordLoading, setShadowsocksServerPasswordLoading] = useState(false)
    const [pendingRenameSubmission, setPendingRenameSubmission] = useState<any | null>(null)
    const [showRenameConfirm, setShowRenameConfirm] = useState(false)
    const [saveLoading, setSaveLoading] = useState(false)

    useEffect(() => {
        setFormData(normalizeInboundForEditor(initialData || getDefaultInbound('vless')))
        setValidationError('')
        setCertError('')
        setShadowsocksServerPasswordLoading(false)
        setPendingRenameSubmission(null)
        setShowRenameConfirm(false)
        setSaveLoading(false)
    }, [initialData, isOpen])

    useEffect(() => {
        if (validationError) setValidationError('')
        if (certError) setCertError('')
    }, [formData, validationError, certError])

    const visibility = useMemo(() => computeInboundVisibility(formData), [formData])
    const originalTag = String(initialData?.tag || '').trim()
    const pendingRenameTag = String(pendingRenameSubmission?.tag || formData.tag || '').trim()
    const isHysteria2 = formData.type === 'hysteria2'
    const isShadowsocks = formData.type === 'shadowsocks'
    const isAnyTLS = formData.type === 'anytls'
    const isNaive = formData.type === 'naive'

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

    const generateShadowsocksPassword = async () => {
        const keyLength = keyLengthForShadowsocksMethod(formData.method || '')
        setShadowsocksServerPasswordLoading(true)
        try {
            const res = await api.generateRandBase64(keyLength)
            updateForm((prev: any) => ({ ...prev, password: res.value }))
        } catch (err: any) {
            setValidationError(err?.message || 'Failed to generate Shadowsocks password')
        } finally {
            setShadowsocksServerPasswordLoading(false)
        }
    }

    const submitPayload = async (payload: any) => {
        setSaveLoading(true)
        try {
            await Promise.resolve(onSave(payload))
            setPendingRenameSubmission(null)
            setShowRenameConfirm(false)
        } finally {
            setSaveLoading(false)
        }
    }

    const handleSubmit = async () => {
        if (!canWrite || saveLoading) return
        const result = buildInboundSubmission(formData)
        if (result.error || !result.submission) {
            setValidationError(result.error || 'Failed to build inbound payload.')
            return
        }
        const nextTag = String(result.submission.tag || '').trim()
        if (originalTag && nextTag && originalTag !== nextTag) {
            setValidationError('')
            setPendingRenameSubmission(result.submission)
            setShowRenameConfirm(true)
            return
        }
        setValidationError('')
        await submitPayload(result.submission)
    }

    return (
        <>
            <Modal
                isOpen={isOpen}
                onClose={onClose}
                title={initialData ? 'Edit Inbound' : 'Add Inbound'}
                size="lg"
                footer={
                    <>
                        <Button variant="secondary" onClick={onClose}>Cancel</Button>
                        <Button onClick={() => void handleSubmit()} icon={<Save size={16} />} disabled={!canWrite || saveLoading} isLoading={saveLoading}>Save Inbound</Button>
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
                                        override_address: prev.override_address || '',
                                        link_allow_insecure: prev.link_allow_insecure || 'auto',
                                    }))
                                }}
                                className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                            >
                                <option value="vless">VLESS</option>
                                <option value="vmess">VMess</option>
                                <option value="trojan">Trojan</option>
                                <option value="hysteria2">Hysteria2</option>
                                <option value="shadowsocks">Shadowsocks</option>
                                <option value="anytls">AnyTLS</option>
                                <option value="naive">Naive</option>
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
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Override Address (optional)</label>
                            <input
                                type="text"
                                value={formData.override_address ?? ''}
                                onChange={e => updateForm(prev => ({ ...prev, override_address: e.target.value }))}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                placeholder="e.g. 1.2.3.4 or other.domain.com"
                            />
                            <p className="text-[11px] text-slate-400">
                                Overrides the generated share-link host while preserving the original public hostname as TLS SNI fallback.
                            </p>
                        </div>
                        {visibility.showLinkTlsVerification && (
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-300">Link TLS Verification</label>
                                <select
                                    value={formData.link_allow_insecure || 'auto'}
                                    onChange={e => updateForm(prev => ({ ...prev, link_allow_insecure: e.target.value }))}
                                    className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                >
                                    <option value="auto">Auto</option>
                                    <option value="enabled">Force allowInsecure=1</option>
                                    <option value="disabled">Force strict verification</option>
                                </select>
                                <p className="text-[11px] text-slate-400">
                                    Controls whether generated share links include <code>allowInsecure=1</code>. Auto keeps the backend heuristic.
                                </p>
                            </div>
                        )}
                    </div>
                </div>

                {visibility.showTlsSection && (
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">TLS Configuration</h3>
                        {isHysteria2 ? (
                            <div className="rounded-full border border-emerald-500/30 bg-emerald-500/10 px-3 py-1 text-[11px] font-medium uppercase tracking-wide text-emerald-300">
                                Required for Hysteria2
                            </div>
                        ) : (
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
                        )}
                    </div>

                    {formData.tls?.enabled && (
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                            {isHysteria2 && (
                                <div className="rounded-lg border border-slate-700/80 bg-slate-900/60 px-3 py-2 text-xs text-slate-300">
                                    Hysteria2 always uses TLS. Use a trusted certificate when possible, and set Server Name to the public hostname clients will validate.
                                </div>
                            )}
                            {!visibility.showRealitySection && (
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Server Name for clients</label>
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
                                        className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                        placeholder={isHysteria2 ? 'e.g. vpn.example.com' : 'Optional SNI/hostname for clients'}
                                    />
                                    <p className="text-[11px] text-slate-400">
                                        Clients use this hostname for TLS verification. Hysteria2 usually needs it to match the public certificate name.
                                    </p>
                                </div>
                            )}
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
                                <div className="space-y-3">
                                    <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-100">
                                        The self-signed helper writes PEM-encoded cert/key files next to the sing-box config. Most clients will still reject them unless that certificate is explicitly trusted.
                                    </div>
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
                                        {certError && <span className="text-[10px] text-red-400">{certError}</span>}
                                    </div>
                                </div>
                            )}
                        </div>
                    )}
                </div>
                )}

                {visibility.showShadowsocksSection && (
                    <div className="space-y-4">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Shadowsocks</h3>
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                            <div className="grid grid-cols-2 gap-4">
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Network</label>
                                    <div className="flex flex-wrap gap-4 rounded border border-slate-800 bg-slate-950 px-3 py-2">
                                        {['tcp', 'udp'].map(network => (
                                            <label key={network} className="flex items-center gap-2 text-sm text-white">
                                                <input
                                                    type="checkbox"
                                                    checked={(formData.network || []).includes(network)}
                                                    onChange={e => updateForm((prev: any) => {
                                                        const next = new Set(Array.isArray(prev.network) ? prev.network : [])
                                                        if (e.target.checked) next.add(network)
                                                        else next.delete(network)
                                                        return { ...prev, network: Array.from(next) }
                                                    })}
                                                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                                />
                                                <span className="uppercase">{network}</span>
                                            </label>
                                        ))}
                                    </div>
                                </div>
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Method</label>
                                    <select
                                        value={formData.method || ''}
                                        onChange={e => updateForm(prev => ({ ...prev, method: e.target.value }))}
                                        className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                    >
                                        {SUPPORTED_SHADOWSOCKS_METHODS.map(method => (
                                            <option key={method} value={method}>{method}</option>
                                        ))}
                                    </select>
                                </div>
                            </div>

                            {isShadowsocks && (
                                <label className="flex cursor-pointer items-center gap-2">
                                    <input
                                        type="checkbox"
                                        checked={!!formData.udp_fragment}
                                        onChange={e => updateForm(prev => ({ ...prev, udp_fragment: e.target.checked }))}
                                        className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                    />
                                    <span className="text-xs font-medium text-white">Enable UDP Fragment</span>
                                </label>
                            )}

                            <div className="grid grid-cols-1 gap-4">
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Server Password</label>
                                    <div className="flex gap-2">
                                        <input
                                            type="text"
                                            value={formData.password || ''}
                                            onChange={e => updateForm(prev => ({ ...prev, password: e.target.value }))}
                                            className="flex-1 rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white transition-colors focus:border-blue-500 focus:outline-none"
                                            placeholder="Required for the inbound"
                                        />
                                        <Button
                                            type="button"
                                            variant="icon"
                                            size="icon"
                                            className="h-[2.625rem] w-[2.625rem] shrink-0 p-0"
                                            onClick={() => void generateShadowsocksPassword()}
                                            disabled={shadowsocksServerPasswordLoading}
                                            title={`Generate base64 password using sing-box (${keyLengthForShadowsocksMethod(formData.method || '')} bytes)`}
                                        >
                                            <RefreshCw size={16} className={shadowsocksServerPasswordLoading ? 'animate-spin' : ''} />
                                        </Button>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                )}

                {visibility.showHysteria2Password && (
                    <div className="space-y-4">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Hysteria2</h3>
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
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

                {visibility.showAnyTLSSection && (
                    <div className="space-y-4">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">AnyTLS</h3>
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                            <div className="rounded-lg border border-slate-700/80 bg-slate-900/60 px-3 py-2 text-xs text-slate-300">
                                AnyTLS always requires TLS. Users are managed per-user below. Padding scheme is optional and advanced.
                            </div>
                            {isAnyTLS && (
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Padding Scheme (optional, one rule per line)</label>
                                    <textarea
                                        value={(formData.padding_scheme || []).join('\n')}
                                        onChange={e => updateForm((prev: any) => ({
                                            ...prev,
                                            padding_scheme: e.target.value.split('\n').map((s: string) => s.trim()).filter(Boolean),
                                        }))}
                                        rows={3}
                                        className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 font-mono text-xs text-white focus:border-blue-500 focus:outline-none"
                                        placeholder="0-99=random_padding(0,100)"
                                    />
                                </div>
                            )}
                        </div>
                    </div>
                )}

                {visibility.showNaiveSection && (
                    <div className="space-y-4">
                        <h3 className="text-sm font-medium uppercase tracking-wider text-slate-400">Naive</h3>
                        <div className="grid grid-cols-1 gap-4 rounded-lg border border-slate-800/50 bg-slate-950/50 p-4">
                            <div className="rounded-lg border border-slate-700/80 bg-slate-900/60 px-3 py-2 text-xs text-slate-300">
                                Naive always requires TLS. Users are managed per-user below.
                            </div>
                            {isNaive && (
                                <>
                                    <div className="space-y-1">
                                        <label className="text-xs font-medium text-slate-300">Network</label>
                                        <select
                                            value={formData.network || 'tcp'}
                                            onChange={e => updateForm((prev: any) => ({ ...prev, network: e.target.value }))}
                                            className="select-field w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                        >
                                            <option value="tcp">TCP (HTTPS)</option>
                                            <option value="udp">UDP (QUIC)</option>
                                        </select>
                                    </div>
                                    {formData.network === 'udp' && (
                                        <div className="space-y-1">
                                            <label className="text-xs font-medium text-slate-300">QUIC Congestion Control (optional)</label>
                                            <input
                                                type="text"
                                                value={formData.quic_congestion_control || ''}
                                                onChange={e => updateForm((prev: any) => ({ ...prev, quic_congestion_control: e.target.value }))}
                                                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                                                placeholder="e.g. bbr"
                                            />
                                        </div>
                                    )}
                                </>
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
            <ConfirmModal
                isOpen={showRenameConfirm}
                onClose={() => {
                    if (saveLoading) return
                    setShowRenameConfirm(false)
                    setPendingRenameSubmission(null)
                }}
                onConfirm={() => pendingRenameSubmission ? submitPayload(pendingRenameSubmission) : undefined}
                title="Change inbound tag?"
                message="Renaming an inbound changes its identifier across the managed Sing-box flow. The update will still be sent through the existing owner path keyed by the current inbound record."
                confirmLabel="Rename Inbound"
                confirmTone="danger"
                isLoading={saveLoading}
            >
                <div className="grid grid-cols-2 gap-4 text-sm">
                    <div className="space-y-1">
                        <p className="text-slate-400">Current tag</p>
                        <p className="font-mono text-slate-200">{originalTag}</p>
                    </div>
                    <div className="space-y-1">
                        <p className="text-slate-400">New tag</p>
                        <p className="font-mono text-slate-200">{pendingRenameTag}</p>
                    </div>
                </div>
            </ConfirmModal>
        </>
    )
}
