import { useEffect, useMemo, useRef, useState } from 'react'
import { Save, RefreshCw, FileJson, List } from 'lucide-react'
import Editor from 'react-simple-code-editor'
import Prism from 'prismjs'
import 'prismjs/components/prism-json'
import 'prismjs/themes/prism-tomorrow.css'
import { api } from '../api'
import InboundList from './singbox/InboundList'

type TabId = 'inbounds' | 'raw'

export default function SingboxConfigEditor() {
    const [activeTab, setActiveTab] = useState<TabId>('inbounds')
    const [config, setConfig] = useState('')
    const [originalConfig, setOriginalConfig] = useState('')
    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [searchTerm, setSearchTerm] = useState('')
    const [searchCursor, setSearchCursor] = useState(0)
    const searchInputRef = useRef<HTMLInputElement>(null)
    const [showDiff, setShowDiff] = useState(false)
    const [lastBackup, setLastBackup] = useState<string>('')

    // Load Config
    useEffect(() => {
        if (activeTab === 'raw') {
            loadConfig()
            loadBackupMeta()
        }
    }, [activeTab])

    const loadConfig = async () => {
        setLoading(true)
        try {
            const content = await api.getSingboxConfig()
            // Ensure pretty print
            try {
                const json = JSON.parse(content)
                const formatted = JSON.stringify(json, null, 2)
                setConfig(formatted)
                setOriginalConfig(formatted)
            } catch {
                // If not valid JSON, show as is
                setConfig(content)
                setOriginalConfig(content)
            }
        } catch (err) {
            console.error('Failed to load config:', err)
            // alert('Failed to load config') // Don't block UI with alerts on load
        } finally {
            setLoading(false)
        }
    }

    const loadBackupMeta = async () => {
        try {
            const meta = await api.getBackupMeta()
            if (meta.singbox_last_backup) {
                setLastBackup(new Date(meta.singbox_last_backup).toLocaleString())
            } else {
                setLastBackup('')
            }
        } catch (err) {
            console.error('Failed to load backup meta', err)
        }
    }

    const handleBackup = async () => {
        try {
            await api.backupConfig()
            alert('Backup created (.bak)')
            loadBackupMeta()
        } catch (err: any) {
            alert('Backup failed: ' + err.message || err)
        }
    }

    const handleRestore = async () => {
        if (!confirm('Restore from backup? This will overwrite current config.')) return
        try {
            const cfg = await api.restoreConfig()
            const formatted = JSON.stringify(cfg, null, 2)
            setConfig(formatted)
            setOriginalConfig(formatted)
            alert('Restored from backup')
            loadBackupMeta()
        } catch (err: any) {
            alert('Restore failed: ' + err.message || err)
        }
    }

    const performFind = (direction: 'next' | 'prev' = 'next', refocusSearch = false) => {
        if (!searchTerm) return
        const textarea = document.getElementById('singbox-raw-editor') as HTMLTextAreaElement | null
        if (!textarea) return
        const haystack = textarea.value
        let idx = -1
        if (direction === 'next') {
            const from = searchCursor || textarea.selectionEnd || 0
            idx = haystack.indexOf(searchTerm, from)
            if (idx < 0) {
                idx = haystack.indexOf(searchTerm, 0)
            }
            if (idx >= 0) {
                setSearchCursor(idx + searchTerm.length)
            }
        } else {
            const from = searchCursor ? Math.max(0, searchCursor - searchTerm.length - 1) : Math.max(0, textarea.selectionStart - 1)
            idx = haystack.lastIndexOf(searchTerm, from)
            if (idx < 0) {
                idx = haystack.lastIndexOf(searchTerm)
            }
            if (idx >= 0) {
                setSearchCursor(idx)
            }
        }
        if (idx >= 0) {
            textarea.blur()
            textarea.focus()
            textarea.setSelectionRange(idx, idx + searchTerm.length)

            // Center scroll
            const linesBefore = haystack.substring(0, idx).split('\n').length - 1
            const lineHeight = 21
            const padding = 16
            const scrollValues = linesBefore * lineHeight + padding
            const shell = document.querySelector('.raw-editor-shell')
            if (shell) {
                shell.scrollTop = Math.max(0, scrollValues - shell.clientHeight / 2)
            }
        }
        if (refocusSearch) {
            setTimeout(() => searchInputRef.current?.focus({ preventScroll: true }), 0)
        }
    }

    const handleSave = async () => {
        setSaving(true)
        try {
            // Validate JSON before sending
            try {
                JSON.parse(config)
            } catch (e: any) {
                alert(`Invalid JSON: ${e.message}`)
                setSaving(false)
                return
            }

            await api.updateSingboxConfig(config)
            setOriginalConfig(config)
            alert('Configuration saved and service restarted!')
        } catch (err: any) {
            alert(`Failed to save: ${err.message || err}`)
        } finally {
            setSaving(false)
        }
    }

    const highlightCode = (code: string) => {
        return Prism.highlight(code, Prism.languages.json, 'json')
    }

    const hasChanges = config !== originalConfig
    const diffLines = useMemo(() => {
        const o = (originalConfig || '').split('\n')
        const c = (config || '').split('\n')
        const maxLen = Math.max(o.length, c.length)
        const rows: { line: number; original: string; current: string }[] = []
        for (let i = 0; i < maxLen; i++) {
            if ((o[i] ?? '') !== (c[i] ?? '')) rows.push({ line: i + 1, original: o[i] ?? '', current: c[i] ?? '' })
        }
        return rows
    }, [config, originalConfig])

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h2 className="text-xl font-bold text-white flex items-center gap-2">
                        Sing-box Configuration
                    </h2>
                </div>
                <div className="flex gap-2">
                    {activeTab === 'raw' && (
                        <>
                            <button
                                onClick={loadConfig}
                                className={`p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 transition-all border border-slate-700 ${loading ? 'animate-spin' : ''}`}
                                title="Refresh"
                            >
                                <RefreshCw size={18} />
                            </button>
                            <button
                                onClick={handleSave}
                                disabled={!hasChanges || saving}
                                className={`flex items-center gap-2 px-4 py-2 rounded-lg shadow-lg font-medium text-sm transition-all ${hasChanges
                                    ? 'bg-blue-600 hover:bg-blue-500 text-white shadow-blue-500/20'
                                    : 'bg-slate-800 text-slate-500 cursor-not-allowed'
                                    }`}
                            >
                                <Save size={16} />
                                {saving ? 'Saving...' : 'Save Changes'}
                            </button>
                        </>
                    )}
                </div>
            </div>

            {/* Editor Container */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex flex-col h-[700px]">
                {/* Tabs */}
                <div className="flex border-b border-slate-800 bg-slate-950/50">
                    <button
                        onClick={() => setActiveTab('inbounds')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 flex items-center gap-2 ${activeTab === 'inbounds' ? 'border-blue-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        <List size={16} />
                        Inbounds
                    </button>
                    <button
                        onClick={() => setActiveTab('raw')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 flex items-center gap-2 ${activeTab === 'raw' ? 'border-amber-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        <FileJson size={16} />
                        Raw Config (JSON)
                    </button>
                </div>

                {/* Content */}
                <div className="flex-1 bg-slate-950 overflow-hidden flex flex-col">
                    {activeTab === 'raw' ? (
                        <>
                            {/* Search Bar */}
                            <div className="flex items-center gap-2 p-2 bg-slate-900 border-b border-slate-800">
                                <div className="relative flex-1 max-w-sm">
                                    <input
                                        type="text"
                                        value={searchTerm}
                                        onChange={e => {
                                            setSearchTerm(e.target.value)
                                            setSearchCursor(0)
                                        }}
                                        onKeyDown={e => {
                                            if (e.key === 'Enter') {
                                                e.preventDefault()
                                                performFind(e.shiftKey ? 'prev' : 'next', true)
                                            }
                                        }}
                                        placeholder="Find in config..."
                                        className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-3 pr-20 py-1.5 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors"
                                        ref={searchInputRef}
                                    />
                                    <div className="absolute right-1 top-1 flex gap-0.5">
                                        <button
                                            onClick={() => performFind('prev')}
                                            className="p-1 hover:bg-slate-800 rounded text-slate-400 hover:text-white"
                                            title="Previous (Shift+Enter)"
                                        >
                                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M12 19V5M5 12l7-7 7 7" /></svg>
                                        </button>
                                        <button
                                            onClick={() => performFind('next')}
                                            className="p-1 hover:bg-slate-800 rounded text-slate-400 hover:text-white"
                                            title="Next (Enter)"
                                        >
                                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M12 5v14M5 12l7 7 7-7" /></svg>
                                        </button>
                                    </div>
                                </div>
                            </div>

                            {/* Actions Bar */}
                            <div className="flex items-center justify-between p-3 border-b border-slate-800 bg-slate-900">
                                <div className="flex items-center gap-2">
                                    <button
                                        onClick={() => setShowDiff(d => !d)}
                                        className="px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium"
                                    >
                                        Compare Changes
                                    </button>
                                    <button
                                        onClick={handleBackup}
                                        className="px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium"
                                    >
                                        Backup Now
                                    </button>
                                    <button
                                        onClick={handleRestore}
                                        className="px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium"
                                    >
                                        Restore
                                    </button>
                                </div>
                                <div className="text-xs text-slate-500 font-mono">
                                    {lastBackup ? `Last Backup: ${lastBackup}` : 'No backups yet'}
                                </div>
                            </div>

                            {showDiff && (
                                <div className="bg-slate-950 border-b border-slate-800 p-4 text-xs font-mono overflow-auto max-h-48 custom-scrollbar">
                                    <div className="flex items-center justify-between mb-2">
                                        <p className="text-slate-400 font-semibold uppercase tracking-wider text-[10px]">Unsaved Changes</p>
                                        {diffLines.length === 0 && <span className="text-emerald-500">No changes detected</span>}
                                    </div>
                                    {diffLines.map(d => (
                                        <div key={d.line} className="grid grid-cols-[40px_1fr] gap-4 mb-1 hover:bg-white/5 p-0.5 rounded">
                                            <span className="text-slate-600 text-right select-none">{d.line}</span>
                                            <div>
                                                <div className="text-red-400/70 line-through decoration-red-400/30">{d.original || <span className="italic opacity-50">empty</span>}</div>
                                                <div className="text-emerald-400">{d.current}</div>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}

                            <div className="flex-1 overflow-auto custom-scrollbar relative">
                                <Editor
                                    value={config}
                                    onValueChange={code => setConfig(code)}
                                    highlight={highlightCode}
                                    padding={16}
                                    className="raw-editor min-h-full font-mono text-sm"
                                    style={{
                                        fontFamily: '"Fira Code", "Fira Mono", monospace',
                                        fontSize: 13,
                                    }}
                                    textareaId="singbox-raw-editor"
                                />
                            </div>
                        </>
                    ) : (
                        <div className="flex-1 overflow-auto custom-scrollbar p-6">
                            <InboundList />
                        </div>
                    )}
                </div>
            </div>
        </div>
    )
}
