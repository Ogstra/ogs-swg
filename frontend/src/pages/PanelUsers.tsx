import React, { useEffect, useState } from 'react';
import { api, type PanelUserInfo, type CreatePanelUserRequest } from '../services/api';
import { useToast } from '../context/ToastContext';
import { useAuth, type PanelUserPermissions } from '../context/AuthContext';
import { Plus, Trash2, Save, RefreshCw, Eye, EyeOff } from 'lucide-react';

const PERM_LABELS: { key: keyof PanelUserPermissions; label: string; description: string }[] = [
    { key: 'can_read_users', label: 'Read Sing-box Users', description: 'View users, links and usage reports' },
    { key: 'can_write_users', label: 'Write Sing-box Users', description: 'Create, update and delete Sing-box users' },
    { key: 'can_read_wireguard', label: 'Read WireGuard', description: 'View peers, traffic and interface info' },
    { key: 'can_write_wireguard', label: 'Write WireGuard', description: 'Create, update and delete WireGuard peers/config' },
    { key: 'can_read_config', label: 'Read Config', description: 'View raw config and sing-box config sections' },
    { key: 'can_write_config', label: 'Write Config', description: 'Edit/apply config and execute service actions' },
    { key: 'can_read_settings', label: 'Read Settings', description: 'View settings and sampler history' },
    { key: 'can_write_settings', label: 'Write Settings', description: 'Change settings and run maintenance actions' },
    { key: 'can_read_panel_users', label: 'Read Admin Users', description: 'View panel users and permission sets' },
    { key: 'can_write_panel_users', label: 'Write Admin Users', description: 'Create, edit and delete panel users' },
    { key: 'can_read_logs', label: 'Read Logs', description: 'View and search sing-box logs' },
];

const emptyPerms = (): PanelUserPermissions => ({
    can_read_users: false,
    can_write_users: false,
    can_read_wireguard: false,
    can_write_wireguard: false,
    can_read_config: false,
    can_write_config: false,
    can_read_settings: false,
    can_write_settings: false,
    can_read_panel_users: false,
    can_write_panel_users: false,
    can_read_logs: false,
});

const WRITE_TO_READ: Partial<Record<keyof PanelUserPermissions, keyof PanelUserPermissions>> = {
    can_write_users: 'can_read_users',
    can_write_wireguard: 'can_read_wireguard',
    can_write_config: 'can_read_config',
    can_write_settings: 'can_read_settings',
    can_write_panel_users: 'can_read_panel_users',
};

const READ_TO_WRITE: Partial<Record<keyof PanelUserPermissions, keyof PanelUserPermissions>> = {
    can_read_users: 'can_write_users',
    can_read_wireguard: 'can_write_wireguard',
    can_read_config: 'can_write_config',
    can_read_settings: 'can_write_settings',
    can_read_panel_users: 'can_write_panel_users',
};

const applyPermissionToggle = (current: PanelUserPermissions, key: keyof PanelUserPermissions, nextValue: boolean): PanelUserPermissions => {
    const next = { ...current, [key]: nextValue };
    if (nextValue) {
        const readKey = WRITE_TO_READ[key];
        if (readKey) next[readKey] = true;
    } else {
        const writeKey = READ_TO_WRITE[key];
        if (writeKey) next[writeKey] = false;
    }
    return next;
};

const PanelUsers: React.FC = () => {
    const { success, error } = useToast();
    const { permissions: myPerms } = useAuth();
    const canWritePanelUsers = !!myPerms?.can_write_panel_users;
    const [users, setUsers] = useState<PanelUserInfo[]>([]);
    const [loading, setLoading] = useState(true);
    const [showCreateModal, setShowCreateModal] = useState(false);
    const [editingPerms, setEditingPerms] = useState<Record<string, PanelUserPermissions>>({});
    const [savingPerms, setSavingPerms] = useState<Record<string, boolean>>({});
    const [deletingUser, setDeletingUser] = useState<string | null>(null);

    // Create form state
    const [newUsername, setNewUsername] = useState('');
    const [newPassword, setNewPassword] = useState('');
    const [showPassword, setShowPassword] = useState(false);
    const [newPerms, setNewPerms] = useState<PanelUserPermissions>(emptyPerms());
    const [creating, setCreating] = useState(false);

    const load = async () => {
        setLoading(true);
        try {
            const data = await api.getPanelUsers();
            setUsers(data ?? []);
            // Seed editing state
            const permsMap: Record<string, PanelUserPermissions> = {};
            (data ?? []).forEach(u => { permsMap[u.username] = { ...u.permissions }; });
            setEditingPerms(permsMap);
        } catch {
            error('Failed to load panel users');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => { load(); }, []);

    const handleSavePerms = async (username: string) => {
        if (!canWritePanelUsers) return;
        setSavingPerms(prev => ({ ...prev, [username]: true }));
        try {
            await api.updatePanelUserPermissions(username, editingPerms[username]);
            success('Permissions updated');
            await load();
        } catch (e: any) {
            error(e.message || 'Failed to update permissions');
        } finally {
            setSavingPerms(prev => ({ ...prev, [username]: false }));
        }
    };

    const handleDelete = async (username: string) => {
        if (!canWritePanelUsers) return;
        if (!confirm(`Delete panel user "${username}"? This cannot be undone.`)) return;
        setDeletingUser(username);
        try {
            await api.deletePanelUser(username);
            success('Panel user deleted');
            await load();
        } catch (e: any) {
            error(e.message || 'Failed to delete panel user');
        } finally {
            setDeletingUser(null);
        }
    };

    const handleCreate = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!canWritePanelUsers) return;
        if (!newUsername.trim() || !newPassword) return;
        setCreating(true);
        try {
            const req: CreatePanelUserRequest = {
                username: newUsername.trim(),
                password: newPassword,
                permissions: newPerms,
            };
            await api.createPanelUser(req);
            success('Panel user created');
            setShowCreateModal(false);
            setNewUsername('');
            setNewPassword('');
            setNewPerms(emptyPerms());
            await load();
        } catch (e: any) {
            error(e.message || 'Failed to create panel user');
        } finally {
            setCreating(false);
        }
    };

    const toggleNewPerm = (key: keyof PanelUserPermissions) => {
        setNewPerms(prev => applyPermissionToggle(prev, key, !prev[key]));
    };

    const toggleEditPerm = (username: string, key: keyof PanelUserPermissions) => {
        setEditingPerms(prev => ({
            ...prev,
            [username]: applyPermissionToggle(prev[username] ?? emptyPerms(), key, !(prev[username]?.[key] ?? false)),
        }));
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <RefreshCw size={24} className="animate-spin text-slate-400" />
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-2xl font-bold text-white">Panel Users</h1>
                    <p className="text-slate-400 text-sm mt-1">Manage admin accounts and their permissions</p>
                </div>
                <button
                    onClick={() => canWritePanelUsers && setShowCreateModal(true)}
                    disabled={!canWritePanelUsers}
                    className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                    <Plus size={16} />
                    New User
                </button>
            </div>

            <div className="space-y-4">
                {users.map(user => (
                    <div key={user.username} className="bg-slate-900 border border-slate-800 rounded-xl p-5">
                        <div className="flex items-center justify-between mb-4">
                            <div>
                                <span className="text-white font-semibold text-base">{user.username}</span>
                                {user.created_at > 0 && (
                                    <span className="ml-3 text-xs text-slate-500">
                                        Created {new Date(user.created_at * 1000).toLocaleDateString()}
                                    </span>
                                )}
                            </div>
                            <button
                                onClick={() => handleDelete(user.username)}
                                disabled={!canWritePanelUsers || deletingUser === user.username}
                                className="p-2 text-red-400 hover:bg-red-500/10 rounded-lg transition-colors disabled:opacity-50"
                                title="Delete user"
                            >
                                <Trash2 size={16} />
                            </button>
                        </div>

                        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 mb-4">
                            {PERM_LABELS.map(({ key, label, description }) => {
                                const checked = editingPerms[user.username]?.[key] ?? false;
                                const disabled = !canWritePanelUsers;
                                return (
                                    <label
                                        key={key}
                                        className={`flex items-start gap-3 p-3 rounded-lg border transition-colors cursor-pointer
                                            ${checked
                                                ? 'bg-blue-600/10 border-blue-600/40'
                                                : 'bg-slate-800/50 border-slate-700/50'}
                                            ${disabled ? 'opacity-50 cursor-not-allowed' : 'hover:border-slate-600'}`}
                                    >
                                        <input
                                            type="checkbox"
                                            checked={checked}
                                            disabled={disabled}
                                            onChange={() => toggleEditPerm(user.username, key)}
                                            className="mt-0.5 accent-blue-500"
                                        />
                                        <div>
                                            <div className="text-sm font-medium text-slate-200">{label}</div>
                                            <div className="text-xs text-slate-500 mt-0.5">{description}</div>
                                        </div>
                                    </label>
                                );
                            })}
                        </div>

                        <div className="flex justify-end">
                            <button
                                onClick={() => handleSavePerms(user.username)}
                                disabled={!canWritePanelUsers || savingPerms[user.username]}
                                className="flex items-center gap-2 px-4 py-2 bg-slate-700 hover:bg-slate-600 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-50"
                            >
                                {savingPerms[user.username]
                                    ? <RefreshCw size={14} className="animate-spin" />
                                    : <Save size={14} />}
                                Save Permissions
                            </button>
                        </div>
                    </div>
                ))}

                {users.length === 0 && (
                    <div className="text-center py-12 text-slate-500">No panel users found.</div>
                )}
            </div>

            {/* Create Modal */}
            {showCreateModal && (
                <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center p-4">
                    <div className="bg-slate-900 border border-slate-700 rounded-xl w-full max-w-lg shadow-xl">
                        <div className="p-5 border-b border-slate-800">
                            <h2 className="text-lg font-semibold text-white">Create Panel User</h2>
                        </div>
                        <form onSubmit={handleCreate} className="p-5 space-y-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-1">Username</label>
                                <input
                                    type="text"
                                    value={newUsername}
                                    onChange={e => setNewUsername(e.target.value)}
                                    required
                                    className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:border-blue-500"
                                    placeholder="username"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-1">Password</label>
                                <div className="relative">
                                    <input
                                        type={showPassword ? 'text' : 'password'}
                                        value={newPassword}
                                        onChange={e => setNewPassword(e.target.value)}
                                        required
                                        minLength={8}
                                        className="w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 pr-10 text-white text-sm focus:outline-none focus:border-blue-500"
                                        placeholder="min. 8 characters"
                                    />
                                    <button
                                        type="button"
                                        onClick={() => setShowPassword(p => !p)}
                                        className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 hover:text-white"
                                    >
                                        {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                                    </button>
                                </div>
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-2">Permissions</label>
                                <div className="space-y-2">
                                    {PERM_LABELS.map(({ key, label, description }) => {
                                        const disabled = !canWritePanelUsers;
                                        return (
                                            <label
                                                key={key}
                                                className={`flex items-start gap-3 p-3 rounded-lg border transition-colors cursor-pointer
                                                    ${newPerms[key]
                                                        ? 'bg-blue-600/10 border-blue-600/40'
                                                        : 'bg-slate-800/50 border-slate-700/50'}
                                                    ${disabled ? 'opacity-50 cursor-not-allowed' : 'hover:border-slate-600'}`}
                                            >
                                                <input
                                                    type="checkbox"
                                                    checked={newPerms[key]}
                                                    disabled={disabled}
                                                    onChange={() => toggleNewPerm(key)}
                                                    className="mt-0.5 accent-blue-500"
                                                />
                                                <div>
                                                    <div className="text-sm font-medium text-slate-200">{label}</div>
                                                    <div className="text-xs text-slate-500 mt-0.5">{description}</div>
                                                </div>
                                            </label>
                                        );
                                    })}
                                </div>
                            </div>
                            <div className="flex justify-end gap-3 pt-2">
                                <button
                                    type="button"
                                    onClick={() => setShowCreateModal(false)}
                                    className="px-4 py-2 text-slate-300 hover:text-white text-sm transition-colors"
                                >
                                    Cancel
                                </button>
                                <button
                                    type="submit"
                                    disabled={!canWritePanelUsers || creating}
                                    className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-50"
                                >
                                    {creating && <RefreshCw size={14} className="animate-spin" />}
                                    Create User
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}
        </div>
    );
};

export default PanelUsers;
