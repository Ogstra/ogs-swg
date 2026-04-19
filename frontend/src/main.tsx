import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App.tsx'
import './index.css'

const disableMobileZoom = () => {
    if (typeof window === 'undefined') return

    let lastTouchEnd = 0

    document.addEventListener('gesturestart', (event) => {
        event.preventDefault()
    }, { passive: false })

    document.addEventListener('gesturechange', (event) => {
        event.preventDefault()
    }, { passive: false })

    document.addEventListener('touchmove', (event) => {
        if (event.touches.length > 1) {
            event.preventDefault()
        }
    }, { passive: false })

    document.addEventListener('touchend', (event) => {
        const now = Date.now()
        if (now - lastTouchEnd <= 300) {
            event.preventDefault()
        }
        lastTouchEnd = now
    }, { passive: false })
}

disableMobileZoom()

if ('serviceWorker' in navigator && import.meta.env.PROD) {
    window.addEventListener('load', () => {
        navigator.serviceWorker.register('/sw.js').catch(err => {
            console.warn('Service worker registration failed:', err)
        })
    })
}

const queryClient = new QueryClient({
    defaultOptions: {
        queries: {
            staleTime: 30_000,
            gcTime: 10 * 60_000,
            refetchOnWindowFocus: false,
            retry: 1,
        },
    },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
        <QueryClientProvider client={queryClient}>
            <App />
        </QueryClientProvider>
    </React.StrictMode>,
)
