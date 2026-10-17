import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, UnifiedChartPoint, Consumer, TrafficStats, DashboardData, DashboardPreferences as StoredDashboardPreferences } from '../../services/api'
import { ArrowDown, ArrowUp, RefreshCw, TriangleAlert, X } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, Tooltip, XAxis, YAxis } from 'recharts'
import { Button } from '../../components/ui/Button'
import { formatBytes } from '../../utils/traffic'
import { useToast } from '../../context/ToastContext'

type DashboardRange = '30m' | '1h' | '6h' | '24h' | '1w' | '1m' | 'custom'

type DashboardPrefs = {
    defaultService?: 'singbox' | 'wireguard'
    refreshMs?: number
    defaultRange?: DashboardRange
    activeUserWindowMinutes?: number
    detailChartTargetPoints?: number
}

type TrafficChartView = 'cumulative' | 'sample'

type DensifiedChartCache = {
    cacheKey: string
    source: UnifiedChartPoint[]
    dense: UnifiedChartPoint[]
}

type DashboardRequestWindow =
    | { range: 'custom'; start: number; end: number; startText: string; endText: string }
    | { range: Exclude<DashboardRange, 'custom'> }

const padDatePart = (value: number) => value.toString().padStart(2, '0')

const formatDateTimeLocalValue = (date: Date) => (
    `${date.getFullYear()}-${padDatePart(date.getMonth() + 1)}-${padDatePart(date.getDate())}T${padDatePart(date.getHours())}:${padDatePart(date.getMinutes())}`
)

const toUnixSeconds = (value: string) => {
    const ms = new Date(value).getTime()
    if (!Number.isFinite(ms)) return 0
    return Math.floor(ms / 1000)
}

const maskedConsumerKey = '********'
const detailChartInterpolationFactor = 4
const defaultDetailChartTargetPoints = 200
const dashboardPrefsStorageKey = 'dashboard:prefs:v1'
const dashboardSnapshotPrefix = 'dashboard:snapshot:v1:'
const defaultDashboardPrefs: DashboardPrefs = {
    defaultService: 'singbox',
    refreshMs: 10000,
    defaultRange: '24h',
    activeUserWindowMinutes: 5,
    detailChartTargetPoints: defaultDetailChartTargetPoints,
}

const getConsumerSelectionId = (consumer: Consumer | null | undefined, mode: 'singbox' | 'wireguard') => {
    if (!consumer) return ''
    if (consumer.key && consumer.key !== maskedConsumerKey) return `${mode}:${consumer.key}`
    return `${mode}:${consumer.name}:${consumer.interface_name || ''}:${consumer.flow || ''}`
}

const dashboardPrefsFromApi = (prefs?: Partial<StoredDashboardPreferences> | null): DashboardPrefs => ({
    defaultService: prefs?.default_service === 'wireguard' ? 'wireguard' : defaultDashboardPrefs.defaultService,
    refreshMs: prefs?.refresh_ms && prefs.refresh_ms >= 1000 ? prefs.refresh_ms : defaultDashboardPrefs.refreshMs,
    defaultRange: (prefs?.default_range && ['30m', '1h', '6h', '24h', '1w', '1m'].includes(prefs.default_range))
        ? prefs.default_range as DashboardRange
        : defaultDashboardPrefs.defaultRange,
    activeUserWindowMinutes: prefs?.active_user_window_minutes && prefs.active_user_window_minutes >= 1
        ? prefs.active_user_window_minutes
        : defaultDashboardPrefs.activeUserWindowMinutes,
    detailChartTargetPoints: [50, 100, 150, 200].includes(Number(prefs?.detail_chart_target_points))
        ? Number(prefs?.detail_chart_target_points)
        : defaultDashboardPrefs.detailChartTargetPoints,
})

type DashboardSnapshot = {
    data: DashboardData
    savedAt: number
}

const readStoredDashboardPrefs = (): DashboardPrefs | null => {
    if (typeof window === 'undefined') return null
    try {
        const raw = window.localStorage.getItem(dashboardPrefsStorageKey)
        return raw ? dashboardPrefsFromApi(JSON.parse(raw)) : null
    } catch {
        return null
    }
}

const writeStoredDashboardPrefs = (prefs?: Partial<StoredDashboardPreferences> | null) => {
    if (typeof window === 'undefined' || !prefs) return
    try {
        window.localStorage.setItem(dashboardPrefsStorageKey, JSON.stringify(prefs))
    } catch {
        // Best-effort startup optimization only.
    }
}

const dashboardSnapshotKey = (requestWindow: DashboardRequestWindow) => {
    if (requestWindow.range === 'custom') {
        return `${dashboardSnapshotPrefix}custom:${requestWindow.startText}:${requestWindow.endText}`
    }
    return `${dashboardSnapshotPrefix}${requestWindow.range}`
}

const readDashboardSnapshot = (key: string): DashboardSnapshot | null => {
    if (typeof window === 'undefined') return null
    try {
        const raw = window.localStorage.getItem(key)
        if (!raw) return null
        const parsed = JSON.parse(raw) as DashboardSnapshot
        return parsed?.data ? parsed : null
    } catch {
        return null
    }
}

const writeDashboardSnapshot = (key: string, data?: DashboardData) => {
    if (typeof window === 'undefined' || !data) return
    try {
        window.localStorage.setItem(key, JSON.stringify({ data, savedAt: Date.now() }))
    } catch {
        // Ignore storage pressure; the network path still works.
    }
}

const chartPointsEqual = (left: UnifiedChartPoint, right: UnifiedChartPoint) => (
    left.ts === right.ts &&
    left.up_sb === right.up_sb &&
    left.down_sb === right.down_sb &&
    left.up_wg === right.up_wg &&
    left.down_wg === right.down_wg
)

const interpolateChartPoint = (start: UnifiedChartPoint, end: UnifiedChartPoint, ratio: number): UnifiedChartPoint => ({
    ts: start.ts + ((end.ts - start.ts) * ratio),
    up_sb: Math.round(start.up_sb + ((end.up_sb - start.up_sb) * ratio)),
    down_sb: Math.round(start.down_sb + ((end.down_sb - start.down_sb) * ratio)),
    up_wg: Math.round(start.up_wg + ((end.up_wg - start.up_wg) * ratio)),
    down_wg: Math.round(start.down_wg + ((end.down_wg - start.down_wg) * ratio)),
})

const densifyChartSeries = (points: UnifiedChartPoint[], factor: number): UnifiedChartPoint[] => {
    if (factor <= 1 || points.length < 2) return points

    const dense: UnifiedChartPoint[] = []
    for (let i = 0; i < points.length - 1; i += 1) {
        const current = points[i]
        const next = points[i + 1]
        dense.push(current)
        for (let step = 1; step < factor; step += 1) {
            dense.push(interpolateChartPoint(current, next, step / factor))
        }
    }
    dense.push(points[points.length - 1])
    return dense
}

const densifyChartSeriesIncremental = (
    points: UnifiedChartPoint[],
    factor: number,
    cache: DensifiedChartCache | null,
    cacheKey: string,
): DensifiedChartCache => {
    if (factor <= 1 || points.length < 2) {
        return { cacheKey, source: points, dense: points }
    }
    if (!cache || cache.cacheKey !== cacheKey) {
        return { cacheKey, source: points, dense: densifyChartSeries(points, factor) }
    }

    const previous = cache.source
    const commonLength = Math.min(previous.length, points.length)
    let firstDiffIndex = -1
    for (let i = 0; i < commonLength; i += 1) {
        if (!chartPointsEqual(previous[i], points[i])) {
            firstDiffIndex = i
            break
        }
    }

    if (firstDiffIndex === -1) {
        if (previous.length === points.length) {
            return cache
        }
        firstDiffIndex = commonLength
    }

    if (firstDiffIndex <= 0) {
        return { cacheKey, source: points, dense: densifyChartSeries(points, factor) }
    }

    const preservedDenseLength = ((firstDiffIndex - 1) * factor) + 1
    const prefix = cache.dense.slice(0, preservedDenseLength)
    const tail = densifyChartSeries(points.slice(firstDiffIndex - 1), factor)
    return {
        cacheKey,
        source: points,
        dense: prefix.concat(tail.slice(1)),
    }
}

const toPerSampleChartData = (points: UnifiedChartPoint[]): UnifiedChartPoint[] => {
    if (points.length === 0) return points

    return points.map((point, index) => {
        const previous = points[index - 1]
        if (!previous) {
            return {
                ...point,
                up_sb: 0,
                down_sb: 0,
                up_wg: 0,
                down_wg: 0,
            }
        }

        return {
            ts: point.ts,
            up_sb: Math.max(point.up_sb - previous.up_sb, 0),
            down_sb: Math.max(point.down_sb - previous.down_sb, 0),
            up_wg: Math.max(point.up_wg - previous.up_wg, 0),
            down_wg: Math.max(point.down_wg - previous.down_wg, 0),
        }
    })
}

// Custom SVG Icons
const SingBoxIcon = ({ className = "w-6 h-6" }: { className?: string }) => (
    <svg viewBox="0 0 1027 1109" className={className} fill="currentColor">
        <path d="M0 336L0 720.69C0 785.41 39.499 796.19 39.499 796.19L456.03 1076.62C520.66 1119.77 502.71 1026.29 502.71 1026.29L502.71 673.96L0 336Z" fillOpacity="0.7" />
        <path d="M1006 336L1006 720.69C1006 785.41 966.5 796.19 966.5 796.19L614.6 1033.48L550 1076.62C485.34 1119.77 503.29 1026.29 503.29 1026.29L503.29 673.96L1006 336Z" fillOpacity="0.8" />
        <path d="M549.71 13.473C520.96-4.491 477.85-4.491 452.7 13.473L21.557 308.07C-7.186 326.04-7.186 358.37 21.557 376.33L452.7 674.53C481.44 692.49 524.56 692.49 549.71 674.53L984.44 372.74C1013.19 354.78 1013.19 322.44 984.44 304.48L549.71 13.473Z" fillOpacity="0.9" />
        <path d="M503 1094C481.4 1094 467 1080.19 467 1062.92L467 676.08C467 658.82 481.4 645 503 645C524.6 645 539 658.82 539 676.08L539 1059.46C539 1080.19 524.6 1094 503 1094Z" fillOpacity="0.9" />
        <path d="M861.92 580.92C861.92 616.89 865.5 631.27 826.03 656.45L736.32 713.99C696.85 739.17 682.5 717.59 682.5 685.22L682.5 591.71C682.5 584.52 682.5 580.92 671.73 573.73C578.43 508.99 219.591 260.84 155 214.09L320.07 99C366.72 127.772 707.61 354.35 847.56 451.45C854.74 455.05 858.33 462.24 858.33 465.84L858.33 580.92Z" />
        <path d="M851.41 455.21C707.82 358.17 366.79 131.752 323.72 103L259.103 142.534L155 214.41C219.615 261.14 578.59 505.53 671.92 570.22C679.1 573.81 679.1 577.41 679.1 581L855 458.8C855 458.8 855 455.21 851.41 455.21Z" fillOpacity="0.95" />
        <path d="M862.9 580.48L862.9 469.19C862.9 462.01 859.3 458.42 852.11 454.83C708.28 357.89 366.67 131.721 323.51 103L248 153.26C370.26 235.83 697.49 451.24 783.79 512.27C794.58 519.45 794.58 526.63 794.58 530.22L794.58 681L830.54 655.87C866.5 630.74 862.9 612.79 862.9 580.48Z" fillOpacity="0.95" />
        <path d="M851.23 454.95C707.6 357.98 366.49 131.731 323.4 103L248 153.28C370.08 235.88 696.83 451.36 783.01 512.41L786.6 516L862 462.13C854.82 458.54 854.82 454.95 851.23 454.95Z" />
    </svg>
)

const WireGuardIcon = ({ className = "w-6 h-6" }: { className?: string }) => (
    <svg role="img" viewBox="0 0 24 24" className={className} fill="currentColor">
        <path d="M23.98 11.645S24.533 0 11.735 0C0.418 0 0.064 11.17 0.064 11.17S-1.6 24 11.997 24C25.04 24 23.98 11.645 23.98 11.645zM8.155 7.576c2.4 -1.47 5.469 -0.571 6.618 1.638 0.218 0.419 0.246 1.063 0.108 1.503 -0.477 1.516 -1.601 2.366 -3.145 2.728 0.455 -0.39 0.817 -0.832 0.933 -1.442a2.112 2.112 0 0 0 -0.364 -1.677 2.14 2.14 0 0 0 -2.465 -0.75c-0.95 0.36 -1.47 1.228 -1.377 2.294 0.087 0.99 0.839 1.632 2.245 1.876 -0.21 0.111 -0.372 0.193 -0.53 0.281a5.113 5.113 0 0 0 -1.644 1.43c-0.143 0.192 -0.24 0.208 -0.458 0.075 -2.827 -1.729 -3.009 -6.067 0.078 -7.956zM6.04 18.258c-0.455 0.116 -0.895 0.286 -1.359 0.438 0.227 -1.532 2.021 -2.943 3.539 -2.782a3.91 3.91 0 0 0 -0.74 2.072c-0.504 0.093 -0.98 0.155 -1.44 0.272zM15.703 3.3c0.448 0.017 0.898 0.01 1.347 0.02a2.324 2.324 0 0 1 0.334 0.047 3.249 3.249 0 0 1 -0.34 0.434c-0.16 0.15 -0.341 0.296 -0.573 0.069 -0.055 -0.055 -0.187 -0.042 -0.283 -0.044 -0.447 -0.005 -0.894 -0.02 -1.34 -0.003a8.323 8.323 0 0 0 -1.154 0.118c-0.072 0.013 -0.178 0.25 -0.146 0.338 0.078 0.207 0.191 0.435 0.359 0.567 0.619 0.49 1.277 0.928 1.9 1.413 0.604 0.472 1.167 0.99 1.51 1.7 0.446 0.928 0.46 1.9 0.267 2.877 -0.322 1.63 -1.147 2.98 -2.483 3.962 -0.538 0.395 -1.205 0.62 -1.821 0.903 -0.543 0.25 -1.1 0.465 -1.644 0.712 -0.98 0.446 -1.53 1.51 -1.369 2.615 0.149 1.015 1.04 1.862 2.059 2.037 1.223 0.21 2.486 -0.586 2.785 -1.83 0.336 -1.397 -0.423 -2.646 -1.845 -3.024l-0.256 -0.066c0.38 -0.17 0.708 -0.291 1.012 -0.458q0.793 -0.437 1.558 -0.925c0.15 -0.096 0.231 -0.096 0.36 0.014 0.977 0.846 1.56 1.898 1.724 3.187 0.27 2.135 -0.74 4.096 -2.646 5.101 -2.948 1.555 -6.557 -0.215 -7.208 -3.484 -0.558 -2.8 1.418 -5.34 3.797 -5.83 1.023 -0.211 1.958 -0.637 2.685 -1.425 0.47 -0.508 0.697 -0.944 0.775 -1.141a3.165 3.165 0 0 0 0.217 -1.158 2.71 2.71 0 0 0 -0.237 -0.992c-0.248 -0.566 -1.2 -1.466 -1.435 -1.656l-2.24 -1.754c-0.079 -0.065 -0.168 -0.06 -0.36 -0.047 -0.23 0.016 -0.815 0.048 -1.067 -0.018 0.204 -0.155 0.76 -0.38 1 -0.56 -0.726 -0.49 -1.554 -0.314 -2.315 -0.46 0.176 -0.328 1.046 -0.831 1.541 -0.888a7.323 7.323 0 0 0 -0.135 -0.822c-0.03 -0.111 -0.154 -0.22 -0.263 -0.283 -0.262 -0.154 -0.541 -0.281 -0.843 -0.434a1.755 1.755 0 0 1 0.906 -0.28 3.385 3.385 0 0 1 0.908 0.088c0.54 0.123 0.97 0.042 1.399 -0.324 -0.338 -0.136 -0.676 -0.26 -1.003 -0.407a9.843 9.843 0 0 1 -0.942 -0.493c0.85 0.118 1.671 0.437 2.54 0.32l0.022 -0.118 -2.018 -0.47c1.203 -0.11 2.323 -0.128 3.384 0.388 0.299 0.146 0.61 0.266 0.897 0.432 0.14 0.08 0.233 0.24 0.348 0.365 0.09 0.098 0.164 0.23 0.276 0.29 0.424 0.225 0.89 0.234 1.366 0.223l0.01 -0.16c0.479 0.15 1.017 0.702 1.017 1.105 -0.776 0 -1.55 -0.003 -2.325 0.004 -0.083 0 -0.165 0.061 -0.247 0.094 0.078 0.046 0.155 0.128 0.235 0.131zm-1 -1.147a0.118 0.118 0 0 0 -0.016 0.19 0.179 0.179 0 0 0 0.246 0.065c0.075 -0.038 0.148 -0.078 0.238 -0.125 -0.072 -0.062 -0.13 -0.114 -0.19 -0.163 -0.106 -0.087 -0.193 -0.032 -0.278 0.033z" />
    </svg>
)

const UPLOAD_COLOR = '#b7b2a5'
const DOWNLOAD_COLOR = '#e58f4e'
const MONO_FONT = '"IBM Plex Mono", ui-monospace, Menlo, monospace'

const splitBytes = (bytes: number): [string, string] => {
    const text = formatBytes(bytes)
    const i = text.lastIndexOf(' ')
    return i === -1 ? [text, ''] : [text.slice(0, i), text.slice(i + 1)]
}

const formatClock = (date: Date, withSeconds = false) => date.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    ...(withSeconds ? { second: '2-digit' } : {}),
    hour12: false,
})

// Recharts picks decimal "nice" ticks (1e9, 2e9...) which formatBytes then
// labels in binary units ("954 MB" for 1e9). Pick the step in the binary unit
// the labels use so ticks land on round values.
const niceByteTicks = (max: number, count = 4): number[] => {
    if (!(max > 0)) return [0]
    const unitIndex = Math.min(Math.floor(Math.log(max) / Math.log(1024)), 8)
    const unit = Math.pow(1024, Math.max(unitIndex, 0))
    const maxInUnit = max / unit
    const rawStep = maxInUnit / count
    const magnitude = Math.pow(10, Math.floor(Math.log10(rawStep)))
    const normalized = rawStep / magnitude
    const step = (normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10) * magnitude
    const ticks: number[] = []
    for (let value = 0; ; value += step) {
        ticks.push(value * unit)
        if (value >= maxInUnit) break
    }
    return ticks
}

function ByteFigure({ bytes, className = '' }: { bytes: number; className?: string }) {
    const [value, unit] = splitBytes(bytes)
    return (
        <span className={`font-mono tabular-nums ${className}`}>
            {value}
            {unit && <span className="ml-1 text-slate-400" style={{ fontSize: 'max(0.8em, 10px)' }}>{unit}</span>}
        </span>
    )
}

function Segmented<T extends string>({ value, onChange, options, label, className = '' }: {
    value: T
    onChange: (value: T) => void
    options: { value: T; label: string; title?: string }[]
    label: string
    className?: string
}) {
    return (
        <div role="group" aria-label={label} className={`inline-flex rounded-md border border-slate-800 bg-slate-950 p-0.5 ${className}`}>
            {options.map(option => (
                <button
                    key={option.value}
                    type="button"
                    title={option.title}
                    aria-pressed={value === option.value}
                    onClick={() => onChange(option.value)}
                    className={`flex-1 whitespace-nowrap rounded px-2.5 py-1 text-xs sm:px-3 font-medium transition-colors sm:flex-none ${value === option.value ? 'bg-slate-800 text-slate-50' : 'text-slate-400 hover:text-slate-200'}`}
                >
                    {option.label}
                </button>
            ))}
        </div>
    )
}

const SERVICE_STATE = {
    up: { word: 'Running', dot: 'bg-emerald-400', text: 'text-emerald-400' },
    down: { word: 'Stopped', dot: 'bg-red-400', text: 'text-red-400' },
    unknown: { word: 'Unknown', dot: 'bg-slate-500', text: 'text-slate-500' },
} as const

function ServiceRow({ name, icon, running, upload, download, activeNames, activeCount }: {
    name: string
    icon: React.ReactNode
    running: boolean | null
    upload: number
    download: number
    activeNames: string[]
    activeCount: number
}) {
    const state = running === true ? SERVICE_STATE.up : running === false ? SERVICE_STATE.down : SERVICE_STATE.unknown
    // The backend sends these busiest/most-recent first.
    const shown = activeNames.slice(0, 8)
    const hidden = Math.max(0, activeNames.length - shown.length)
    return (
        <div className={`grid grid-cols-2 gap-x-4 gap-y-3 px-4 py-4 md:grid-cols-[minmax(11rem,1.2fr)_repeat(3,minmax(5.5rem,1fr))_minmax(11rem,1.8fr)] md:items-center sm:px-5 ${running === false ? 'bg-red-500/5' : ''}`}>
            <div className="order-1 flex items-center gap-3 md:order-none">
                <span className="text-slate-300">{icon}</span>
                <div className="leading-tight">
                    <div className="text-sm font-semibold text-slate-50">{name}</div>
                    <div className={`mt-0.5 flex items-center gap-1.5 text-xs ${state.text}`}>
                        <span className={`h-1.5 w-1.5 rounded-full ${state.dot}`} />
                        {state.word}
                    </div>
                </div>
            </div>
            <div className="order-2 flex flex-col items-end gap-1 justify-self-end md:hidden">
                <ByteFigure bytes={upload + download} className="text-lg font-medium leading-tight text-slate-50" />
                <div className="flex items-center gap-3">
                    <span className="flex items-center gap-1" title="Upload">
                        <ArrowUp size={12} className="shrink-0 text-slate-500" aria-label="Upload" />
                        <ByteFigure bytes={upload} className="text-xs text-slate-300" />
                    </span>
                    <span className="flex items-center gap-1" title="Download">
                        <ArrowDown size={12} className="shrink-0 text-slate-500" aria-label="Download" />
                        <ByteFigure bytes={download} className="text-xs text-slate-300" />
                    </span>
                </div>
            </div>
            <div className="hidden items-center gap-1.5 md:flex" title="Upload">
                <ArrowUp size={12} className="shrink-0 text-slate-500" aria-label="Upload" />
                <ByteFigure bytes={upload} className="text-sm text-slate-200" />
            </div>
            <div className="hidden items-center gap-1.5 md:flex" title="Download">
                <ArrowDown size={12} className="shrink-0 text-slate-500" aria-label="Download" />
                <ByteFigure bytes={download} className="text-sm text-slate-200" />
            </div>
            <div className="hidden md:block" title="Total">
                <ByteFigure bytes={upload + download} className="text-lg font-medium text-slate-50" />
            </div>
            <div className="order-5 col-span-2 md:order-none md:col-span-1">
                <div className="text-sm text-slate-200">
                    <span className="font-mono tabular-nums">{activeCount}</span>
                    <span className="ml-1.5 text-xs text-slate-500">active</span>
                </div>
                {shown.length > 0 && (
                    <div className="mt-1 flex flex-wrap gap-x-2 gap-y-0.5 font-mono text-xs text-slate-400">
                        {shown.map(item => (
                            <span key={item} className="max-w-[10rem] truncate" title={item}>{item}</span>
                        ))}
                        {hidden > 0 && <span className="text-slate-500">+{hidden}</span>}
                    </div>
                )}
            </div>
        </div>
    )
}

export default function Dashboard() {
    const { success, error: toastError } = useToast()
    const queryClient = useQueryClient()
    const [initialDashboardPrefs] = useState(() => readStoredDashboardPrefs())
    const [timeRange, setTimeRange] = useState<DashboardRange>(initialDashboardPrefs?.defaultRange || '24h')
    const [chartMode, setChartMode] = useState<'singbox' | 'wireguard'>(initialDashboardPrefs?.defaultService || 'singbox')
    const [trafficChartView, setTrafficChartView] = useState<TrafficChartView>('cumulative')
    const [refreshInterval, setRefreshInterval] = useState<number>(initialDashboardPrefs?.refreshMs || 10000)
    const [prefsLoaded, setPrefsLoaded] = useState(true)
    const [selectedConsumers, setSelectedConsumers] = useState<Record<'singbox' | 'wireguard', Consumer | null>>({
        singbox: null,
        wireguard: null,
    })

    const now = new Date()
    const initialCustomStart = new Date(now)
    initialCustomStart.setHours(0, 0, 0, 0)
    const [customStart, setCustomStart] = useState(formatDateTimeLocalValue(initialCustomStart))
    const [customEnd, setCustomEnd] = useState(formatDateTimeLocalValue(now))
    const chartContainerRef = useRef<HTMLDivElement | null>(null)
    const [chartSize, setChartSize] = useState({ width: 0, height: 200 })
    const detailChartCacheRef = useRef<DensifiedChartCache | null>(null)
    const [detailChartTargetPoints, setDetailChartTargetPoints] = useState<number>(initialDashboardPrefs?.detailChartTargetPoints || defaultDetailChartTargetPoints)
    const [singboxApplyLoading, setSingboxApplyLoading] = useState(false)
    const prefsInitializedRef = useRef(false)

    const computeRangeSeconds = (range: string) => {
        const nowSec = Math.floor(Date.now() / 1000)
        const day = 24 * 60 * 60
        switch (range) {
            case '30m': return { start: nowSec - 30 * 60, end: nowSec }
            case '1h': return { start: nowSec - 60 * 60, end: nowSec }
            case '6h': return { start: nowSec - 6 * 60 * 60, end: nowSec }
            case '24h': return { start: nowSec - day, end: nowSec }
            case '1w': return { start: nowSec - 7 * day, end: nowSec }
            case '1m': return { start: nowSec - 30 * day, end: nowSec }
            default: return { start: nowSec - day, end: nowSec }
        }
    }

    const dashboardPrefsQuery = useQuery({
        queryKey: ['dashboard-preferences'],
        queryFn: () => api.getDashboardPreferences(),
        retry: false,
    })

    useEffect(() => {
        if (prefsInitializedRef.current) return
        if (dashboardPrefsQuery.isPending) return

        const prefs = dashboardPrefsFromApi(dashboardPrefsQuery.data)
        writeStoredDashboardPrefs(dashboardPrefsQuery.data)
        setChartMode(prefs.defaultService || 'singbox')
        if (prefs.defaultRange) setTimeRange(prefs.defaultRange)
        if (prefs.refreshMs && prefs.refreshMs >= 1000) setRefreshInterval(prefs.refreshMs)
        setDetailChartTargetPoints(prefs.detailChartTargetPoints ?? defaultDetailChartTargetPoints)
        prefsInitializedRef.current = true
        setPrefsLoaded(true)
    }, [dashboardPrefsQuery.data, dashboardPrefsQuery.isPending])

    useEffect(() => {
        const el = chartContainerRef.current
        if (!el || typeof ResizeObserver === 'undefined') return
        const update = () => {
            const nextWidth = Math.max(0, Math.floor(el.clientWidth))
            const nextHeight = Math.max(160, Math.floor(el.clientHeight))
            setChartSize({ width: nextWidth, height: nextHeight })
        }
        update()
        const ro = new ResizeObserver(update)
        ro.observe(el)
        return () => ro.disconnect()
    }, [])

    const customRangeState = useMemo(() => {
        const start = toUnixSeconds(customStart)
        const end = toUnixSeconds(customEnd)
        return {
            start,
            end,
            isValid: start > 0 && end > start,
        }
    }, [customStart, customEnd])

    const requestWindow = useMemo<DashboardRequestWindow>(() => {
        if (timeRange === 'custom') {
            return {
                range: 'custom' as const,
                start: customRangeState.start,
                end: customRangeState.end,
                startText: String(customRangeState.start),
                endText: String(customRangeState.end),
            }
        }
        return { range: timeRange }
    }, [timeRange, customRangeState])
    const dashboardSnapshotStorageKey = useMemo(() => dashboardSnapshotKey(requestWindow), [requestWindow])
    const dashboardSnapshot = useMemo(() => readDashboardSnapshot(dashboardSnapshotStorageKey), [dashboardSnapshotStorageKey])

    const dashboardQuery = useQuery({
        queryKey: requestWindow.range === 'custom'
            ? ['dashboard-data', requestWindow.range, requestWindow.startText, requestWindow.endText]
            : ['dashboard-data', requestWindow.range],
        queryFn: () => requestWindow.range === 'custom'
            ? api.getDashboardData(requestWindow.range, requestWindow.startText, requestWindow.endText)
            : api.getDashboardData(requestWindow.range),
        enabled: prefsLoaded && (timeRange !== 'custom' || customRangeState.isValid),
        refetchInterval: refreshInterval,
        placeholderData: previousData => previousData ?? dashboardSnapshot?.data,
    })

    useEffect(() => {
        if (!dashboardQuery.data || dashboardQuery.isPlaceholderData || dashboardQuery.dataUpdatedAt === 0) return
        writeDashboardSnapshot(dashboardSnapshotStorageKey, dashboardQuery.data)
    }, [dashboardQuery.data, dashboardQuery.dataUpdatedAt, dashboardQuery.isPlaceholderData, dashboardSnapshotStorageKey])

    const loading = dashboardQuery.isFetching
    const lastUpdated = dashboardQuery.dataUpdatedAt
        ? new Date(dashboardQuery.dataUpdatedAt)
        : dashboardSnapshot?.savedAt
            ? new Date(dashboardSnapshot.savedAt)
            : new Date()
    const chartData: UnifiedChartPoint[] = dashboardQuery.data?.chart_data || []
    const chartDomain: [number, number] = useMemo(() => {
        if (chartData.length > 1) {
            const fallbackRange = requestWindow.range === 'custom'
                ? { start: requestWindow.start, end: requestWindow.end }
                : computeRangeSeconds(timeRange)
            const first = chartData[0]?.ts ?? fallbackRange.start
            const last = chartData[chartData.length - 1]?.ts ?? fallbackRange.end
            if (last > first) return [first, last]
            return [first, first + 1]
        }
        if (requestWindow.range === 'custom') {
            return [requestWindow.start, requestWindow.end]
        }
        const fallbackRange = computeRangeSeconds(timeRange)
        return [fallbackRange.start, fallbackRange.end]
    }, [chartData, requestWindow, timeRange])
    const statsCards: Record<string, TrafficStats> = dashboardQuery.data?.stats_cards || {
        singbox: { uplink: 0, downlink: 0 },
        wireguard: { uplink: 0, downlink: 0 }
    }
    const topConsumersMap: Record<string, Consumer[]> = dashboardQuery.data?.top_consumers || {
        singbox: [],
        wireguard: []
    }
    const status = dashboardQuery.data?.status || {
        singbox: null,
        wireguard: null,
        active_users_singbox: 0,
        active_users_wireguard: 0,
        active_users_singbox_list: [],
        active_users_wireguard_list: []
    }
    const singboxPendingChanges = !!dashboardQuery.data?.singbox_pending_changes

    const setSingboxPendingChanges = (pending: boolean) => {
        queryClient.setQueryData(['dashboard-pending-changes'], (old: any) => ({
            ...(old || {}),
            singbox_pending_changes: pending,
        }))
        queryClient.setQueriesData({ queryKey: ['dashboard-data'] }, (old: any) =>
            old ? { ...old, singbox_pending_changes: pending } : old
        )
    }

    const handleApplySingboxChanges = async () => {
        setSingboxApplyLoading(true)
        try {
            const result = await api.applySingboxChanges()
            if (result.restart_required) {
                await api.restartService('sing-box')
                success('Sing-box restart started')
            } else {
                success('Sing-box configuration applied successfully')
            }

            setSingboxPendingChanges(false)

            // Wait a moment for the service restart/reload to be fully reflected in the OS
            await new Promise(resolve => setTimeout(resolve, 2000))

            await Promise.all([
                dashboardQuery.refetch(),
                queryClient.invalidateQueries({ queryKey: ['dashboard-pending-changes'] }),
            ])
        } catch (err) {
            setSingboxPendingChanges(true)
            console.error('Failed to apply Sing-box changes:', err)
            toastError('Failed to apply changes. Please try again.')
        } finally {
            setSingboxApplyLoading(false)
        }
    }


    // Derived values for UI
    const topConsumers = (topConsumersMap[chartMode] || []).slice(0, 20)
    const selectedConsumer = selectedConsumers[chartMode]
    const selectedConsumerSelectionId = getConsumerSelectionId(selectedConsumer, chartMode)
    const selectedConsumerQuery = useQuery({
        queryKey: requestWindow.range === 'custom'
            ? ['dashboard-consumer-chart', chartMode, selectedConsumerSelectionId, detailChartTargetPoints, requestWindow.range, requestWindow.startText, requestWindow.endText]
            : ['dashboard-consumer-chart', chartMode, selectedConsumerSelectionId, detailChartTargetPoints, requestWindow.range],
        queryFn: () => requestWindow.range === 'custom'
            ? api.getDashboardConsumerChart(chartMode, selectedConsumer!.key, selectedConsumer!.name, selectedConsumer!.interface_name, requestWindow.range, requestWindow.startText, requestWindow.endText, detailChartTargetPoints)
            : api.getDashboardConsumerChart(chartMode, selectedConsumer!.key, selectedConsumer!.name, selectedConsumer!.interface_name, requestWindow.range, undefined, undefined, detailChartTargetPoints),
        enabled: prefsLoaded && (timeRange !== 'custom' || customRangeState.isValid) && !!selectedConsumerSelectionId,
        refetchInterval: selectedConsumer ? refreshInterval : false,
    })
    const detailChartData = selectedConsumerQuery.data?.chart_data || []
    const denseDetailChartData = useMemo(() => {
        if (!selectedConsumer) return detailChartData
        const rangeKey = requestWindow.range === 'custom'
            ? `${requestWindow.range}:${requestWindow.startText}:${requestWindow.endText}`
            : requestWindow.range
        const cacheKey = `${chartMode}:${selectedConsumerSelectionId}:${rangeKey}:${detailChartInterpolationFactor}`
        const nextCache = densifyChartSeriesIncremental(
            detailChartData,
            detailChartInterpolationFactor,
            detailChartCacheRef.current,
            cacheKey,
        )
        detailChartCacheRef.current = nextCache
        return nextCache.dense
    }, [chartMode, detailChartData, requestWindow, selectedConsumer, selectedConsumerSelectionId])
    const cumulativeChartData = selectedConsumer ? denseDetailChartData : chartData
    const sampleChartData = selectedConsumer ? detailChartData : chartData
    const chartDisplayData = useMemo(
        () => trafficChartView === 'sample' ? toPerSampleChartData(sampleChartData) : cumulativeChartData,
        [cumulativeChartData, sampleChartData, trafficChartView],
    )

    useEffect(() => {
        setSelectedConsumers(prev => {
            const next = { ...prev }
            let changed = false

            for (const mode of ['singbox', 'wireguard'] as const) {
                const current = prev[mode]
                if (!current) continue

                const currentSelectionId = getConsumerSelectionId(current, mode)
                const stillExists = (topConsumersMap[mode] || []).some(item => getConsumerSelectionId(item, mode) === currentSelectionId)
                if (!stillExists) {
                    next[mode] = null
                    changed = true
                }
            }

            return changed ? next : prev
        })
    }, [topConsumersMap])

    const handleRefreshDashboard = async () => {
        if (timeRange === 'custom' && !customRangeState.isValid) {
            return
        }
        await Promise.all([
            dashboardQuery.refetch(),
            selectedConsumer ? selectedConsumerQuery.refetch() : Promise.resolve(),
        ])
    }


    const uploadKey = chartMode === 'singbox' ? 'up_sb' : 'down_wg'
    const downloadKey = chartMode === 'singbox' ? 'down_sb' : 'up_wg'
    const chartHasTraffic = chartDisplayData.some(point => {
        const values = point as unknown as Record<string, number>
        return (values[uploadKey] || 0) + (values[downloadKey] || 0) > 0
    })
    const yTicks = niceByteTicks(chartDisplayData.reduce((max, point) => {
        const values = point as unknown as Record<string, number>
        return Math.max(max, values[uploadKey] || 0, values[downloadKey] || 0)
    }, 0))
    const chartIsLoading = !!selectedConsumer && selectedConsumerQuery.isPending

    return (
        <div className="space-y-6 pb-4 sm:pb-0">
            {singboxPendingChanges && (
                <div className="flex flex-col gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex items-start gap-3">
                        <TriangleAlert className="mt-0.5 shrink-0 text-amber-400" size={18} />
                        <div>
                            <p className="text-sm font-medium text-slate-50">sing-box changes not applied</p>
                            <p className="mt-0.5 text-xs text-slate-400">The running service still uses the previous configuration.</p>
                        </div>
                    </div>
                    <Button
                        onClick={handleApplySingboxChanges}
                        variant="primary"
                        size="sm"
                        isLoading={singboxApplyLoading}
                        className="whitespace-nowrap px-3 py-1.5 text-sm"
                    >
                        Apply changes
                    </Button>
                </div>
            )}

            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-end">
                <div className="flex w-full items-center justify-end gap-2 sm:w-auto sm:flex-wrap">
                    <span className="mr-1 hidden font-mono text-xs text-slate-400 sm:inline">Updated {formatClock(lastUpdated, true)}</span>
                    <div className="flex flex-1 items-center rounded-md border border-slate-800 bg-slate-950 px-3 py-1.5 sm:flex-none">
                        <select
                            value={timeRange}
                            onChange={(e) => setTimeRange(e.target.value as DashboardRange)}
                            className="select-field w-full min-w-[9.5rem] cursor-pointer border-none bg-transparent p-1 text-xs font-medium text-slate-200 outline-none focus:ring-0 sm:w-auto"
                        >
                            <option value="30m">Last 30 minutes</option>
                            <option value="1h">Last hour</option>
                            <option value="6h">Last 6 hours</option>
                            <option value="24h">Last 24 hours</option>
                            <option value="1w">Last 7 days</option>
                            <option value="1m">Last 30 days</option>
                            <option value="custom">Custom range</option>
                        </select>
                    </div>

                    {timeRange === 'custom' && (
                        <div className="flex flex-col gap-1">
                            <div className="flex flex-col gap-2 rounded-md border border-slate-800 bg-slate-950 px-3 py-1.5 sm:flex-row sm:items-center">
                                <input
                                    type="datetime-local"
                                    value={customStart}
                                    onChange={e => setCustomStart(e.target.value)}
                                    className="cursor-pointer border-none bg-transparent text-xs text-slate-200 outline-none focus:ring-0"
                                />
                                <span className="hidden text-slate-500 sm:inline">to</span>
                                <input
                                    type="datetime-local"
                                    value={customEnd}
                                    onChange={e => setCustomEnd(e.target.value)}
                                    className="cursor-pointer border-none bg-transparent text-xs text-slate-200 outline-none focus:ring-0"
                                />
                            </div>
                            {!customRangeState.isValid && (
                                <span className="text-[11px] text-amber-400">The end must be after the start.</span>
                            )}
                        </div>
                    )}

                    <Button
                        onClick={handleRefreshDashboard}
                        isLoading={loading}
                        disabled={timeRange === 'custom' && !customRangeState.isValid}
                        variant="icon"
                        size="icon"
                        aria-label="Refresh"
                        icon={<RefreshCw size={16} />}
                    />
                </div>
            </div>

            <section className="overflow-hidden rounded-lg border border-slate-800 bg-slate-900/60">
                <div className="divide-y divide-slate-800">
                    <ServiceRow
                        name="sing-box"
                        icon={<SingBoxIcon className="h-6 w-6" />}
                        running={status.singbox}
                        upload={statsCards.singbox.uplink}
                        download={statsCards.singbox.downlink}
                        activeNames={status.active_users_singbox_list || []}
                        activeCount={status.active_users_singbox}
                    />
                    <ServiceRow
                        name="WireGuard"
                        icon={<WireGuardIcon className="h-6 w-6" />}
                        running={status.wireguard}
                        upload={statsCards.wireguard.downlink}
                        download={statsCards.wireguard.uplink}
                        activeNames={status.active_users_wireguard_list || []}
                        activeCount={status.active_users_wireguard}
                    />
                </div>
            </section>

            <div className="grid grid-cols-[auto_1fr] items-center gap-2 sm:flex sm:flex-wrap sm:justify-between sm:gap-3">
                <div className="flex items-center gap-3">
                    <Segmented
                        className="w-full sm:w-auto"
                        label="Service"
                        value={chartMode}
                        onChange={setChartMode}
                        options={[{ value: 'singbox', label: 'sing-box' }, { value: 'wireguard', label: 'WireGuard' }]}
                    />
                    {selectedConsumer && (
                        <button
                            onClick={() => setSelectedConsumers(prev => ({ ...prev, [chartMode]: null }))}
                            className="hidden items-center gap-1.5 sm:inline-flex rounded-md border border-blue-500/40 bg-blue-500/10 px-2.5 py-1 text-xs font-medium text-blue-300 transition-colors hover:bg-blue-500/20"
                        >
                            <span className="max-w-[12rem] truncate">{selectedConsumer.name}</span>
                            <X size={12} />
                        </button>
                    )}
                </div>
                <div className="flex items-center gap-2">
                    <Segmented
                        className="w-full sm:w-auto"
                        label="Chart view"
                        value={trafficChartView}
                        onChange={setTrafficChartView}
                        options={[
                            { value: 'cumulative', label: 'Cumulative', title: 'Running total across the range' },
                            { value: 'sample', label: 'Per interval', title: 'Traffic in each sampling interval' },
                        ]}
                    />
                </div>
                {selectedConsumer && (
                    <button
                        onClick={() => setSelectedConsumers(prev => ({ ...prev, [chartMode]: null }))}
                        className="col-span-2 inline-flex items-center justify-between gap-1.5 rounded-md border border-blue-500/40 bg-blue-500/10 px-3 py-1.5 text-xs font-medium text-blue-300 transition-colors hover:bg-blue-500/20 sm:hidden"
                    >
                        <span className="truncate">{selectedConsumer.name}</span>
                        <X size={12} className="shrink-0" />
                    </button>
                )}
            </div>

            <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
                <section className="rounded-lg border border-slate-800 bg-slate-900/60 p-4 sm:p-5 lg:col-span-2">
                    <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-slate-400">
                        <span className="flex items-center gap-2">
                            <span className="h-0.5 w-4 rounded" style={{ backgroundColor: DOWNLOAD_COLOR }} />
                            Download
                        </span>
                        <span className="flex items-center gap-2">
                            <span className="h-0.5 w-4 rounded" style={{ backgroundColor: UPLOAD_COLOR }} />
                            Upload
                        </span>
                    </div>
                    <div ref={chartContainerRef} className="mt-3 h-[200px] min-h-[200px] w-full min-w-0 sm:mt-4 sm:h-[300px] sm:min-h-[300px]">
                        {chartIsLoading ? (
                            <div className="flex h-full items-center justify-center text-sm text-slate-500">Loading…</div>
                        ) : !chartHasTraffic ? (
                            <div className="flex h-full items-center justify-center text-sm text-slate-500">No traffic in this range</div>
                        ) : chartSize.width > 0 ? (
                            <AreaChart
                                width={chartSize.width}
                                height={chartSize.height}
                                data={chartDisplayData}
                                margin={{ top: 4, right: 16, bottom: 0, left: 0 }}
                            >
                                <defs>
                                    <linearGradient id="gradDownload" x1="0" y1="0" x2="0" y2="1">
                                        <stop offset="0%" stopColor={DOWNLOAD_COLOR} stopOpacity={0.28} />
                                        <stop offset="100%" stopColor={DOWNLOAD_COLOR} stopOpacity={0} />
                                    </linearGradient>
                                    <linearGradient id="gradUpload" x1="0" y1="0" x2="0" y2="1">
                                        <stop offset="0%" stopColor={UPLOAD_COLOR} stopOpacity={0.16} />
                                        <stop offset="100%" stopColor={UPLOAD_COLOR} stopOpacity={0} />
                                    </linearGradient>
                                </defs>
                                <CartesianGrid stroke="#2a2926" vertical={false} />
                                <XAxis
                                    dataKey="ts"
                                    tickFormatter={(ts) => {
                                        const date = new Date(ts * 1000)
                                        if (['30m', '1h', '6h', '24h'].includes(timeRange)) {
                                            return formatClock(date)
                                        }
                                        return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
                                    }}
                                    stroke="#7a766a"
                                    tick={{ fontFamily: MONO_FONT }}
                                    fontSize={11}
                                    tickMargin={10}
                                    axisLine={false}
                                    tickLine={false}
                                    type="number"
                                    domain={chartDomain}
                                />
                                <YAxis
                                    tickFormatter={(bytes) => formatBytes(bytes, 1)}
                                    ticks={yTicks}
                                    domain={[0, yTicks[yTicks.length - 1]]}
                                    stroke="#7a766a"
                                    tick={{ fontFamily: MONO_FONT }}
                                    fontSize={11}
                                    width={56}
                                    tickMargin={10}
                                    axisLine={false}
                                    tickLine={false}
                                />
                                <Tooltip
                                    contentStyle={{ backgroundColor: '#1d1c1a', border: '1px solid #3b3935', borderRadius: '0.5rem', color: '#f6f4f0', fontFamily: MONO_FONT, fontSize: 12 }}
                                    itemStyle={{ color: '#f6f4f0' }}
                                    cursor={{ stroke: '#504d45' }}
                                    formatter={(val) => formatBytes(typeof val === 'number' ? val : 0)}
                                    labelFormatter={(ts) => new Date(ts * 1000).toLocaleString([], { hour12: false })}
                                />
                                <Area
                                    type={trafficChartView === 'sample' ? 'stepAfter' : 'monotone'}
                                    dataKey={downloadKey}
                                    name="Download"
                                    stroke={DOWNLOAD_COLOR}
                                    strokeWidth={1.75}
                                    isAnimationActive={false}
                                    fillOpacity={1}
                                    fill="url(#gradDownload)"
                                />
                                <Area
                                    type={trafficChartView === 'sample' ? 'stepAfter' : 'monotone'}
                                    dataKey={uploadKey}
                                    name="Upload"
                                    stroke={UPLOAD_COLOR}
                                    strokeWidth={1.75}
                                    isAnimationActive={false}
                                    fillOpacity={1}
                                    fill="url(#gradUpload)"
                                />
                            </AreaChart>
                        ) : null}
                    </div>
                </section>

                <section className="rounded-lg border border-slate-800 bg-slate-900/60 p-4 sm:p-5">
                    <div className="-mx-3 -my-1 max-h-80 overflow-y-auto">
                        {topConsumers.length === 0 ? (
                            <div className="py-10 text-center text-sm text-slate-500">No activity in this range</div>
                        ) : (
                            topConsumers.map(u => {
                                const selectionId = getConsumerSelectionId(u, chartMode)
                                const isSelected = selectedConsumerSelectionId === selectionId
                                const canSelect = !!selectionId
                                const interfaceLabel = chartMode === 'wireguard'
                                    ? (u.interface_name || (u.flow || '').replace(/^wireguard:/i, '').trim() || 'default')
                                    : ''

                                return (
                                    <button
                                        key={selectionId || u.name}
                                        type="button"
                                        disabled={!canSelect}
                                        onClick={() => {
                                            setSelectedConsumers(prev => ({
                                                ...prev,
                                                [chartMode]: getConsumerSelectionId(prev[chartMode], chartMode) === selectionId ? null : u,
                                            }))
                                        }}
                                        className={`block w-full border-b border-l-2 border-b-slate-800/70 px-3 py-2.5 text-left transition-colors last:border-b-0 ${
                                            isSelected
                                                ? 'border-l-blue-500 bg-blue-500/10'
                                                : 'border-l-transparent hover:bg-slate-800/50'
                                        } ${canSelect ? 'cursor-pointer' : 'cursor-not-allowed'}`}
                                    >
                                        <div className="flex items-baseline justify-between gap-3">
                                            <div className="flex min-w-0 items-baseline gap-2.5">
                                                <span className="truncate text-sm text-slate-100" title={u.name}>{u.name}</span>
                                                {interfaceLabel && (
                                                    <span className="shrink-0 font-mono text-xs text-slate-400" title={interfaceLabel}>{interfaceLabel}</span>
                                                )}
                                            </div>
                                            <ByteFigure bytes={u.total} className="shrink-0 text-sm text-slate-100" />
                                        </div>
                                        {(u.upload !== undefined || u.download !== undefined) && (
                                            <div className="mt-1 flex items-center gap-3">
                                                <span className="flex items-center gap-1" title="Upload">
                                                    <ArrowUp size={12} className="shrink-0 text-slate-500" aria-label="Upload" />
                                                    <ByteFigure bytes={u.upload ?? 0} className="text-xs text-slate-400" />
                                                </span>
                                                <span className="flex items-center gap-1" title="Download">
                                                    <ArrowDown size={12} className="shrink-0 text-slate-500" aria-label="Download" />
                                                    <ByteFigure bytes={u.download ?? 0} className="text-xs text-slate-400" />
                                                </span>
                                            </div>
                                        )}
                                    </button>
                                )
                            })
                        )}
                    </div>
                </section>
            </div>
        </div>
    )
}
