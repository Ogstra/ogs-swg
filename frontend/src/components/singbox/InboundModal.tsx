import { useState, useEffect } from 'react'
import { Modal } from '../ui/Modal'
import { Button } from '../ui/Button'
import { Save, Plus, Trash2 } from 'lucide-react'
import { v4 as uuidv4 } from 'uuid'

interface InboundModalProps {
    isOpen: boolean
    onClose: () => void
    initialData?: any
    onSave: (data: any) => void
}

const DEFAULT_VLESS = {
    type: 'vless',
    tag: 'vless-in',
    listen: '::',
    listen_port: 443,
    users: [],
    tls: {
        enabled: false,
        server_name: 'example.com',
        certificate_path: '',
        key_path: ''
    },
    transport: {
        type: 'tcp'
    }
}

export default function InboundModal({ isOpen, onClose, initialData, onSave }: InboundModalProps) {
    const [formData, setFormData] = useState<any>(DEFAULT_VLESS)

    useEffect(() => {
        if (initialData) {
            // Deep copy to avoid mutating prop
            setFormData(JSON.parse(JSON.stringify(initialData)))
        } else {
            setFormData(JSON.parse(JSON.stringify(DEFAULT_VLESS)))
        }
    }, [initialData, isOpen])

    const handleSubmit = () => {
        // Basic validation
        if (!formData.tag || !formData.listen_port) {
            alert('Tag and Port are required')
            return
        }

        // Ensure numbers are numbers
        const submission = {
            ...formData,
            listen_port: parseInt(formData.listen_port)
        }

        onSave(submission)
    }

    const addUser = () => {
        setFormData({
            ...formData,
            users: [...(formData.users || []), { name: `user-${(formData.users?.length || 0) + 1}`, uuid: uuidv4(), flow: 'xtls-rprx-vision' }]
        })
    }

    const removeUser = (idx: number) => {
        const newUsers = [...(formData.users || [])]
        newUsers.splice(idx, 1)
        setFormData({ ...formData, users: newUsers })
    }

    const updateUser = (idx: number, field: string, value: string) => {
        const newUsers = [...(formData.users || [])]
        newUsers[idx] = { ...newUsers[idx], [field]: value }
        setFormData({ ...formData, users: newUsers })
    }

    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={initialData ? 'Edit Inbound' : 'Add Inbound'}
            size="lg"
            footer={
                <>
                    <Button variant="secondary" onClick={onClose}>Cancel</Button>
                    <Button onClick={handleSubmit} icon={<Save size={16} />}>Save Inbound</Button>
                </>
            }
        >
            <div className="space-y-6">
                {/* Basic Info */}
                <div className="space-y-4">
                    <h3 className="text-sm font-medium text-slate-400 uppercase tracking-wider">Basic Settings</h3>
                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Tag (ID)</label>
                            <input
                                type="text"
                                value={formData.tag}
                                onChange={e => setFormData({ ...formData, tag: e.target.value })}
                                disabled={!!initialData} // Tag is ID, cannot change on edit generally unless we handle rename logic
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors disabled:opacity-50"
                                placeholder="e.g. vless-in"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Protocol</label>
                            <select
                                value={formData.type}
                                onChange={e => setFormData({ ...formData, type: e.target.value })}
                                disabled // Start with only VLESS support
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors disabled:opacity-50"
                            >
                                <option value="vless">VLESS</option>
                                <option value="vmess">VMess</option>
                                <option value="trojan">Trojan</option>
                            </select>
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Listen Address</label>
                            <input
                                type="text"
                                value={formData.listen}
                                onChange={e => setFormData({ ...formData, listen: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                placeholder="::"
                            />
                        </div>
                        <div className="space-y-1">
                            <label className="text-xs font-medium text-slate-300">Listen Port</label>
                            <input
                                type="number"
                                value={formData.listen_port}
                                onChange={e => setFormData({ ...formData, listen_port: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                            />
                        </div>
                    </div>
                </div>

                <div className="w-full h-px bg-slate-800/50" />

                {/* TLS Settings */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-medium text-slate-400 uppercase tracking-wider">TLS Configuration</h3>
                        <label className="flex items-center gap-2 cursor-pointer">
                            <input
                                type="checkbox"
                                checked={formData.tls?.enabled}
                                onChange={e => setFormData({ ...formData, tls: { ...(formData.tls || {}), enabled: e.target.checked } })}
                                className="w-4 h-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                            />
                            <span className="text-xs font-medium text-white">Enable TLS</span>
                        </label>
                    </div>

                    {formData.tls?.enabled && (
                        <div className="grid grid-cols-1 gap-4 p-4 bg-slate-950/50 rounded-lg border border-slate-800/50">
                            <div className="space-y-1">
                                <label className="text-xs font-medium text-slate-300">Server Name (SNI)</label>
                                <input
                                    type="text"
                                    value={formData.tls?.server_name || ''}
                                    onChange={e => setFormData({ ...formData, tls: { ...formData.tls, server_name: e.target.value } })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                    placeholder="example.com"
                                />
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Certificate Path</label>
                                    <input
                                        type="text"
                                        value={formData.tls?.certificate_path || ''}
                                        onChange={e => setFormData({ ...formData, tls: { ...formData.tls, certificate_path: e.target.value } })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                        placeholder="/path/to/cert.pem"
                                    />
                                </div>
                                <div className="space-y-1">
                                    <label className="text-xs font-medium text-slate-300">Key Path</label>
                                    <input
                                        type="text"
                                        value={formData.tls?.key_path || ''}
                                        onChange={e => setFormData({ ...formData, tls: { ...formData.tls, key_path: e.target.value } })}
                                        className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors"
                                        placeholder="/path/to/key.pem"
                                    />
                                </div>
                            </div>
                        </div>
                    )}
                </div>

                <div className="w-full h-px bg-slate-800/50" />

                {/* Users */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        <h3 className="text-sm font-medium text-slate-400 uppercase tracking-wider">Users</h3>
                        <Button size="sm" onClick={addUser} variant="secondary" icon={<Plus size={14} />}>Add User</Button>
                    </div>

                    <div className="space-y-3">
                        {(!formData.users || formData.users.length === 0) && (
                            <p className="text-slate-500 text-xs italic text-center py-4">No users configured</p>
                        )}
                        {formData.users?.map((user: any, idx: number) => (
                            <div key={idx} className="flex gap-2 items-start p-3 bg-slate-950 border border-slate-800 rounded-lg group">
                                <div className="grid grid-cols-3 gap-2 flex-1">
                                    <div className="space-y-1">
                                        <label className="text-[10px] font-medium text-slate-500 uppercase">Name</label>
                                        <input
                                            type="text"
                                            value={user.name || ''}
                                            onChange={e => updateUser(idx, 'name', e.target.value)}
                                            className="w-full bg-slate-900 border border-slate-800 rounded px-2 py-1.5 text-xs text-white focus:outline-none focus:border-blue-500 transition-colors"
                                            placeholder="User Name"
                                        />
                                    </div>
                                    <div className="space-y-1 col-span-2">
                                        <label className="text-[10px] font-medium text-slate-500 uppercase">UUID</label>
                                        <div className="flex gap-1">
                                            <input
                                                type="text"
                                                value={user.uuid || ''}
                                                onChange={e => updateUser(idx, 'uuid', e.target.value)}
                                                className="w-full bg-slate-900 border border-slate-800 rounded px-2 py-1.5 text-xs text-white  font-mono focus:outline-none focus:border-blue-500 transition-colors"
                                            />
                                            <button
                                                onClick={() => updateUser(idx, 'uuid', uuidv4())}
                                                className="px-2 py-1 bg-slate-800 rounded text-slate-400 hover:text-white"
                                                title="Generate New UUID"
                                            >
                                                ↻
                                            </button>
                                        </div>
                                    </div>
                                    <div className="space-y-1 col-span-3">
                                        <label className="text-[10px] font-medium text-slate-500 uppercase">Flow</label>
                                        <input
                                            type="text"
                                            value={user.flow || ''}
                                            onChange={e => updateUser(idx, 'flow', e.target.value)}
                                            className="w-full bg-slate-900 border border-slate-800 rounded px-2 py-1.5 text-xs text-white font-mono focus:outline-none focus:border-blue-500 transition-colors"
                                            placeholder="xtls-rprx-vision or empty"
                                        />
                                    </div>
                                </div>
                                <button
                                    onClick={() => removeUser(idx)}
                                    className="p-1.5 mt-5 text-slate-500 hover:text-red-400 hover:bg-red-400/10 rounded-lg transition-colors"
                                >
                                    <Trash2 size={16} />
                                </button>
                            </div>
                        ))}
                    </div>
                </div>
            </div>
        </Modal>
    )
}
