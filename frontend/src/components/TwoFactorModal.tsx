import { useEffect, useState } from 'react'
import QRCode from 'react-qr-code'
import { api } from '../services/api'
import { useToast } from '../context/ToastContext'
import { Modal } from './ui/Modal'
import { Button } from './ui/Button'

type Stage = 'loading' | 'off' | 'scan' | 'codes' | 'on'

const inputClass = 'w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-white outline-none focus:border-blue-500'

export function TwoFactorModal({ onClose }: { onClose: () => void }) {
    const { success, error } = useToast()
    const [stage, setStage] = useState<Stage>('loading')
    const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
    const [code, setCode] = useState('')
    const [password, setPassword] = useState('')
    const [recovery, setRecovery] = useState<string[]>([])
    const [busy, setBusy] = useState(false)

    useEffect(() => {
        api.getTwoFactorStatus()
            .then(status => setStage(status.enabled ? 'on' : 'off'))
            .catch(() => {
                error('Failed to load two-factor status')
                onClose()
            })
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    const run = async (action: () => Promise<void>) => {
        setBusy(true)
        try {
            await action()
        } catch (err) {
            error(err instanceof Error ? err.message : 'Request failed')
        } finally {
            setBusy(false)
        }
    }

    const startSetup = () => run(async () => {
        setSetup(await api.setupTwoFactor())
        setCode('')
        setStage('scan')
    })

    const confirmSetup = () => run(async () => {
        const result = await api.enableTwoFactor(code)
        setRecovery(result.recovery_codes)
        setCode('')
        setStage('codes')
    })

    const disable = () => run(async () => {
        await api.disableTwoFactor(password, code)
        success('Two-factor authentication disabled')
        setPassword('')
        setCode('')
        setStage('off')
    })

    return (
        <Modal isOpen onClose={onClose} title="Two-factor authentication" size="md" footer={<Button variant="secondary" onClick={onClose}>Close</Button>}>
            {stage === 'loading' && <p className="py-6 text-center text-sm text-slate-400">Loading…</p>}

            {stage === 'off' && (
                <div className="space-y-4">
                    <p className="text-sm text-slate-300">
                        Ask for a code from an authenticator app (Google Authenticator, Aegis, 1Password…) at every sign-in.
                    </p>
                    <Button variant="primary" onClick={startSetup} disabled={busy}>Set up</Button>
                </div>
            )}

            {stage === 'scan' && setup && (
                <div className="space-y-4">
                    <p className="text-sm text-slate-300">Scan this code with your authenticator app, then enter the 6-digit code it shows.</p>
                    <div className="flex justify-center rounded-lg bg-white p-3">
                        <QRCode value={setup.uri} size={168} />
                    </div>
                    <p className="break-all text-center font-mono text-xs text-slate-400">{setup.secret}</p>
                    <input
                        className={`${inputClass} font-mono tracking-widest`}
                        inputMode="numeric"
                        autoComplete="one-time-code"
                        placeholder="123456"
                        value={code}
                        onChange={event => setCode(event.target.value)}
                    />
                    <Button variant="primary" onClick={confirmSetup} disabled={busy || code.trim().length < 6}>Enable</Button>
                </div>
            )}

            {stage === 'codes' && (
                <div className="space-y-4">
                    <p className="text-sm text-slate-300">
                        Two-factor authentication is on. Save these recovery codes now; each works once if you lose your device, and they are not shown again.
                    </p>
                    <div className="grid grid-cols-2 gap-2 rounded-lg border border-slate-800 bg-slate-950 p-3 font-mono text-sm text-slate-100">
                        {recovery.map(item => <span key={item}>{item}</span>)}
                    </div>
                    <Button variant="secondary" onClick={() => { void navigator.clipboard?.writeText(recovery.join('\n')); success('Copied') }}>Copy</Button>
                </div>
            )}

            {stage === 'on' && (
                <div className="space-y-4">
                    <p className="text-sm text-slate-300">Two-factor authentication is on. To turn it off, enter your password and a current code.</p>
                    <input className={inputClass} type="password" autoComplete="current-password" placeholder="Password" value={password} onChange={event => setPassword(event.target.value)} />
                    <input
                        className={`${inputClass} font-mono tracking-widest`}
                        inputMode="numeric"
                        autoComplete="one-time-code"
                        placeholder="123456"
                        value={code}
                        onChange={event => setCode(event.target.value)}
                    />
                    <Button variant="danger" onClick={disable} disabled={busy || !password || code.trim().length < 6}>Turn off</Button>
                </div>
            )}
        </Modal>
    )
}
