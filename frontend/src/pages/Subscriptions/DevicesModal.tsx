import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type HWIDMigration, type Subscription, type SubscriptionDevice } from '../../services/api'
import { Modal } from '../../components/ui/Modal'
import { Badge } from '../../components/ui/Badge'
import { Button } from '../../components/ui/Button'
import { useToast } from '../../context/ToastContext'
import { formatTimeAgo } from '../../utils/traffic'

interface DevicesModalProps {
    sub: Subscription | null
    subs: Subscription[]
    canWrite: boolean
    onClose: () => void
}

const liveStatuses = new Set<HWIDMigration['status']>(['pending', 'delivered'])

function migrationBadge(migration: HWIDMigration, targetName: string) {
    switch (migration.status) {
        case 'pending':
            return <Badge variant="warning">Waiting for the next request</Badge>
        case 'delivered':
            return <Badge variant="warning">Sent, waiting for the device on {targetName}</Badge>
        case 'confirmed':
            return <Badge variant="success">Moved to {targetName}</Badge>
        case 'failed':
            return <Badge variant="error">Gave up after {migration.attempts} tries</Badge>
        default:
            return null
    }
}

export function DevicesModal({ sub, subs, canWrite, onClose }: DevicesModalProps) {
    const { success, error: toastError } = useToast()
    const queryClient = useQueryClient()
    const [targets, setTargets] = useState<Record<string, string>>({})
    const [busy, setBusy] = useState<string | null>(null)
    const [confirmAll, setConfirmAll] = useState(false)

    const devicesQuery = useQuery({
        queryKey: ['subscription-devices', sub?.id],
        queryFn: () => api.getSubscriptionDevices(sub!.id),
        enabled: !!sub,
        refetchInterval: 15000,
    })

    if (!sub) return null
    const others = subs.filter(candidate => candidate.id !== sub.id)
    const nameOf = (id: number) => subs.find(candidate => candidate.id === id)?.name || `#${id}`
    const devices = devicesQuery.data || []
    const movedHashes = devices.filter(device => device.migration?.status === 'confirmed').map(device => device.hwid_hash)

    const refresh = () => queryClient.invalidateQueries({ queryKey: ['subscription-devices', sub.id] })

    const move = async (device: SubscriptionDevice) => {
        const target = Number(targets[device.hwid_hash])
        if (!target) return
        setBusy(device.hwid_hash)
        try {
            await api.createHWIDMigration(sub.id, { hwid_hash: device.hwid_hash, target_sub_id: target })
            success(`Move to ${nameOf(target)} armed: it applies on the device's next request`)
            await refresh()
        } catch (err) {
            toastError('Failed to move device: ' + err)
        } finally {
            setBusy(null)
        }
    }

    const cancel = async (device: SubscriptionDevice) => {
        setBusy(device.hwid_hash)
        try {
            await api.cancelHWIDMigration(sub.id, device.hwid_hash)
            await refresh()
        } catch (err) {
            toastError('Failed to cancel move: ' + err)
        } finally {
            setBusy(null)
        }
    }

    const removeMoved = async (hashes: string[]) => {
        setBusy('remove')
        try {
            await Promise.all(hashes.map(hash => api.removeSubscriptionDevice(sub.id, hash)))
            await refresh()
        } catch (err) {
            toastError('Failed to remove device: ' + err)
            await refresh()
        } finally {
            setBusy(null)
        }
    }

    const cancelAll = async () => {
        try {
            const result = await api.cancelAllHWIDMigrations()
            success(`Cancelled ${result.cancelled} pending move${result.cancelled === 1 ? '' : 's'}`)
            setConfirmAll(false)
            await refresh()
        } catch (err) {
            toastError('Failed to cancel moves: ' + err)
        }
    }

    return (
        <Modal
            isOpen
            onClose={onClose}
            title={`Devices · ${sub.name}`}
            size="lg"
            footer={
                <div className="flex w-full items-center justify-between gap-3">
                    {confirmAll ? (
                        <div className="flex items-center gap-2 text-sm text-slate-300">
                            <span>Cancel every pending move, on all subscriptions?</span>
                            <Button variant="danger" size="sm" onClick={cancelAll} disabled={!canWrite}>Cancel all</Button>
                            <Button variant="ghost" size="sm" onClick={() => setConfirmAll(false)}>Keep</Button>
                        </div>
                    ) : (
                        <div className="flex items-center gap-1">
                            <Button variant="ghost" size="sm" onClick={() => setConfirmAll(true)} disabled={!canWrite}>Cancel all pending moves</Button>
                            {movedHashes.length > 0 && (
                                <Button variant="ghost" size="sm" onClick={() => removeMoved(movedHashes)} disabled={!canWrite || busy === 'remove'}>
                                    Remove moved ({movedHashes.length})
                                </Button>
                            )}
                        </div>
                    )}
                    <Button variant="secondary" onClick={onClose}>Close</Button>
                </div>
            }
        >
            {devicesQuery.isPending ? (
                <p className="py-8 text-center text-sm text-slate-400">Loading…</p>
            ) : devices.length === 0 ? (
                <p className="py-8 text-center text-sm text-slate-400">
                    No devices yet. A device shows up here after a Happ client requests this subscription.
                </p>
            ) : (
                <div className="-mx-2 divide-y divide-slate-800">
                    {devices.map(device => {
                        const migration = device.migration
                        const live = !!migration && liveStatuses.has(migration.status)
                        const moved = migration?.status === 'confirmed'
        const failed = migration?.status === 'failed'
                        const title = [device.device_model, device.device_os].filter(Boolean).join(' · ') || 'Unknown device'
                        return (
                            <div key={device.hwid_hash} className="space-y-2 px-2 py-3">
                                <div className="flex items-baseline justify-between gap-3">
                                    <span className="truncate font-medium text-slate-100" title={title}>{title}</span>
                                    <span className="shrink-0 text-xs text-slate-400">{device.last_seen_at ? formatTimeAgo(device.last_seen_at) : 'Never seen'}</span>
                                </div>
                                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs text-slate-400">
                                    {device.hwid_prefix && <span>{device.hwid_prefix}</span>}
                                    {device.country && <span>{device.country}</span>}
                                </div>
                                <div className="flex flex-wrap items-center gap-2">
                                    {migration && migrationBadge(migration, nameOf(migration.target_sub_id))}
                                    {moved ? (
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            className="ml-auto"
                                            disabled={!canWrite || busy === 'remove'}
                                            onClick={() => removeMoved([device.hwid_hash])}
                                        >
                                            Remove
                                        </Button>
                                    ) : live ? (
                                        <Button variant="ghost" size="sm" disabled={!canWrite || busy === device.hwid_hash} onClick={() => cancel(device)}>
                                            Cancel move
                                        </Button>
                                    ) : (
                                        <div className="ml-auto flex items-center gap-2">
                                            <select
                                                value={targets[device.hwid_hash] || ''}
                                                onChange={event => setTargets(prev => ({ ...prev, [device.hwid_hash]: event.target.value }))}
                                                className="select-field rounded-md border border-slate-700 bg-slate-950 px-2 py-1.5 text-xs text-slate-200 outline-none"
                                                aria-label="Move to subscription"
                                                disabled={!canWrite || others.length === 0}
                                            >
                                                <option value="">Move to…</option>
                                                {others.map(candidate => (
                                                    <option key={candidate.id} value={String(candidate.id)}>{candidate.name}</option>
                                                ))}
                                            </select>
                                            <Button
                                                variant="primary"
                                                size="sm"
                                                disabled={!canWrite || !targets[device.hwid_hash] || busy === device.hwid_hash}
                                                onClick={() => move(device)}
                                            >
                                                {failed ? 'Retry' : 'Move'}
                                            </Button>
                                        </div>
                                    )}
                                </div>
                            </div>
                        )
                    })}
                </div>
            )}
        </Modal>
    )
}
