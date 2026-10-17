import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Search, X } from 'lucide-react'

interface SearchInputProps {
    value: string
    onChange: (value: string) => void
    placeholder?: string
    // Classes for the inline (lg and up) field.
    className?: string
}

// From lg up the field is rendered inline in the page toolbar. Below lg the
// layout has a top app bar, so the control becomes a magnifier in that bar
// (slot provided by Layout) that expands over the page title — it never adds a
// row to the page's own toolbar.
export function SearchInput({ value, onChange, placeholder = 'Search...', className = '' }: SearchInputProps) {
    const [open, setOpen] = useState(false)
    const [slot, setSlot] = useState<HTMLElement | null>(null)
    const barInputRef = useRef<HTMLInputElement>(null)
    const expanded = open || value !== ''

    useEffect(() => {
        setSlot(document.getElementById('page-search-slot'))
    }, [])

    useEffect(() => {
        if (open) barInputRef.current?.focus()
    }, [open])

    const clear = () => {
        onChange('')
        setOpen(false)
    }

    const fieldClasses = 'w-full rounded-lg border border-slate-700 bg-slate-800 py-2 pl-9 pr-8 text-sm text-slate-200 placeholder-slate-500 outline-none transition-colors focus:border-blue-500'

    const clearButton = value !== '' && (
        <button
            type="button"
            onMouseDown={e => e.preventDefault()}
            onClick={clear}
            aria-label="Clear search"
            className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-slate-500 hover:text-slate-200"
        >
            <X size={14} />
        </button>
    )

    return (
        <>
            <div className={`relative hidden lg:block ${className}`}>
                <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                <input
                    type="text"
                    value={value}
                    onChange={e => onChange(e.target.value)}
                    placeholder={placeholder}
                    aria-label={placeholder}
                    className={fieldClasses}
                />
                {clearButton}
            </div>
            {slot && createPortal(
                expanded ? (
                    <div className="absolute right-0 top-1/2 z-10 w-[calc(100vw-5rem)] -translate-y-1/2 bg-slate-900">
                        <div className="relative">
                            <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                            <input
                                ref={barInputRef}
                                type="text"
                                value={value}
                                onChange={e => onChange(e.target.value)}
                                onBlur={() => { if (value === '') setOpen(false) }}
                                onKeyDown={e => { if (e.key === 'Escape') clear() }}
                                placeholder={placeholder}
                                aria-label={placeholder}
                                className={fieldClasses}
                            />
                            <button
                                type="button"
                                onMouseDown={e => e.preventDefault()}
                                onClick={clear}
                                aria-label="Close search"
                                className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-slate-500 hover:text-slate-200"
                            >
                                <X size={14} />
                            </button>
                        </div>
                    </div>
                ) : (
                    <button
                        type="button"
                        onClick={() => setOpen(true)}
                        aria-label={placeholder}
                        className="inline-flex h-10 w-10 items-center justify-center rounded-lg text-slate-400 hover:text-white"
                    >
                        <Search size={22} />
                    </button>
                ),
                slot,
            )}
        </>
    )
}
