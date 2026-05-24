import { type ReactNode, useMemo, useRef, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import Editor from 'react-simple-code-editor'
import Prism from 'prismjs'
import 'prismjs/components/prism-json'
import 'prismjs/components/prism-ini'
import 'prismjs/themes/prism-tomorrow.css'

type RawLanguage = 'json' | 'ini'

interface RawEditorPanelProps {
    value: string
    originalValue: string
    onChange: (next: string) => void
    onRefresh: () => void
    onSave: () => void
    onBackup: () => void
    onRestore: () => void
    loading?: boolean
    saving?: boolean
    canWrite?: boolean
    lastBackupText?: string
    language?: RawLanguage
    textareaId?: string
    saveLabel?: string
    bottomBarExtraDesktop?: ReactNode
    bottomBarExtraMobile?: ReactNode
    hideCompareOnMobile?: boolean
}

export function RawEditorPanel({
    value,
    originalValue,
    onChange,
    onRefresh,
    onSave,
    onBackup,
    onRestore,
    loading = false,
    saving = false,
    canWrite = true,
    lastBackupText = '',
    language = 'json',
    textareaId = 'raw-editor',
    saveLabel = 'Save Changes',
    bottomBarExtraDesktop,
    bottomBarExtraMobile,
    hideCompareOnMobile = false,
}: RawEditorPanelProps) {
    const [searchTerm, setSearchTerm] = useState('')
    const [searchCursor, setSearchCursor] = useState(0)
    const [showDiff, setShowDiff] = useState(false)
    const searchInputRef = useRef<HTMLInputElement>(null)
    const shellRef = useRef<HTMLDivElement>(null)

    const hasChanges = useMemo(() => value !== originalValue, [value, originalValue])
    const lineNumbers = useMemo(() => {
        const count = Math.max(1, (value.match(/\n/g)?.length ?? 0) + 1)
        return Array.from({ length: count }, (_, index) => index + 1)
    }, [value])

    const highlightCode = (code: string) => {
        const highlighted = language === 'ini'
            ? Prism.highlight(code, Prism.languages.ini, 'ini')
            : Prism.highlight(code, Prism.languages.json, 'json')

        const lines = highlighted.split('\n')
        return lines.map((line, index) => (
            <div key={index} className="raw-editor-line">
                <span className="raw-editor-line-number" aria-hidden="true">
                    {index + 1}
                </span>
                <span
                    className="raw-editor-line-content"
                    dangerouslySetInnerHTML={{ __html: line || ' ' }}
                />
            </div>
        ))
    }

    const diffLines = useMemo(() => {
        const o = (originalValue || '').split('\n')
        const c = (value || '').split('\n')
        const maxLen = Math.max(o.length, c.length)
        const rows: { line: number; original: string; current: string }[] = []
        for (let i = 0; i < maxLen; i++) {
            if ((o[i] ?? '') !== (c[i] ?? '')) rows.push({ line: i + 1, original: o[i] ?? '', current: c[i] ?? '' })
        }
        return rows
    }, [value, originalValue])

    const performFind = (direction: 'next' | 'prev' = 'next', refocusSearch = false) => {
        if (!searchTerm) return
        const textarea = document.getElementById(textareaId) as HTMLTextAreaElement | null
        if (!textarea) return
        const haystack = textarea.value
        let idx = -1
        if (direction === 'next') {
            const from = searchCursor || textarea.selectionEnd || 0
            idx = haystack.indexOf(searchTerm, from)
            if (idx < 0) idx = haystack.indexOf(searchTerm, 0)
            if (idx >= 0) setSearchCursor(idx + searchTerm.length)
        } else {
            const from = searchCursor ? Math.max(0, searchCursor - searchTerm.length - 1) : Math.max(0, textarea.selectionStart - 1)
            idx = haystack.lastIndexOf(searchTerm, from)
            if (idx < 0) idx = haystack.lastIndexOf(searchTerm)
            if (idx >= 0) setSearchCursor(idx)
        }
        if (idx >= 0) {
            textarea.blur()
            textarea.focus()
            textarea.setSelectionRange(idx, idx + searchTerm.length)
            const linesBefore = haystack.substring(0, idx).split('\n').length - 1
            const lineHeight = 21
            const padding = 16
            const scrollValues = linesBefore * lineHeight + padding
            const shell = shellRef.current
            if (shell) shell.scrollTop = Math.max(0, scrollValues - shell.clientHeight / 2)
        }
        if (refocusSearch) {
            setTimeout(() => searchInputRef.current?.focus({ preventScroll: true }), 0)
        }
    }

    const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
        if (e.metaKey || e.ctrlKey) {
            if (e.key === 'f') {
                e.preventDefault()
                searchInputRef.current?.focus()
            }
        } else if (e.key === 'F3') {
            e.preventDefault()
            performFind(e.shiftKey ? 'prev' : 'next')
        } else if (e.key === '/') {
            e.stopPropagation()
        }
    }

    return (
        <>
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
                        className="w-full h-9 bg-slate-950 border border-slate-700 rounded-lg pl-3 pr-20 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors"
                        ref={searchInputRef}
                    />
                    <div className="absolute right-2 top-1/2 -translate-y-1/2 flex gap-0.5">
                        <button
                            onClick={() => performFind('prev')}
                            className="p-1 hover:bg-slate-800 rounded text-slate-400 hover:text-white flex items-center justify-center leading-none"
                            title="Previous (Shift+Enter)"
                        >
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M12 19V5M5 12l7-7 7 7" /></svg>
                        </button>
                        <button
                            onClick={() => performFind('next')}
                            className="p-1 hover:bg-slate-800 rounded text-slate-400 hover:text-white flex items-center justify-center leading-none"
                            title="Next (Enter)"
                        >
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><path d="M12 5v14M5 12l7 7 7-7" /></svg>
                        </button>
                    </div>
                </div>
                <button
                    onClick={onRefresh}
                    className={`p-2 rounded-lg bg-slate-800 text-slate-300 hover:text-white hover:bg-slate-700 transition-all border border-slate-700 ${loading ? 'animate-spin' : ''}`}
                    title="Refresh"
                >
                    <RefreshCw size={18} />
                </button>
                <button
                    onClick={onSave}
                    disabled={!canWrite || !hasChanges || saving}
                    className={`flex items-center justify-center gap-2 w-9 h-9 p-0 sm:w-auto sm:h-auto sm:px-4 sm:py-2 rounded-lg shadow-lg font-medium text-sm transition-all ${!canWrite || !hasChanges || saving
                        ? 'bg-slate-800 text-slate-500 cursor-not-allowed'
                        : 'bg-blue-600 hover:bg-blue-500 text-white shadow-blue-500/20'
                        }`}
                >
                    <Save size={16} />
                    <span className="sm:hidden sr-only">{saving ? 'Saving...' : saveLabel}</span>
                    <span className="hidden sm:inline">{saving ? 'Saving...' : saveLabel}</span>
                </button>
                <div className="hidden sm:block ml-auto text-xs text-slate-500 font-mono">
                    {lastBackupText ? `Last Backup: ${lastBackupText}` : 'No backups yet'}
                </div>
            </div>

            <div className="flex items-center justify-between p-3 border-b border-slate-800 bg-slate-900">
                <div className={`grid ${bottomBarExtraMobile ? 'grid-cols-4' : 'grid-cols-3'} sm:flex items-center gap-2 w-full sm:w-auto`}>
                    <button
                        onClick={() => setShowDiff(d => !d)}
                        className={`w-full sm:w-auto px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium whitespace-nowrap ${hideCompareOnMobile ? 'hidden sm:inline-flex' : ''}`}
                    >
                        {showDiff ? (
                            <>
                                <span className="sm:hidden">Hide</span>
                                <span className="hidden sm:inline">Hide Diff</span>
                            </>
                        ) : (
                            <>
                                <span className="sm:hidden">Compare</span>
                                <span className="hidden sm:inline">Compare Changes</span>
                            </>
                        )}
                    </button>
                    <button
                        onClick={onBackup}
                        disabled={!canWrite}
                        className="w-full sm:w-auto px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium disabled:opacity-60 whitespace-nowrap"
                    >
                        <span className="sm:hidden">Backup</span>
                        <span className="hidden sm:inline">Backup Now</span>
                    </button>
                    <button
                        onClick={onRestore}
                        disabled={!canWrite}
                        className="w-full sm:w-auto px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-200 hover:text-white transition-colors text-sm font-medium disabled:opacity-60 whitespace-nowrap"
                    >
                        Restore
                    </button>
                    {bottomBarExtraMobile && (
                        <div className="sm:hidden contents">
                            {bottomBarExtraMobile}
                        </div>
                    )}
                </div>
                <div className="hidden sm:flex items-center gap-3">{bottomBarExtraDesktop}</div>
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

            <div
                ref={shellRef}
                className="flex-1 overflow-auto custom-scrollbar relative raw-editor-shell"
                onKeyDown={handleKeyDown}
            >
                <div className="sr-only" aria-hidden="true">
                    {lineNumbers.join('\n')}
                </div>
                <Editor
                    value={value}
                    onValueChange={onChange}
                    highlight={highlightCode}
                    padding={16}
                    className="raw-editor min-h-full font-mono text-sm"
                    style={{
                        fontFamily: '"Fira Code", "Fira Mono", monospace',
                        fontSize: 13,
                    }}
                    textareaId={textareaId}
                />
            </div>
        </>
    )
}
