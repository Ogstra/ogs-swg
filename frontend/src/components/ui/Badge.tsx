import React from 'react'

interface BadgeProps {
    children: React.ReactNode
    variant?: 'success' | 'warning' | 'error' | 'info' | 'neutral'
    className?: string
    title?: string
    // wrap lets a long label break onto several lines instead of being cut with an ellipsis.
    wrap?: boolean
}

export function Badge({ children, variant = 'neutral', className = '', title, wrap = false }: BadgeProps) {
    const variants = {
        success: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
        warning: 'bg-amber-500/10 text-amber-400 border-amber-500/20',
        error: 'bg-red-500/10 text-red-400 border-red-500/20',
        info: 'bg-slate-800 text-slate-200 border-slate-600',
        neutral: 'bg-slate-800 text-slate-400 border-slate-700'
    }

    return (
        <span title={title} className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 ${wrap ? 'rounded-xl' : 'rounded-full'} text-xs font-medium border ${variants[variant]} ${className}`}>
            <span className={`inline-flex min-w-0 items-center gap-1.5 align-middle ${wrap ? 'whitespace-normal [overflow-wrap:anywhere]' : 'truncate'}`}>
                {children}
            </span>
        </span>
    )
}
