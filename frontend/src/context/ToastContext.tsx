import { createContext, useContext, useState, useCallback, ReactNode } from 'react'
import { X, CheckCircle, Info, AlertTriangle } from 'lucide-react'

type ToastType = 'success' | 'error' | 'info' | 'warning'

interface Toast {
    id: string
    type: ToastType
    message: string
    duration?: number
}

interface ToastContextType {
    showToast: (message: string, type: ToastType, duration?: number) => void
    success: (message: string) => void
    error: (message: string) => void
}

const ToastContext = createContext<ToastContextType | undefined>(undefined)

export function ToastProvider({ children }: { children: ReactNode }) {
    const [toasts, setToasts] = useState<Toast[]>([])

    const removeToast = useCallback((id: string) => {
        setToasts(prev => prev.filter(t => t.id !== id))
    }, [])

    const showToast = useCallback((message: string, type: ToastType, duration?: number) => {
        const resolvedDuration = typeof duration === 'number'
            ? duration
            : (type === 'error' ? 8000 : 5000)
        const id = Math.random().toString(36).substring(2, 9)
        const toast = { id, message, type, duration: resolvedDuration }
        setToasts(prev => [...prev, toast])

        if (resolvedDuration > 0) {
            setTimeout(() => removeToast(id), resolvedDuration)
        }
    }, [removeToast])

    const success = (msg: string) => showToast(msg, 'success')
    const error = (msg: string) => showToast(msg, 'error')

    return (
        <ToastContext.Provider value={{ showToast, success, error }}>
            {children}
            <div className="fixed bottom-4 right-4 z-[9999] flex flex-col gap-2">
                {toasts.map(toast => (
                    <div
                        key={toast.id}
                        className={`
                            flex items-center gap-3 min-w-[300px] p-4 rounded-xl shadow-lg border backdrop-blur-md animate-in slide-in-from-right-full transition-all
                            ${toast.type === 'success' ? 'bg-emerald-900/90 border-emerald-800 text-emerald-100' : ''}
                            ${toast.type === 'error' ? 'bg-red-900/90 border-red-800 text-red-100' : ''}
                            ${toast.type === 'info' ? 'bg-blue-900/90 border-blue-800 text-blue-100' : ''}
                            ${toast.type === 'warning' ? 'bg-amber-900/90 border-amber-800 text-amber-100' : ''}
                        `}
                    >
                        {toast.type === 'success' && <CheckCircle size={20} className="text-emerald-400" />}
                        {toast.type === 'error' && <AlertTriangle size={20} className="text-red-400" />}
                        {toast.type === 'info' && <Info size={20} className="text-blue-400" />}
                        {toast.type === 'warning' && <AlertTriangle size={20} className="text-amber-400" />}

                        <p className="text-sm font-medium flex-1">{toast.message}</p>

                        <button
                            onClick={() => removeToast(toast.id)}
                            className="p-1 hover:bg-white/10 rounded-full transition-colors"
                        >
                            <X size={16} />
                        </button>
                    </div>
                ))}
            </div>
        </ToastContext.Provider>
    )
}

export function useToast() {
    const context = useContext(ToastContext)
    if (!context) throw new Error('useToast must be used within a ToastProvider')
    return context
}
