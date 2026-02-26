import React from 'react'

type Tone = 'default' | 'primary' | 'danger'

interface ActionIconButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
    tone?: Tone
    children: React.ReactNode
}

export function ActionIconButton({ tone = 'default', children, className = '', ...props }: ActionIconButtonProps) {
    const toneClass =
        tone === 'primary'
            ? 'hover:text-blue-400'
            : tone === 'danger'
                ? 'hover:text-red-400'
                : 'hover:text-white'

    return (
        <button
            {...props}
            className={`p-2 rounded-lg bg-slate-800 text-slate-300 ${toneClass} hover:bg-slate-700 border border-slate-700 transition-all disabled:opacity-50 disabled:cursor-not-allowed ${className}`}
        >
            {children}
        </button>
    )
}
