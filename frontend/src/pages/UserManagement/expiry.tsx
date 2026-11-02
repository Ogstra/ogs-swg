export type ExpiryMode = 'never' | 'date' | 'first_use'

export interface ExpiryDraft {
    mode: ExpiryMode
    date: string // YYYY-MM-DD, local
    days: string
}

export const emptyExpiry = (): ExpiryDraft => ({ mode: 'never', date: '', days: '30' })

const pad = (n: number) => String(n).padStart(2, '0')

export const toDateInput = (unixSeconds: number): string => {
    const d = new Date(unixSeconds * 1000)
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

// endOfLocalDay is the last second of the chosen day, so "expires on the 5th"
// means the user works through the 5th.
export const endOfLocalDay = (date: string): number => {
    const [y, m, d] = date.split('-').map(Number)
    if (!y || !m || !d) return 0
    return Math.floor(new Date(y, m - 1, d, 23, 59, 59).getTime() / 1000)
}

export const expiryFromUser = (expiresAt?: number, afterDays?: number): ExpiryDraft => {
    if (expiresAt && expiresAt > 0) return { mode: 'date', date: toDateInput(expiresAt), days: '30' }
    if (afterDays && afterDays > 0) return { mode: 'first_use', date: '', days: String(afterDays) }
    return emptyExpiry()
}

// expiryToRequest returns the two request fields for a draft. Both are always
// sent, so switching modes clears the other setting.
export const expiryToRequest = (draft: ExpiryDraft): { expires_at: number; expire_after_days: number } => {
    switch (draft.mode) {
        case 'date':
            return { expires_at: endOfLocalDay(draft.date), expire_after_days: 0 }
        case 'first_use':
            return { expires_at: 0, expire_after_days: Math.max(0, Math.floor(Number(draft.days) || 0)) }
        default:
            return { expires_at: 0, expire_after_days: 0 }
    }
}

export const expiryDraftIsValid = (draft: ExpiryDraft): boolean => {
    if (draft.mode === 'date') return endOfLocalDay(draft.date) > 0
    if (draft.mode === 'first_use') return Math.floor(Number(draft.days) || 0) > 0
    return true
}

// expiryFromTemplate turns "valid for N days" into a draft for a new user.
export const expiryFromTemplate = (days: number, onFirstUse: boolean): ExpiryDraft => {
    if (!days || days <= 0) return emptyExpiry()
    if (onFirstUse) return { mode: 'first_use', date: '', days: String(days) }
    return { mode: 'date', date: toDateInput(Math.floor(Date.now() / 1000) + days * 86400), days: String(days) }
}

// describeExpiry is the short label for lists, or null when nothing is set.
export const describeExpiry = (expiresAt?: number, afterDays?: number, now: number = Math.floor(Date.now() / 1000)): { text: string; tone: 'ok' | 'soon' | 'expired' | 'pending' } | null => {
    if (expiresAt && expiresAt > 0) {
        const left = expiresAt - now
        if (left <= 0) return { text: 'Expired', tone: 'expired' }
        const days = Math.ceil(left / 86400)
        return { text: days <= 1 ? 'Expires today' : `Expires in ${days}d`, tone: days <= 3 ? 'soon' : 'ok' }
    }
    if (afterDays && afterDays > 0) return { text: `${afterDays}d from first use`, tone: 'pending' }
    return null
}

interface ExpiryFieldsProps {
    value: ExpiryDraft
    onChange: (next: ExpiryDraft) => void
    disabled?: boolean
}

const fieldClass = 'w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors'

export function ExpiryFields({ value, onChange, disabled }: ExpiryFieldsProps) {
    return (
        <div className="grid grid-cols-2 gap-4">
            <div>
                <label className="block text-sm font-medium text-slate-400 mb-1">Expiry</label>
                <select
                    value={value.mode}
                    disabled={disabled}
                    onChange={e => onChange({ ...value, mode: e.target.value as ExpiryMode })}
                    className={`select-field ${fieldClass}`}
                >
                    <option value="never">Never</option>
                    <option value="date">On a date</option>
                    <option value="first_use">Days after first use</option>
                </select>
            </div>
            {value.mode === 'date' && (
                <div>
                    <label className="block text-sm font-medium text-slate-400 mb-1">Last day</label>
                    <input type="date" disabled={disabled} value={value.date} onChange={e => onChange({ ...value, date: e.target.value })} className={fieldClass} />
                </div>
            )}
            {value.mode === 'first_use' && (
                <div>
                    <label className="block text-sm font-medium text-slate-400 mb-1">Days</label>
                    <input type="number" min={1} disabled={disabled} value={value.days} onChange={e => onChange({ ...value, days: e.target.value })} className={fieldClass} />
                </div>
            )}
        </div>
    )
}
