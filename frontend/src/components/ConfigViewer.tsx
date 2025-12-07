import { useEffect, useState } from 'react'
import { api } from '../api'
import { FileJson, Copy, Check } from 'lucide-react'

export default function ConfigViewer() {
    const [config, setConfig] = useState<string>('')
    const [loading, setLoading] = useState(true)
    const [copied, setCopied] = useState(false)

    useEffect(() => {
        api.getConfig().then(data => {
            setConfig(JSON.stringify(data, null, 2))
            setLoading(false)
        }).catch(err => {
            console.error(err)
            setConfig('Error loading config: ' + err.message)
            setLoading(false)
        })
    }, [])

    const handleCopy = () => {
        navigator.clipboard.writeText(config)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
    }

    if (loading) return <div className="p-8 text-center text-slate-400">Loading configuration...</div>

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white">System Configuration</h1>
                    <p className="text-slate-400 text-sm mt-1">View current sing-box configuration</p>
                </div>
                <button
                    onClick={handleCopy}
                    className="flex items-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-white rounded-lg transition-colors"
                >
                    {copied ? <Check size={16} className="text-emerald-400" /> : <Copy size={16} />}
                    <span className="text-sm font-medium">{copied ? 'Copied!' : 'Copy JSON'}</span>
                </button>
            </div>

            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm">
                <div className="p-4 border-b border-slate-800 bg-slate-950/50 flex items-center gap-2">
                    <FileJson size={16} className="text-blue-400" />
                    <span className="text-xs font-mono text-slate-400">config.json</span>
                </div>
                <div className="p-0 overflow-x-auto">
                    <pre className="p-4 text-sm font-mono text-slate-300 leading-relaxed custom-scrollbar max-h-[70vh] overflow-y-auto">
                        {config}
                    </pre>
                </div>
            </div>
        </div>
    )
}
