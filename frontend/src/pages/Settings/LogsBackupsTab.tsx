import { useEffect, useState } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { Save, Download, HardDrive } from 'lucide-react'
import { Card } from '../../components/ui/Card'
import { Button } from '../../components/ui/Button'
import { api, type FeatureFlags, type LogStoreStats, downloadDBBackup } from '../../services/api'

interface LogsBackupsTabProps {
    features: FeatureFlags
    setFeatures: Dispatch<SetStateAction<FeatureFlags>>
    canWriteSettings: boolean
    success: (msg: string) => void
    toastError: (msg: string) => void
}

function formatBytes(bytes: number): string {
    if (bytes <= 0) return '0 B'
    const mb = bytes / (1024 * 1024)
    if (mb >= 1) return `${mb.toFixed(2)} MB`
    const kb = bytes / 1024
    if (kb >= 1) return `${kb.toFixed(1)} KB`
    return `${bytes} B`
}

function formatTs(ts: number): string {
    if (!ts) return '—'
    return new Date(ts).toLocaleString()
}

export default function LogsBackupsTab({
    features,
    setFeatures,
    canWriteSettings,
    success,
    toastError,
}: LogsBackupsTabProps) {
    const [retentionSaving, setRetentionSaving] = useState(false)
    const [backupSaving, setBackupSaving] = useState(false)
    const [backupTriggering, setBackupTriggering] = useState(false)
    const [stats, setStats] = useState<LogStoreStats | null>(null)
    const [statsError, setStatsError] = useState(false)

    useEffect(() => {
        let cancelled = false

        const fetchStats = async () => {
            try {
                const data = await api.getLogStoreStats()
                if (!cancelled) {
                    setStats(data)
                    setStatsError(false)
                }
            } catch {
                if (!cancelled) setStatsError(true)
            }
        }

        void fetchStats()
        const interval = setInterval(() => void fetchStats(), 10_000)
        return () => {
            cancelled = true
            clearInterval(interval)
        }
    }, [])

    const handleSaveRetention = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        setRetentionSaving(true)
        try {
            await api.updateFeatures(features)
            success('Log retention settings saved')
        } catch (err) {
            toastError('Failed to save retention settings: ' + err)
        } finally {
            setRetentionSaving(false)
        }
    }

    const handleSaveBackup = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        setBackupSaving(true)
        try {
            await api.updateFeatures(features)
            success('Backup settings saved')
        } catch (err) {
            toastError('Failed to save backup settings: ' + err)
        } finally {
            setBackupSaving(false)
        }
    }

    const handleBackupNow = async () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        setBackupTriggering(true)
        try {
            const result = await api.triggerDBBackup()
            const created = result.created ?? []
            if (created.length > 0) {
                success(`Backup complete: ${created.join(', ')}`)
            } else {
                success('Backup triggered (no files listed)')
            }
        } catch (err) {
            toastError('Backup failed: ' + err)
        } finally {
            setBackupTriggering(false)
        }
    }

    const mode = features.log_retention_mode ?? 'size'

    return (
        <div className="space-y-4 sm:space-y-6 pb-4 sm:pb-0">
            {/* Log Retention */}
            <Card
                title="Log Retention"
                action={
                    <Button
                        onClick={handleSaveRetention}
                        size="sm"
                        icon={<Save size={16} />}
                        disabled={!canWriteSettings}
                        isLoading={retentionSaving}
                    >
                        Save
                    </Button>
                }
            >
                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">Retention mode</label>
                        <div className="flex gap-3">
                            <label className="flex items-center gap-2 cursor-pointer">
                                <input
                                    type="radio"
                                    name="retention_mode"
                                    value="size"
                                    checked={mode === 'size'}
                                    onChange={() => setFeatures(prev => ({ ...prev, log_retention_mode: 'size' }))}
                                    disabled={!canWriteSettings}
                                    className="h-4 w-4 border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <span className="text-sm text-slate-300">Size-based</span>
                            </label>
                            <label className="flex items-center gap-2 cursor-pointer">
                                <input
                                    type="radio"
                                    name="retention_mode"
                                    value="time"
                                    checked={mode === 'time'}
                                    onChange={() => setFeatures(prev => ({ ...prev, log_retention_mode: 'time' }))}
                                    disabled={!canWriteSettings}
                                    className="h-4 w-4 border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <span className="text-sm text-slate-300">Time-based</span>
                            </label>
                        </div>
                    </div>

                    {mode === 'size' && (
                        <div>
                            <label className="block text-sm font-medium text-slate-300 mb-2">Max size (MB)</label>
                            <input
                                type="number"
                                min={10}
                                value={features.log_retention_mb ?? 200}
                                onChange={e => setFeatures(prev => ({ ...prev, log_retention_mb: Math.max(10, Number(e.target.value) || 200) }))}
                                disabled={!canWriteSettings}
                                className="w-40 bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                            />
                        </div>
                    )}

                    {mode === 'time' && (
                        <div className="flex items-end gap-3">
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-2">Keep last N</label>
                                <input
                                    type="number"
                                    min={1}
                                    value={features.log_retention_days ?? 30}
                                    onChange={e => setFeatures(prev => ({ ...prev, log_retention_days: Math.max(1, Number(e.target.value) || 30) }))}
                                    disabled={!canWriteSettings}
                                    className="w-28 bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-2">Unit</label>
                                <select
                                    value={features.log_retention_unit ?? 'days'}
                                    onChange={e => setFeatures(prev => ({ ...prev, log_retention_unit: e.target.value as 'days' | 'weeks' | 'months' }))}
                                    disabled={!canWriteSettings}
                                    className="select-field bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                                >
                                    <option value="days">Days</option>
                                    <option value="weeks">Weeks</option>
                                    <option value="months">Months</option>
                                </select>
                            </div>
                        </div>
                    )}
                </div>
            </Card>

            {/* Cold Storage */}
            <Card title="Cold Storage">
                <div>
                    <label className="block text-sm font-medium text-slate-300 mb-2">Cold segment directory</label>
                    <input
                        type="text"
                        value={features.log_cold_dir ?? 'data/logs'}
                        onChange={e => setFeatures(prev => ({ ...prev, log_cold_dir: e.target.value }))}
                        disabled={!canWriteSettings}
                        placeholder="data/logs"
                        className="w-full max-w-md bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono"
                    />
                    <p className="text-xs text-slate-500 mt-1">Directory where cold log segments are archived.</p>
                    <div className="flex justify-end mt-3 max-w-md">
                        <Button
                            onClick={handleSaveRetention}
                            size="sm"
                            icon={<Save size={16} />}
                            disabled={!canWriteSettings}
                            isLoading={retentionSaving}
                        >
                            Save
                        </Button>
                    </div>
                </div>
            </Card>

            {/* Log Store Stats */}
            <Card title="Log Store Stats">
                {statsError ? (
                    <p className="text-sm text-amber-400">Could not load stats — log store may be unavailable.</p>
                ) : stats === null ? (
                    <p className="text-sm text-slate-500 animate-pulse">Loading stats...</p>
                ) : (
                    <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
                        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3">
                            <div className="text-xs text-slate-400 mb-1">Hot size</div>
                            <div className="text-white font-mono text-sm">{formatBytes(stats.size_bytes)}</div>
                        </div>
                        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3">
                            <div className="text-xs text-slate-400 mb-1">Row count</div>
                            <div className="text-white font-mono text-sm">{stats.row_count.toLocaleString()}</div>
                        </div>
                        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3">
                            <div className="text-xs text-slate-400 mb-1">Oldest entry</div>
                            <div className="text-white font-mono text-sm">{formatTs(stats.oldest_ts)}</div>
                        </div>
                        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3">
                            <div className="text-xs text-slate-400 mb-1">Newest entry</div>
                            <div className="text-white font-mono text-sm">{formatTs(stats.newest_ts)}</div>
                        </div>
                        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3">
                            <div className="text-xs text-slate-400 mb-1">Cold segments</div>
                            <div className="text-white font-mono text-sm">{stats.segment_count}</div>
                        </div>
                        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3">
                            <div className="text-xs text-slate-400 mb-1">Total cold size</div>
                            <div className="text-white font-mono text-sm">{formatBytes(stats.segment_total_bytes)}</div>
                        </div>
                    </div>
                )}
            </Card>

            {/* DB Backups */}
            <Card
                title="DB Backups"
                action={
                    <Button
                        onClick={handleSaveBackup}
                        size="sm"
                        icon={<Save size={16} />}
                        disabled={!canWriteSettings}
                        isLoading={backupSaving}
                    >
                        Save backup settings
                    </Button>
                }
            >
                <div className="space-y-4">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-300 mb-2">Backup directory</label>
                            <input
                                type="text"
                                value={features.db_backup_path ?? ''}
                                onChange={e => setFeatures(prev => ({ ...prev, db_backup_path: e.target.value }))}
                                disabled={!canWriteSettings}
                                placeholder="data/backups"
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-300 mb-2">Backup interval (hours)</label>
                            <input
                                type="number"
                                min={1}
                                value={features.db_backup_interval_hours ?? 24}
                                onChange={e => setFeatures(prev => ({ ...prev, db_backup_interval_hours: Math.max(1, Number(e.target.value) || 24) }))}
                                disabled={!canWriteSettings}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                            />
                        </div>
                    </div>

                    <div>
                        <div className="text-sm font-medium text-slate-300 mb-2">Download backups</div>
                        <div className="flex flex-wrap gap-2">
                            {(['main', 'audit', 'logs'] as const).map(target => (
                                <button
                                    key={target}
                                    onClick={() => downloadDBBackup(target).catch(() => toastError(`Failed to download ${target} backup`))}
                                    className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded-lg hover:bg-slate-700 hover:text-white transition-colors"
                                >
                                    <Download size={14} />
                                    {target.charAt(0).toUpperCase() + target.slice(1)} DB
                                </button>
                            ))}
                        </div>
                    </div>

                    <div className="flex items-center gap-3 pt-2 border-t border-slate-800">
                        <Button
                            onClick={handleBackupNow}
                            variant="secondary"
                            size="sm"
                            icon={<HardDrive size={16} />}
                            isLoading={backupTriggering}
                            disabled={!canWriteSettings}
                        >
                            Backup Now
                        </Button>
                        <span className="text-xs text-slate-500">Runs all three backups immediately and returns filenames.</span>
                    </div>
                </div>
            </Card>
        </div>
    )
}
