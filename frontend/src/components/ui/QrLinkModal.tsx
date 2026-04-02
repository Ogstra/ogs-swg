import { useEffect, useState } from 'react'
import QRCode from 'react-qr-code'
import { Copy, Check } from 'lucide-react'
import { Modal } from './Modal'
import { Button } from './Button'

interface LinkVariant {
    id: string
    label: string
    link: string
}

interface Props {
    isOpen: boolean
    onClose: () => void
    title?: string
    link: string
    linkVariants?: LinkVariant[]
    loading?: boolean
    error?: string
}

/**
 * Shared QR code modal used by both /subscriptions and /user-management.
 * Shows a scannable QR for any URL plus a copyable link input.
 */
export function QrLinkModal({ isOpen, onClose, title = 'QR Code', link, linkVariants, loading = false, error }: Props) {
    const [copied, setCopied] = useState(false)
    const [selectedVariantId, setSelectedVariantId] = useState<string>('default')

    const variants = (linkVariants && linkVariants.length > 0)
        ? linkVariants
        : [{ id: 'default', label: 'Link', link }]
    const selectedVariant = variants.find(variant => variant.id === selectedVariantId) || variants[0]
    const activeLink = selectedVariant?.link || link || ''

    useEffect(() => {
        setSelectedVariantId(variants[0]?.id || 'default')
        setCopied(false)
    }, [isOpen, link, linkVariants])

    const handleCopy = async () => {
        if (!activeLink) return
        try {
            if (navigator.clipboard?.writeText) {
                await navigator.clipboard.writeText(activeLink)
            } else {
                const ta = document.createElement('textarea')
                ta.value = activeLink
                ta.style.position = 'absolute'
                ta.style.left = '-9999px'
                document.body.appendChild(ta)
                ta.select()
                document.execCommand('copy')
                document.body.removeChild(ta)
            }
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
        } catch {
            // silently fail
        }
    }

    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={title}
            size="sm"
            footer={<Button variant="ghost" className="w-full" onClick={onClose}>Close</Button>}
        >
            <div className="flex flex-col items-center space-y-4">
                {variants.length > 1 && (
                    <div className="grid w-full grid-cols-3 gap-2">
                        {variants.map(variant => (
                            <Button
                                key={variant.id}
                                size="sm"
                                variant={selectedVariant?.id === variant.id ? 'primary' : 'secondary'}
                                onClick={() => setSelectedVariantId(variant.id)}
                            >
                                {variant.label}
                            </Button>
                        ))}
                    </div>
                )}

                {error ? (
                    <div className="w-full bg-red-900/20 border border-red-700/40 rounded-lg p-3 text-xs text-red-300">
                        {error}
                    </div>
                ) : (
                    <div className="relative p-4 bg-white rounded-xl shadow-lg w-full">
                        <div className={loading ? 'blur-sm opacity-70' : ''}>
                            <QRCode
                                size={256}
                                style={{ height: 'auto', maxWidth: '100%', width: '100%' }}
                                value={activeLink || 'placeholder'}
                                viewBox="0 0 256 256"
                            />
                        </div>
                        {loading && (
                            <div className="absolute inset-0 flex items-center justify-center">
                                <div className="w-8 h-8 rounded-full border-2 border-slate-300 border-t-slate-700 animate-spin" />
                            </div>
                        )}
                    </div>
                )}

                <div className="w-full space-y-2">
                    <label className="text-xs font-semibold text-slate-500 uppercase tracking-wider">{selectedVariant?.label || 'Link'}</label>
                    <div className="flex gap-2">
                        <input
                            readOnly
                            value={loading ? 'Loading…' : activeLink}
                            className="w-full bg-slate-950 border border-slate-800 rounded px-2 py-1.5 text-xs text-slate-400 font-mono focus:outline-none"
                        />
                        <Button
                            size="sm"
                            variant="secondary"
                            onClick={handleCopy}
                            icon={copied ? <Check size={14} /> : <Copy size={14} />}
                            disabled={!activeLink || loading}
                        >
                            {copied ? 'Copied' : 'Copy'}
                        </Button>
                    </div>
                </div>
            </div>
        </Modal>
    )
}
