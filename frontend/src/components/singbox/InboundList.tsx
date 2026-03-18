import { useEffect, useState } from 'react'
import { Plus, Edit, Trash2, Shield, Radio } from 'lucide-react'
import { api } from '../../services/api'
import { useToast } from '../../context/ToastContext'
import { Button } from '../ui/Button'
import { Badge } from '../ui/Badge'
import InboundModal from './InboundModal'
import { useAuth } from '../../context/AuthContext'
import { ConfirmModal } from '../ui/ConfirmModal'

export default function InboundList() {
    const { success, error: toastError, warning } = useToast()
    const { permissions } = useAuth()
    const canWriteConfig = !!permissions?.can_write_config
    const [inbounds, setInbounds] = useState<any[]>([])
    const [loading, setLoading] = useState(false)
    const [isModalOpen, setIsModalOpen] = useState(false)
    const [editingInbound, setEditingInbound] = useState<any>(null)
    const [confirmDeleteTag, setConfirmDeleteTag] = useState<string | null>(null)
    const [deleteLoading, setDeleteLoading] = useState(false)

    useEffect(() => {
        loadInbounds()
    }, [])

    const loadInbounds = async () => {
        setLoading(true)
        try {
            const data = await api.getSingboxInbounds()
            setInbounds(data || [])
        } catch (err) {
            console.error(err)
            toastError('Failed to load inbounds')
        } finally {
            setLoading(false)
        }
    }

    const handleDelete = async () => {
        const tag = confirmDeleteTag
        if (!tag) return
        if (!canWriteConfig) return
        setDeleteLoading(true)
        try {
            await api.deleteSingboxInbound(tag)
            success('Inbound deleted successfully')
            setConfirmDeleteTag(null)
            await loadInbounds()
        } catch (err) {
            toastError('Failed to delete inbound: ' + err)
        } finally {
            setDeleteLoading(false)
        }
    }

    const handleEdit = (inbound: any) => {
        if (!canWriteConfig) return
        setEditingInbound(inbound)
        setIsModalOpen(true)
    }

    const handleAdd = () => {
        if (!canWriteConfig) return
        setEditingInbound(null)
        setIsModalOpen(true)
    }

    const handleSave = async (config: any) => {
        if (!canWriteConfig) return
        try {
            if (editingInbound) {
                const res = await api.updateSingboxInbound(editingInbound.tag, config)
                success('Inbound updated successfully')
                for (const warningMessage of res.warnings || []) {
                    warning(warningMessage, 0)
                }
            } else {
                await api.addSingboxInbound(config)
                success('Inbound created successfully')
            }
            setIsModalOpen(false)
            loadInbounds()
        } catch (err) {
            toastError('Failed to save inbound: ' + err)
        }
    }

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <h3 className="text-lg font-semibold text-white">Configured Inbounds</h3>
                <Button onClick={handleAdd} size="sm" icon={<Plus size={16} />} disabled={!canWriteConfig}>
                    Add Inbound
                </Button>
            </div>

            {loading ? (
                <div className="text-slate-400 text-sm animate-pulse">Loading inbounds...</div>
            ) : inbounds.length === 0 ? (
                <div className="p-8 border border-dashed border-slate-800 rounded-xl text-center text-slate-500">
                    No inbounds configured. Click "Add Inbound" to create one.
                </div>
            ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {inbounds.map((inbound, idx) => (
                        <div key={idx} className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex flex-col justify-between gap-4 group shadow-sm hover:border-slate-700 hover:shadow-md transition-all">
                            <div className="space-y-2">
                                <div className="flex items-center justify-between">
                                    <Badge variant="neutral" className="font-mono text-[10px] uppercase tracking-wider px-2 py-1 bg-slate-800 text-slate-200">
                                        {inbound.type?.toUpperCase()}
                                    </Badge>
                                    <div className="flex gap-2">

                                        <button
                                            onClick={() => handleEdit(inbound)}
                                            disabled={!canWriteConfig}
                                            className="w-10 h-10 flex items-center justify-center text-slate-300 hover:text-white rounded-xl border border-slate-700 bg-slate-800 hover:bg-slate-700 shadow-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-slate-800 disabled:hover:text-slate-300"
                                            title="Edit"
                                        >
                                            <Edit size={17} strokeWidth={1.6} />
                                        </button>
                                        <button
                                            onClick={() => setConfirmDeleteTag(inbound.tag)}
                                            disabled={!canWriteConfig}
                                            className="w-10 h-10 flex items-center justify-center text-slate-300 hover:text-white rounded-xl border border-slate-700 bg-slate-800 hover:bg-slate-700 shadow-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-slate-800 disabled:hover:text-slate-300"
                                            title="Delete"
                                        >
                                            <Trash2 size={17} strokeWidth={1.6} />
                                        </button>
                                    </div>
                                </div>
                                <div>
                                    <div className="text-white font-semibold truncate" title={inbound.tag}>
                                        {inbound.tag}
                                    </div>
                                    <div className="text-slate-500 text-xs mt-1 font-mono">
                                        {inbound.listen || '::'}:{inbound.listen_port}
                                    </div>
                                </div>
                            </div>

                            {/* Quick Stats / Info */}
                            <div className="pt-4 border-t border-slate-800/50 flex gap-4 text-xs text-slate-400 items-center">
                                <div className="flex items-center gap-1.5">
                                    <Shield size={12} className={inbound.tls?.enabled ? 'text-emerald-400' : 'text-slate-600'} />
                                    <span>TLS {inbound.tls?.enabled ? 'On' : 'Off'}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <Radio size={12} className={inbound.transport?.type ? 'text-blue-400' : 'text-slate-600'} />
                                    <span>{inbound.transport?.type || 'tcp'}</span>
                                </div>
                            </div>
                        </div>
                    ))}
                </div>
            )}

            <InboundModal
                isOpen={isModalOpen}
                onClose={() => setIsModalOpen(false)}
                initialData={editingInbound}
                onSave={handleSave}
                canWrite={canWriteConfig}
            />

            <ConfirmModal
                isOpen={!!confirmDeleteTag}
                onClose={() => !deleteLoading && setConfirmDeleteTag(null)}
                onConfirm={handleDelete}
                title="Delete inbound?"
                message="This removes the inbound from the Sing-box configuration."
                confirmLabel="Delete"
                confirmTone="danger"
                isLoading={deleteLoading}
            >
                {confirmDeleteTag && (
                    <p className="text-sm text-slate-400">{confirmDeleteTag}</p>
                )}
            </ConfirmModal>
        </div>
    )
}
