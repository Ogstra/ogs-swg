import { useEffect, useState } from 'react'
import { api } from '../../services/api'
import { useAuth } from '../../context/AuthContext'
import { RawEditorPanel } from '../../components/raw/RawEditorPanel'

type TabId = 'raw-singbox' | 'raw-wireguard'

export default function RawConfig() {
    const { permissions } = useAuth()
    const canReadConfig = !!permissions?.can_read_config
    const canWriteConfig = !!permissions?.can_write_config
    const canReadWireguard = !!permissions?.can_read_wireguard
    const canWriteWireguard = !!permissions?.can_write_wireguard
    const [activeTab, setActiveTab] = useState<TabId>('raw-singbox')
    const [loading, setLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [lastBackup, setLastBackup] = useState<{ singbox?: string; wireguard?: string }>({})
    const [singboxConfig, setSingboxConfig] = useState('')
    const [wireguardConfig, setWireguardConfig] = useState('')
    const [originalSingboxConfig, setOriginalSingboxConfig] = useState('')
    const [originalWireguardConfig, setOriginalWireguardConfig] = useState('')
    const [wireguardInterfaces, setWireguardInterfaces] = useState<string[]>([])
    const [activeWireguardInterface, setActiveWireguardInterface] = useState('')

    const resolveActiveWireguardInterface = (available: string[], preferred: string) => {
        if (preferred && available.includes(preferred)) {
            return preferred
        }
        return available[0] || ''
    }

    const normalizeJson = (value: string) => {
        try {
            return JSON.stringify(JSON.parse(value), null, 2)
        } catch {
            return value
        }
    }

    const loadBackupMeta = async () => {
        try {
            const meta = await api.getBackupMeta()
            setLastBackup({
                singbox: meta.singbox_last_backup,
                wireguard: meta.wireguard_last_backup,
            })
        } catch (err) {
            console.error('Failed to load backup metadata', err)
        }
    }

    const loadCurrentConfig = async () => {
        if (!canReadConfig) return
        setLoading(true)
        try {
            if (activeTab === 'raw-singbox') {
                const content = await api.getSingboxConfig()
                const normalized = normalizeJson(content)
                setSingboxConfig(normalized)
                setOriginalSingboxConfig(normalized)
            } else if (canReadWireguard) {
                if (!activeWireguardInterface) {
                    setWireguardConfig('')
                    setOriginalWireguardConfig('')
                } else {
                    const content = await api.getWireGuardConfigForInterface(activeWireguardInterface)
                    setWireguardConfig(content)
                    setOriginalWireguardConfig(content)
                }
            }
            await loadBackupMeta()
        } catch (err) {
            console.error('Failed to load config', err)
        } finally {
            setLoading(false)
        }
    }

    const loadAll = async () => {
        if (!canReadConfig) return
        setLoading(true)
        try {
            try {
                const singboxRaw = await api.getSingboxConfig()
                const normalized = normalizeJson(singboxRaw)
                setSingboxConfig(normalized)
                setOriginalSingboxConfig(normalized)
            } catch (err) {
                console.error('Failed to load sing-box config', err)
                // Keep previous state on transient/permission errors instead of blanking editor.
            }

            if (canReadWireguard) {
                try {
                    const ifaces = await api.getWireGuardInterfaces()
                    const normalizedIfaces = Array.isArray(ifaces) ? ifaces : []
                    setWireguardInterfaces(normalizedIfaces)
                    const selectedIface = resolveActiveWireguardInterface(normalizedIfaces, activeWireguardInterface)
                    setActiveWireguardInterface(selectedIface)
                    if (selectedIface) {
                        const wgRaw = await api.getWireGuardConfigForInterface(selectedIface)
                        setWireguardConfig(wgRaw)
                        setOriginalWireguardConfig(wgRaw)
                    } else {
                        setWireguardConfig('')
                        setOriginalWireguardConfig('')
                    }
                } catch (err) {
                    console.error('Failed to load WireGuard config', err)
                    // Keep previous WG state on errors.
                }
            } else {
                setWireguardInterfaces([])
                setActiveWireguardInterface('')
                setWireguardConfig('')
                setOriginalWireguardConfig('')
            }

            await loadBackupMeta()
        } catch (err) {
            console.error('Failed to load configs', err)
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        loadAll()
    }, [canReadConfig, canReadWireguard])

    useEffect(() => {
        if (!canReadWireguard && activeTab === 'raw-wireguard') {
            setActiveTab('raw-singbox')
        }
    }, [canReadWireguard, activeTab])

    useEffect(() => {
        if (activeTab !== 'raw-wireguard' || !canReadWireguard) {
            return
        }
        if (!activeWireguardInterface) {
            setWireguardConfig('')
            setOriginalWireguardConfig('')
            return
        }
        void loadCurrentConfig()
    }, [activeTab, canReadWireguard, activeWireguardInterface])

    const handleSave = async () => {
        setSaving(true)
        try {
            if (activeTab === 'raw-singbox') {
                JSON.parse(singboxConfig)
                await api.updateSingboxConfig(singboxConfig)
                setOriginalSingboxConfig(singboxConfig)
            } else if (canReadWireguard) {
                if (!activeWireguardInterface) {
                    alert('Select a WireGuard interface first')
                    return
                }
                await api.updateWireGuardConfigForInterface(activeWireguardInterface, wireguardConfig)
                setOriginalWireguardConfig(wireguardConfig)
            }
            alert('Configuration saved successfully')
        } catch (err: any) {
            alert(`Failed to save: ${err?.message || err}`)
        } finally {
            setSaving(false)
        }
    }

    const handleBackup = async () => {
        try {
            if (activeTab === 'raw-singbox') {
                await api.backupConfig()
            } else if (canReadWireguard) {
                await api.backupWireGuardConfig()
            }
            await loadBackupMeta()
            alert('Backup created (.bak)')
        } catch (err: any) {
            alert(`Backup failed: ${err?.message || err}`)
        }
    }

    const handleRestore = async () => {
        if (!confirm('Restore from backup? This will overwrite current config.')) return
        try {
            if (activeTab === 'raw-singbox') {
                const restored = await api.restoreConfig()
                const normalized = JSON.stringify(restored, null, 2)
                setSingboxConfig(normalized)
                setOriginalSingboxConfig(normalized)
            } else if (canReadWireguard) {
                const restored = await api.restoreWireGuardConfig()
                setWireguardConfig(restored)
                setOriginalWireguardConfig(restored)
            }
            await loadBackupMeta()
            alert('Restored from backup')
        } catch (err: any) {
            alert(`Restore failed: ${err?.message || err}`)
        }
    }

    const currentValue = activeTab === 'raw-singbox' ? singboxConfig : wireguardConfig
    const currentOriginal = activeTab === 'raw-singbox' ? originalSingboxConfig : originalWireguardConfig
    const currentLastBackup = activeTab === 'raw-singbox' ? lastBackup.singbox : lastBackup.wireguard
    const canWriteCurrent = activeTab === 'raw-singbox' ? canWriteConfig : canWriteWireguard
    const mobileTabWidthClass = canReadWireguard ? 'w-1/2 sm:w-auto' : 'w-full sm:w-auto'

    if (!canReadConfig) {
        return (
            <div className="h-full min-h-0 flex items-center justify-center">
                <div className="text-sm text-slate-400">You do not have permission to read raw config.</div>
            </div>
        )
    }

    return (
        <div className="h-full min-h-0 flex flex-col gap-0 sm:gap-4">

            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm flex-1 min-h-0 flex flex-col">
                <div className="flex border-b border-slate-800 bg-slate-950/50">
                    <button
                        onClick={() => setActiveTab('raw-singbox')}
                        className={`${mobileTabWidthClass} px-6 py-3 text-center sm:text-left text-sm font-medium transition-colors border-b-2 ${activeTab === 'raw-singbox' ? 'border-blue-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                    >
                        sing-box
                    </button>
                    {canReadWireguard && (
                        <button
                            onClick={() => setActiveTab('raw-wireguard')}
                            className={`${mobileTabWidthClass} px-6 py-3 text-center sm:text-left text-sm font-medium transition-colors border-b-2 ${activeTab === 'raw-wireguard' ? 'border-emerald-500 text-white bg-slate-900' : 'border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50'}`}
                        >
                            WireGuard
                        </button>
                    )}
                </div>

                <div className="flex-1 min-h-0 bg-slate-950 overflow-hidden flex flex-col">
                    <RawEditorPanel
                        value={currentValue}
                        originalValue={currentOriginal}
                        onChange={activeTab === 'raw-singbox' ? setSingboxConfig : setWireguardConfig}
                        onRefresh={loadCurrentConfig}
                        onSave={handleSave}
                        onBackup={handleBackup}
                        onRestore={handleRestore}
                        loading={loading}
                        saving={saving}
                        canWrite={canWriteCurrent}
                        lastBackupText={currentLastBackup ? new Date(currentLastBackup).toLocaleString() : ''}
                        language={activeTab === 'raw-singbox' ? 'json' : 'ini'}
                        textareaId={activeTab === 'raw-singbox' ? 'raw-editor-singbox' : 'raw-editor-wireguard'}
                        saveLabel="Save Changes"
                        bottomBarExtraDesktop={activeTab === 'raw-wireguard' && canReadWireguard ? (
                            <div className="flex items-center gap-2">
                                <span className="text-xs font-medium text-slate-400 uppercase tracking-wider whitespace-nowrap">
                                    Interface
                                </span>
                                <select
                                    value={activeWireguardInterface}
                                    onChange={e => setActiveWireguardInterface(e.target.value)}
                                    className="select-field h-[38px] min-w-[120px] max-w-[180px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors font-mono"
                                >
                                    {wireguardInterfaces.length === 0 ? (
                                        <option value="">No interfaces</option>
                                    ) : (
                                        wireguardInterfaces.map(iface => (
                                            <option key={iface} value={iface}>
                                                {iface}
                                            </option>
                                        ))
                                    )}
                                </select>
                            </div>
                        ) : undefined}
                        bottomBarExtraMobile={activeTab === 'raw-wireguard' && canReadWireguard ? (
                            <select
                                value={activeWireguardInterface}
                                onChange={e => setActiveWireguardInterface(e.target.value)}
                                className="select-field w-full h-[38px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors font-mono"
                                aria-label="WireGuard interface"
                            >
                                {wireguardInterfaces.length === 0 ? (
                                    <option value="">No interfaces</option>
                                ) : (
                                    wireguardInterfaces.map(iface => (
                                        <option key={iface} value={iface}>
                                            {iface}
                                        </option>
                                    ))
                                )}
                            </select>
                        ) : undefined}
                    />
                </div>
            </div>
        </div>
    )
}
