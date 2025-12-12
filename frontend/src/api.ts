export interface UserStatus {
    name: string;
    uuid?: string;
    flow?: string;
    uplink: number;
    downlink: number;
    total: number;
    quota_limit: number;
    quota_period: string;
    reset_day: number;
    enabled?: boolean;
    last_seen?: number;
    inbound_tags?: string[];
}

export interface CreateUserRequest {
    name: string;
    original_name?: string;
    uuid: string;
    flow: string;
    quota_limit: number;
    quota_period: string;
    reset_day: number;
    enabled?: boolean;
    inbound_tag?: string;
}

export interface FeatureFlags {
    enable_singbox: boolean;
    enable_wireguard: boolean;
    retention_enabled?: boolean;
    retention_days?: number;
    sampler_interval_sec?: number;
    sampler_paused?: boolean;
    active_threshold_bytes?: number;
    wg_sampler_interval_sec?: number;
    wg_retention_days?: number;
    aggregation_enabled?: boolean;
    aggregation_days?: number;
    log_source?: 'journal' | 'file';
    access_log_path?: string;
    systemctl_available?: boolean;
    journalctl_available?: boolean;
}

export interface UnifiedChartPoint {
    ts: number;
    up_sb: number;
    down_sb: number;
    up_wg: number;
    down_wg: number;
}

export interface Consumer {
    name: string;
    total: number;
    flow: string;
    quota_limit: number;
    key: string;
}

export interface TrafficStats {
    uplink: number;
    downlink: number;
}

export interface DashboardData {
    status: { [key: string]: any };
    stats_cards: { [key: string]: TrafficStats };
    chart_data: UnifiedChartPoint[];
    top_consumers: { [key: string]: Consumer[] };
    singbox_pending_changes: boolean;
    public_ip: string;
}

const buildHeaders = (contentType?: string) => {
    const headers: Record<string, string> = {};
    if (contentType) headers['Content-Type'] = contentType;
    const token = localStorage.getItem('token');
    if (token) headers['Authorization'] = `Bearer ${token}`;
    return headers;
};

const handleResponse = async (res: Response, errorMsg: string = 'Request failed') => {
    if (res.status === 401) {
        window.dispatchEvent(new Event('auth:unauthorized'));
        throw new Error('Unauthorized');
    }
    if (!res.ok) {
        const text = await res.text();
        throw new Error(text || errorMsg);
    }
    return res;
};


export const api = {
    getUsers: async (): Promise<UserStatus[]> => {
        const res = await fetch('/api/users', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch users');
        return res.json();
    },
    pauseSampler: async (): Promise<void> => {
        const res = await fetch('/api/sampler/pause', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to pause sampler');
    },
    resumeSampler: async (): Promise<void> => {
        const res = await fetch('/api/sampler/resume', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to resume sampler');
    },
    updatePassword: async (currentPassword: string, newPassword: string): Promise<void> => {
        const res = await fetch('/api/auth/password', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ current_password: currentPassword, new_password: newPassword })
        });
        await handleResponse(res, 'Failed to update password');
    },
    updateUsername: async (currentPassword: string, newUsername: string): Promise<void> => {
        const res = await fetch('/api/auth/username', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ current_password: currentPassword, new_username: newUsername })
        });
        await handleResponse(res, 'Failed to update username');
    },
    createUser: async (user: CreateUserRequest): Promise<void> => {
        const res = await fetch('/api/users', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(user)
        });
        await handleResponse(res, 'Failed to create user');
    },
    updateUser: async (user: CreateUserRequest): Promise<void> => {
        const res = await fetch('/api/users', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(user)
        });
        await handleResponse(res, 'Failed to update user');
    },
    deleteUser: async (name: string): Promise<void> => {
        const res = await fetch(`/api/users?name=${encodeURIComponent(name)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to delete user');
    },
    removeUserFromInbound: async (name: string, inboundTag: string): Promise<void> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/inbounds/${encodeURIComponent(inboundTag)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to remove user from inbound');
    },
    bulkCreateUsers: async (users: CreateUserRequest[]): Promise<void> => {
        const res = await fetch('/api/users/bulk', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(users)
        });
        await handleResponse(res, 'Failed to bulk create users');
    },
    getReport: async (start?: string, end?: string): Promise<UserStatus[]> => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        const res = await fetch(`/api/report?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch report');
        return res.json();
    },
    getReportSummary: async (start?: string, end?: string, limitBytes?: number): Promise<any[]> => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        if (limitBytes) params.append('limit_bytes', limitBytes.toString());
        const res = await fetch(`/api/report/summary?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch report summary');
        return res.json();
    },
    getConfig: async (): Promise<any> => {
        const res = await fetch('/api/config', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch config');
        return res.json();
    },
    getLogs: async (user?: string): Promise<{ logs: string[] }> => {
        const url = user ? `/api/logs?user=${encodeURIComponent(user)}` : '/api/logs';
        const res = await fetch(url, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch logs');
        return res.json();
    },
    searchLogs: async (query: string, limit?: number, page?: number): Promise<{ logs: string[]; page?: number; page_size?: number; has_more?: boolean }> => {
        const params = new URLSearchParams({ q: query });
        if (limit) params.set('limit', String(limit));
        if (page) params.set('page', String(page));
        const res = await fetch(`/api/logs/search?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to search logs');
        const text = await res.text();
        try {
            return JSON.parse(text);
        } catch {
            return { logs: [text || 'Search returned non-JSON response'] };
        }
    },

    // WireGuard
    getWireGuardPeers: async (): Promise<any[]> => {
        const res = await fetch('/api/wireguard/peers', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch peers');
        return res.json();
    },
    createWireGuardPeer: async (payload: { alias: string; ip: string; endpoint?: string }): Promise<any> => {
        const res = await fetch('/api/wireguard/peers', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload)
        });
        await handleResponse(res, 'Failed to create peer');
        return res.json();
    },
    deleteWireGuardPeer: async (publicKey: string): Promise<void> => {
        const res = await fetch(`/api/wireguard/peers?public_key=${encodeURIComponent(publicKey)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to delete peer');
    },
    getWireGuardInterface: async (): Promise<any> => {
        const res = await fetch('/api/wireguard/interface', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch interface config');
        return res.json();
    },
    updateWireGuardInterface: async (config: any): Promise<void> => {
        const res = await fetch('/api/wireguard/interface', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(config)
        });
        await handleResponse(res, 'Failed to update interface');
    },
    updateWireGuardPeer: async (publicKey: string, config: any): Promise<void> => {
        const res = await fetch(`/api/wireguard/peer?public_key=${encodeURIComponent(publicKey)}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(config)
        });
        await handleResponse(res, 'Failed to update peer');
    },
    getWireGuardPeerConfig: async (publicKey: string, privateKey?: string): Promise<{ config: string }> => {
        const params = new URLSearchParams({ public_key: publicKey })
        if (privateKey) params.set('private_key', privateKey)
        const res = await fetch(`/api/wireguard/peer/config?${params.toString()}`, {
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to fetch peer config');
        return res.json();
    },
    getWireGuardTraffic: async (range: string): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ range })
        const res = await fetch(`/api/wireguard/traffic?${params.toString()}`, { headers: buildHeaders() })
        await handleResponse(res, 'Failed to fetch WireGuard traffic');
        return res.json()
    },
    getWireGuardTrafficRange: async (start: number, end: number): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ start: String(start), end: String(end) })
        const res = await fetch(`/api/wireguard/traffic?${params.toString()}`, { headers: buildHeaders() })
        await handleResponse(res, 'Failed to fetch WireGuard traffic');
        return res.json()
    },
    getWireGuardTrafficSeries: async (range?: string, peer?: string, limit?: number, start?: number, end?: number): Promise<Record<string, { timestamp: number; rx: number; tx: number; endpoint?: string }[]>> => {
        const params = new URLSearchParams()
        if (range) params.append('range', range)
        if (peer) params.append('peer', peer)
        if (limit) params.append('limit', String(limit))
        if (start) params.append('start', String(start))
        if (end) params.append('end', String(end))
        const res = await fetch(`/api/wireguard/traffic/series?${params.toString()}`, { headers: buildHeaders() })
        await handleResponse(res, 'Failed to fetch WireGuard traffic series');
        return res.json()
    },

    // Service Control
    restartService: async (service: string): Promise<void> => {
        const res = await fetch('/api/service/restart', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ service })
        });
        await handleResponse(res, 'Failed to restart service');
    },
    startService: async (service: string): Promise<void> => {
        const res = await fetch('/api/service/start', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ service })
        });
        await handleResponse(res, 'Failed to start service');
    },
    stopService: async (service: string): Promise<void> => {
        const res = await fetch('/api/service/stop', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ service })
        });
        await handleResponse(res, 'Failed to stop service');
    },

    // Feature toggles
    getFeatures: async (): Promise<FeatureFlags> => {
        const res = await fetch('/api/settings/features', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch features');
        return res.json();
    },
    updateFeatures: async (flags: FeatureFlags): Promise<void> => {
        const res = await fetch('/api/settings/features', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(flags)
        });
        await handleResponse(res, 'Failed to update features');
    },

    // Sing-box Configuration
    getSingboxConfig: async (): Promise<string> => {
        const res = await fetch('/api/singbox/config', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch Sing-box config');
        return res.text();
    },
    updateSingboxConfig: async (config: string): Promise<void> => {
        const res = await fetch('/api/singbox/config', {
            method: 'PUT',
            headers: buildHeaders('text/plain'),
            body: config
        });
        await handleResponse(res, 'Failed to update Sing-box config');
    },
    getSingboxInbounds: async (): Promise<any[]> => {
        const res = await fetch('/api/singbox/inbounds', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch Sing-box inbounds');
        return res.json();
    },
    addSingboxInbound: async (inbound: any): Promise<void> => {
        const res = await fetch('/api/singbox/inbound', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(inbound)
        });
        await handleResponse(res, 'Failed to add Sing-box inbound');
    },
    updateSingboxInbound: async (tag: string, inbound: any): Promise<void> => {
        const res = await fetch(`/api/singbox/inbound?tag=${encodeURIComponent(tag)}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(inbound)
        });
        await handleResponse(res, 'Failed to update Sing-box inbound');
    },
    deleteSingboxInbound: async (tag: string): Promise<void> => {
        const res = await fetch(`/api/singbox/inbound?tag=${encodeURIComponent(tag)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to delete Sing-box inbound');
    },

    // Raw Config
    updateConfig: async (configText: string): Promise<void> => {
        const res = await fetch('/api/config', {
            method: 'PUT',
            headers: buildHeaders('text/plain'),
            body: configText
        });
        await handleResponse(res, 'Failed to update config');
    },
    getWireGuardConfig: async (): Promise<string> => {
        const res = await fetch('/api/wireguard/config', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard config');
        return res.text();
    },
    backupWireGuardConfig: async (): Promise<void> => {
        const res = await fetch('/api/wireguard/config/backup', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to backup WireGuard config');
    },
    restoreWireGuardConfig: async (): Promise<string> => {
        const res = await fetch('/api/wireguard/config/restore', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to restore WireGuard config');
        return res.text();
    },
    getBackupMeta: async (): Promise<{ singbox_last_backup?: string; wireguard_last_backup?: string }> => {
        const res = await fetch('/api/config/backup/meta', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to load backup metadata');
        return res.json();
    },
    updateWireGuardConfig: async (config: string): Promise<void> => {
        const res = await fetch('/api/wireguard/config', {
            method: 'PUT',
            headers: buildHeaders('text/plain'),
            body: config
        });
        await handleResponse(res, 'Failed to update WireGuard config');
    },

    // Stats & Status
    getStats: async (range: string = '24h', start?: string, end?: string): Promise<any[]> => {
        let url = `/api/stats?range=${range}`;
        if (start && end) {
            url += `&start=${start}&end=${end}`;
        }
        const res = await fetch(url, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch stats');
        return res.json();
    },
    getSystemStatus: async (): Promise<{ singbox: boolean; wireguard: boolean; wireguard_pending_restart?: boolean; wg_sample_interval_sec?: number; active_users_singbox: number; active_users_wireguard: number; active_users_singbox_list?: string[]; active_users_wireguard_list?: string[]; singbox_sys_stats?: any; samples_count?: number; db_size_bytes?: number; sampler_paused?: boolean; systemctl_available?: boolean; journalctl_available?: boolean }> => {
        const res = await fetch('/api/status', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch system status');
        return res.json();
    },

    // Sampler
    runSampler: async (): Promise<void> => {
        const res = await fetch('/api/sampler/run', {
            method: 'POST',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to run sampler');
    },
    getSamplerHistory: async (limit?: number): Promise<any[]> => {
        const url = limit ? `/api/sampler/history?limit=${limit}` : '/api/sampler/history';
        const res = await fetch(url, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch sampler history');
        return res.json();
    },
    pruneNow: async (): Promise<{ deleted: number; cutoff: number }> => {
        const res = await fetch('/api/retention/prune', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to prune');
        return res.json();
    },
    backupConfig: async (): Promise<void> => {
        const res = await fetch('/api/config/backup', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to backup config');
    },
    restoreConfig: async (): Promise<any> => {
        const res = await fetch('/api/config/restore', { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to restore config');
        return res.json();
    },
    getDashboardData: async (range: string = '24h', start?: string, end?: string): Promise<DashboardData> => {
        const params = new URLSearchParams({ range });
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        const res = await fetch(`/api/dashboard?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch dashboard data');
        return res.json();
    },
    generateRealityKeys: async (): Promise<{ private_key: string; public_key: string; short_id: string[] }> => {
        const res = await fetch('/api/tools/reality-keys', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to generate Reality keys');
        return res.json();
    },
    applySingboxChanges: async (): Promise<{ success: boolean; message: string }> => {
        const res = await fetch('/api/singbox/apply', {
            method: 'POST',
            headers: buildHeaders('application/json')
        });
        await handleResponse(res, 'Failed to apply Sing-box changes');
        return res.json();
    }
};
