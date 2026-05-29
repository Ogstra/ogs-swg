import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { api } from '../../services/api'
import { Terminal, RefreshCw, Search, X, Download } from 'lucide-react'

function hasBooleanOperator(query: string): boolean {
    return /\b(AND|OR)\b/i.test(query)
}

function shouldUseServerTailQuery(query: string): boolean {
    const trimmed = query.trim()
    if (!trimmed) return false
    return trimmed.includes('[') || trimmed.includes(']') || hasBooleanOperator(trimmed)
}

function filterTailLinesLocally(lines: string[], query: string): string[] {
    const trimmed = query.trim().toLowerCase()
    if (!trimmed) return lines
    return lines.filter(line => line.toLowerCase().includes(trimmed))
}

function toOffsetDateTime(value: string): string | undefined {
    const trimmed = value.trim()
    if (!trimmed) return undefined
    const date = new Date(trimmed)
    if (Number.isNaN(date.getTime())) return undefined

    const offsetMinutes = -date.getTimezoneOffset()
    const sign = offsetMinutes >= 0 ? '+' : '-'
    const absMinutes = Math.abs(offsetMinutes)
    const hours = String(Math.floor(absMinutes / 60)).padStart(2, '0')
    const minutes = String(absMinutes % 60).padStart(2, '0')
    return `${trimmed}:00${sign}${hours}:${minutes}`
}

interface LogTerminalProps {
    lines: string[]
    autoScroll: boolean
    setAutoScroll: (v: boolean) => void
    viewMode: 'tail' | 'search'
    searching: boolean
    searchStatus: string
    containerRef: React.RefObject<HTMLDivElement | null>
    onScroll: () => void
    initialScrollPendingRef: React.RefObject<boolean>
    onDownload: () => void
}

function LogTerminal({
    lines,
    autoScroll,
    setAutoScroll,
    viewMode,
    searching,
    searchStatus,
    containerRef,
    onScroll,
    initialScrollPendingRef,
    onDownload,
}: LogTerminalProps) {
    const virtualizer = useVirtualizer({
        count: lines.length,
        getScrollElement: () => containerRef.current,
        estimateSize: () => 22,
        overscan: 20,
    })

    // Keep tail mode pinned to the newest line while auto-scroll is enabled.
    // Virtual rows can be measured after React commits, so repeat the scroll
    // across animation frames to land on the final measured height.
    useLayoutEffect(() => {
        if (!autoScroll || viewMode !== 'tail') return
        if (lines.length === 0) return

        let cancelled = false
        const scrollToBottom = () => {
            if (cancelled) return
            virtualizer.scrollToIndex(lines.length - 1, { align: 'end' })
            const el = containerRef.current
            if (el) {
                el.scrollTop = el.scrollHeight
            }
        }

        scrollToBottom()
        let secondFrame: number | null = null
        const firstFrame = requestAnimationFrame(() => {
            scrollToBottom()
            secondFrame = requestAnimationFrame(scrollToBottom)
        })

        initialScrollPendingRef.current = false
        return () => {
            cancelled = true
            cancelAnimationFrame(firstFrame)
            if (secondFrame !== null) cancelAnimationFrame(secondFrame)
        }
    }, [lines.length, autoScroll, viewMode])

    // Scroll to bottom on each search chunk and on completion.
    // Synchronous scroll runs immediately inside useLayoutEffect before the next
    // render — unlike rAFs it cannot be cancelled by the next chunk arriving.
    // The rAF is a fine-tune pass for after the virtualizer measures new items;
    // cancelling it on cleanup is fine because the sync scroll already ran.
    useLayoutEffect(() => {
        if (viewMode !== 'search') return
        if (lines.length === 0) return
        if (!autoScroll) return

        const el = containerRef.current
        virtualizer.scrollToIndex(lines.length - 1, { align: 'end' })
        if (el) el.scrollTop = el.scrollHeight

        const frame = requestAnimationFrame(() => {
            virtualizer.scrollToIndex(lines.length - 1, { align: 'end' })
            if (el) el.scrollTop = el.scrollHeight
        })
        return () => cancelAnimationFrame(frame)
    }, [lines.length, viewMode, autoScroll, searching])

    const virtualItems = virtualizer.getVirtualItems()

    return (
        <div className="bg-slate-950 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex flex-col flex-1 min-h-0">
            <div className="flex items-center justify-between px-4 py-2 bg-slate-900 border-b border-slate-800">
                <div className="flex items-center gap-2 text-xs font-mono text-slate-400">
                    <Terminal size={14} className="text-emerald-400" />
                    <span>Console Output</span>
                </div>
                <div className="flex items-center gap-4 text-[10px] text-slate-500 font-medium uppercase tracking-wider">
                    <span>{lines.length} lines</span>
                    {viewMode === 'search' && searchStatus && <span className="text-slate-400 normal-case tracking-normal">{searchStatus}</span>}
                    <label className="flex items-center gap-1.5 cursor-pointer hover:text-slate-300 transition-colors">
                        <input
                            type="checkbox"
                            checked={autoScroll}
                            onChange={e => setAutoScroll(e.target.checked)}
                            className="rounded border-slate-700 bg-slate-800 text-blue-500 focus:ring-0 w-3 h-3"
                        />
                        Auto-scroll
                    </label>
                    <button
                        onClick={onDownload}
                        disabled={lines.length === 0}
                        className="flex items-center gap-1 hover:text-slate-200 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                        title="Download logs as .log file"
                    >
                        <Download size={12} />
                        Download
                    </button>
                </div>
            </div>

            <div
                className="logs-scrollbar flex-1 overflow-y-auto p-4 font-mono text-xs md:text-sm bg-black/20"
                ref={containerRef}
                onScroll={onScroll}
            >
                {lines.length === 0 ? (
                    <div className="h-full flex flex-col items-center justify-center text-slate-500 opacity-50">
                        <Terminal size={48} className="mb-4" />
                        <p>No logs to display</p>
                    </div>
                ) : (
                    <div
                        style={{ height: `${virtualizer.getTotalSize()}px`, position: 'relative' }}
                    >
                        {virtualItems.map(virtualRow => (
                            <div
                                key={virtualRow.key}
                                data-index={virtualRow.index}
                                ref={virtualizer.measureElement}
                                style={{
                                    position: 'absolute',
                                    top: 0,
                                    left: 0,
                                    width: '100%',
                                    transform: `translateY(${virtualRow.start}px)`,
                                }}
                                className="flex items-center gap-2 hover:bg-white/5 py-0.5 rounded -mx-2 group"
                            >
                                <span className="text-slate-400 select-none w-[3ch] text-center shrink-0 opacity-50 text-[10px]">
                                    {virtualRow.index + 1}
                                </span>
                                <span className="text-slate-300 break-all whitespace-pre-wrap">
                                    {lines[virtualRow.index]}
                                </span>
                            </div>
                        ))}
                    </div>
                )}
                <div className="h-4" />
            </div>
        </div>
    )
}

export default function LogViewer() {
    const [lines, setLines] = useState<string[]>([])
    const [tailRawLines, setTailRawLines] = useState<string[]>([])
    const [loading, setLoading] = useState(false)
    const [refreshInterval, setRefreshInterval] = useState<number>(5000)
    const [query, setQuery] = useState<string>('')
    const [searchQuery, setSearchQuery] = useState<string>('')
    const containerRef = useRef<HTMLDivElement>(null)
    const initialTailScrollPendingRef = useRef(true)
    const [autoScroll, setAutoScroll] = useState(true)
    const [searchLimit, setSearchLimit] = useState(500)
    const [searching, setSearching] = useState(false)
    const [searchStatus, setSearchStatus] = useState<string>('')
    const [searchFrom, setSearchFrom] = useState<string>('')
    const [searchTo, setSearchTo] = useState<string>('')
    const [viewMode, setViewMode] = useState<'tail' | 'search'>('tail')
    const [tailLimit, setTailLimit] = useState<number>(50)
    const searchAbortRef = useRef<AbortController | null>(null)
    const lastMaxIDRef = useRef<number>(0)
    // Pending lines buffer: chunks are accumulated here and flushed into state
    // via rAF so each rAF only triggers one React render regardless of how many
    // chunks the backend sends in quick succession.
    const pendingLinesRef = useRef<string[]>([])
    const rafRef = useRef<number | null>(null)
    const backendTailQuery = shouldUseServerTailQuery(query) ? query.trim() : ''

    const fetchLogs = (silent = false, currentQuery = query) => {
        const serverQuery = shouldUseServerTailQuery(currentQuery) ? currentQuery.trim() : ''
        const afterId = lastMaxIDRef.current
        const isIncremental = afterId > 0 && !serverQuery

        if (!silent && !isIncremental) setLoading(true)

        api.getLogs({ q: serverQuery || undefined, limit: isIncremental ? 200 : tailLimit, after_id: isIncremental ? afterId : undefined }).then(data => {
            const incoming = data.logs || []
            if (data.max_id && data.max_id > lastMaxIDRef.current) {
                lastMaxIDRef.current = data.max_id
            }
            if (isIncremental) {
                if (incoming.length === 0) return
                setTailRawLines(prev => {
                    const combined = [...prev, ...incoming]
                    return combined.length > 2000 ? combined.slice(combined.length - 2000) : combined
                })
            } else {
                setTailRawLines(incoming)
                setLines(serverQuery ? incoming : filterTailLinesLocally(incoming, currentQuery))
                if (!silent) setLoading(false)
            }
        }).catch(err => {
            console.error(err)
            if (!isIncremental) {
                setTailRawLines([])
                setLines(['Error loading logs: ' + err.message])
                if (!silent) setLoading(false)
            }
        })
    }

    useEffect(() => {
        if (viewMode !== 'tail') {
            initialTailScrollPendingRef.current = false
            return
        }
        initialTailScrollPendingRef.current = true
        setAutoScroll(true)
    }, [viewMode])

    useEffect(() => {
        return () => {
            searchAbortRef.current?.abort()
        }
    }, [])

    useEffect(() => {
        if (viewMode !== 'tail') return
        lastMaxIDRef.current = 0
        fetchLogs(true, query)
        const interval = setInterval(() => {
            fetchLogs(true, query)
        }, 500)
        return () => clearInterval(interval)
    }, [refreshInterval, backendTailQuery, viewMode, tailLimit])

    useEffect(() => {
        if (viewMode !== 'tail') return
        if (backendTailQuery) return
        setLines(filterTailLinesLocally(tailRawLines, query))
    }, [tailRawLines, query, backendTailQuery, viewMode])

    const handleScroll = () => {
        if (searching) return
        const el = containerRef.current
        if (!el) return
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 10
        setAutoScroll(atBottom)
    }

    const handleSearch = async () => {
        const q = searchQuery.trim()
        if (!q) {
            setLines(['Ingresa un término para buscar'])
            setViewMode('search')
            return
        }
        searchAbortRef.current?.abort()
        const controller = new AbortController()
        searchAbortRef.current = controller

        // Reset buffering state
        if (rafRef.current !== null) {
            cancelAnimationFrame(rafRef.current)
            rafRef.current = null
        }
        pendingLinesRef.current = []

        setSearching(true)
        setLines([])
        setViewMode('search')
        setAutoScroll(true)
        setSearchStatus('Searching logs...')
        let completed = false
        let matchedCount = 0
        let truncated = false

        // Schedule a rAF flush: drain pendingLinesRef into React state in a single
        // render, preventing one setState per chunk from the stream.
        const scheduleFlush = () => {
            if (rafRef.current !== null) return
            rafRef.current = requestAnimationFrame(() => {
                rafRef.current = null
                const batch = pendingLinesRef.current
                if (batch.length === 0) return
                pendingLinesRef.current = []
                setLines(prev => [...batch, ...prev])
            })
        }

        try {
            await api.searchLogsStream({
                query: q,
                limit: searchLimit,
                from: toOffsetDateTime(searchFrom),
                to: toOffsetDateTime(searchTo),
                signal: controller.signal,
            }, event => {
                if (searchAbortRef.current !== controller) return
                if (completed && event.type !== 'error') return
                if (event.type === 'status') {
                    matchedCount = event.matched ?? matchedCount
                    if (!completed) {
                        setSearchStatus(event.message || 'Searching logs...')
                    }
                    return
                }
                if (event.type === 'chunk') {
                    matchedCount = event.matched ?? matchedCount
                    // Prepend newest lines: backend sends oldest-first within each chunk,
                    // reverse so newest is at top when prepended.
                    const nextLines = [...(event.logs || [])].reverse()
                    // Accumulate into buffer; flush will batch these into one React render.
                    pendingLinesRef.current = [...nextLines, ...pendingLinesRef.current]
                    scheduleFlush()
                    if (!completed) {
                        setSearchStatus(`Found ${event.matched ?? 0} lines...`)
                    }
                    return
                }
                if (event.type === 'done') {
                    completed = true
                    matchedCount = event.matched ?? matchedCount
                    truncated = event.truncated === true
                    // Flush any remaining buffered lines before marking done
                    if (rafRef.current !== null) {
                        cancelAnimationFrame(rafRef.current)
                        rafRef.current = null
                    }
                    const remaining = pendingLinesRef.current
                    pendingLinesRef.current = []
                    if (remaining.length > 0) {
                        setLines(prev => [...remaining, ...prev])
                    }
                    const suffix = truncated ? ' (limit reached)' : ''
                    setSearchStatus(`Showing ${event.matched ?? 0} lines${suffix}`)
                    return
                }
                if (event.type === 'error') {
                    completed = true
                    setLines([event.message || 'Search failed'])
                    setSearchStatus('Search failed')
                }
            })
        } catch (err: any) {
            if (err?.name === 'AbortError') {
                if (searchAbortRef.current === controller) {
                    setSearchStatus('Search cancelled')
                }
                return
            }
            setLines([`Search failed: ${err.message}`])
            setViewMode('search')
            setSearchStatus('Search failed')
        } finally {
            if (searchAbortRef.current === controller) {
                searchAbortRef.current = null
                if (!completed && matchedCount > 0) {
                    const suffix = truncated || matchedCount >= searchLimit ? ' (limit reached)' : ''
                    setSearchStatus(`Showing ${matchedCount} lines${suffix}`)
                }
                setSearching(false)
            }
        }
    }

    const handleCancelSearch = () => {
        searchAbortRef.current?.abort()
        searchAbortRef.current = null
        setSearching(false)
        setSearchStatus('Search cancelled')
    }

    const handleDownload = () => {
        const blob = new Blob([lines.join('\n')], { type: 'text/plain' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `sing-box-logs-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.log`
        a.click()
        URL.revokeObjectURL(url)
    }

    return (
        <div className="h-full min-h-0 flex flex-col gap-4">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white hidden sm:block">sing-box</h1>
                    <div className="hidden sm:flex items-center gap-2 mt-1">
                        <span className="text-xs px-2 py-0.5 rounded border bg-indigo-500/10 text-indigo-400 border-indigo-500/20">
                            Access log
                        </span>
                    </div>
                </div>
                <div className="flex items-center justify-end gap-2 w-full sm:w-auto flex-nowrap">
                    {viewMode === 'tail' ? (
                        <div className="flex items-center gap-2 shrink-0">
                            <div className="flex items-center gap-2">
                                <span className="hidden sm:inline text-slate-500 text-xs font-medium uppercase tracking-wider">Lines</span>
                                <select
                                    value={tailLimit}
                                    onChange={e => setTailLimit(parseInt(e.target.value))}
                                    className="select-field h-[38px] w-[100px] sm:hidden bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                    aria-label="Lines"
                                >
                                    <option value={50}>50 lines</option>
                                    <option value={100}>100 lines</option>
                                    <option value={200}>200 lines</option>
                                </select>
                                <select
                                    value={tailLimit}
                                    onChange={e => setTailLimit(parseInt(e.target.value))}
                                    className="select-field hidden sm:block h-[38px] w-[65px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                    aria-label="Lines"
                                >
                                    <option value={50}>50</option>
                                    <option value={100}>100</option>
                                    <option value={200}>200</option>
                                </select>
                            </div>
                            <div className="flex items-center gap-2">
                                <span className="hidden sm:inline text-slate-500 text-xs font-medium uppercase tracking-wider">Poll</span>
                                <select
                                    value={refreshInterval}
                                    onChange={e => setRefreshInterval(parseInt(e.target.value))}
                                    className="select-field h-[38px] w-[100px] sm:hidden bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                    aria-label="Poll interval"
                                >
                                    <option value={2000}>2s poll</option>
                                    <option value={5000}>5s poll</option>
                                    <option value={10000}>10s poll</option>
                                    <option value={30000}>30s poll</option>
                                </select>
                                <select
                                    value={refreshInterval}
                                    onChange={e => setRefreshInterval(parseInt(e.target.value))}
                                    className="select-field hidden sm:block h-[38px] w-[65px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                    aria-label="Poll interval"
                                >
                                    <option value={2000}>2s</option>
                                    <option value={5000}>5s</option>
                                    <option value={10000}>10s</option>
                                    <option value={30000}>30s</option>
                                </select>
                            </div>
                        </div>
                    ) : (
                        <div className="flex items-center gap-2 shrink-0">
                            <span className="hidden sm:inline text-slate-500 text-xs font-medium uppercase tracking-wider">Limit</span>
                            <select
                                value={searchLimit}
                                onChange={e => setSearchLimit(parseInt(e.target.value))}
                                className="select-field h-[38px] w-[100px] sm:hidden bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                aria-label="Search limit"
                            >
                                <option value={100}>100 lines</option>
                                <option value={500}>500 lines</option>
                                <option value={1000}>1k lines</option>
                                <option value={5000}>5k lines</option>
                                <option value={10000}>10k lines</option>
                                <option value={25000}>25k lines</option>
                                <option value={50000}>50k lines</option>
                                <option value={100000}>100k lines</option>
                                <option value={250000}>250k lines</option>
                                <option value={500000}>500k lines</option>
                            </select>
                            <select
                                value={searchLimit}
                                onChange={e => setSearchLimit(parseInt(e.target.value))}
                                className="select-field hidden sm:block h-[38px] w-[65px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                aria-label="Search limit"
                            >
                                <option value={100}>100</option>
                                <option value={500}>500</option>
                                <option value={1000}>1k</option>
                                <option value={5000}>5k</option>
                                <option value={10000}>10k</option>
                                <option value={25000}>25k</option>
                                <option value={50000}>50k</option>
                                <option value={100000}>100k</option>
                                <option value={250000}>250k</option>
                                <option value={500000}>500k</option>
                            </select>
                        </div>
                    )}
                    <div className="flex items-center justify-end gap-2 shrink-0">
                        <div className="flex bg-slate-900 border border-slate-800 p-1 rounded-lg h-[38px] shrink-0">
                            <button
                                onClick={() => setViewMode('tail')}
                                className={`px-3 sm:px-4 h-full rounded-md text-sm font-medium transition-all ${viewMode === 'tail' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-200'}`}
                            >
                                <span className="sm:hidden">Live</span>
                                <span className="hidden sm:inline">Live Tail</span>
                            </button>
                            <button
                                onClick={() => setViewMode('search')}
                                className={`px-3 sm:px-4 h-full rounded-md text-sm font-medium transition-all ${viewMode === 'search' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-200'}`}
                            >
                                <span className="sm:hidden">History</span>
                                <span className="hidden sm:inline">Search History</span>
                            </button>
                        </div>
                        <button
                            onClick={() => fetchLogs()}
                            className="w-[38px] h-[38px] shrink-0 flex items-center justify-center rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 text-xs border border-slate-700 transition-colors"
                            title="Refresh Now"
                        >
                            <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
                        </button>
                    </div>
                </div>
            </div>

            {/* Controls */}
            <div className={`bg-slate-900 border border-slate-800 rounded-xl p-3 flex gap-4 items-center shadow-sm ${viewMode === 'tail' ? 'flex-wrap' : 'flex-nowrap flex-wrap'}`}>
                {viewMode === 'tail' ? (
                    <>
                        <div className="flex-1 min-w-0 relative">
                            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                            <input
                                type="text"
                                value={query}
                                onChange={e => {
                                    setQuery(e.target.value)
                                    setAutoScroll(false)
                                }}
                                placeholder="text, [user], AND, OR..."
                                className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-9 pr-3 py-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors placeholder:text-slate-600"
                            />
                        </div>

                        <div className="flex items-center gap-3 shrink-0">
                            <button
                                onClick={() => fetchLogs()}
                                disabled={loading}
                                className="h-[38px] px-4 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors shadow-lg shadow-blue-500/20 flex items-center gap-2 disabled:opacity-50"
                            >
                                {loading ? <RefreshCw size={14} className="animate-spin" /> : <Search size={14} />}
                                Search
                            </button>
                        </div>
                    </>
                ) : (
                    <>
                        <div className="flex-1 min-w-[200px] relative">
                            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                            <input
                                type="text"
                                value={searchQuery}
                                onChange={e => {
                                    setSearchQuery(e.target.value)
                                }}
                                onKeyDown={e => e.key === 'Enter' && handleSearch()}
                                placeholder="text, [user], AND, OR..."
                                className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-9 pr-3 py-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors placeholder:text-slate-600"
                            />
                        </div>

                        <input
                            type="datetime-local"
                            value={searchFrom}
                            onChange={e => setSearchFrom(e.target.value)}
                            className="hidden sm:block h-[38px] min-w-[190px] bg-slate-950 border border-slate-700 rounded-lg px-3 text-sm text-slate-200 outline-none focus:border-blue-500"
                            aria-label="Search from"
                        />

                        <input
                            type="datetime-local"
                            value={searchTo}
                            onChange={e => setSearchTo(e.target.value)}
                            className="hidden sm:block h-[38px] min-w-[190px] bg-slate-950 border border-slate-700 rounded-lg px-3 text-sm text-slate-200 outline-none focus:border-blue-500"
                            aria-label="Search to"
                        />

                        <div className="flex items-center gap-3 shrink-0">
                            <button
                                onClick={() => handleSearch()}
                                disabled={searching}
                                className="h-[38px] px-4 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors shadow-lg shadow-blue-500/20 flex items-center gap-2 disabled:opacity-50"
                                aria-label={searching ? 'Searching' : 'Search'}
                                title={searching ? 'Searching' : 'Search'}
                            >
                                {searching ? <RefreshCw size={14} className="animate-spin" /> : <Search size={14} />}
                                <span className="hidden sm:inline">{searching ? 'Searching...' : 'Search'}</span>
                            </button>
                            {searching && (
                                <button
                                    onClick={handleCancelSearch}
                                    className="h-[38px] px-4 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-sm font-medium transition-colors border border-slate-700 flex items-center gap-2"
                                    aria-label="Cancel search"
                                    title="Cancel search"
                                >
                                    <X size={14} />
                                    <span className="hidden sm:inline">Cancel</span>
                                </button>
                            )}
                        </div>
                    </>
                )}
            </div>

            {/* Log Terminal */}
            <LogTerminal
                lines={lines}
                autoScroll={autoScroll}
                setAutoScroll={setAutoScroll}
                viewMode={viewMode}
                searching={searching}
                searchStatus={searchStatus}
                containerRef={containerRef}
                onScroll={handleScroll}
                initialScrollPendingRef={initialTailScrollPendingRef}
                onDownload={handleDownload}
            />
        </div>
    )
}
