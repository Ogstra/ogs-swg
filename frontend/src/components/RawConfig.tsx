import { useEffect, useMemo, useRef, useState } from 'react'
import { Save, RefreshCw, AlertTriangle } from 'lucide-react'
import Editor from 'react-simple-code-editor'
import Prism from 'prismjs'
import 'prismjs/components/prism-json'
import 'prismjs/components/prism-ini'
import 'prismjs/themes/prism-tomorrow.css'
import { api } from '../api'

type TabId = 'raw-singbox' | 'raw-wireguard'

/**
 * RawConfig renders the sing-box and WireGuard configs with a minimal highlighted editor.
 */
export default function RawConfig() {
    const [activeTab, setActiveTab] = useState<TabId>('raw-singbox')
    const [singboxConfig, setSingboxConfig] = useState('')
    const [wgConfig, setWgConfig] = useState('')
    const [originalSingboxConfig, setOriginalSingboxConfig] = useState('')
    const [originalWgConfig, setOriginalWgConfig] = useState('')
    const [showDiff, setShowDiff] = useState(false)
    const [loading, setLoading] = useState(false)
    const [lastBackup, setLastBackup] = useState<{ singbox?: string; wireguard?: string }>({})
    const [searchTerm, setSearchTerm] = useState('')
    const [searchCursor, setSearchCursor] = useState(0)
    const searchInputRef = useRef<HTMLInputElement>(null)

    const performFind = (direction: 'next' | 'prev' = 'next', refocusSearch = false) => {
        if (!searchTerm) return
        const textarea = document.getElementById('raw-config-editor') as HTMLTextAreaElement | null
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

            // Manual scroll calculation
            // Estimate line height ~21px (1.5 * 14px) + 16px padding
            const linesBefore = haystack.substring(0, idx).split('\n').length - 1
            const lineHeight = 21
            const padding = 16
            const scrollValues = linesBefore * lineHeight + padding

            const shell = document.querySelector('.raw-editor-shell')
            if (shell) {
                // Centering slightly
                shell.scrollTop = Math.max(0, scrollValues - shell.clientHeight / 2)
            }
        }
        if (refocusSearch) {
            // Wait a tick so the selection stays highlighted before returning focus
            setTimeout(() => searchInputRef.current?.focus({ preventScroll: true }), 0)
        }
    }

    const diffLines = useMemo(() => {
        const orig = activeTab === 'raw-singbox' ? originalSingboxConfig : originalWgConfig
        const curr = activeTab === 'raw-singbox' ? singboxConfig : wgConfig
        const o = orig.split('\n')
        const c = curr.split('\n')
        const maxLen = Math.max(o.length, c.length)
        const rows: { line: number; original: string; current: string }[] = []
        for (let i = 0; i < maxLen; i++) {
            if ((o[i] ?? '') !== (c[i] ?? '')) rows.push({ line: i + 1, original: o[i] ?? '', current: c[i] ?? '' })
        }
        return rows
    }, [activeTab, originalSingboxConfig, originalWgConfig, singboxConfig, wgConfig])

    const loadConfigs = async () => {
        setLoading(true)
        try {
            const sb = await api.getConfig()
            setSingboxConfig(JSON.stringify(sb, null, 2))
            setOriginalSingboxConfig(JSON.stringify(sb, null, 2))
            const wg = await api.getWireGuardConfig()
            setWgConfig(wg)
            setOriginalWgConfig(wg)
            const meta = await api.getBackupMeta()
            setLastBackup({
                singbox: meta.singbox_last_backup,
                wireguard: meta.wireguard_last_backup
            })
        } catch (err) {
            console.error(err)
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        loadConfigs()
    }, [])

    const handleSave = async () => {
        try {
            if (activeTab === 'raw-singbox') {
                JSON.parse(singboxConfig)
                await api.updateConfig(singboxConfig)
                setOriginalSingboxConfig(singboxConfig)
            } else {
                await api.updateWireGuardConfig(wgConfig)
                setOriginalWgConfig(wgConfig)
            }
            alert('Config saved successfully!')
        } catch (err) {
            alert('Failed to save: ' + err)
        }
    }

    const handleBackup = async () => {
        try {
            if (activeTab === 'raw-singbox') {
                await api.backupConfig()
            } else {
                await api.backupWireGuardConfig()
            }
            alert('Backup created (.bak)')
        } catch (err) {
            alert('Backup failed: ' + err)
        }
    }

    const handleRestore = async () => {
        if (!confirm('Restore from backup? This will overwrite current config.')) return
        try {
            if (activeTab === 'raw-singbox') {
                const cfg = await api.restoreConfig()
                setSingboxConfig(JSON.stringify(cfg, null, 2))
                setOriginalSingboxConfig(JSON.stringify(cfg, null, 2))
            } else {
                const cfg = await api.restoreWireGuardConfig()
                setWgConfig(cfg)
                setOriginalWgConfig(cfg)
            }
            alert('Restored from backup')
        } catch (err) {
            alert('Restore failed: ' + err)
        }
    }

    const currentValue = activeTab === 'raw-singbox' ? singboxConfig : wgConfig
    const setCurrentValue = activeTab === 'raw-singbox' ? setSingboxConfig : setWgConfig
    const currentLastBackup = activeTab === 'raw-singbox' ? lastBackup.singbox : lastBackup.wireguard

    const highlightCode = (code: string) => {
        return activeTab === 'raw-singbox'
            ? Prism.highlight(code, Prism.languages.json, 'json')
            : Prism.highlight(code, Prism.languages.ini, 'ini')
    }

    const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
        if (e.metaKey || e.ctrlKey) {
            if (e.key === 'f') {
                e.preventDefault()
                searchInputRef.current?.focus()
            }
        } else {
            if (e.key === 'F3') {
                e.preventDefault()
                performFind(e.shiftKey ? 'prev' : 'next')
            } else if (e.key === '/' && !e.shiftKey) {
                e.preventDefault()
                searchInputRef.current?.focus()
            }
        }
    }

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold text-white">Raw Config</h1>
                </div>
                <div className="flex flex-wrap gap-3">
                    <button
                        onClick={loadConfigs}
                        className={`p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 transition-all border border-slate-700 ${loading ? 'animate-spin' : ''}`}
                        title="Refresh configs"
                    >
                        <RefreshCw size={18} />
                    </button>
                    <button
                        onClick={handleSave}
                        className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg shadow-lg shadow-blue-500/20 font-medium text-sm transition-colors"
                    >
                        <Save size={16} />
                        Save Changes
                    </button>
                </div>
            </div>

            {/* Editor Container */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex flex-col h-[700px]">
                {/* Tabs */}
                <div className="flex border-b border-slate-800 bg-slate-950/50">
                    <button
                        onClick={() => setActiveTab('raw-singbox')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 ${activeTab === 'raw-singbox' ? 'border-blue-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        sing-box (config.json)
                    </button>
                    <button
                        onClick={() => setActiveTab('raw-wireguard')}
                        className={`px-6 py-3 text-sm font-medium transition-colors border-b-2 ${activeTab === 'raw-wireguard' ? 'border-emerald-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        WireGuard (wg0.conf)
                    </button>
                </div>

                {/* Toolbar */}
                <div className="flex items-center justify-between p-2 border-b border-slate-800 bg-slate-900 overflow-x-auto gap-4 custom-scrollbar">
                    <div className="flex items-center gap-2">
                        <button
                            onClick={() => setShowDiff(!showDiff)}
                            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-colors border ${showDiff ? 'bg-slate-800 text-white border-slate-600' : 'bg-transparent text-slate-400 border-slate-700 hover:text-slate-200'}`}
                        >
                            {showDiff ? 'Hide Diff' : 'Compare Changes'}
                        </button>
                        <div className="h-4 w-px bg-slate-700 mx-1"></div>
                        <button
                            onClick={handleBackup}
                            className="px-3 py-1.5 bg-slate-800 text-slate-300 rounded-lg hover:bg-slate-700 border border-slate-700 text-xs font-medium transition-colors"
                        >
                            Backup Now
                        </button>
                        <button
                            onClick={handleRestore}
                            className="px-3 py-1.5 bg-slate-800 text-slate-300 rounded-lg hover:bg-slate-700 border border-slate-700 text-xs font-medium transition-colors"
                        >
                            Restore
                        </button>
                    </div>
                    <div className="text-[10px] text-slate-500 font-mono whitespace-nowrap px-2">
                        Last Backup: {currentLastBackup ? new Date(currentLastBackup).toLocaleString() : 'Never'}
                    </div>
                </div>

                <div className="flex items-start gap-3 p-4 bg-amber-500/5 border-b border-amber-500/10 text-amber-400/80 text-xs">
                    <AlertTriangle size={16} className="shrink-0 mt-0.5 opacity-70" />
                    <div>
                        <p className="font-bold">Caution</p>
                        <p className="opacity-80 leading-relaxed">Editing raw configurations can break your service. Search (Ctrl/Cmd+F) available. Validate syntax before saving.</p>
                    </div>
                </div>

                {showDiff && (
                    <div className="bg-slate-950 border-b border-slate-800 p-4 text-xs font-mono overflow-auto max-h-64 custom-scrollbar">
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
                    <div className="text-[10px] text-slate-600 font-medium hidden sm:block">
                        Ctrl+F to focus • F3 Next
                    </div>
                </div>

                <div
                    className="flex-1 min-h-0 bg-slate-950 raw-editor-shell overflow-auto"
                    onKeyDown={handleKeyDown}
                    style={{ minHeight: 320, resize: 'vertical' }}
                >
                    <Editor
                        value={currentValue}
                        onValueChange={code => setCurrentValue(code)}
                        highlight={highlightCode}
                        padding={16}
                        textareaId="raw-config-editor"
                        className="raw-editor"
                    />
                </div>
            </div>
        </div>
    )
}
