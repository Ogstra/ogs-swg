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
}

export interface FeatureFlags {
    enable_singbox: boolean;
    enable_wireguard: boolean;
    retention_enabled?: boolean;
    retention_days?: number;
    sampler_interval_sec?: number;
    sampler_paused?: boolean;
    active_threshold_bytes?: number;
}

const buildHeaders = (contentType?: string) => {
    const headers: Record<string, string> = {};
    if (contentType) headers['Content-Type'] = contentType;
    const apiKey = localStorage.getItem('xpanel_api_key');
    if (apiKey) headers['X-API-Key'] = apiKey;
    return headers;
};

export const api = {
    getUsers: async (): Promise<UserStatus[]> => {
        const res = await fetch('/api/users', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch users');
        return res.json();
    },
    pauseSampler: async (): Promise<void> => {
        const res = await fetch('/api/sampler/pause', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to pause sampler');
    },
    resumeSampler: async (): Promise<void> => {
        const res = await fetch('/api/sampler/resume', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to resume sampler');
    },
    createUser: async (user: CreateUserRequest): Promise<void> => {
        const res = await fetch('/api/users', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(user)
        });
        if (!res.ok) throw new Error('Failed to create user');
    },
    updateUser: async (user: CreateUserRequest): Promise<void> => {
        const res = await fetch('/api/users', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(user)
        });
        if (!res.ok) throw new Error('Failed to update user');
    },
    deleteUser: async (name: string): Promise<void> => {
        const res = await fetch(`/api/users?name=${encodeURIComponent(name)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        if (!res.ok) throw new Error('Failed to delete user');
    },
    bulkCreateUsers: async (users: CreateUserRequest[]): Promise<void> => {
        const res = await fetch('/api/users/bulk', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(users)
        });
        if (!res.ok) throw new Error('Failed to bulk create users');
    },
    getReport: async (start?: string, end?: string): Promise<UserStatus[]> => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        const res = await fetch(`/api/report?${params.toString()}`, { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch report');
        return res.json();
    },
    getReportSummary: async (start?: string, end?: string, limitBytes?: number): Promise<any[]> => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        if (limitBytes) params.append('limit_bytes', limitBytes.toString());
        const res = await fetch(`/api/report/summary?${params.toString()}`, { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch report summary');
        return res.json();
    },
    getConfig: async (): Promise<any> => {
        const res = await fetch('/api/config', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch config');
        return res.json();
    },
    getLogs: async (user?: string): Promise<{ logs: string[] }> => {
        const url = user ? `/api/logs?user=${encodeURIComponent(user)}` : '/api/logs';
        const res = await fetch(url, { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch logs');
        return res.json();
    },
    searchLogs: async (query: string, limit?: number): Promise<{ logs: string[] }> => {
        const params = new URLSearchParams({ q: query });
        if (limit) params.set('limit', String(limit));
        const res = await fetch(`/api/logs/search?${params.toString()}`, { headers: buildHeaders() });
        const text = await res.text();
        if (!res.ok) throw new Error(text || 'Failed to search logs');
        try {
            return JSON.parse(text);
        } catch {
            return { logs: [text || 'Search returned non-JSON response'] };
        }
    },

    // WireGuard
    getWireGuardPeers: async (): Promise<any[]> => {
        const res = await fetch('/api/wireguard/peers', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch peers');
        return res.json();
    },
    createWireGuardPeer: async (alias: string): Promise<any> => {
        const res = await fetch('/api/wireguard/peers', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ alias })
        });
        if (!res.ok) throw new Error('Failed to create peer');
        return res.json();
    },
    deleteWireGuardPeer: async (publicKey: string): Promise<void> => {
        const res = await fetch(`/api/wireguard/peers?public_key=${encodeURIComponent(publicKey)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        if (!res.ok) throw new Error('Failed to delete peer');
    },
    getWireGuardInterface: async (): Promise<any> => {
        const res = await fetch('/api/wireguard/interface', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch interface config');
        return res.json();
    },
    updateWireGuardInterface: async (config: any): Promise<void> => {
        const res = await fetch('/api/wireguard/interface', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(config)
        });
        if (!res.ok) throw new Error('Failed to update interface');
    },
    updateWireGuardPeer: async (publicKey: string, config: any): Promise<void> => {
        const res = await fetch(`/api/wireguard/peer?public_key=${encodeURIComponent(publicKey)}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(config)
        });
        if (!res.ok) throw new Error('Failed to update peer');
    },

    // Service Control
    restartService: async (service: string): Promise<void> => {
        const res = await fetch('/api/service/restart', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ service })
        });
        if (!res.ok) throw new Error('Failed to restart service');
    },
    stopService: async (service: string): Promise<void> => {
        const res = await fetch('/api/service/stop', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ service })
        });
        if (!res.ok) throw new Error('Failed to stop service');
    },

    // Feature toggles
    getFeatures: async (): Promise<FeatureFlags> => {
        const res = await fetch('/api/settings/features', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch features');
        return res.json();
    },
    updateFeatures: async (flags: FeatureFlags): Promise<void> => {
        const res = await fetch('/api/settings/features', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(flags)
        });
        if (!res.ok) throw new Error('Failed to update features');
    },

    // Raw Config
    updateConfig: async (configText: string): Promise<void> => {
        const res = await fetch('/api/config', {
            method: 'PUT',
            headers: buildHeaders('text/plain'),
            body: configText
        });
        if (!res.ok) throw new Error('Failed to update config');
    },
    getWireGuardConfig: async (): Promise<string> => {
        const res = await fetch('/api/wireguard/config', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch WireGuard config');
        return res.text();
    },
    backupWireGuardConfig: async (): Promise<void> => {
        const res = await fetch('/api/wireguard/config/backup', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to backup WireGuard config');
    },
    restoreWireGuardConfig: async (): Promise<string> => {
        const res = await fetch('/api/wireguard/config/restore', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to restore WireGuard config');
        return res.text();
    },
    updateWireGuardConfig: async (config: string): Promise<void> => {
        const res = await fetch('/api/wireguard/config', {
            method: 'PUT',
            headers: buildHeaders('text/plain'),
            body: config
        });
        if (!res.ok) throw new Error('Failed to update WireGuard config');
    },

    // Stats & Status
    getStats: async (range: string = '24h', start?: string, end?: string): Promise<any[]> => {
        let url = `/api/stats?range=${range}`;
        if (start && end) {
            url += `&start=${start}&end=${end}`;
        }
        const res = await fetch(url, { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch stats');
        return res.json();
    },
    getSystemStatus: async (): Promise<{ singbox: boolean; wireguard: boolean; active_users_singbox: number; active_users_wireguard: number; active_users_singbox_list?: string[]; active_users_wireguard_list?: string[]; singbox_sys_stats?: any; samples_count?: number; db_size_bytes?: number; sampler_paused?: boolean }> => {
        const res = await fetch('/api/status', { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch system status');
        return res.json();
    },

    // Sampler
    runSampler: async (): Promise<void> => {
        const res = await fetch('/api/sampler/run', {
            method: 'POST',
            headers: buildHeaders()
        });
        if (!res.ok) throw new Error('Failed to run sampler');
    },
    getSamplerHistory: async (limit?: number): Promise<any[]> => {
        const url = limit ? `/api/sampler/history?limit=${limit}` : '/api/sampler/history';
        const res = await fetch(url, { headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to fetch sampler history');
        return res.json();
    },
    pruneNow: async (): Promise<{ deleted: number; cutoff: number }> => {
        const res = await fetch('/api/retention/prune', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to prune');
        return res.json();
    },
    backupConfig: async (): Promise<void> => {
        const res = await fetch('/api/config/backup', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to backup config');
    },
    restoreConfig: async (): Promise<any> => {
        const res = await fetch('/api/config/restore', { method: 'POST', headers: buildHeaders() });
        if (!res.ok) throw new Error('Failed to restore config');
        return res.json();
    }
};
