import { useState, useEffect } from 'react'
import { Modal } from '../ui/Modal'
import { Button } from '../ui/Button'
import { RefreshCw } from 'lucide-react'
import { v4 as uuidv4 } from 'uuid'

interface InboundUserModalProps {
    isOpen: boolean
    onClose: () => void
    initialData?: { name: string; uuid: string; flow: string } | null
    onSave: (user: { name: string; uuid: string; flow: string }) => void
}

export default function InboundUserModal({ isOpen, onClose, initialData, onSave }: InboundUserModalProps) {
    const [formData, setFormData] = useState({
        name: '',
        uuid: '',
        flow: 'xtls-rprx-vision'
    })

    useEffect(() => {
        if (initialData) {
            setFormData(initialData)
        } else {
            setFormData({
                name: '',
                uuid: uuidv4(),
                flow: 'xtls-rprx-vision'
            })
        }
    }, [initialData, isOpen])

    const handleSubmit = () => {
        if (!formData.name || !formData.uuid) {
            // Simple validation, parent should handle better alerts if needed, or we use toast
            return
        }
        onSave(formData)
        onClose()
    }

    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={initialData ? 'Edit Inbound User' : 'Add Inbound User'}
            footer={
                <>
                    <Button variant="ghost" onClick={onClose}>Cancel</Button>
                    <Button variant="primary" onClick={handleSubmit}>{initialData ? 'Save Changes' : 'Add User'}</Button>
                </>
            }
        >
            <div className="space-y-6">
                <div>
                    <label className="block text-sm font-medium text-slate-400 mb-1">Username</label>
                    <input
                        type="text"
                        value={formData.name}
                        onChange={e => setFormData({ ...formData, name: e.target.value })}
                        className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                        placeholder="e.g. john_doe"
                    />
                </div>

                <div>
                    <label className="block text-sm font-medium text-slate-400 mb-1">UUID</label>
                    <div className="flex gap-2">
                        <input
                            type="text"
                            value={formData.uuid}
                            onChange={e => setFormData({ ...formData, uuid: e.target.value })}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                        />
                        <Button
                            variant="icon"
                            size="icon"
                            className="aspect-square"
                            onClick={() => setFormData({ ...formData, uuid: uuidv4() })}
                            title="Generate New UUID"
                        >
                            <RefreshCw size={16} />
                        </Button>
                    </div>
                </div>

                <div>
                    <label className="block text-sm font-medium text-slate-400 mb-1">Flow</label>
                    <select
                        value={formData.flow}
                        onChange={e => setFormData({ ...formData, flow: e.target.value })}
                        className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                    >
                        <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                        <option value="">none</option>
                    </select>
                </div>
            </div>
        </Modal>
    )
}
