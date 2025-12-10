import { useEffect, useState, useRef } from 'react'
import { api } from '../api'
import { Terminal, RefreshCw, Search } from 'lucide-react'

export default function LogViewer() {
    const [lines, setLines] = useState<string[]>([])
    const [loading, setLoading] = useState(true)
    const [refreshInterval, setRefreshInterval] = useState<number>(5000)
    const [query, setQuery] = useState<string>('')
    const [searchQuery, setSearchQuery] = useState<string>('')
    const [logSource, setLogSource] = useState<'journal' | 'file'>('journal')
    const bottomRef = useRef<HTMLDivElement>(null)
    const containerRef = useRef<HTMLDivElement>(null)
    const [autoScroll, setAutoScroll] = useState(true)
    const [searchLimit, setSearchLimit] = useState(200)
    const [searching, setSearching] = useState(false)
    const [viewMode, setViewMode] = useState<'tail' | 'search'>('tail')
    const [searchPage, setSearchPage] = useState(1)
    const [searchHasMore, setSearchHasMore] = useState(false)

    const fetchLogs = (forceTail = false) => {
        setLoading(true)
        api.getLogs(query || undefined).then(data => {
            setLines(data.logs)
            if (forceTail) setViewMode('tail')
            setLoading(false)
        }).catch(err => {
            console.error(err)
            setLines(['Error loading logs: ' + err.message])
            if (forceTail) setViewMode('tail')
            setLoading(false)
        })
    }

    useEffect(() => {
        api.getFeatures().then(f => {
            if (f.log_source === 'journal' || f.log_source === 'file') {
                setLogSource(f.log_source)
            }
        }).catch(err => console.error('Failed to load features', err))
    }, [])

    useEffect(() => {
        if (viewMode !== 'tail') return
        fetchLogs(true)
        const interval = setInterval(() => {
            fetchLogs(true)
        }, refreshInterval)
        return () => clearInterval(interval)
    }, [refreshInterval, query, viewMode])

    useEffect(() => {
        if (autoScroll && bottomRef.current) {
            bottomRef.current.scrollIntoView({ behavior: 'smooth' })
        }
    }, [lines, autoScroll])

    const handleScroll = () => {
        const el = containerRef.current
        if (!el) return
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 10
        setAutoScroll(atBottom)
    }

    const handleSearch = async (page = 1) => {
        const q = searchQuery.trim()
        if (!q) {
            setLines(['Ingresa un término para buscar'])
            setViewMode('search')
            setSearchHasMore(false)
            setSearchPage(1)
            return
        }
        setSearching(true)
        try {
            const res = await api.searchLogs(q, searchLimit, page)
            setLines(res.logs || [])
            setViewMode('search')
            setSearchPage(res.page || page)
            setSearchHasMore(!!res.has_more)
        } catch (err: any) {
            setLines([`Search failed: ${err.message}`])
            setViewMode('search')
        } finally {
            setSearching(false)
        }
    }

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white">sing-box Logs</h1>
                    <div className="flex items-center gap-2 mt-1">
                        <p className="text-slate-400 text-sm">System and Service Logs</p>
                        <span className="text-slate-600">•</span>
                        <span className={`text-xs px-2 py-0.5 rounded border ${logSource === 'journal' ? 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20' : 'bg-amber-500/10 text-amber-400 border-amber-500/20'}`}>
                            {logSource === 'journal' ? 'journalctl' : 'File'}
                        </span>
                    </div>
                </div>
                <div className="flex bg-slate-900 border border-slate-800 p-1 rounded-lg">
                    <button
                        onClick={() => setViewMode('tail')}
                        className={`px-4 py-1.5 rounded-md text-sm font-medium transition-all ${viewMode === 'tail' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-200'}`}
                    >
                        Live Tail
                    </button>
                    <button
                        onClick={() => setViewMode('search')}
                        className={`px-4 py-1.5 rounded-md text-sm font-medium transition-all ${viewMode === 'search' ? 'bg-slate-800 text-white shadow-sm' : 'text-slate-400 hover:text-slate-200'}`}
                    >
                        Search History
                    </button>
                </div>
            </div>

            {/* Controls */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-3 flex flex-wrap gap-4 items-center shadow-sm">
                {viewMode === 'tail' ? (
                    <>
                        <div className="flex-1 min-w-[200px] relative">
                            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                            <input
                                type="text"
                                value={query}
                                onChange={e => setQuery(e.target.value)}
                                placeholder="Filter live logs..."
                                className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-9 pr-3 py-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors placeholder:text-slate-600"
                            />
                        </div>

                        <div className="flex items-center gap-3 border-l border-slate-800 pl-4">
                            <div className="flex items-center gap-2">
                                <span className="text-slate-500 text-xs font-medium uppercase tracking-wider">Poll</span>
                                <select
                                    value={refreshInterval}
                                    onChange={e => setRefreshInterval(parseInt(e.target.value))}
                                    className="bg-slate-950 border border-slate-700 rounded-lg px-2 py-1.5 text-xs text-slate-300 outline-none focus:border-blue-500"
                                >
                                    <option value={2000}>2s</option>
                                    <option value={5000}>5s</option>
                                    <option value={10000}>10s</option>
                                    <option value={30000}>30s</option>
                                </select>
                            </div>

                            <button
                                onClick={() => fetchLogs(true)}
                                className={`p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 text-xs border border-slate-700 transition-all ${loading ? 'animate-spin' : ''}`}
                                title="Refresh Now"
                            >
                                <RefreshCw size={16} />
                            </button>

                            <button
                                onClick={() => fetchLogs(true)}
                                className="px-3 py-1.5 bg-blue-600/10 text-blue-400 hover:bg-blue-600/20 border border-blue-600/20 rounded-lg text-xs font-medium transition-colors"
                            >
                                Apply Filter
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
                                    setSearchPage(1)
                                }}
                                onKeyDown={e => e.key === 'Enter' && handleSearch()}
                                placeholder="Search query..."
                                className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-9 pr-3 py-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors placeholder:text-slate-600"
                            />
                        </div>

                        <div className="flex items-center gap-3 border-l border-slate-800 pl-4">
                            <div className="flex items-center gap-2">
                                <span className="text-slate-500 text-xs font-medium uppercase tracking-wider">Limit</span>
                                <select
                                    value={searchLimit}
                                    onChange={e => {
                                        setSearchLimit(parseInt(e.target.value))
                                        setSearchPage(1)
                                    }}
                                    className="bg-slate-950 border border-slate-700 rounded-lg px-2 py-1.5 text-xs text-slate-300 outline-none focus:border-blue-500"
                                >
                                    <option value={100}>100</option>
                                    <option value={500}>500</option>
                                    <option value={1000}>1kb</option>
                                    <option value={5000}>5kb</option>
                                </select>
                            </div>

                            <button
                                onClick={() => handleSearch()}
                                disabled={searching}
                                className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors shadow-lg shadow-blue-500/20 flex items-center gap-2 disabled:opacity-50"
                            >
                                {searching ? <RefreshCw size={14} className="animate-spin" /> : <Search size={14} />}
                                {searching ? 'Searching...' : 'Search'}
                            </button>

                            <div className="flex items-center rounded-lg bg-slate-950 border border-slate-800 p-0.5">
                                <button
                                    onClick={() => handleSearch(Math.max(1, searchPage - 1))}
                                    disabled={searchPage <= 1 || searching}
                                    className="px-2 py-1 text-slate-400 hover:text-white disabled:opacity-30 transition-colors"
                                >
                                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M15 18l-6-6 6-6" /></svg>
                                </button>
                                <span className="text-xs font-mono text-slate-500 px-2 min-w-[3ch] text-center">{searchPage}</span>
                                <button
                                    onClick={() => handleSearch(searchPage + 1)}
                                    disabled={!searchHasMore || searching}
                                    className="px-2 py-1 text-slate-400 hover:text-white disabled:opacity-30 transition-colors"
                                >
                                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M9 18l6-6-6-6" /></svg>
                                </button>
                            </div>
                        </div>
                    </>
                )}
            </div>

            {/* Log Terminal */}
            <div className="bg-slate-950 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex flex-col h-[70vh]">
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
                            <div key={i} className="flex gap-3 hover:bg-white/5 py-0.5 px-2 rounded -mx-2 group">
                                <span className="text-slate-700 select-none w-[3ch] text-right shrink-0 opacity-50 text-[10px] pt-0.5">{i + 1}</span>
                                <span className="text-slate-300 break-all whitespace-pre-wrap">{line}</span>
                            </div>
                        ))
                    )}
                    <div ref={bottomRef} className="h-4" />
                </div>
            </div>
        </div>
    )
}
