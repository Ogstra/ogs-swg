import { useEffect, useRef, useState, type ReactNode } from 'react'
import { MoreVertical } from 'lucide-react'
import { ActionIconButton } from './ActionIconButton'

export interface ActionMenuItem {
    label: string
    icon?: ReactNode
    onClick: () => void
    disabled?: boolean
    tone?: 'default' | 'danger'
}

export function ActionMenu({ items, label = 'More actions' }: { items: ActionMenuItem[]; label?: string }) {
    const [open, setOpen] = useState(false)
    const ref = useRef<HTMLDivElement>(null)

    useEffect(() => {
        if (!open) return
        const onPointerDown = (event: MouseEvent) => {
            if (!ref.current?.contains(event.target as Node)) setOpen(false)
        }
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') setOpen(false)
        }
        document.addEventListener('mousedown', onPointerDown)
        document.addEventListener('keydown', onKeyDown)
        return () => {
            document.removeEventListener('mousedown', onPointerDown)
            document.removeEventListener('keydown', onKeyDown)
        }
    }, [open])

    return (
        <div ref={ref} className="relative">
            <ActionIconButton
                type="button"
                aria-haspopup="menu"
                aria-expanded={open}
                aria-label={label}
                title={label}
                onClick={() => setOpen(value => !value)}
            >
                <MoreVertical size={16} />
            </ActionIconButton>
            {open && (
                <div role="menu" className="absolute right-0 top-full z-30 mt-1 min-w-[11rem] rounded-lg border border-slate-700 bg-slate-900 p-1 shadow-xl">
                    {items.map(item => (
                        <button
                            key={item.label}
                            type="button"
                            role="menuitem"
                            disabled={item.disabled}
                            onClick={() => {
                                setOpen(false)
                                item.onClick()
                            }}
                            className={`flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm transition-colors disabled:cursor-not-allowed ${item.tone === 'danger' ? 'text-red-400 hover:bg-red-500/10' : 'text-slate-200 hover:bg-slate-800'}`}
                        >
                            {item.icon}
                            {item.label}
                        </button>
                    ))}
                </div>
            )}
        </div>
    )
}
