import { useId, useState } from 'react'
import { X } from 'lucide-react'

interface ChipInputProps {
    value: string[]
    onChange: (next: string[]) => void
    suggestions?: string[]
    placeholder?: string
    disabled?: boolean
    // Called with a pasted or typed chunk; returns the values to add.
    parse?: (text: string) => string[]
    ariaLabel?: string
}

const defaultParse = (text: string) => text.split(/[\s,]+/).map(part => part.trim()).filter(Boolean)

// ChipInput edits a list of short values: type or paste, Enter or comma adds.
export function ChipInput({ value, onChange, suggestions = [], placeholder, disabled, parse = defaultParse, ariaLabel }: ChipInputProps) {
    const [draft, setDraft] = useState('')
    const listId = useId()

    const commit = (text: string) => {
        const added = parse(text).filter(item => !value.includes(item))
        if (added.length > 0) onChange([...value, ...added])
        setDraft('')
    }

    return (
        <div className={`flex min-h-[38px] flex-wrap items-center gap-1.5 rounded-lg border border-slate-800 bg-slate-950 px-2 py-1.5 focus-within:border-blue-500/50 ${disabled ? 'opacity-60' : ''}`}>
            {value.map(item => (
                <span key={item} className="inline-flex max-w-full items-center gap-1 rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-100">
                    <span className="truncate font-mono">{item}</span>
                    {!disabled && (
                        <button type="button" aria-label={`Remove ${item}`} onClick={() => onChange(value.filter(existing => existing !== item))} className="text-slate-400 hover:text-white">
                            <X size={12} />
                        </button>
                    )}
                </span>
            ))}
            <input
                list={listId}
                value={draft}
                disabled={disabled}
                aria-label={ariaLabel}
                placeholder={value.length === 0 ? placeholder : ''}
                onChange={event => {
                    const text = event.target.value
                    if (/[,\n]$/.test(text)) commit(text)
                    else setDraft(text)
                }}
                onPaste={event => {
                    const text = event.clipboardData.getData('text')
                    if (/[\s,]/.test(text.trim())) {
                        event.preventDefault()
                        commit(draft + text)
                    }
                }}
                onKeyDown={event => {
                    if (event.key === 'Enter') {
                        event.preventDefault()
                        if (draft.trim()) commit(draft)
                    } else if (event.key === 'Backspace' && draft === '' && value.length > 0) {
                        onChange(value.slice(0, -1))
                    }
                }}
                onBlur={() => { if (draft.trim()) commit(draft) }}
                className="min-w-[8rem] flex-1 bg-transparent px-1 py-0.5 text-sm text-white outline-none placeholder:text-slate-600"
            />
            {suggestions.length > 0 && (
                <datalist id={listId}>
                    {suggestions.filter(item => !value.includes(item)).map(item => <option key={item} value={item} />)}
                </datalist>
            )}
        </div>
    )
}
