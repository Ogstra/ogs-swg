import React, { useEffect, useState } from 'react';
import { api, type PanelUserInfo, type CreatePanelUserRequest } from '../../../services/api';
import { useToast } from '../../../context/ToastContext';
import { useAuth, type PanelUserPermissions } from '../../../context/AuthContext';
import { Plus, Trash2, RefreshCw, Edit } from 'lucide-react';
import { ActionIconButton } from '../../../components/ui/ActionIconButton';
import { Modal } from '../../../components/ui/Modal';
import { Button } from '../../../components/ui/Button';

const PERMISSION_GROUPS: {
    id: string
    label: string
    description: string
    readKey?: keyof PanelUserPermissions
    writeKey?: keyof PanelUserPermissions
}[] = [
        {
            id: 'singbox',
            label: 'Sing-box Users',
            description: '',
            readKey: 'can_read_users',
            writeKey: 'can_write_users',
        },
        {
            id: 'wireguard',
            label: 'WireGuard',
            description: '',
            readKey: 'can_read_wireguard',
            writeKey: 'can_write_wireguard',
        },
        {
            id: 'config',
            label: 'Raw Config',
            description: '',
            readKey: 'can_read_config',
            writeKey: 'can_write_config',
        },
        {
            id: 'settings',
            label: 'Settings',
            description: '',
            readKey: 'can_read_settings',
            writeKey: 'can_write_settings',
        },
        {
            id: 'panel-users',
            label: 'Admin Users',
            description: 'Manage panel users',
            readKey: 'can_read_panel_users',
            writeKey: 'can_write_panel_users',
        },
        {
            id: 'logs',
            label: 'Logs',
            description: '',
            readKey: 'can_read_logs',
        },
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
    const [editorMode, setEditorMode] = useState<'create' | 'edit' | null>(null);
    const [editorOriginalUsername, setEditorOriginalUsername] = useState<string | null>(null);
    const [editorUsername, setEditorUsername] = useState('');
    const [editorPassword, setEditorPassword] = useState('');
    const [editorPerms, setEditorPerms] = useState<PanelUserPermissions>(emptyPerms());
    const [savingEditor, setSavingEditor] = useState(false);
    const [deletingUser, setDeletingUser] = useState<string | null>(null);

    const load = async () => {
        setLoading(true);
        try {
            const data = await api.getPanelUsers();
            setUsers(data ?? []);
        } catch {
            error('Failed to load panel users');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => { load(); }, []);

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

    const openCreateModal = () => {
        setEditorMode('create');
        setEditorOriginalUsername(null);
        setEditorUsername('');
        setEditorPassword('');
        setEditorPerms(emptyPerms());
    };

    const openEditModal = (user: PanelUserInfo) => {
        setEditorMode('edit');
        setEditorOriginalUsername(user.username);
        setEditorUsername(user.username);
        setEditorPassword('');
        setEditorPerms({ ...user.permissions });
    };

    const closeEditorModal = () => {
        setEditorMode(null);
        setEditorOriginalUsername(null);
        setEditorUsername('');
        setEditorPassword('');
        setEditorPerms(emptyPerms());
    };

    const toggleEditorPerm = (key: keyof PanelUserPermissions) => {
        setEditorPerms(prev => applyPermissionToggle(prev, key, !prev[key]));
    };

    const handleSaveEditor = async () => {
        if (!canWritePanelUsers || !editorMode) return;

        const trimmedUsername = editorUsername.trim();
        const trimmedPassword = editorPassword.trim();

        if (editorMode === 'create') {
            if (!trimmedUsername || !trimmedPassword) {
                error('Username and password are required');
                return;
            }
            if (trimmedPassword.length < 8) {
                error('Password must be at least 8 characters');
                return;
            }
        }

        if (editorMode === 'edit' && trimmedPassword && trimmedPassword.length < 8) {
            error('Password must be at least 8 characters');
            return;
        }

        try {
            setSavingEditor(true);

            if (editorMode === 'create') {
                const req: CreatePanelUserRequest = {
                    username: trimmedUsername,
                    password: trimmedPassword,
                    permissions: editorPerms,
                };
                await api.createPanelUser(req);
                success('Panel user created');
                closeEditorModal();
                await load();
                return;
            }

            if (!editorOriginalUsername) return;

            await api.updatePanelUserPermissions(editorOriginalUsername, editorPerms);

            let targetUsername = editorOriginalUsername;
            const hasUsernameChange = !!trimmedUsername && trimmedUsername !== editorOriginalUsername;
            if (hasUsernameChange) {
                await api.updatePanelUserUsername(editorOriginalUsername, trimmedUsername);
                targetUsername = trimmedUsername;
            }

            if (trimmedPassword) {
                await api.updatePanelUserPassword(targetUsername, trimmedPassword);
            }

            success('Panel user updated');
            closeEditorModal();
            await load();
        } catch (e: any) {
            error(e.message || 'Failed to save panel user');
        } finally {
            setSavingEditor(false);
        }
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
                </div>
                <button
                    onClick={() => canWritePanelUsers && openCreateModal()}
                    disabled={!canWritePanelUsers}
                    className="flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                    <Plus size={16} />
                    New User
                </button>
            </div>

            <div className="space-y-4">
                {users.map(user => (
                    <div key={user.username} className="bg-slate-900 border border-slate-800 rounded-xl px-4 py-3">
                        <div className="flex items-center justify-between gap-3">
                            <div className="min-w-0">
                                <div className="text-white font-semibold text-base truncate">{user.username}</div>
                                {user.created_at > 0 && (
                                    <div className="text-xs text-slate-500">
                                        Created {new Date(user.created_at * 1000).toLocaleDateString()}
                                    </div>
                                )}
                            </div>
                            <div className="flex items-center gap-2">
                                <ActionIconButton
                                    onClick={() => openEditModal(user)}
                                    disabled={!canWritePanelUsers}
                                    tone="primary"
                                    title="Edit user permissions"
                                >
                                    <Edit size={16} />
                                </ActionIconButton>
                                <ActionIconButton
                                    onClick={() => handleDelete(user.username)}
                                    disabled={!canWritePanelUsers || deletingUser === user.username}
                                    tone="danger"
                                    title="Delete user"
                                >
                                    <Trash2 size={16} />
                                </ActionIconButton>
                            </div>
                        </div>
                    </div>
                ))}

                {users.length === 0 && (
                    <div className="text-center py-12 text-slate-500">No panel users found.</div>
                )}
            </div>

            <Modal
                isOpen={!!editorMode}
                onClose={closeEditorModal}
                title={editorMode === 'create' ? 'Create Panel User' : `Edit Panel User: ${editorOriginalUsername}`}
                size="lg"
                footer={
                    <>
                        <Button variant="ghost" onClick={closeEditorModal}>Cancel</Button>
                        <Button
                            variant="primary"
                            onClick={handleSaveEditor}
                            disabled={!canWritePanelUsers || !editorMode}
                            isLoading={savingEditor}
                        >
                            {editorMode === 'create' ? 'Create User' : 'Save Changes'}
                        </Button>
                    </>
                }
            >
                <div className="space-y-5">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">
                                {editorMode === 'create' ? 'Username' : 'Change Username'}
                            </label>
                            <input
                                type="text"
                                value={editorUsername}
                                onChange={e => setEditorUsername(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2.5 text-white text-sm outline-none focus:border-blue-500/50"
                                placeholder={editorOriginalUsername || 'username'}
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">
                                {editorMode === 'create' ? 'Password' : 'Change Password'}
                            </label>
                            <input
                                type="password"
                                value={editorPassword}
                                onChange={e => setEditorPassword(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2.5 text-white text-sm outline-none focus:border-blue-500/50"
                                placeholder={editorMode === 'create' ? 'min. 8 characters' : 'Empty to keep current password'}
                            />
                        </div>
                    </div>

                    <div className="rounded-lg border border-slate-800 overflow-hidden">
                        <div className="grid grid-cols-[1fr_80px_80px] bg-slate-950/50 border-b border-slate-800 px-3 py-2 text-xs font-semibold text-slate-400">
                            <div>Resource</div>
                            <div className="text-center">Read</div>
                            <div className="text-center">Write</div>
                        </div>
                        {PERMISSION_GROUPS.map(({ id, label, description, readKey, writeKey }) => {
                            const disabled = !canWritePanelUsers;
                            const current = editorPerms;
                            return (
                                <div
                                    key={id}
                                    className="grid grid-cols-[1fr_80px_80px] items-center px-3 py-3 border-b border-slate-800 last:border-b-0 bg-slate-900/40"
                                >
                                    <div>
                                        <div className="text-sm font-medium text-slate-200">{label}</div>
                                        <div className="text-xs text-slate-500 mt-0.5">{description}</div>
                                    </div>
                                    <div className="flex justify-center">
                                        {readKey ? (
                                            <input
                                                type="checkbox"
                                                checked={current[readKey]}
                                                disabled={disabled}
                                                onChange={() => toggleEditorPerm(readKey)}
                                                className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                            />
                                        ) : (
                                            <span className="text-slate-600">-</span>
                                        )}
                                    </div>
                                    <div className="flex justify-center">
                                        {writeKey ? (
                                            <input
                                                type="checkbox"
                                                checked={current[writeKey]}
                                                disabled={disabled}
                                                onChange={() => toggleEditorPerm(writeKey)}
                                                className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                            />
                                        ) : (
                                            <span className="text-slate-600">-</span>
                                        )}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            </Modal>
        </div>
    );
};

export default PanelUsers;
