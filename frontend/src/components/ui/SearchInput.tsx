import { useEffect, useRef, useState } from 'react'
import { Search, X } from 'lucide-react'

interface SearchInputProps {
    value: string
    onChange: (value: string) => void
    placeholder?: string
    // Always applied.
    className?: string
    // Applied only while the mobile field is collapsed to the magnifier / expanded.
    collapsedClassName?: string
    expandedClassName?: string
    // Breakpoint from which the full field is always shown inline instead of the magnifier.
    inlineFrom?: 'md' | '2xl'
}

const INLINE_CLASSES = {
    md: { hideButton: 'md:hidden', showField: 'hidden md:block' },
    '2xl': { hideButton: '2xl:hidden', showField: 'hidden 2xl:block' },
} as const

// Below md the control is a magnifier button that expands into the field; from
// md up it is always the full field.
export function SearchInput({
    value,
    onChange,
    placeholder = 'Search...',
    className = '',
    collapsedClassName = '',
    expandedClassName = '',
    inlineFrom = 'md',
}: SearchInputProps) {
    const inline = INLINE_CLASSES[inlineFrom]
    const [open, setOpen] = useState(false)
    const inputRef = useRef<HTMLInputElement>(null)
    const showField = open || value !== ''

    useEffect(() => {
        if (open) inputRef.current?.focus()
    }, [open])

    const clear = () => {
        onChange('')
        setOpen(false)
    }

    return (
        <div className={`${showField ? expandedClassName : collapsedClassName} ${className}`}>
            {!showField && (
                <button
                    type="button"
                    onClick={() => setOpen(true)}
                    aria-label={placeholder}
                    className={`${inline.hideButton} inline-flex h-9 w-9 items-center justify-center rounded-lg border border-slate-700 bg-slate-800 text-slate-300 hover:bg-slate-700`}
                >
                    <Search size={16} />
                </button>
            )}
            <div className={`relative w-full ${showField ? 'block' : inline.showField}`}>
                <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                <input
                    ref={inputRef}
                    type="text"
                    value={value}
                    onChange={e => onChange(e.target.value)}
                    onBlur={() => { if (value === '') setOpen(false) }}
                    placeholder={placeholder}
                    aria-label={placeholder}
                    className="w-full rounded-lg border border-slate-700 bg-slate-800 py-2 pl-9 pr-8 text-sm text-slate-200 placeholder-slate-500 outline-none transition-colors focus:border-blue-500"
                />
                {showField && (
                    <button
                        type="button"
                        onMouseDown={e => e.preventDefault()}
                        onClick={clear}
                        aria-label="Clear search"
                        className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-slate-500 hover:text-slate-200"
                    >
                        <X size={14} />
                    </button>
                )}
            </div>
        </div>
    )
}
