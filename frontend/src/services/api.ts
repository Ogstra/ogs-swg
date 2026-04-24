import type { PanelUserPermissions } from '../context/AuthContext';

export interface PanelUserInfo {
    username: string;
    permissions: PanelUserPermissions;
    created_at: number;
}

export interface CreatePanelUserRequest {
    username: string;
    password: string;
    permissions: PanelUserPermissions;
}

export interface UserStatus {
    name: string;
    uuid?: string;
    flow?: string;
    vmess_security?: string;
    vmess_alter_id?: number;
    uplink: number;
    downlink: number;
    total: number;
    quota_limit: number;
    quota_period: string;
    reset_day: number;
    enabled?: boolean;
    last_seen?: number;
    inbound_tags?: string[];
    route_tags?: UserRouteTag[];
}

export interface UserRouteTag {
    id: number;
    name: string;
    color: string;
    description: string;
    rule_match_json: string;
    linked: boolean;
    broken: boolean;
    broken_reason?: string;
    auth_users: string[];
}

export interface CompatibleUserRouteRule {
    index: number;
    rule_match_json: string;
    outbound: string;
    auth_users: string[];
    summary: string;
    already_linked: boolean;
}

export interface CreateUserRouteTagRequest {
    name: string;
    color?: string;
    description?: string;
    rule_index: number;
}

export interface UpdateUserRouteTagsResponse {
    singbox_pending_changes: boolean;
}

export interface CreateUserRequest {
    name: string;
    original_name?: string;
    uuid: string;
    flow: string;
    vmess_security?: string;
    vmess_alter_id?: number;
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

export interface ConfigBackupEntry {
    name: string;
    created_at: string;
}

export interface LogSearchParams {
    query: string;
    limit?: number;
    page?: number;
    from?: string;
    to?: string;
    signal?: AbortSignal;
}

export interface LogSearchStreamEvent {
    type: 'status' | 'chunk' | 'done' | 'error';
    message?: string;
    logs?: string[];
    matched?: number;
    truncated?: boolean;
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
    interface_name?: string;
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
    wireguard_interfaces?: { [key: string]: TrafficStats };
    chart_data: UnifiedChartPoint[];
    top_consumers: { [key: string]: Consumer[] };
    singbox_pending_changes: boolean;
    public_ip: string;
}

export interface DashboardConsumerChartData {
    chart_data: UnifiedChartPoint[];
}

export interface DashboardPreferences {
    default_service: 'singbox' | 'wireguard';
    refresh_ms: number;
    default_range: '30m' | '1h' | '6h' | '24h' | '1w' | '1m';
    active_user_window_minutes: number;
    detail_chart_target_points: number;
}

export interface ApplySingboxChangesResponse {
    success: boolean;
    message: string;
    restart_required?: boolean;
}

export interface SamplerHistoryEntry {
    ts?: number;
    timestamp?: number;
    duration_ms: number;
    inserted: number;
    error: string;
    source: string;
}

export interface SubscriptionRequestHistoryEntry {
    id: number;
    sub_id: number;
    name: string;
    user_name: string;
    request_ip: string;
    request_host: string;
    request_path: string;
    user_agent: string;
    device_model: string;
    device_os: string;
    device_os_version: string;
    app_version: string;
    country: string;
    hwid_hash: string;
    hwid_prefix: string;
    requested_at: number;
    served_from_cache: number;
    blocked: number;
    block_reason: string;
}

export interface SubscriptionRequestHistoryPage {
    items: SubscriptionRequestHistoryEntry[];
    has_more: boolean;
    next_offset: number;
}

export interface SubscriptionProtectionConfig {
    max_requests: number;
    window_seconds: number;
    ua_filter_enabled: boolean;
    social_fetchers_block_enabled: boolean;
}

export type ProtectionRuleType = 'ip_block' | 'token_block' | 'ip_allow';

export interface ProtectionRule {
    id: number;
    rule_type: ProtectionRuleType;
    value: string;
    note: string;
    created_at: number;
}

export interface CreateProtectionRuleRequest {
    rule_type: ProtectionRuleType;
    value: string;
    note: string;
}

export interface BlockedSubscriptionRequestEntry {
    id: number;
    sub_id: number;
    sub_name: string;
    request_ip: string;
    requested_at: number;
    block_reason: string;
    user_agent: string;
}

export interface CreateWireGuardInterfaceRequest {
    name: string;
    subnet: string;
    listen_port: number;
}

export interface WireGuardInterfaceSummary {
    name: string;
    address: string;
    listen_port: number;
    peer_count: number;
    is_up: boolean;
}

export interface CreateWireGuardInterfaceResponse {
    name: string;
    subnet: string;
    address: string;
    listen_port: number;
    public_key: string;
    path: string;
}

export interface SingboxDNSConfig {
    servers?: Array<Record<string, any>>;
    strategy?: string;
    independent_cache?: boolean;
    [key: string]: any;
}
export interface SingboxOutboundView {
    tag: string;
    type: string;
    server?: string;
    server_port?: number;
    domain_strategy?: string;
    domain_resolver?: string;
}

export interface SingboxOutboundDomainStrategyUpdate {
    tag: string;
    domain_strategy?: string;
}

export interface SingboxInboundUpdateResponse {
    warnings: string[];
}

export interface Subscription {
    id: number;
    token?: string;
    name: string;
    quota_limit: number;
    quota_period: string;
    used_bytes: number;
    users: string[];
    members: SubscriptionMember[];
    profile_update_interval_hours?: number | null;
    update_always?: boolean;
    last_request_at?: number | null;
    created_at: number;
    updated_at: number;
}

export interface SubscriptionMember {
    username: string;
    alias: string;
}

export interface SubscriptionMutationRequest {
    name: string;
    quota_limit: number;
    quota_period: string;
    users: string[];
    members?: SubscriptionMember[];
    profile_update_interval_hours: number | null;
    update_always: boolean;
}

export interface SubscriptionDefaults {
    profile_update_interval_hours: number | null;
    update_always: boolean;
    destinations: string[];
}

export interface SubscriptionHappParameter {
    key: string;
    value: string;
}

export interface SubscriptionHappConfig {
    provider_id: string;
    hide_settings: '' | '0' | '1';
    subscription_always_hwid_enable: '' | '0' | '1';
    subscription_auto_update_open_enable: '' | '0' | '1';
    subscription_ping_onopen_enabled: '' | '0' | '1';
    color_profile: string;
    profile_flag: string;
    advanced_parameters: SubscriptionHappParameter[];
}

export interface SubscriptionDefaultDestinationsResponse {
    destinations: string[];
}


const buildHeaders = (contentType?: string) => {
    const headers: Record<string, string> = {};
    if (contentType) headers['Content-Type'] = contentType;
    const token = localStorage.getItem('token');
    const apiKey = localStorage.getItem('api_key');
    const demoMode = localStorage.getItem('demo_mode') === '1';
    if (demoMode && apiKey) {
        headers['X-API-Key'] = apiKey;
    } else if (token) {
        headers['Authorization'] = `Bearer ${token}`;
    } else if (apiKey) {
        headers['X-API-Key'] = apiKey;
    }
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

const wireGuardInterfaceBase = (iface: string) => `/api/wireguard/interfaces/${encodeURIComponent(iface)}`;

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
    getUserInbounds: async (name: string): Promise<{ tag: string; uuid: string; password?: string; flow?: string; vmess_security?: string; vmess_alter_id?: number }[]> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/inbounds`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch user inbounds');
        return res.json();
    },
    getUserRouteTags: async (): Promise<UserRouteTag[]> => {
        const res = await fetch('/api/user-route-tags', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch user route tags');
        return res.json();
    },
    getCompatibleUserRouteRules: async (): Promise<CompatibleUserRouteRule[]> => {
        const res = await fetch('/api/user-route-tags/compatible-rules', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch compatible user route rules');
        return res.json();
    },
    createUserRouteTag: async (payload: CreateUserRouteTagRequest): Promise<UserRouteTag> => {
        const res = await fetch('/api/user-route-tags', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload)
        });
        await handleResponse(res, 'Failed to create user route tag');
        return res.json();
    },
    updateUserRouteTag: async (id: number, payload: { name: string; color?: string; description?: string; rule_index?: number }): Promise<UserRouteTag> => {
        const res = await fetch(`/api/user-route-tags/${encodeURIComponent(String(id))}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload)
        });
        await handleResponse(res, 'Failed to update user route tag');
        return res.json();
    },
    deleteUserRouteTag: async (id: number): Promise<void> => {
        const res = await fetch(`/api/user-route-tags/${encodeURIComponent(String(id))}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to delete user route tag');
    },
    updateUserRouteTags: async (name: string, tagIds: number[]): Promise<UpdateUserRouteTagsResponse> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/route-tags`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ tag_ids: tagIds })
        });
        await handleResponse(res, 'Failed to update user route tags');
        return res.json();
    },
    removeUserFromInbound: async (name: string, inboundTag: string): Promise<void> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/inbounds/${encodeURIComponent(inboundTag)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to remove user from inbound');
    },
    updateUserInbound: async (name: string, inboundTag: string, payload: { uuid: string; flow?: string; vmess_security?: string; vmess_alter_id?: number }): Promise<void> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/inbounds/${encodeURIComponent(inboundTag)}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload)
        });
        await handleResponse(res, 'Failed to update user inbound');
    },
    getUserLink: async (name: string, inboundTag: string): Promise<{ link: string; type?: string }> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/link?inbound=${encodeURIComponent(inboundTag)}`, {
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to fetch link');
        return res.json();
    },
    getUserVlessLink: async (name: string, inboundTag: string): Promise<{ link: string }> => {
        const res = await fetch(`/api/users/${encodeURIComponent(name)}/vless?inbound=${encodeURIComponent(inboundTag)}`, {
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to fetch VLESS link');
        return res.json();
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
    getSingboxDNS: async (): Promise<SingboxDNSConfig> => {
        const res = await fetch('/api/singbox/dns', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch sing-box DNS config');
        return res.json();
    },
    updateSingboxDNS: async (dns: SingboxDNSConfig): Promise<void> => {
        const res = await fetch('/api/singbox/dns', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(dns),
        });
        await handleResponse(res, 'Failed to update sing-box DNS config');
    },
    getSingboxOutbounds: async (): Promise<SingboxOutboundView[]> => {
        const res = await fetch('/api/singbox/outbounds', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch sing-box outbounds');
        return res.json();
    },
    updateSingboxOutboundDomainStrategies: async (updates: SingboxOutboundDomainStrategyUpdate[]): Promise<void> => {
        const res = await fetch('/api/singbox/outbounds/domain-strategy', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(updates),
        });
        await handleResponse(res, 'Failed to update sing-box outbound domain_strategy values');
    },
    getLogs: async (params?: { q?: string; limit?: number; signal?: AbortSignal }): Promise<{ logs: string[] }> => {
        const query = new URLSearchParams();
        if (params?.q) query.set('q', params.q);
        if (params?.limit) query.set('limit', String(params.limit));
        const url = query.toString() ? `/api/logs?${query.toString()}` : '/api/logs';
        const res = await fetch(url, { headers: buildHeaders(), signal: params?.signal });
        await handleResponse(res, 'Failed to fetch logs');
        return res.json();
    },
    searchLogs: async ({ query, limit, page, from, to, signal }: LogSearchParams): Promise<{ logs: string[]; page?: number; page_size?: number; has_more?: boolean }> => {
        const params = new URLSearchParams({ q: query });
        if (limit) params.set('limit', String(limit));
        if (page) params.set('page', String(page));
        if (from) params.set('from', from);
        if (to) params.set('to', to);
        const res = await fetch(`/api/logs/search?${params.toString()}`, { headers: buildHeaders(), signal });
        await handleResponse(res, 'Failed to search logs');
        const text = await res.text();
        try {
            return JSON.parse(text);
        } catch {
            return { logs: [text || 'Search returned non-JSON response'] };
        }
    },
    searchLogsStream: async (
        { query, limit, from, to, signal }: LogSearchParams,
        onEvent: (event: LogSearchStreamEvent) => void
    ): Promise<void> => {
        const params = new URLSearchParams({ q: query });
        if (limit) params.set('limit', String(limit));
        if (from) params.set('from', from);
        if (to) params.set('to', to);
        const res = await fetch(`/api/logs/search/stream?${params.toString()}`, { headers: buildHeaders(), signal });
        await handleResponse(res, 'Failed to search logs');
        if (!res.body) {
            throw new Error('Streaming response body missing')
        }

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        while (true) {
            const { done, value } = await reader.read()
            if (done) break
            buffer += decoder.decode(value, { stream: true })
            const lines = buffer.split('\n')
            buffer = lines.pop() ?? ''

            for (const line of lines) {
                const trimmed = line.trim()
                if (!trimmed) continue
                onEvent(JSON.parse(trimmed) as LogSearchStreamEvent)
            }
        }

        const tail = buffer.trim()
        if (tail) {
            onEvent(JSON.parse(tail) as LogSearchStreamEvent)
        }
    },

    // WireGuard
    getWireGuardInterfaces: async (): Promise<string[]> => {
        const res = await fetch('/api/wireguard/interfaces', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard interfaces');
        return res.json();
    },
    createWireGuardInterface: async (payload: CreateWireGuardInterfaceRequest): Promise<CreateWireGuardInterfaceResponse> => {
        const res = await fetch('/api/wireguard/interfaces', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload),
        });
        await handleResponse(res, 'Failed to create WireGuard interface');
        return res.json();
    },
    getWireGuardInterfacesStatus: async (): Promise<WireGuardInterfaceSummary[]> => {
        const res = await fetch('/api/wireguard/interfaces/status', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard interface status');
        return res.json();
    },
    getWireGuardPeers: async (): Promise<any[]> => {
        const res = await fetch('/api/wireguard/peers', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch peers');
        return res.json();
    },
    getWireGuardPeersForInterface: async (iface: string): Promise<any[]> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/peers`, { headers: buildHeaders() });
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
    createWireGuardPeerForInterface: async (iface: string, payload: { alias: string; ip: string; endpoint?: string }): Promise<any> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/peers`, {
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
    deleteWireGuardPeerForInterface: async (iface: string, publicKey: string): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/peers?public_key=${encodeURIComponent(publicKey)}`, {
            method: 'DELETE',
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to delete peer');
    },
    restoreWireGuardPeer: async (peer: { public_key: string; allowed_ips: string; endpoint?: string; alias?: string; preshared_key?: string }) => {
        const res = await fetch('/api/wireguard/peers/restore', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(peer)
        });
        await handleResponse(res, 'Failed to restore peer');
    },
    restoreWireGuardPeerForInterface: async (iface: string, peer: { public_key: string; allowed_ips: string; endpoint?: string; alias?: string; preshared_key?: string }) => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/peers/restore`, {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(peer)
        });
        await handleResponse(res, 'Failed to restore peer');
    },
    getWireGuardInterface: async (): Promise<any> => {
        const res = await fetch('/api/wireguard/interface', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch interface config');
        return res.json();
    },
    getWireGuardInterfaceForInterface: async (iface: string): Promise<any> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/interface`, { headers: buildHeaders() });
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
    updateWireGuardInterfaceForInterface: async (iface: string, config: any): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/interface`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(config)
        });
        await handleResponse(res, 'Failed to update interface');
    },
    getWireGuardConfigForInterface: async (iface: string): Promise<string> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/config`, {
            headers: buildHeaders()
        });
        await handleResponse(res, 'Failed to fetch WireGuard raw config');
        return res.text();
    },
    getWireGuardConfigBackups: async (): Promise<ConfigBackupEntry[]> => {
        const res = await fetch('/api/wireguard/config/backups', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard config backups');
        return res.json();
    },
    getWireGuardConfigBackupsForInterface: async (iface: string): Promise<ConfigBackupEntry[]> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/config/backups`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard config backups');
        return res.json();
    },
    getWireGuardConfigBackupContent: async (name: string): Promise<string> => {
        const res = await fetch(`/api/wireguard/config/backup?name=${encodeURIComponent(name)}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard backup');
        return res.text();
    },
    getWireGuardConfigBackupContentForInterface: async (iface: string, name: string): Promise<string> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/config/backup?name=${encodeURIComponent(name)}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch WireGuard backup');
        return res.text();
    },
    updateWireGuardConfigForInterface: async (iface: string, config: string): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/config`, {
            method: 'PUT',
            headers: buildHeaders('text/plain'),
            body: config
        });
        await handleResponse(res, 'Failed to update WireGuard raw config');
    },
    updateWireGuardPeer: async (publicKey: string, config: any): Promise<void> => {
        const res = await fetch(`/api/wireguard/peer?public_key=${encodeURIComponent(publicKey)}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(config)
        });
        await handleResponse(res, 'Failed to update peer');
    },
    updateWireGuardPeerForInterface: async (iface: string, publicKey: string, config: any): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/peer?public_key=${encodeURIComponent(publicKey)}`, {
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
    getWireGuardPeerConfigForInterface: async (iface: string, publicKey: string, privateKey?: string): Promise<{ config: string }> => {
        const params = new URLSearchParams({ public_key: publicKey })
        if (privateKey) params.set('private_key', privateKey)
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/peer/config?${params.toString()}`, {
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
    getWireGuardTrafficForInterface: async (iface: string, range: string): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ range })
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/traffic?${params.toString()}`, { headers: buildHeaders() })
        await handleResponse(res, 'Failed to fetch WireGuard traffic');
        return res.json()
    },
    getWireGuardTrafficRange: async (start: number, end: number): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ start: String(start), end: String(end) })
        const res = await fetch(`/api/wireguard/traffic?${params.toString()}`, { headers: buildHeaders() })
        await handleResponse(res, 'Failed to fetch WireGuard traffic');
        return res.json()
    },
    getWireGuardTrafficRangeForInterface: async (iface: string, start: number, end: number): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ start: String(start), end: String(end) })
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/traffic?${params.toString()}`, { headers: buildHeaders() })
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
    getWireGuardTrafficSeriesForInterface: async (iface: string, range?: string, peer?: string, limit?: number, start?: number, end?: number): Promise<Record<string, { timestamp: number; rx: number; tx: number; endpoint?: string }[]>> => {
        const params = new URLSearchParams()
        if (range) params.append('range', range)
        if (peer) params.append('peer', peer)
        if (limit) params.append('limit', String(limit))
        if (start) params.append('start', String(start))
        if (end) params.append('end', String(end))
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/traffic/series?${params.toString()}`, { headers: buildHeaders() })
        await handleResponse(res, 'Failed to fetch WireGuard traffic series');
        return res.json()
    },
    enableWireGuardInterface: async (iface: string): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/enable`, { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to enable WireGuard interface');
    },
    disableWireGuardInterface: async (iface: string): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/disable`, { method: 'POST', headers: buildHeaders() });
        await handleResponse(res, 'Failed to disable WireGuard interface');
    },
    deleteWireGuardInterface: async (iface: string): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}`, { method: 'DELETE', headers: buildHeaders() });
        await handleResponse(res, 'Failed to delete WireGuard interface');
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
    getDashboardPreferences: async (): Promise<DashboardPreferences> => {
        const res = await fetch('/api/settings/dashboard-preferences', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch dashboard preferences');
        return res.json();
    },
    updateDashboardPreferences: async (prefs: DashboardPreferences): Promise<void> => {
        const res = await fetch('/api/settings/dashboard-preferences', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(prefs),
        });
        await handleResponse(res, 'Failed to update dashboard preferences');
    },
    getPublicIP: async (): Promise<string> => {
        const res = await fetch('/api/settings/public-ip', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch public IP');
        const data = await res.json();
        return data.public_ip || '';
    },
    updatePublicIP: async (publicIP: string): Promise<void> => {
        const res = await fetch('/api/settings/public-ip', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ public_ip: publicIP })
        });
        await handleResponse(res, 'Failed to update public IP');
    },

    // Sing-box Configuration
    getSingboxRouteRules: async (): Promise<any[]> => {
        const res = await fetch('/api/singbox/route/rules', { headers: buildHeaders() });
        const handled = await handleResponse(res, 'Failed to fetch route rules');
        return handled.json();
    },
    upsertSingboxRouteRules: async (rules: any[]): Promise<void> => {
        const res = await fetch('/api/singbox/route/rules/upsert', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(rules),
        });
        await handleResponse(res, 'Failed to upsert route rules');
    },
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
    updateSingboxInbound: async (tag: string, inbound: any): Promise<SingboxInboundUpdateResponse> => {
        const res = await fetch(`/api/singbox/inbound?tag=${encodeURIComponent(tag)}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(inbound)
        });
        await handleResponse(res, 'Failed to update Sing-box inbound');
        const text = await res.text();
        if (!text.trim()) {
            return { warnings: [] };
        }
        const data = JSON.parse(text) as Partial<SingboxInboundUpdateResponse>;
        return {
            warnings: Array.isArray(data.warnings) ? data.warnings.filter(Boolean) : [],
        };
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
    backupWireGuardConfigForInterface: async (iface: string): Promise<void> => {
        const res = await fetch(`${wireGuardInterfaceBase(iface)}/config/backup`, { method: 'POST', headers: buildHeaders() });
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
    getConfigBackups: async (): Promise<ConfigBackupEntry[]> => {
        const res = await fetch('/api/config/backups', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch config backups');
        return res.json();
    },
    getConfigBackupContent: async (name: string): Promise<string> => {
        const res = await fetch(`/api/config/backup?name=${encodeURIComponent(name)}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch config backup');
        return res.text();
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
    getSamplerHistory: async (limit?: number): Promise<SamplerHistoryEntry[]> => {
        const url = limit ? `/api/sampler/history?limit=${limit}` : '/api/sampler/history';
        const res = await fetch(url, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch sampler history');
        return res.json();
    },
    getSubscriptionRequestHistory: async (limit?: number): Promise<SubscriptionRequestHistoryEntry[]> => {
        const url = limit ? `/api/subscription-requests/history?limit=${limit}` : '/api/subscription-requests/history';
        const res = await fetch(url, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscription request history');
        return res.json();
    },
    getSubscriptionRequestHistoryPage: async (limit: number = 20, offset: number = 0, subId?: number): Promise<SubscriptionRequestHistoryPage> => {
        const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
        if (subId && subId > 0) params.set('sub_id', String(subId));
        const res = await fetch(`/api/subscription-requests/history?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscription request history');
        return res.json();
    },
    getSubscriptionProtection: async (): Promise<SubscriptionProtectionConfig> => {
        const res = await fetch('/api/settings/subscription-protection', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscription protection settings');
        return res.json();
    },
    updateSubscriptionProtection: async (payload: Partial<SubscriptionProtectionConfig>): Promise<void> => {
        const res = await fetch('/api/settings/subscription-protection', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload),
        });
        await handleResponse(res, 'Failed to update subscription protection settings');
    },
    getProtectionRules: async (): Promise<ProtectionRule[]> => {
        const res = await fetch('/api/settings/protection-rules', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch protection rules');
        return res.json();
    },
    createProtectionRule: async (payload: CreateProtectionRuleRequest): Promise<void> => {
        const res = await fetch('/api/settings/protection-rules', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload),
        });
        await handleResponse(res, 'Failed to create protection rule');
    },
    deleteProtectionRule: async (id: number): Promise<void> => {
        const res = await fetch(`/api/settings/protection-rules/${id}`, {
            method: 'DELETE',
            headers: buildHeaders(),
        });
        await handleResponse(res, 'Failed to delete protection rule');
    },
    getBlockedSubscriptionRequestLog: async (limit: number, offset: number): Promise<BlockedSubscriptionRequestEntry[]> => {
        const params = new URLSearchParams({
            limit: String(limit),
            offset: String(offset),
        });
        const res = await fetch(`/api/settings/protection-rules/blocked-log?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch blocked request log');
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
    getDashboardConsumerChart: async (
        mode: 'singbox' | 'wireguard',
        key: string,
        name?: string,
        interfaceName?: string,
        range: string = '24h',
        start?: string,
        end?: string,
        targetPoints?: number,
    ): Promise<DashboardConsumerChartData> => {
        const params = new URLSearchParams({ mode, key, range });
        if (name) params.append('name', name);
        if (interfaceName) params.append('interface_name', interfaceName);
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        if (targetPoints && targetPoints > 0) params.append('target_points', String(targetPoints));
        const res = await fetch(`/api/dashboard/consumer-chart?${params.toString()}`, { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch consumer chart');
        return res.json();
    },
    generateRealityKeys: async (): Promise<{ private_key: string; public_key: string; short_id: string[] }> => {
        const res = await fetch('/api/tools/reality-keys', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to generate Reality keys');
        return res.json();
    },
    generateSelfSignedCert: async (payload: { tag?: string; common_name?: string }): Promise<{ cert_path: string; key_path: string }> => {
        const res = await fetch('/api/tools/self-signed-cert', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(payload)
        });
        await handleResponse(res, 'Failed to generate self-signed certificate');
        return res.json();
    },
    generateRandBase64: async (keyLength: number): Promise<{ value: string }> => {
        const res = await fetch('/api/tools/rand-base64', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ key_length: keyLength })
        });
        await handleResponse(res, 'Failed to generate random base64');
        return res.json();
    },
    applySingboxChanges: async (): Promise<ApplySingboxChangesResponse> => {
        const res = await fetch('/api/singbox/apply', {
            method: 'POST',
            headers: buildHeaders('application/json')
        });
        if (res.status === 409) {
            return res.json();
        }
        await handleResponse(res, 'Failed to apply Sing-box changes');
        return res.json();
    },

    // Panel user management
    getPanelUsers: async (): Promise<PanelUserInfo[]> => {
        const res = await fetch('/api/panel-users', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch panel users');
        return res.json();
    },
    createPanelUser: async (data: CreatePanelUserRequest): Promise<void> => {
        const res = await fetch('/api/panel-users', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(data),
        });
        await handleResponse(res, 'Failed to create panel user');
    },
    updatePanelUserPermissions: async (username: string, permissions: PanelUserPermissions): Promise<void> => {
        const res = await fetch('/api/panel-users/permissions', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ username, permissions }),
        });
        await handleResponse(res, 'Failed to update permissions');
    },
    updatePanelUserUsername: async (username: string, new_username: string): Promise<void> => {
        const res = await fetch('/api/panel-users/username', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ username, new_username }),
        });
        await handleResponse(res, 'Failed to update username');
    },
    updatePanelUserPassword: async (username: string, new_password: string): Promise<void> => {
        const res = await fetch('/api/panel-users/password', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ username, new_password }),
        });
        await handleResponse(res, 'Failed to update password');
    },
    deletePanelUser: async (username: string): Promise<void> => {
        const res = await fetch(`/api/panel-users?username=${encodeURIComponent(username)}`, {
            method: 'DELETE',
            headers: buildHeaders(),
        });
        await handleResponse(res, 'Failed to delete panel user');
    },

    // Subscriptions
    getSubscriptions: async (): Promise<Subscription[]> => {
        const res = await fetch('/api/subscriptions', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscriptions');
        return res.json();
    },
    getSubscriptionDefaults: async (): Promise<SubscriptionDefaults> => {
        const res = await fetch('/api/subscriptions/defaults', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscription defaults');
        return res.json();
    },
    updateSubscriptionDefaults: async (data: SubscriptionDefaults): Promise<SubscriptionDefaults> => {
        const res = await fetch('/api/subscriptions/defaults', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(data),
        });
        await handleResponse(res, 'Failed to update subscription defaults');
        return res.json();
    },
    getSubscriptionDefaultDestinations: async (): Promise<SubscriptionDefaultDestinationsResponse> => {
        const res = await fetch('/api/subscriptions/default-destinations', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscription destination suggestions');
        return res.json();
    },
    getSubscriptionHappConfig: async (): Promise<SubscriptionHappConfig> => {
        const res = await fetch('/api/subscriptions/happ-config', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch Happ config');
        return res.json();
    },
    updateSubscriptionHappConfig: async (data: SubscriptionHappConfig): Promise<SubscriptionHappConfig> => {
        const res = await fetch('/api/subscriptions/happ-config', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(data),
        });
        await handleResponse(res, 'Failed to update Happ config');
        return res.json();
    },
    encryptHappLink: async (url: string): Promise<{ encrypted_url: string }> => {
        const res = await fetch('/api/happ/encrypt-link', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ url }),
        });
        await handleResponse(res, 'Failed to encrypt Happ link');
        return res.json();
    },
    createSubscription: async (data: SubscriptionMutationRequest): Promise<{ id: number; token: string }> => {
        const res = await fetch('/api/subscriptions', {
            method: 'POST',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(data),
        });
        await handleResponse(res, 'Failed to create subscription');
        return res.json();
    },
    updateSubscription: async (id: number, data: SubscriptionMutationRequest): Promise<void> => {
        const res = await fetch(`/api/subscriptions/${id}`, {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify(data),
        });
        await handleResponse(res, 'Failed to update subscription');
    },
    deleteSubscription: async (id: number): Promise<void> => {
        const res = await fetch(`/api/subscriptions/${id}`, {
            method: 'DELETE',
            headers: buildHeaders(),
        });
        await handleResponse(res, 'Failed to delete subscription');
    },
    regenerateSubscriptionToken: async (id: number): Promise<{ token: string }> => {
        const res = await fetch(`/api/subscriptions/${id}/regenerate`, {
            method: 'POST',
            headers: buildHeaders(),
        });
        await handleResponse(res, 'Failed to regenerate token');
        return res.json();
    },
    getSubscriptionDomain: async (): Promise<string> => {
        const res = await fetch('/api/settings/subscription-domain', { headers: buildHeaders() });
        await handleResponse(res, 'Failed to fetch subscription domain');
        const data = await res.json();
        return data.subscription_domain || '';
    },
    updateSubscriptionDomain: async (domain: string): Promise<void> => {
        const res = await fetch('/api/settings/subscription-domain', {
            method: 'PUT',
            headers: buildHeaders('application/json'),
            body: JSON.stringify({ subscription_domain: domain }),
        });
        await handleResponse(res, 'Failed to update subscription domain');
    },
};
