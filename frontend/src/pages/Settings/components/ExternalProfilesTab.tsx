import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ExternalProfile } from '../../../services/api'
import { useToast } from '../../../context/ToastContext'
import { Button } from '../../../components/ui/Button'
import { Badge } from '../../../components/ui/Badge'
import { Modal } from '../../../components/ui/Modal'
import { Plus, Edit, Trash2 } from 'lucide-react'

const SS_METHODS = [
    '2022-blake3-aes-128-gcm',
    '2022-blake3-aes-256-gcm',
    '2022-blake3-chacha20-poly1305',
    'chacha20-ietf-poly1305',
    'aes-256-gcm',
] as const

type SSMethod = typeof SS_METHODS[number]

type FormData = {
    name: string
    type: 'vless' | 'shadowsocks'
    host_ipv4: string
    host_ipv6_file: string
    port: string
    uuid: string
    public_key: string
    short_id: string
    server_name: string
    flow: string
    password: string
    ss_method: SSMethod
    ss_server_key: string
    enabled: boolean
    position: string
}

const DEFAULT_FORM: FormData = {
    name: '',
    type: 'vless',
    host_ipv4: '',
    host_ipv6_file: '',
    port: '',
    uuid: '',
    public_key: '',
    short_id: '',
    server_name: '',
    flow: 'xtls-rprx-vision',
    password: '',
    ss_method: '2022-blake3-aes-128-gcm',
    ss_server_key: '',
    enabled: true,
    position: '',
}

function profileToForm(p: ExternalProfile): FormData {
    return {
        name: p.name,
        type: p.type,
        host_ipv4: p.host_ipv4,
        host_ipv6_file: p.host_ipv6_file,
        port: String(p.port),
        uuid: p.uuid,
        public_key: p.public_key,
        short_id: p.short_id,
        server_name: p.server_name,
        flow: p.flow,
        password: p.password,
        ss_method: (p.ss_method || '2022-blake3-aes-128-gcm') as SSMethod,
        ss_server_key: p.ss_server_key,
        enabled: p.enabled,
        position: p.position !== undefined ? String(p.position) : '',
    }
}

export default function ExternalProfilesTab() {
    const { success, error: toastError } = useToast()
    const queryClient = useQueryClient()
    const [modalOpen, setModalOpen] = useState(false)
    const [editingId, setEditingId] = useState<number | null>(null)
    const [form, setForm] = useState<FormData>(DEFAULT_FORM)
    const [saving, setSaving] = useState(false)

    const { data: profiles = [], isLoading } = useQuery({
        queryKey: ['external-profiles'],
        queryFn: api.getExternalProfiles,
    })

    const invalidate = () => {
        queryClient.invalidateQueries({ queryKey: ['external-profiles'] })
        queryClient.invalidateQueries({ queryKey: ['users'] })
    }

    const openCreate = () => {
        setEditingId(null)
        setForm(DEFAULT_FORM)
        setModalOpen(true)
    }

    const openEdit = (profile: ExternalProfile) => {
        setEditingId(profile.id)
        setForm(profileToForm(profile))
        setModalOpen(true)
    }

    const closeModal = () => {
        setModalOpen(false)
        setEditingId(null)
        setForm(DEFAULT_FORM)
    }

    const handleSave = async () => {
        if (!form.name.trim()) {
            toastError('Profile name is required')
            return
        }
        const port = parseInt(form.port)
        if (!form.port || isNaN(port) || port < 1 || port > 65535) {
            toastError('A valid port (1-65535) is required')
            return
        }
        if (form.type === 'vless' && !form.uuid.trim()) {
            toastError('UUID is required for VLESS profiles')
            return
        }
        if (form.type === 'shadowsocks' && !form.password.trim()) {
            toastError('Password is required for Shadowsocks profiles')
            return
        }
        setSaving(true)
        try {
            const payload: Partial<ExternalProfile> = {
                name: form.name.trim(),
                type: form.type,
                host_ipv4: form.host_ipv4.trim(),
                host_ipv6_file: form.host_ipv6_file.trim(),
                port,
                enabled: form.enabled,
                position: form.position !== '' ? parseInt(form.position) : 0,
                uuid: form.uuid.trim(),
                public_key: form.public_key.trim(),
                short_id: form.short_id.trim(),
                server_name: form.server_name.trim(),
                flow: form.flow.trim(),
                password: form.password.trim(),
                ss_method: form.ss_method,
                ss_server_key: form.ss_server_key.trim(),
            }
            if (editingId !== null) {
                payload.id = editingId
            }
            await api.upsertExternalProfile(payload)
            success(editingId !== null ? 'Profile updated' : 'Profile created')
            invalidate()
            closeModal()
        } catch (err) {
            toastError('Failed to save profile: ' + err)
        } finally {
            setSaving(false)
        }
    }

    const handleDelete = async (profile: ExternalProfile) => {
        if (!window.confirm(`Delete profile "${profile.name}"?`)) return
        try {
            await api.deleteExternalProfile(profile.id)
            success('Profile deleted')
            invalidate()
        } catch (err) {
            toastError('Failed to delete profile: ' + err)
        }
    }

    const field = (label: string, children: React.ReactNode, hint?: string) => (
        <div>
            <label className="block text-sm font-medium text-slate-400 mb-1">{label}</label>
            {children}
            {hint && <p className="mt-1 text-xs text-slate-500">{hint}</p>}
        </div>
    )

    const inputClass = "w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600 text-sm"
    const selectClass = "select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors text-sm"

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-lg font-semibold text-white">External Profiles</h2>
                    <p className="text-sm text-slate-400">VLESS and Shadowsocks homelab profiles with dynamic IPv6 support</p>
                </div>
                <Button variant="primary" icon={<Plus size={16} />} onClick={openCreate}>
                    Add Profile
                </Button>
            </div>

            {isLoading && (
                <div className="text-slate-400 text-sm">Loading profiles...</div>
            )}

            {!isLoading && profiles.length === 0 && (
                <div className="rounded-lg border border-slate-800 bg-slate-950 p-8 text-center text-slate-500">
                    No external profiles configured. Click "Add Profile" to create one.
                </div>
            )}

            <div className="space-y-3">
                {profiles.map(profile => (
                    <div key={profile.id} className="rounded-xl border border-slate-800 bg-slate-900 p-4">
                        <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0 flex-1 space-y-1">
                                <div className="flex flex-wrap items-center gap-2">
                                    <span className="font-semibold text-white">{profile.name}</span>
                                    <Badge variant={profile.type === 'vless' ? 'info' : 'warning'}>
                                        {profile.type === 'vless' ? 'VLESS' : 'SS'}
                                    </Badge>
                                    {!profile.enabled && <Badge variant="neutral">Disabled</Badge>}
                                </div>
                                <div className="text-xs text-slate-400 font-mono">
                                    {profile.host_ipv4 || '[no ipv4]'}
                                    {profile.host_ipv6_file && <span className="ml-2 text-slate-500">IPv6: {profile.host_ipv6_file}</span>}
                                    <span className="ml-2">:{profile.port}</span>
                                </div>
                                {profile.server_name && (
                                    <div className="text-xs text-slate-500">SNI: {profile.server_name}</div>
                                )}
                            </div>
                            <div className="flex items-center gap-2 shrink-0">
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    icon={<Edit size={14} />}
                                    onClick={() => openEdit(profile)}
                                >
                                    Edit
                                </Button>
                                <Button
                                    variant="danger"
                                    size="sm"
                                    icon={<Trash2 size={14} />}
                                    onClick={() => handleDelete(profile)}
                                >
                                    Delete
                                </Button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            <Modal
                isOpen={modalOpen}
                onClose={closeModal}
                title={editingId !== null ? 'Edit Profile' : 'Add External Profile'}
                size="lg"
                footer={
                    <>
                        <Button variant="ghost" onClick={closeModal} disabled={saving}>Cancel</Button>
                        <Button variant="primary" onClick={handleSave} isLoading={saving}>
                            {editingId !== null ? 'Save Changes' : 'Create Profile'}
                        </Button>
                    </>
                }
            >
                <div className="space-y-4 modal-form-uniform">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        {field('Name',
                            <input
                                type="text"
                                value={form.name}
                                onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                                className={inputClass}
                                placeholder="Homelab VLESS"
                            />
                        )}
                        {field('Type',
                            <select
                                value={form.type}
                                onChange={e => setForm(f => ({ ...f, type: e.target.value as 'vless' | 'shadowsocks' }))}
                                className={selectClass}
                            >
                                <option value="vless">VLESS</option>
                                <option value="shadowsocks">Shadowsocks</option>
                            </select>
                        )}
                        {field('IPv4 Host',
                            <input
                                type="text"
                                value={form.host_ipv4}
                                onChange={e => setForm(f => ({ ...f, host_ipv4: e.target.value }))}
                                className={inputClass}
                                placeholder="1.2.3.4"
                            />
                        )}
                        {field('IPv6 File Path (dynamic)',
                            <input
                                type="text"
                                value={form.host_ipv6_file}
                                onChange={e => setForm(f => ({ ...f, host_ipv6_file: e.target.value }))}
                                className={inputClass}
                                placeholder="/etc/homelab-ipv6"
                            />,
                            'Path on VPS containing current IPv6 address'
                        )}
                        {field('Port',
                            <input
                                type="number"
                                value={form.port}
                                onChange={e => setForm(f => ({ ...f, port: e.target.value }))}
                                className={inputClass}
                                placeholder="443"
                                min={1}
                                max={65535}
                            />
                        )}
                        {field('Position',
                            <input
                                type="number"
                                value={form.position}
                                onChange={e => setForm(f => ({ ...f, position: e.target.value }))}
                                className={inputClass}
                                placeholder="0"
                            />,
                            'Display order (lower = first)'
                        )}
                    </div>

                    {form.type === 'vless' && (
                        <div className="space-y-4">
                            <div className="text-xs uppercase tracking-wider text-slate-500 font-semibold border-b border-slate-800 pb-1">VLESS / Reality</div>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                {field('UUID',
                                    <input
                                        type="text"
                                        value={form.uuid}
                                        onChange={e => setForm(f => ({ ...f, uuid: e.target.value }))}
                                        className={inputClass}
                                        placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
                                    />
                                )}
                                {field('Public Key (Reality)',
                                    <input
                                        type="text"
                                        value={form.public_key}
                                        onChange={e => setForm(f => ({ ...f, public_key: e.target.value }))}
                                        className={inputClass}
                                        placeholder="43-char base64 public key"
                                    />
                                )}
                                {field('Short ID',
                                    <input
                                        type="text"
                                        value={form.short_id}
                                        onChange={e => setForm(f => ({ ...f, short_id: e.target.value }))}
                                        className={inputClass}
                                        placeholder="abc123"
                                    />
                                )}
                                {field('SNI (server_name)',
                                    <input
                                        type="text"
                                        value={form.server_name}
                                        onChange={e => setForm(f => ({ ...f, server_name: e.target.value }))}
                                        className={inputClass}
                                        placeholder="example.com"
                                    />
                                )}
                                {field('Flow',
                                    <input
                                        type="text"
                                        value={form.flow}
                                        onChange={e => setForm(f => ({ ...f, flow: e.target.value }))}
                                        className={inputClass}
                                        placeholder="xtls-rprx-vision"
                                    />,
                                    'Leave empty to omit'
                                )}
                            </div>
                        </div>
                    )}

                    {form.type === 'shadowsocks' && (
                        <div className="space-y-4">
                            <div className="text-xs uppercase tracking-wider text-slate-500 font-semibold border-b border-slate-800 pb-1">Shadowsocks</div>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                {field('Password',
                                    <input
                                        type="text"
                                        value={form.password}
                                        onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                                        className={inputClass}
                                        placeholder="Password or base64 key"
                                    />
                                )}
                                {field('Method',
                                    <select
                                        value={form.ss_method}
                                        onChange={e => setForm(f => ({ ...f, ss_method: e.target.value as SSMethod }))}
                                        className={selectClass}
                                    >
                                        {SS_METHODS.map(m => <option key={m} value={m}>{m}</option>)}
                                    </select>
                                )}
                                {field('Server Key (SS 2022)',
                                    <input
                                        type="text"
                                        value={form.ss_server_key}
                                        onChange={e => setForm(f => ({ ...f, ss_server_key: e.target.value }))}
                                        className={inputClass}
                                        placeholder="Optional: base64 server key"
                                    />,
                                    'Required for 2022 methods'
                                )}
                            </div>
                        </div>
                    )}

                    <div className="flex items-center gap-3">
                        <input
                            type="checkbox"
                            id="ep-enabled"
                            checked={form.enabled}
                            onChange={e => setForm(f => ({ ...f, enabled: e.target.checked }))}
                            className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                        />
                        <label htmlFor="ep-enabled" className="text-sm text-slate-300 cursor-pointer">Enabled</label>
                    </div>
                </div>
            </Modal>
        </div>
    )
}
