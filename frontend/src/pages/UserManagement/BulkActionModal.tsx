import { useState } from 'react'
import { api, type BulkUserActionRequest, type BulkUserActionResponse } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { Modal } from '../../components/ui/Modal'
import { Button } from '../../components/ui/Button'
import { endOfLocalDay } from './expiry'

const BYTES_PER_GB = 1024 * 1024 * 1024
const inputClass = 'w-full rounded-lg border border-slate-800 bg-slate-950 p-2.5 text-white outline-none transition-colors focus:border-blue-500/50'

type Action = BulkUserActionRequest['action']

const ACTIONS: { id: Action; label: string }[] = [
    { id: 'extend_days', label: 'Add days to the expiry' },
    { id: 'add_quota', label: 'Add data to the quota' },
    { id: 'set_quota', label: 'Set the quota' },
    { id: 'set_expiry', label: 'Set the expiry date' },
    { id: 'reset_usage', label: 'Reset usage (start counting from now)' },
    { id: 'delete', label: 'Delete users' },
]

interface Props {
    names: string[]
    onClose: () => void
    onDone: () => void
}

export function BulkActionModal({ names, onClose, onDone }: Props) {
    const { success, error } = useToast()
    const [action, setAction] = useState<Action>('extend_days')
    const [days, setDays] = useState('30')
    const [gb, setGb] = useState('')
    const [date, setDate] = useState('')
    const [confirmDelete, setConfirmDelete] = useState(false)
    const [busy, setBusy] = useState(false)
    const [result, setResult] = useState<BulkUserActionResponse | null>(null)

    const parseGb = () => {
        const value = parseFloat(gb.replace(',', '.'))
        return Number.isFinite(value) && value >= 0 ? Math.round(value * BYTES_PER_GB) : NaN
    }

    const buildRequest = (): BulkUserActionRequest | string => {
        switch (action) {
            case 'extend_days': {
                const n = Math.trunc(Number(days))
                if (!n) return 'Enter how many days to add (use a negative number to remove days).'
                return { names, action, days: n }
            }
            case 'add_quota': {
                const bytes = parseGb()
                if (!(bytes > 0)) return 'Enter how many GB to add.'
                return { names, action, quota_bytes: bytes }
            }
            case 'set_quota': {
                const bytes = parseGb()
                if (Number.isNaN(bytes)) return 'Enter the quota in GB (0 for unlimited).'
                return { names, action, quota_bytes: bytes }
            }
            case 'set_expiry':
                return { names, action, expires_at: date ? endOfLocalDay(date) : 0 }
            case 'reset_usage':
                return { names, action }
            default:
                return { names, action: 'delete' }
        }
    }

    const apply = async () => {
        const request = buildRequest()
        if (typeof request === 'string') {
            error(request)
            return
        }
        setBusy(true)
        try {
            const response = await api.bulkUserAction(request)
            setResult(response)
            if (response.failed === 0) {
                success(`Applied to ${response.done} user${response.done === 1 ? '' : 's'}`)
                onDone()
                onClose()
            } else {
                onDone()
            }
        } catch (err) {
            error(err instanceof Error ? err.message : 'Failed to apply the action')
        } finally {
            setBusy(false)
        }
    }

    const failures = result?.results.filter(item => !item.ok) ?? []
    const deleting = action === 'delete'

    return (
        <Modal
            isOpen
            onClose={onClose}
            title={`${names.length} user${names.length === 1 ? '' : 's'} selected`}
            footer={
                <>
                    <Button variant="ghost" onClick={onClose}>{result ? 'Close' : 'Cancel'}</Button>
                    <Button
                        variant={deleting ? 'danger' : 'primary'}
                        onClick={apply}
                        disabled={busy || (deleting && !confirmDelete) || !!result}
                        isLoading={busy}
                    >
                        {deleting ? 'Delete' : 'Apply'}
                    </Button>
                </>
            }
        >
            <div className="space-y-4">
                <div className="space-y-1">
                    <label className="text-sm font-medium text-slate-400">Action</label>
                    <select className={`select-field ${inputClass}`} value={action} disabled={!!result} onChange={e => { setAction(e.target.value as Action); setConfirmDelete(false) }}>
                        {ACTIONS.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
                    </select>
                </div>

                {action === 'extend_days' && (
                    <div className="space-y-1">
                        <label className="text-sm font-medium text-slate-400">Days</label>
                        <input className={inputClass} type="number" value={days} disabled={!!result} onChange={e => setDays(e.target.value)} />
                        <p className="text-xs text-slate-400">Expired users restart from today. Users counting from their first use get more days. Users with no expiry get one.</p>
                    </div>
                )}
                {(action === 'add_quota' || action === 'set_quota') && (
                    <div className="space-y-1">
                        <label className="text-sm font-medium text-slate-400">{action === 'add_quota' ? 'GB to add' : 'Quota (GB)'}</label>
                        <input className={inputClass} inputMode="decimal" value={gb} disabled={!!result} onChange={e => setGb(e.target.value)} placeholder={action === 'set_quota' ? '0 for unlimited' : '10'} />
                        {action === 'add_quota' && <p className="text-xs text-slate-400">Users with an unlimited quota are skipped.</p>}
                    </div>
                )}
                {action === 'set_expiry' && (
                    <div className="space-y-1">
                        <label className="text-sm font-medium text-slate-400">Last day</label>
                        <input className={inputClass} type="date" value={date} disabled={!!result} onChange={e => setDate(e.target.value)} />
                        <p className="text-xs text-slate-400">Leave it empty to remove the expiry.</p>
                    </div>
                )}
                {action === 'reset_usage' && (
                    <p className="text-xs text-slate-400">Usage counts from now for the quota. The recorded traffic is kept. Users that were disabled for being over quota come back.</p>
                )}
                {deleting && (
                    <label className="flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-200">
                        <input type="checkbox" className="mt-0.5 h-4 w-4 shrink-0" checked={confirmDelete} disabled={!!result} onChange={e => setConfirmDelete(e.target.checked)} />
                        <span>Delete these users from the config, their subscriptions and their stored data. This cannot be undone.</span>
                    </label>
                )}

                {failures.length > 0 && (
                    <div className="space-y-1 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-100">
                        <p className="font-medium">{result?.done} applied, {failures.length} not:</p>
                        <ul className="max-h-40 list-disc space-y-0.5 overflow-auto pl-5 text-xs">
                            {failures.map(item => <li key={item.name}><span className="font-mono">{item.name}</span>: {item.error}</li>)}
                        </ul>
                    </div>
                )}
            </div>
        </Modal>
    )
}
