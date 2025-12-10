import React from 'react'

interface CardProps {
    children: React.ReactNode
    className?: string
    title?: string
    action?: React.ReactNode
}

export function Card({ children, className = '', title, action }: CardProps) {
    return (
        <div className={`bg-slate-900 border border-slate-800 rounded-xl p-6 ${className}`}>
            {(title || action) && (
                <div className="flex items-center justify-between mb-4">
                    {title && <h2 className="text-lg font-bold text-white">{title}</h2>}
                    {action && <div>{action}</div>}
                </div>
            )}
            {children}
        </div>
    )
}
