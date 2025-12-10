import React from 'react'
import { RotateCw } from 'lucide-react'

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
    variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'icon'
    size?: 'sm' | 'md' | 'lg' | 'icon'
    isLoading?: boolean
    icon?: React.ReactNode
}

export function Button({
    children,
    className = '',
    variant = 'primary',
    size = 'md',
    isLoading = false,
    icon,
    disabled,
    ...props
}: ButtonProps) {
    const baseStyles = 'inline-flex items-center justify-center rounded-lg transition-all font-medium disabled:opacity-50 disabled:cursor-not-allowed'

    const variants = {
        primary: 'bg-blue-600 hover:bg-blue-500 text-white',
        secondary: 'bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700',
        danger: 'bg-red-500/10 hover:bg-red-500/20 text-red-500 border border-red-500/20',
        ghost: 'bg-transparent hover:bg-slate-800/50 text-slate-400 hover:text-white',
        icon: 'bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 p-2'
    }

    const sizes = {
        sm: 'px-3 py-1.5 text-xs',
        md: 'px-4 py-2 text-sm',
        lg: 'px-6 py-3 text-base',
        icon: 'p-2'
    }

    const appliedVariant = variant === 'icon' ? variants.icon : variants[variant]
    const appliedSize = variant === 'icon' ? '' : sizes[size]

    return (
        <button
            className={`${baseStyles} ${appliedVariant} ${appliedSize} ${className}`}
            disabled={disabled || isLoading}
            {...props}
        >
            {isLoading && <RotateCw className="mr-2 h-4 w-4 animate-spin" />}
            {!isLoading && icon && <span className={`${children ? 'mr-2' : ''}`}>{icon}</span>}
            {children}
        </button>
    )
}
