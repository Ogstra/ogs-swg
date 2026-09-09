import { useEffect, useState } from 'react'
import { api } from '../../../services/api'
import { useToast } from '../../../context/ToastContext'
import { Button } from '../../../components/ui/Button'
import { Modal } from '../../../components/ui/Modal'

interface WireGuardRawConfigModalProps {
    isOpen: boolean
    iface: string | null
    canWriteWG: boolean
    onClose: () => void
    onSaved: (iface: string) => Promise<void> | void
}

export function WireGuardRawConfigModal({
    isOpen,
    iface,
    canWriteWG,
    onClose,
    onSaved,
}: WireGuardRawConfigModalProps) {
    const { success, error: toastError } = useToast()
    const [loadingConfig, setLoadingConfig] = useState(false)
    const [savingConfig, setSavingConfig] = useState(false)
    const [configText, setConfigText] = useState('')

    useEffect(() => {
        if (!isOpen || !iface) return

        let canceled = false
        const load = async () => {
            setLoadingConfig(true)
            try {
                const nextConfig = await api.getWireGuardConfig(iface)
                if (!canceled) {
                    setConfigText(nextConfig)
                }
            } catch (err) {
                if (!canceled) {
                    toastError('Failed to load WireGuard raw config: ' + err)
                }
            } finally {
                if (!canceled) {
                    setLoadingConfig(false)
                }
            }
        }

        void load()
        return () => {
            canceled = true
        }
    }, [isOpen, iface, toastError])

    const handleSave = async () => {
        if (!iface) return
        if (!canWriteWG) {
            toastError('No write permission for WireGuard')
            return
        }
        if (!/^\s*\[Interface\]\s*$/m.test(configText)) {
            toastError('Config must contain an [Interface] section')
            return
        }

        setSavingConfig(true)
        try {
            await api.updateWireGuardConfig(configText, iface)
            await onSaved(iface)
            success(`WireGuard raw config saved for ${iface}`)
            onClose()
        } catch (err) {
            toastError('Failed to save WireGuard raw config: ' + err)
        } finally {
            setSavingConfig(false)
        }
    }

    return (
        <Modal
            isOpen={isOpen}
            onClose={() => {
                if (savingConfig) return
                onClose()
            }}
            size="xl"
            title={iface ? `Raw Config — ${iface}` : 'Raw Config'}
            footer={
                <>
                    <Button variant="ghost" onClick={onClose} disabled={savingConfig}>
                        Cancel
                    </Button>
                    <Button variant="primary" onClick={handleSave} isLoading={savingConfig} disabled={!canWriteWG || loadingConfig || !iface}>
                        Save
                    </Button>
                </>
            }
        >
            <div className="space-y-3">
                {loadingConfig ? (
                    <div className="text-sm text-slate-400 animate-pulse">Loading raw config...</div>
                ) : (
                    <textarea
                        value={configText}
                        onChange={(e) => setConfigText(e.target.value)}
                        className="w-full min-h-[420px] bg-slate-950 border border-slate-800 rounded-lg p-3 text-white outline-none focus:border-blue-500/50 transition-colors font-mono text-xs leading-6"
                        spellCheck={false}
                    />
                )}
                <p className="text-xs text-slate-500">
                    Save only valid WireGuard config. If backend rejects changes, the modal stays open for corrections.
                </p>
            </div>
        </Modal>
    )
}
