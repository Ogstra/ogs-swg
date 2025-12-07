import { useEffect, useMemo, useState } from 'react'
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

    const highlightCode = (code: string) => {
        return activeTab === 'raw-singbox'
            ? Prism.highlight(code, Prism.languages.json, 'json')
            : Prism.highlight(code, Prism.languages.ini, 'ini')
    }

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between gap-3">
                <div>
                    <h1 className="text-2xl font-bold text-white">Raw Config</h1>
                    <p className="text-slate-400 text-sm">Edit raw JSON/text for sing-box and WireGuard</p>
                </div>
                <div className="flex gap-2">
                    <button
                        onClick={loadConfigs}
                        className={`p-2 rounded-lg bg-slate-800 text-slate-400 hover:text-white hover:bg-slate-700 transition-all ${loading ? 'animate-spin' : ''}`}
                        title="Refresh configs"
                    >
                        <RefreshCw size={18} />
                    </button>
                    <button
                        onClick={handleSave}
                        className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg shadow"
                    >
                        <Save size={16} />
                        Save Changes
                    </button>
                </div>
            </div>

            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex flex-col h-[700px]">
                <div className="flex border-b border-slate-800">
                    <button
                        onClick={() => setActiveTab('raw-singbox')}
                        className={`px-6 py-3 text-sm font-medium transition-colors ${activeTab === 'raw-singbox' ? 'bg-slate-800 text-white border-b-2 border-blue-500' : 'text-slate-400 hover:text-slate-200'}`}
                    >
                        sing-box (config.json)
                    </button>
                    <button
                        onClick={() => setActiveTab('raw-wireguard')}
                        className={`px-6 py-3 text-sm font-medium transition-colors ${activeTab === 'raw-wireguard' ? 'bg-slate-800 text-white border-b-2 border-emerald-500' : 'text-slate-400 hover:text-slate-200'}`}
                    >
                        WireGuard (wg0.conf)
                    </button>
                </div>

                <div className="flex items-center gap-2 p-3 border-b border-slate-800 bg-slate-950 flex-wrap">
                    <button
                        onClick={() => setShowDiff(!showDiff)}
                        className="px-3 py-2 bg-slate-800 text-slate-200 rounded-lg hover:bg-slate-700"
                    >
                        {showDiff ? 'Hide diff' : 'Show diff'}
                    </button>
                    <button
                        onClick={handleBackup}
                        className="px-3 py-2 bg-slate-800 text-slate-200 rounded-lg hover:bg-slate-700"
                    >
                        Backup
                    </button>
                    <button
                        onClick={handleRestore}
                        className="px-3 py-2 bg-slate-800 text-slate-200 rounded-lg hover:bg-slate-700"
                    >
                        Restore
                    </button>
                </div>

                <div className="flex items-start gap-3 p-4 bg-amber-500/10 border-b border-amber-500/20 text-amber-400 text-sm">
                    <AlertTriangle size={20} className="shrink-0 mt-0.5" />
                    <div>
                        <p className="font-bold">Warning</p>
                        <p className="opacity-90">Editing raw configurations can break your service. Validate JSON/WireGuard syntax before saving. Restart the service after applying changes.</p>
                    </div>
                </div>

                {showDiff && (
                    <div className="bg-slate-950 border-b border-slate-800 p-3 text-sm text-slate-200 max-h-48 overflow-auto">
                        <p className="text-xs text-slate-400 mb-2">Diff (line-by-line) vs last load</p>
                        {diffLines.length === 0 && <p className="text-emerald-400 text-xs">No changes</p>}
                        {diffLines.map(d => (
                            <div key={d.line} className="mb-2">
                                <p className="text-xs text-slate-500">Line {d.line}</p>
                                <p className="text-red-300 line-through">{d.original}</p>
                                <p className="text-emerald-300">{d.current}</p>
                            </div>
                        ))}
                    </div>
                )}

                <div className="flex-1 min-h-0 bg-slate-950 raw-editor-shell">
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
