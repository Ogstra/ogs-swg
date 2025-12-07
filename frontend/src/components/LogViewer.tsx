import { useEffect, useState, useRef } from 'react'
import { api } from '../api'
import { Terminal, RefreshCw, Search } from 'lucide-react'

export default function LogViewer() {
    const [lines, setLines] = useState<string[]>([])
    const [loading, setLoading] = useState(true)
    const [refreshInterval, setRefreshInterval] = useState<number>(5000)
    const [query, setQuery] = useState<string>('')
    const [logSource, setLogSource] = useState<'journal' | 'file'>('journal')
    const bottomRef = useRef<HTMLDivElement>(null)
    const containerRef = useRef<HTMLDivElement>(null)
    const [autoScroll, setAutoScroll] = useState(true)
    const [searchLimit, setSearchLimit] = useState(200)
    const [searching, setSearching] = useState(false)
    const [viewMode, setViewMode] = useState<'tail' | 'search'>('tail')

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

    const handleSearch = async () => {
        if (!query.trim()) {
            setLines(['Ingresa un término para buscar'])
            setViewMode('search')
            return
        }
        setSearching(true)
        try {
            const res = await api.searchLogs(query.trim(), searchLimit)
            setLines(res.logs)
            setViewMode('search')
        } catch (err: any) {
            setLines([`Search failed: ${err.message}`])
            setViewMode('search')
        } finally {
            setSearching(false)
        }
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white">sing-box Logs</h1>
                    <p className="text-slate-400 text-sm mt-1">Tail from journal/file (latest lines)</p>
                    <p className="text-slate-500 text-xs">Source: {logSource === 'journal' ? 'journalctl' : 'log file'}</p>
                </div>
                <div className="flex flex-wrap items-center gap-3">
                    <div className="flex items-center bg-slate-900 border border-slate-800 rounded-lg px-3 py-2 text-xs text-slate-300 gap-2 min-w-[260px]">
                        <Search size={14} className="text-slate-400" />
                        <input
                            type="text"
                            value={query}
                            onChange={e => setQuery(e.target.value)}
                            placeholder="Filter current tail / search full log"
                            className="bg-transparent outline-none text-slate-200 placeholder:text-slate-500 flex-1"
                        />
                    </div>
                    <button
                        onClick={() => fetchLogs(true)}
                        className="px-3 py-2 bg-slate-800 text-slate-200 rounded-lg hover:bg-slate-700"
                    >
                        Apply (tail)
                    </button>
                    <div className="flex items-center gap-2 text-sm text-slate-300">
                        <span className="text-slate-400">Limit</span>
                        <select
                            value={searchLimit}
                            onChange={e => setSearchLimit(parseInt(e.target.value))}
                            className="bg-slate-900 border border-slate-800 rounded px-2 py-1"
                        >
                            <option value={50}>50</option>
                            <option value={200}>200</option>
                            <option value={500}>500</option>
                            <option value={1000}>1000</option>
                        </select>
                    </div>
                    <button
                        onClick={handleSearch}
                        className="px-3 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg flex items-center gap-2"
                    >
                        <Search size={16} />
                        {searching ? 'Searching...' : 'Search full log'}
                    </button>
                    <div className="bg-slate-900 border border-slate-800 rounded-lg px-2 py-1 text-xs text-slate-300">
                        <label className="mr-2 text-slate-400">Auto-refresh</label>
                        <select
                            value={refreshInterval}
                            onChange={e => setRefreshInterval(parseInt(e.target.value))}
                            className="bg-transparent border-none outline-none text-slate-200"
                        >
                            <option value={5000}>5s</option>
                            <option value={10000}>10s</option>
                            <option value={20000}>20s</option>
                            <option value={60000}>1m</option>
                        </select>
                    </div>
                    <button
                        onClick={() => fetchLogs(true)}
                        className={`p-2 rounded-lg bg-slate-800 text-slate-400 hover:text-white hover:bg-slate-700 transition-all ${loading ? 'animate-spin' : ''}`}
                        title="Refresh Logs"
                    >
                        <RefreshCw size={18} />
                    </button>
                </div>
            </div>

            <div className="bg-slate-950 border border-slate-800 rounded-xl overflow-hidden shadow-sm font-mono text-xs md:text-sm">
                <div className="p-3 border-b border-slate-800 bg-slate-900 flex items-center gap-2">
                    <Terminal size={16} className="text-emerald-400" />
                    <span className="text-slate-400">
                        sing-box {viewMode === 'search' ? '(full log search)' : '(tail)'}
                    </span>
                </div>
                <div className="p-4 h-[60vh] overflow-y-auto custom-scrollbar bg-black/50" ref={containerRef} onScroll={handleScroll}>
                    {lines.map((line, i) => (
                        <div key={i} className="py-0.5 text-slate-300 hover:bg-slate-800/50 px-2 rounded whitespace-pre-wrap break-all">
                            <span className="text-slate-600 mr-3 select-none">{i + 1}</span>
                            {line}
                        </div>
                    ))}
                    <div ref={bottomRef} />
                </div>
            </div>
        </div>
    )
}
