import { useEffect, useState } from 'react'
import { api } from '../../services/api'
import { useAuth } from '../../context/AuthContext'
import { useToast } from '../../context/ToastContext'
import { RawEditorPanel } from '../../components/raw/RawEditorPanel'
import { ConfirmModal } from '../../components/ui/ConfirmModal'

type TabId = 'raw-singbox' | 'raw-wireguard'

export default function RawConfig() {
    const { permissions } = useAuth()
    const { success, error: toastError } = useToast()
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
    const [availableBackups, setAvailableBackups] = useState<Array<{ name: string; created_at: string }>>([])
    const [selectedBackupName, setSelectedBackupName] = useState('')
    const [pendingBackupName, setPendingBackupName] = useState('')

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

    const loadAvailableBackups = async (nextTab: TabId = activeTab, nextWireGuardInterface: string = activeWireguardInterface) => {
        try {
            if (nextTab === 'raw-singbox') {
                setAvailableBackups(await api.getConfigBackups())
                return
            }

            if (!canReadWireguard || !nextWireGuardInterface) {
                setAvailableBackups([])
                return
            }

            setAvailableBackups(await api.getWireGuardConfigBackupsForInterface(nextWireGuardInterface))
        } catch (err) {
            console.error('Failed to load backups', err)
            setAvailableBackups([])
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
            await loadAvailableBackups()
            setSelectedBackupName('')
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
            await loadAvailableBackups(activeTab, activeWireguardInterface || resolveActiveWireguardInterface(wireguardInterfaces, activeWireguardInterface))
            setSelectedBackupName('')
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

    useEffect(() => {
        if (activeTab === 'raw-singbox') {
            void loadAvailableBackups('raw-singbox')
            setSelectedBackupName('')
            return
        }

        if (canReadWireguard && activeWireguardInterface) {
            void loadAvailableBackups('raw-wireguard', activeWireguardInterface)
        } else {
            setAvailableBackups([])
        }
        setSelectedBackupName('')
    }, [activeTab, canReadWireguard, activeWireguardInterface])

    const handleSave = async () => {
        setSaving(true)
        try {
            if (activeTab === 'raw-singbox') {
                await api.updateSingboxConfig(singboxConfig)
                setOriginalSingboxConfig(singboxConfig)
            } else if (canReadWireguard) {
                if (!activeWireguardInterface) {
                    toastError('Select a WireGuard interface first')
                    return
                }
                await api.updateWireGuardConfigForInterface(activeWireguardInterface, wireguardConfig)
                setOriginalWireguardConfig(wireguardConfig)
            }
            success('Configuration saved successfully')
        } catch (err: any) {
            toastError(`Failed to save: ${err?.message || err}`)
        } finally {
            setSaving(false)
        }
    }

    const handleBackup = async () => {
        try {
            if (activeTab === 'raw-singbox') {
                await api.backupConfig()
            } else if (canReadWireguard) {
                if (!activeWireguardInterface) {
                    toastError('Select a WireGuard interface first')
                    return
                }
                await api.backupWireGuardConfigForInterface(activeWireguardInterface)
            }
            await loadBackupMeta()
            await loadAvailableBackups()
            setSelectedBackupName('')
            success('Backup created')
        } catch (err: any) {
            toastError(`Backup failed: ${err?.message || err}`)
        }
    }

    const handleRestore = async () => {
        try {
            await loadCurrentConfig()
            success('Restored live config in editor')
        } catch (err: any) {
            toastError(`Restore failed: ${err?.message || err}`)
        }
    }

    const loadBackupIntoEditor = async (backupName: string) => {
        try {
            if (activeTab === 'raw-singbox') {
                const content = await api.getConfigBackupContent(backupName)
                setSingboxConfig(normalizeJson(content))
            } else if (canReadWireguard) {
                if (!activeWireguardInterface) {
                    toastError('Select a WireGuard interface first')
                    return
                }
                const content = await api.getWireGuardConfigBackupContentForInterface(activeWireguardInterface, backupName)
                setWireguardConfig(content)
            }
            setSelectedBackupName(backupName)
        } catch (err: any) {
            toastError(`Failed to load backup: ${err?.message || err}`)
            setSelectedBackupName('')
        }
    }

    const handleSelectBackup = async (backupName: string) => {
        if (!backupName) {
            setSelectedBackupName('')
            return
        }
        if (currentValue !== currentOriginal) {
            setPendingBackupName(backupName)
            return
        }
        await loadBackupIntoEditor(backupName)
    }

    const currentValue = activeTab === 'raw-singbox' ? singboxConfig : wireguardConfig
    const currentOriginal = activeTab === 'raw-singbox' ? originalSingboxConfig : originalWireguardConfig
    const currentLastBackup = activeTab === 'raw-singbox' ? lastBackup.singbox : lastBackup.wireguard
    const canWriteCurrent = activeTab === 'raw-singbox' ? canWriteConfig : canWriteWireguard
    const mobileTabWidthClass = canReadWireguard ? 'w-1/2 sm:w-auto' : 'w-full sm:w-auto'
    const liveOptionLabel = availableBackups.length > 0 ? 'Live' : 'Live (no backups yet)'
    const backupSelector = (
        <div className="flex items-center gap-2 min-w-0">
            <span className="hidden sm:inline text-xs font-medium text-slate-400 uppercase tracking-wider whitespace-nowrap">
                Backups
            </span>
            <select
                value={selectedBackupName}
                onChange={e => void handleSelectBackup(e.target.value)}
                className="select-field w-full sm:w-auto h-[38px] min-w-[150px] max-w-[220px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors"
                aria-label="Backup versions"
            >
                <option value="">{liveOptionLabel}</option>
                {availableBackups.map(backup => (
                    <option key={backup.name} value={backup.name}>
                        {new Date(backup.created_at).toLocaleString()}
                    </option>
                ))}
            </select>
        </div>
    )
    const wireGuardInterfaceSelector = activeTab === 'raw-wireguard' && canReadWireguard ? (
        <>
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
        </>
    ) : null

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
                        hideCompareOnMobile={activeTab === 'raw-wireguard'}
                        bottomBarExtraDesktop={
                            <div className="flex items-center gap-3 min-w-0">
                                {wireGuardInterfaceSelector}
                                {backupSelector}
                            </div>
                        }
                        bottomBarExtraMobile={
                            <>
                                {activeTab === 'raw-wireguard' && canReadWireguard && (
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
                                )}
                                <select
                                    value={selectedBackupName}
                                    onChange={e => void handleSelectBackup(e.target.value)}
                                    className="select-field w-full h-[38px] bg-slate-950 border border-slate-700 rounded-lg px-2 text-sm text-slate-200 outline-none focus:border-blue-500 transition-colors"
                                    aria-label="Backup versions"
                                >
                                    <option value="">{liveOptionLabel}</option>
                                    {availableBackups.map(backup => (
                                        <option key={backup.name} value={backup.name}>
                                            {new Date(backup.created_at).toLocaleString()}
                                        </option>
                                    ))}
                                </select>
                            </>
                        }
                    />
                </div>
            </div>
            <ConfirmModal
                isOpen={!!pendingBackupName}
                title="Unsaved Changes"
                message="You have unsaved changes in the editor. Loading a backup will replace them in the editor. Continue?"
                confirmLabel="Load Backup"
                onConfirm={async () => {
                    const nextBackup = pendingBackupName
                    setPendingBackupName('')
                    if (nextBackup) {
                        await loadBackupIntoEditor(nextBackup)
                    }
                }}
                onClose={() => {
                    setPendingBackupName('')
                    setSelectedBackupName('')
                }}
                confirmTone="danger"
            />
        </div>
    )
}
