import { useEffect, useState } from 'react'
import { Save, RefreshCw, AlertTriangle, FileJson, List } from 'lucide-react'
import Editor from 'react-simple-code-editor'
import Prism from 'prismjs'
import 'prismjs/components/prism-json'
import 'prismjs/themes/prism-tomorrow.css'
import { api } from '../api'
import InboundList from './singbox/InboundList'

type TabId = 'inbounds' | 'raw'

export default function SingboxConfigEditor() {
    const [activeTab, setActiveTab] = useState<TabId>('raw') // Default to Raw for now until Inbounds is ready
    const [config, setConfig] = useState('')
    const [originalConfig, setOriginalConfig] = useState('')
    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)

    // Load Config
    useEffect(() => {
        if (activeTab === 'raw') {
            loadConfig()
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

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                    <h2 className="text-xl font-bold text-white flex items-center gap-2">
                        Sing-box Configuration
                    </h2>
                    <p className="text-slate-400 text-sm mt-1">Manage Inbounds and edit raw JSON configuration</p>
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
                            {/* Toolbar */}
                            <div className="flex items-center justify-between p-2 border-b border-slate-800 bg-slate-900">
                                <div className="flex items-start gap-2 text-amber-400/80 text-xs px-2">
                                    <AlertTriangle size={14} className="mt-0.5" />
                                    <span>Editing raw JSON. Ensure syntax is correct.</span>
                                </div>
                            </div>

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
