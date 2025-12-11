import { useEffect, useState } from 'react'
import { Plus, Edit2, Trash2, Shield, Radio } from 'lucide-react'
import { api } from '../../api'
import { useToast } from '../../context/ToastContext'
import { Button } from '../ui/Button'
import { Badge } from '../ui/Badge'
import InboundModal from './InboundModal'

export default function InboundList() {
    const { success, error: toastError } = useToast()
    const [inbounds, setInbounds] = useState<any[]>([])
    const [loading, setLoading] = useState(false)
    const [isModalOpen, setIsModalOpen] = useState(false)
    const [editingInbound, setEditingInbound] = useState<any>(null)

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

    const handleDelete = async (tag: string) => {
        if (!confirm(`Are you sure you want to delete inbound "${tag}"?`)) return
        try {
            await api.deleteSingboxInbound(tag)
            success('Inbound deleted successfully')
            loadInbounds()
        } catch (err) {
            toastError('Failed to delete inbound: ' + err)
        }
    }

    const handleEdit = (inbound: any) => {
        setEditingInbound(inbound)
        setIsModalOpen(true)
    }

    const handleAdd = () => {
        setEditingInbound(null)
        setIsModalOpen(true)
    }

    const handleSave = async (config: any) => {
        try {
            if (editingInbound) {
                await api.updateSingboxInbound(editingInbound.tag, config)
                success('Inbound updated successfully')
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
                <Button onClick={handleAdd} size="sm" icon={<Plus size={16} />}>
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
                        <div key={idx} className="bg-slate-950 border border-slate-800 rounded-xl p-4 flex flex-col justify-between gap-4 group hover:border-slate-700 transition-colors">
                            <div className="space-y-2">
                                <div className="flex items-center justify-between">
                                    <Badge variant="neutral" className="font-mono text-xs">
                                        {inbound.type?.toUpperCase()}
                                    </Badge>
                                    <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">

                                        <button
                                            onClick={() => handleEdit(inbound)}
                                            className="p-1.5 text-slate-400 hover:text-blue-400 hover:bg-blue-400/10 rounded-lg transition-colors"
                                            title="Edit"
                                        >
                                            <Edit2 size={16} />
                                        </button>
                                        <button
                                            onClick={() => handleDelete(inbound.tag)}
                                            className="p-1.5 text-slate-400 hover:text-red-400 hover:bg-red-400/10 rounded-lg transition-colors"
                                            title="Delete"
                                        >
                                            <Trash2 size={16} />
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
                            <div className="pt-4 border-t border-slate-800/50 flex gap-4 text-xs text-slate-400">
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
            />
        </div>
    )
}
