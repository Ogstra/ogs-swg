import { useEffect, useLayoutEffect, useState, useRef } from 'react'
import { api } from '../../services/api'
import { Terminal, RefreshCw, Search } from 'lucide-react'

export default function LogViewer() {
    const [lines, setLines] = useState<string[]>([])
    const [loading, setLoading] = useState(false)
    const [refreshInterval, setRefreshInterval] = useState<number>(5000)
    const [query, setQuery] = useState<string>('')
    const [searchQuery, setSearchQuery] = useState<string>('')
    const [logSource, setLogSource] = useState<'journal' | 'file'>('journal')
    const containerRef = useRef<HTMLDivElement>(null)
    const initialTailScrollPendingRef = useRef(true)
    const [autoScroll, setAutoScroll] = useState(true)
    const [searchLimit, setSearchLimit] = useState(500)
    const [searching, setSearching] = useState(false)
    const [viewMode, setViewMode] = useState<'tail' | 'search'>('tail')
    const [tailLimit, setTailLimit] = useState<number>(50)
    const demoMode = typeof window !== 'undefined' && localStorage.getItem('demo_mode') === '1'

    const fetchLogs = (silent = false) => {
        if (!silent) setLoading(true)
        api.getLogs({ user: query || undefined, limit: tailLimit }).then(data => {
            setLines(data.logs)
            if (!silent) setLoading(false)
        }).catch(err => {
            console.error(err)
            setLines(['Error loading logs: ' + err.message])
            if (!silent) setLoading(false)
        })
    }

    useEffect(() => {
        api.getFeatures().then(f => {
            if (f.log_source === 'journal' || f.log_source === 'file') {
                if (demoMode && f.log_source === 'file') {
                    setLogSource('journal')
                } else {
                    setLogSource(f.log_source)
                }
            }
        }).catch(err => console.error('Failed to load features', err))
    }, [demoMode])

    useEffect(() => {
        if (viewMode !== 'tail') {
            initialTailScrollPendingRef.current = false
            return
        }
        initialTailScrollPendingRef.current = true
        setAutoScroll(true)
    }, [viewMode])

    useEffect(() => {
        if (viewMode !== 'tail') return
        fetchLogs(true)
        const interval = setInterval(() => {
            fetchLogs(true)
        }, refreshInterval)
        return () => clearInterval(interval)
    }, [refreshInterval, query, viewMode, tailLimit])

    useLayoutEffect(() => {
        const el = containerRef.current
        if (!el || !autoScroll || viewMode !== 'tail') return
        if (!initialTailScrollPendingRef.current) return
        el.scrollTop = el.scrollHeight
        initialTailScrollPendingRef.current = false
        setAutoScroll(false)
    }, [lines, autoScroll, viewMode])

    const handleScroll = () => {
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
        setSearching(true)
        try {
            const res = await api.searchLogs(q, searchLimit, 1)
            setLines(res.logs || [])
            setViewMode('search')
        } catch (err: any) {
            setLines([`Search failed: ${err.message}`])
            setViewMode('search')
        } finally {
            setSearching(false)
        }
    }

    return (
        <div className="h-full min-h-0 flex flex-col gap-4">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white hidden sm:block">sing-box</h1>
                    <div className="hidden sm:flex items-center gap-2 mt-1">
                        <span className={`text-xs px-2 py-0.5 rounded border ${logSource === 'journal' ? 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20' : 'bg-amber-500/10 text-amber-400 border-amber-500/20'}`}>
                            {logSource === 'journal' ? 'journalctl' : 'File'}
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
                                <option value={100}>100 limit</option>
                                <option value={500}>500 limit</option>
                                <option value={1000}>1kb limit</option>
                                <option value={5000}>5kb limit</option>
                            </select>
                            <select
                                value={searchLimit}
                                onChange={e => setSearchLimit(parseInt(e.target.value))}
                                className="select-field hidden sm:block h-[38px] w-[65px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-xs text-slate-300 outline-none focus:border-blue-500"
                                aria-label="Search limit"
                            >
                                <option value={100}>100</option>
                                <option value={500}>500</option>
                                <option value={1000}>1kb</option>
                                <option value={5000}>5kb</option>
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
            <div className={`bg-slate-900 border border-slate-800 rounded-xl p-3 flex gap-4 items-center shadow-sm ${viewMode === 'tail' ? 'flex-wrap' : 'flex-nowrap'}`}>
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
                                placeholder="Filter logs, user or foo AND bar..."
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
                                placeholder="Search logs, user or foo AND bar..."
                                className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-9 pr-3 py-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors placeholder:text-slate-600"
                            />
                        </div>

                        <div className="flex items-center gap-3 shrink-0">
                            <button
                                onClick={() => handleSearch()}
                                disabled={searching}
                                className="h-[38px] px-4 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors shadow-lg shadow-blue-500/20 flex items-center gap-2 disabled:opacity-50"
                            >
                                {searching ? <RefreshCw size={14} className="animate-spin" /> : <Search size={14} />}
                                {searching ? 'Searching...' : 'Search'}
                            </button>
                        </div>
                    </>
                )}
            </div>

            {/* Log Terminal */}
            <div className="bg-slate-950 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex flex-col flex-1 min-h-0">
                <div className="flex items-center justify-between px-4 py-2 bg-slate-900 border-b border-slate-800">
                    <div className="flex items-center gap-2 text-xs font-mono text-slate-400">
                        <Terminal size={14} className="text-emerald-400" />
                        <span>Console Output</span>
                    </div>
                    <div className="flex items-center gap-4 text-[10px] text-slate-500 font-medium uppercase tracking-wider">
                        <span>{lines.length} lines</span>
                        <label className="flex items-center gap-1.5 cursor-pointer hover:text-slate-300 transition-colors">
                            <input
                                type="checkbox"
                                checked={autoScroll}
                                onChange={e => setAutoScroll(e.target.checked)}
                                className="rounded border-slate-700 bg-slate-800 text-blue-500 focus:ring-0 w-3 h-3"
                            />
                            Auto-scroll
                        </label>
                    </div>
                </div>

                <div
                    className="flex-1 overflow-y-auto p-4 font-mono text-xs md:text-sm custom-scrollbar bg-black/20"
                    ref={containerRef}
                    onScroll={handleScroll}
                >
                    {lines.length === 0 ? (
                        <div className="h-full flex flex-col items-center justify-center text-slate-500 opacity-50">
                            <Terminal size={48} className="mb-4" />
                            <p>No logs to display</p>
                        </div>
                    ) : (
                        lines.map((line, i) => (
                            <div key={i} className="flex items-center gap-2 hover:bg-white/5 py-0.5 rounded -mx-2 group">
                                <span className="text-slate-400 select-none w-[3ch] text-center shrink-0 opacity-50 text-[10px]">{i + 1}</span>
                                <span className="text-slate-300 break-all whitespace-pre-wrap">{line}</span>
                            </div>
                        ))
                    )}
                    <div className="h-4" />
                </div>
            </div>
        </div>
    )
}
