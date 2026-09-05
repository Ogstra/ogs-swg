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
    external_profiles?: ExternalProfile[];
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

export interface ExternalProfile {
    id: number;
    name: string;
    flag: string;
    type: 'vless' | 'shadowsocks';
    host_ipv4: string;
    host_ipv6_file: string;
    port: number;
    uuid: string;
    password: string;
    ss_method: string;
    ss_server_key: string;
    public_key: string;
    short_id: string;
    server_name: string;
    alpn: string;
    flow: string;
    enabled: boolean;
    position: number;
    created_at: number;
    updated_at: number;
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
    audit_log_max_mb?: number;
    access_log_path?: string;
    systemctl_available?: boolean;
    journalctl_available?: boolean;
    log_retention_mode?: 'size' | 'time';
    log_retention_mb?: number;
    log_retention_target_percent?: number;
    log_retention_max_export_percent?: number;
    log_retention_days?: number;
    log_retention_unit?: 'days' | 'weeks' | 'months';
    log_cold_dir?: string;
    db_backup_path?: string;
    db_backup_interval_hours?: number;
}

export interface LogStoreStats {
    size_bytes: number;
    row_count: number;
    oldest_ts: number;
    newest_ts: number;
    segment_count: number;
    segment_total_bytes: number;
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
    via_worker: number;
}

export interface SubscriptionRequestHistoryPage {
    items: SubscriptionRequestHistoryEntry[];
    has_more: boolean;
    next_offset: number;
}

export interface AuditEntry {
    id: number
    ts: number
    actor: string
    ip: string
    action: string
    domain: string
    entity_id: string
    detail: string
}

export interface AuditLogPage {
    items: AuditEntry[]
    next_offset: number
    has_more: boolean
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
    alias: string;
    happ_routing_profile?: string;
    happ_color_profile?: string;
    happ_direct_sites?: string;
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
    alias?: string;
    quota_limit: number;
    quota_period: string;
    users: string[];
    members?: SubscriptionMember[];
    profile_update_interval_hours: number | null;
    update_always: boolean;
    happ_routing_profile?: string;
    happ_color_profile?: string;
    happ_direct_sites?: string;
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
    routing_profile?: string;
    direct_sites?: string[];
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

const validateRawSingboxConfig = (config: string) => {
    if (!config.trim()) {
        throw new Error('Config cannot be empty');
    }
};

type ParseMode = 'json' | 'text' | 'none' | 'raw';
type ErrorMode = 'handled' | 'plain' | 'status';

interface RequestOptions {
    method?: string;
    json?: unknown;
    body?: BodyInit;
    contentType?: string;
    signal?: AbortSignal;
    errorMsg?: string;
    parse?: ParseMode;
    errorMode?: ErrorMode;
    allowStatus?: number[];
}

async function request<T = void>(path: string, options: RequestOptions = {}): Promise<T> {
    const {
        method = 'GET',
        json,
        body,
        contentType,
        signal,
        errorMsg = 'Request failed',
        parse = 'json',
        errorMode = 'handled',
        allowStatus,
    } = options;

    const headerContentType = contentType ?? (json !== undefined ? 'application/json' : undefined);
    const init: RequestInit = { method, headers: buildHeaders(headerContentType) };
    if (json !== undefined) {
        init.body = JSON.stringify(json);
    } else if (body !== undefined) {
        init.body = body;
    }
    if (signal) init.signal = signal;

    const res = await fetch(path, init);

    if (!allowStatus || !allowStatus.includes(res.status)) {
        if (errorMode === 'handled') {
            await handleResponse(res, errorMsg);
        } else if (errorMode === 'plain') {
            if (!res.ok) throw new Error(errorMsg);
        } else if (!res.ok) {
            throw new Error(`${errorMsg}: ${res.status}`);
        }
    }

    if (parse === 'raw') return res as unknown as T;
    if (parse === 'none') return undefined as T;
    if (parse === 'text') return (await res.text()) as unknown as T;
    return (await res.json()) as T;
}

export const api = {
    getUsers: async (): Promise<UserStatus[]> =>
        request<UserStatus[]>('/api/users', { errorMode: 'plain', errorMsg: 'Failed to fetch users' }),
    pauseSampler: async (): Promise<void> =>
        request('/api/sampler/pause', { method: 'POST', parse: 'none', errorMsg: 'Failed to pause sampler' }),
    resumeSampler: async (): Promise<void> =>
        request('/api/sampler/resume', { method: 'POST', parse: 'none', errorMsg: 'Failed to resume sampler' }),
    updatePassword: async (currentPassword: string, newPassword: string): Promise<void> =>
        request('/api/auth/password', {
            method: 'PUT',
            json: { current_password: currentPassword, new_password: newPassword },
            parse: 'none',
            errorMsg: 'Failed to update password',
        }),
    updateUsername: async (currentPassword: string, newUsername: string): Promise<void> =>
        request('/api/auth/username', {
            method: 'PUT',
            json: { current_password: currentPassword, new_username: newUsername },
            parse: 'none',
            errorMsg: 'Failed to update username',
        }),
    createUser: async (user: CreateUserRequest): Promise<void> =>
        request('/api/users', { method: 'POST', json: user, parse: 'none', errorMsg: 'Failed to create user' }),
    updateUser: async (user: CreateUserRequest): Promise<void> =>
        request('/api/users', { method: 'PUT', json: user, parse: 'none', errorMsg: 'Failed to update user' }),
    deleteUser: async (name: string): Promise<void> =>
        request(`/api/users?name=${encodeURIComponent(name)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete user' }),
    getUserInbounds: async (name: string): Promise<{ tag: string; uuid: string; password?: string; flow?: string; vmess_security?: string; vmess_alter_id?: number }[]> =>
        request(`/api/users/${encodeURIComponent(name)}/inbounds`, { errorMsg: 'Failed to fetch user inbounds' }),
    getUserRouteTags: async (): Promise<UserRouteTag[]> =>
        request<UserRouteTag[]>('/api/user-route-tags', { errorMsg: 'Failed to fetch user route tags' }),
    getCompatibleUserRouteRules: async (): Promise<CompatibleUserRouteRule[]> =>
        request<CompatibleUserRouteRule[]>('/api/user-route-tags/compatible-rules', { errorMsg: 'Failed to fetch compatible user route rules' }),
    createUserRouteTag: async (payload: CreateUserRouteTagRequest): Promise<UserRouteTag> =>
        request<UserRouteTag>('/api/user-route-tags', { method: 'POST', json: payload, errorMsg: 'Failed to create user route tag' }),
    updateUserRouteTag: async (id: number, payload: { name: string; color?: string; description?: string; rule_index?: number }): Promise<UserRouteTag> =>
        request<UserRouteTag>(`/api/user-route-tags/${encodeURIComponent(String(id))}`, { method: 'PUT', json: payload, errorMsg: 'Failed to update user route tag' }),
    deleteUserRouteTag: async (id: number): Promise<void> =>
        request(`/api/user-route-tags/${encodeURIComponent(String(id))}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete user route tag' }),
    updateUserRouteTags: async (name: string, tagIds: number[]): Promise<UpdateUserRouteTagsResponse> =>
        request<UpdateUserRouteTagsResponse>(`/api/users/${encodeURIComponent(name)}/route-tags`, { method: 'PUT', json: { tag_ids: tagIds }, errorMsg: 'Failed to update user route tags' }),
    removeUserFromInbound: async (name: string, inboundTag: string): Promise<void> =>
        request(`/api/users/${encodeURIComponent(name)}/inbounds/${encodeURIComponent(inboundTag)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to remove user from inbound' }),
    updateUserInbound: async (name: string, inboundTag: string, payload: { uuid: string; flow?: string; vmess_security?: string; vmess_alter_id?: number }): Promise<void> =>
        request(`/api/users/${encodeURIComponent(name)}/inbounds/${encodeURIComponent(inboundTag)}`, { method: 'PUT', json: payload, parse: 'none', errorMsg: 'Failed to update user inbound' }),
    getUserLink: async (name: string, inboundTag: string): Promise<{ link: string; type?: string }> =>
        request(`/api/users/${encodeURIComponent(name)}/link?inbound=${encodeURIComponent(inboundTag)}`, { errorMsg: 'Failed to fetch link' }),
    getUserVlessLink: async (name: string, inboundTag: string): Promise<{ link: string }> =>
        request(`/api/users/${encodeURIComponent(name)}/vless?inbound=${encodeURIComponent(inboundTag)}`, { errorMsg: 'Failed to fetch VLESS link' }),
    getExternalProfiles: async (): Promise<ExternalProfile[]> =>
        request<ExternalProfile[]>('/api/external-profiles', { errorMsg: 'Failed to fetch external profiles' }),
    upsertExternalProfile: async (profile: Partial<ExternalProfile>): Promise<{ id: number }> =>
        request<{ id: number }>('/api/external-profiles', { method: 'POST', json: profile, errorMsg: 'Failed to save external profile' }),
    deleteExternalProfile: async (id: number): Promise<void> =>
        request(`/api/external-profiles/${id}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete external profile' }),
    updateUserExternalProfiles: async (name: string, profileIds: number[]): Promise<void> =>
        request(`/api/users/${encodeURIComponent(name)}/external-profiles`, { method: 'PUT', json: { profile_ids: profileIds }, parse: 'none', errorMsg: 'Failed to update user external profiles' }),
    getExternalProfileLink: async (id: number, displayName: string): Promise<{ link: string; type: string }> =>
        request<{ link: string; type: string }>(`/api/external-profiles/${id}/link?name=${encodeURIComponent(displayName)}`, { errorMsg: 'Failed to fetch external profile link' }),
    bulkCreateUsers: async (users: CreateUserRequest[]): Promise<void> =>
        request('/api/users/bulk', { method: 'POST', json: users, parse: 'none', errorMsg: 'Failed to bulk create users' }),
    getReport: async (start?: string, end?: string): Promise<UserStatus[]> => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        return request<UserStatus[]>(`/api/report?${params.toString()}`, { errorMsg: 'Failed to fetch report' });
    },
    getReportSummary: async (start?: string, end?: string, limitBytes?: number): Promise<any[]> => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        if (limitBytes) params.append('limit_bytes', limitBytes.toString());
        return request<any[]>(`/api/report/summary?${params.toString()}`, { errorMsg: 'Failed to fetch report summary' });
    },
    getConfig: async (): Promise<any> =>
        request<any>('/api/config', { errorMsg: 'Failed to fetch config' }),
    getSingboxDNS: async (): Promise<SingboxDNSConfig> =>
        request<SingboxDNSConfig>('/api/singbox/dns', { errorMsg: 'Failed to fetch sing-box DNS config' }),
    updateSingboxDNS: async (dns: SingboxDNSConfig): Promise<void> =>
        request('/api/singbox/dns', { method: 'PUT', json: dns, parse: 'none', errorMsg: 'Failed to update sing-box DNS config' }),
    getSingboxOutbounds: async (): Promise<SingboxOutboundView[]> =>
        request<SingboxOutboundView[]>('/api/singbox/outbounds', { errorMsg: 'Failed to fetch sing-box outbounds' }),
    updateSingboxOutboundDomainStrategies: async (updates: SingboxOutboundDomainStrategyUpdate[]): Promise<void> =>
        request('/api/singbox/outbounds/domain-strategy', { method: 'PUT', json: updates, parse: 'none', errorMsg: 'Failed to update sing-box outbound domain_strategy values' }),
    getLogs: async (params?: { q?: string; limit?: number; after_id?: number; signal?: AbortSignal }): Promise<{ logs: string[]; max_id?: number }> => {
        const query = new URLSearchParams();
        if (params?.q) query.set('q', params.q);
        if (params?.limit) query.set('limit', String(params.limit));
        if (params?.after_id && params.after_id > 0) query.set('after_id', String(params.after_id));
        const url = query.toString() ? `/api/logs?${query.toString()}` : '/api/logs';
        return request<{ logs: string[]; max_id?: number }>(url, { signal: params?.signal, errorMsg: 'Failed to fetch logs' });
    },
    searchLogs: async ({ query, limit, page, from, to, signal }: LogSearchParams): Promise<{ logs: string[]; page?: number; page_size?: number; has_more?: boolean }> => {
        const params = new URLSearchParams({ q: query });
        if (limit) params.set('limit', String(limit));
        if (page) params.set('page', String(page));
        if (from) params.set('from', from);
        if (to) params.set('to', to);
        const text = await request<string>(`/api/logs/search?${params.toString()}`, { signal, parse: 'text', errorMsg: 'Failed to search logs' });
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
        const res = await request<Response>(`/api/logs/search/stream?${params.toString()}`, { signal, parse: 'raw', errorMsg: 'Failed to search logs' });
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
    getWireGuardInterfaces: async (): Promise<string[]> =>
        request<string[]>('/api/wireguard/interfaces', { errorMsg: 'Failed to fetch WireGuard interfaces' }),
    createWireGuardInterface: async (payload: CreateWireGuardInterfaceRequest): Promise<CreateWireGuardInterfaceResponse> =>
        request<CreateWireGuardInterfaceResponse>('/api/wireguard/interfaces', { method: 'POST', json: payload, errorMsg: 'Failed to create WireGuard interface' }),
    getWireGuardInterfacesStatus: async (): Promise<WireGuardInterfaceSummary[]> =>
        request<WireGuardInterfaceSummary[]>('/api/wireguard/interfaces/status', { errorMsg: 'Failed to fetch WireGuard interface status' }),
    getWireGuardPeers: async (): Promise<any[]> =>
        request<any[]>('/api/wireguard/peers', { errorMsg: 'Failed to fetch peers' }),
    getWireGuardPeersForInterface: async (iface: string): Promise<any[]> =>
        request<any[]>(`${wireGuardInterfaceBase(iface)}/peers`, { errorMsg: 'Failed to fetch peers' }),
    createWireGuardPeer: async (payload: { alias: string; ip: string; endpoint?: string }): Promise<any> =>
        request<any>('/api/wireguard/peers', { method: 'POST', json: payload, errorMsg: 'Failed to create peer' }),
    createWireGuardPeerForInterface: async (iface: string, payload: { alias: string; ip: string; endpoint?: string }): Promise<any> =>
        request<any>(`${wireGuardInterfaceBase(iface)}/peers`, { method: 'POST', json: payload, errorMsg: 'Failed to create peer' }),
    deleteWireGuardPeer: async (publicKey: string): Promise<void> =>
        request(`/api/wireguard/peers?public_key=${encodeURIComponent(publicKey)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete peer' }),
    deleteWireGuardPeerForInterface: async (iface: string, publicKey: string): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/peers?public_key=${encodeURIComponent(publicKey)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete peer' }),
    restoreWireGuardPeer: async (peer: { public_key: string; allowed_ips: string; endpoint?: string; alias?: string; preshared_key?: string }) =>
        request('/api/wireguard/peers/restore', { method: 'POST', json: peer, parse: 'none', errorMsg: 'Failed to restore peer' }),
    restoreWireGuardPeerForInterface: async (iface: string, peer: { public_key: string; allowed_ips: string; endpoint?: string; alias?: string; preshared_key?: string }) =>
        request(`${wireGuardInterfaceBase(iface)}/peers/restore`, { method: 'POST', json: peer, parse: 'none', errorMsg: 'Failed to restore peer' }),
    getWireGuardInterface: async (): Promise<any> =>
        request<any>('/api/wireguard/interface', { errorMsg: 'Failed to fetch interface config' }),
    getWireGuardInterfaceForInterface: async (iface: string): Promise<any> =>
        request<any>(`${wireGuardInterfaceBase(iface)}/interface`, { errorMsg: 'Failed to fetch interface config' }),
    updateWireGuardInterface: async (config: any): Promise<void> =>
        request('/api/wireguard/interface', { method: 'PUT', json: config, parse: 'none', errorMsg: 'Failed to update interface' }),
    updateWireGuardInterfaceForInterface: async (iface: string, config: any): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/interface`, { method: 'PUT', json: config, parse: 'none', errorMsg: 'Failed to update interface' }),
    getWireGuardConfigForInterface: async (iface: string): Promise<string> =>
        request<string>(`${wireGuardInterfaceBase(iface)}/config`, { parse: 'text', errorMsg: 'Failed to fetch WireGuard raw config' }),
    getWireGuardConfigBackups: async (): Promise<ConfigBackupEntry[]> =>
        request<ConfigBackupEntry[]>('/api/wireguard/config/backups', { errorMsg: 'Failed to fetch WireGuard config backups' }),
    getWireGuardConfigBackupsForInterface: async (iface: string): Promise<ConfigBackupEntry[]> =>
        request<ConfigBackupEntry[]>(`${wireGuardInterfaceBase(iface)}/config/backups`, { errorMsg: 'Failed to fetch WireGuard config backups' }),
    getWireGuardConfigBackupContent: async (name: string): Promise<string> =>
        request<string>(`/api/wireguard/config/backup?name=${encodeURIComponent(name)}`, { parse: 'text', errorMsg: 'Failed to fetch WireGuard backup' }),
    getWireGuardConfigBackupContentForInterface: async (iface: string, name: string): Promise<string> =>
        request<string>(`${wireGuardInterfaceBase(iface)}/config/backup?name=${encodeURIComponent(name)}`, { parse: 'text', errorMsg: 'Failed to fetch WireGuard backup' }),
    updateWireGuardConfigForInterface: async (iface: string, config: string): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/config`, { method: 'PUT', body: config, contentType: 'text/plain', parse: 'none', errorMsg: 'Failed to update WireGuard raw config' }),
    updateWireGuardPeer: async (publicKey: string, config: any): Promise<void> =>
        request(`/api/wireguard/peer?public_key=${encodeURIComponent(publicKey)}`, { method: 'PUT', json: config, parse: 'none', errorMsg: 'Failed to update peer' }),
    updateWireGuardPeerForInterface: async (iface: string, publicKey: string, config: any): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/peer?public_key=${encodeURIComponent(publicKey)}`, { method: 'PUT', json: config, parse: 'none', errorMsg: 'Failed to update peer' }),
    getWireGuardPeerConfig: async (publicKey: string, privateKey?: string): Promise<{ config: string }> => {
        const params = new URLSearchParams({ public_key: publicKey })
        if (privateKey) params.set('private_key', privateKey)
        return request<{ config: string }>(`/api/wireguard/peer/config?${params.toString()}`, { errorMsg: 'Failed to fetch peer config' });
    },
    getWireGuardPeerConfigForInterface: async (iface: string, publicKey: string, privateKey?: string): Promise<{ config: string }> => {
        const params = new URLSearchParams({ public_key: publicKey })
        if (privateKey) params.set('private_key', privateKey)
        return request<{ config: string }>(`${wireGuardInterfaceBase(iface)}/peer/config?${params.toString()}`, { errorMsg: 'Failed to fetch peer config' });
    },
    getWireGuardTraffic: async (range: string): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ range })
        return request<Record<string, { rx: number; tx: number }>>(`/api/wireguard/traffic?${params.toString()}`, { errorMsg: 'Failed to fetch WireGuard traffic' })
    },
    getWireGuardTrafficForInterface: async (iface: string, range: string): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ range })
        return request<Record<string, { rx: number; tx: number }>>(`${wireGuardInterfaceBase(iface)}/traffic?${params.toString()}`, { errorMsg: 'Failed to fetch WireGuard traffic' })
    },
    getWireGuardTrafficRange: async (start: number, end: number): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ start: String(start), end: String(end) })
        return request<Record<string, { rx: number; tx: number }>>(`/api/wireguard/traffic?${params.toString()}`, { errorMsg: 'Failed to fetch WireGuard traffic' })
    },
    getWireGuardTrafficRangeForInterface: async (iface: string, start: number, end: number): Promise<Record<string, { rx: number; tx: number }>> => {
        const params = new URLSearchParams({ start: String(start), end: String(end) })
        return request<Record<string, { rx: number; tx: number }>>(`${wireGuardInterfaceBase(iface)}/traffic?${params.toString()}`, { errorMsg: 'Failed to fetch WireGuard traffic' })
    },
    getWireGuardTrafficSeries: async (range?: string, peer?: string, limit?: number, start?: number, end?: number): Promise<Record<string, { timestamp: number; rx: number; tx: number; endpoint?: string }[]>> => {
        const params = new URLSearchParams()
        if (range) params.append('range', range)
        if (peer) params.append('peer', peer)
        if (limit) params.append('limit', String(limit))
        if (start) params.append('start', String(start))
        if (end) params.append('end', String(end))
        return request<Record<string, { timestamp: number; rx: number; tx: number; endpoint?: string }[]>>(`/api/wireguard/traffic/series?${params.toString()}`, { errorMsg: 'Failed to fetch WireGuard traffic series' })
    },
    getWireGuardTrafficSeriesForInterface: async (iface: string, range?: string, peer?: string, limit?: number, start?: number, end?: number): Promise<Record<string, { timestamp: number; rx: number; tx: number; endpoint?: string }[]>> => {
        const params = new URLSearchParams()
        if (range) params.append('range', range)
        if (peer) params.append('peer', peer)
        if (limit) params.append('limit', String(limit))
        if (start) params.append('start', String(start))
        if (end) params.append('end', String(end))
        return request<Record<string, { timestamp: number; rx: number; tx: number; endpoint?: string }[]>>(`${wireGuardInterfaceBase(iface)}/traffic/series?${params.toString()}`, { errorMsg: 'Failed to fetch WireGuard traffic series' })
    },
    enableWireGuardInterface: async (iface: string): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/enable`, { method: 'POST', parse: 'none', errorMsg: 'Failed to enable WireGuard interface' }),
    disableWireGuardInterface: async (iface: string): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/disable`, { method: 'POST', parse: 'none', errorMsg: 'Failed to disable WireGuard interface' }),
    deleteWireGuardInterface: async (iface: string): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete WireGuard interface' }),

    // Service Control
    restartService: async (service: string): Promise<void> =>
        request('/api/service/restart', { method: 'POST', json: { service }, parse: 'none', errorMsg: 'Failed to restart service' }),
    startService: async (service: string): Promise<void> =>
        request('/api/service/start', { method: 'POST', json: { service }, parse: 'none', errorMsg: 'Failed to start service' }),
    stopService: async (service: string): Promise<void> =>
        request('/api/service/stop', { method: 'POST', json: { service }, parse: 'none', errorMsg: 'Failed to stop service' }),

    // Feature toggles
    getFeatures: async (): Promise<FeatureFlags> =>
        request<FeatureFlags>('/api/settings/features', { errorMsg: 'Failed to fetch features' }),
    updateFeatures: async (flags: FeatureFlags): Promise<void> =>
        request('/api/settings/features', { method: 'PUT', json: flags, parse: 'none', errorMsg: 'Failed to update features' }),
    getDashboardPreferences: async (): Promise<DashboardPreferences> =>
        request<DashboardPreferences>('/api/settings/dashboard-preferences', { errorMsg: 'Failed to fetch dashboard preferences' }),
    updateDashboardPreferences: async (prefs: DashboardPreferences): Promise<void> =>
        request('/api/settings/dashboard-preferences', { method: 'PUT', json: prefs, parse: 'none', errorMsg: 'Failed to update dashboard preferences' }),
    getPublicIP: async (): Promise<string> => {
        const data = await request<{ public_ip?: string }>('/api/settings/public-ip', { errorMsg: 'Failed to fetch public IP' });
        return data.public_ip || '';
    },
    updatePublicIP: async (publicIP: string): Promise<void> =>
        request('/api/settings/public-ip', { method: 'PUT', json: { public_ip: publicIP }, parse: 'none', errorMsg: 'Failed to update public IP' }),

    // Sing-box Configuration
    getSingboxRouteRules: async (): Promise<any[]> =>
        request<any[]>('/api/singbox/route/rules', { errorMsg: 'Failed to fetch route rules' }),
    upsertSingboxRouteRules: async (rules: any[]): Promise<void> =>
        request('/api/singbox/route/rules/upsert', { method: 'POST', json: rules, parse: 'none', errorMsg: 'Failed to upsert route rules' }),
    getSingboxConfig: async (): Promise<string> =>
        request<string>('/api/singbox/config', { parse: 'text', errorMsg: 'Failed to fetch Sing-box config' }),
    updateSingboxConfig: async (config: string): Promise<void> => {
        validateRawSingboxConfig(config);
        return request('/api/singbox/config', {
            method: 'PUT',
            body: config,
            contentType: 'application/json',
            parse: 'none',
            errorMsg: 'Failed to update Sing-box config',
        });
    },
    getSingboxInbounds: async (): Promise<any[]> =>
        request<any[]>('/api/singbox/inbounds', { errorMsg: 'Failed to fetch Sing-box inbounds' }),
    addSingboxInbound: async (inbound: any): Promise<void> =>
        request('/api/singbox/inbound', { method: 'POST', json: inbound, parse: 'none', errorMsg: 'Failed to add Sing-box inbound' }),
    updateSingboxInbound: async (tag: string, inbound: any): Promise<SingboxInboundUpdateResponse> => {
        const text = await request<string>(`/api/singbox/inbound?tag=${encodeURIComponent(tag)}`, {
            method: 'PUT',
            json: inbound,
            parse: 'text',
            errorMsg: 'Failed to update Sing-box inbound',
        });
        if (!text.trim()) {
            return { warnings: [] };
        }
        const data = JSON.parse(text) as Partial<SingboxInboundUpdateResponse>;
        return {
            warnings: Array.isArray(data.warnings) ? data.warnings.filter(Boolean) : [],
        };
    },
    deleteSingboxInbound: async (tag: string): Promise<void> =>
        request(`/api/singbox/inbound?tag=${encodeURIComponent(tag)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete Sing-box inbound' }),

    // Raw Config
    updateConfig: async (configText: string): Promise<void> =>
        request('/api/config', { method: 'PUT', body: configText, contentType: 'text/plain', parse: 'none', errorMsg: 'Failed to update config' }),
    getWireGuardConfig: async (): Promise<string> =>
        request<string>('/api/wireguard/config', { parse: 'text', errorMsg: 'Failed to fetch WireGuard config' }),
    backupWireGuardConfig: async (): Promise<void> =>
        request('/api/wireguard/config/backup', { method: 'POST', parse: 'none', errorMsg: 'Failed to backup WireGuard config' }),
    backupWireGuardConfigForInterface: async (iface: string): Promise<void> =>
        request(`${wireGuardInterfaceBase(iface)}/config/backup`, { method: 'POST', parse: 'none', errorMsg: 'Failed to backup WireGuard config' }),
    restoreWireGuardConfig: async (): Promise<string> =>
        request<string>('/api/wireguard/config/restore', { method: 'POST', parse: 'text', errorMsg: 'Failed to restore WireGuard config' }),
    getBackupMeta: async (): Promise<{ singbox_last_backup?: string; wireguard_last_backup?: string }> =>
        request('/api/config/backup/meta', { errorMsg: 'Failed to load backup metadata' }),
    getConfigBackups: async (): Promise<ConfigBackupEntry[]> =>
        request<ConfigBackupEntry[]>('/api/config/backups', { errorMsg: 'Failed to fetch config backups' }),
    getConfigBackupContent: async (name: string): Promise<string> =>
        request<string>(`/api/config/backup?name=${encodeURIComponent(name)}`, { parse: 'text', errorMsg: 'Failed to fetch config backup' }),
    updateWireGuardConfig: async (config: string): Promise<void> =>
        request('/api/wireguard/config', { method: 'PUT', body: config, contentType: 'text/plain', parse: 'none', errorMsg: 'Failed to update WireGuard config' }),

    // Stats & Status
    getStats: async (range: string = '24h', start?: string, end?: string): Promise<any[]> => {
        let url = `/api/stats?range=${range}`;
        if (start && end) {
            url += `&start=${start}&end=${end}`;
        }
        return request<any[]>(url, { errorMsg: 'Failed to fetch stats' });
    },
    getSystemStatus: async (): Promise<{ singbox: boolean; wireguard: boolean; wireguard_pending_restart?: boolean; wg_sample_interval_sec?: number; active_users_singbox: number; active_users_wireguard: number; active_users_singbox_list?: string[]; active_users_wireguard_list?: string[]; singbox_sys_stats?: any; samples_count?: number; db_size_bytes?: number; audit_log_size_bytes?: number; sampler_paused?: boolean; systemctl_available?: boolean; journalctl_available?: boolean }> =>
        request('/api/status', { errorMsg: 'Failed to fetch system status' }),

    // Sampler
    runSampler: async (): Promise<void> =>
        request('/api/sampler/run', { method: 'POST', parse: 'none', errorMsg: 'Failed to run sampler' }),
    getSamplerHistory: async (limit?: number, offset?: number): Promise<SamplerHistoryEntry[]> => {
        const params = new URLSearchParams()
        if (typeof limit === 'number') params.set('limit', String(limit))
        if (typeof offset === 'number' && offset > 0) params.set('offset', String(offset))
        const qs = params.toString()
        const url = qs ? `/api/sampler/history?${qs}` : '/api/sampler/history'
        return request<SamplerHistoryEntry[]>(url, { errorMsg: 'Failed to fetch sampler history' });
    },
    getSubscriptionRequestHistory: async (limit?: number): Promise<SubscriptionRequestHistoryEntry[]> => {
        const url = limit ? `/api/subscription-requests/history?limit=${limit}` : '/api/subscription-requests/history';
        return request<SubscriptionRequestHistoryEntry[]>(url, { errorMsg: 'Failed to fetch subscription request history' });
    },
    getSubscriptionRequestHistoryPage: async (limit: number = 20, offset: number = 0, subId?: number): Promise<SubscriptionRequestHistoryPage> => {
        const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
        if (subId && subId > 0) params.set('sub_id', String(subId));
        return request<SubscriptionRequestHistoryPage>(`/api/subscription-requests/history?${params.toString()}`, { errorMsg: 'Failed to fetch subscription request history' });
    },
    getAuditLogPage: async (limit: number = 50, offset: number = 0, domain?: string, action?: string): Promise<AuditLogPage> => {
        const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
        if (domain) params.set('domain', domain)
        if (action) params.set('action', action)
        return request<AuditLogPage>(`/api/audit-log?${params.toString()}`, { errorMsg: 'Failed to fetch audit log' })
    },
    getSubscriptionProtection: async (): Promise<SubscriptionProtectionConfig> =>
        request<SubscriptionProtectionConfig>('/api/settings/subscription-protection', { errorMsg: 'Failed to fetch subscription protection settings' }),
    updateSubscriptionProtection: async (payload: Partial<SubscriptionProtectionConfig>): Promise<void> =>
        request('/api/settings/subscription-protection', { method: 'PUT', json: payload, parse: 'none', errorMsg: 'Failed to update subscription protection settings' }),
    getProtectionRules: async (): Promise<ProtectionRule[]> =>
        request<ProtectionRule[]>('/api/settings/protection-rules', { errorMsg: 'Failed to fetch protection rules' }),
    createProtectionRule: async (payload: CreateProtectionRuleRequest): Promise<void> =>
        request('/api/settings/protection-rules', { method: 'POST', json: payload, parse: 'none', errorMsg: 'Failed to create protection rule' }),
    deleteProtectionRule: async (id: number): Promise<void> =>
        request(`/api/settings/protection-rules/${id}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete protection rule' }),
    getBlockedSubscriptionRequestLog: async (limit: number, offset: number): Promise<BlockedSubscriptionRequestEntry[]> => {
        const params = new URLSearchParams({
            limit: String(limit),
            offset: String(offset),
        });
        return request<BlockedSubscriptionRequestEntry[]>(`/api/settings/protection-rules/blocked-log?${params.toString()}`, { errorMsg: 'Failed to fetch blocked request log' });
    },
    pruneNow: async (): Promise<{ deleted: number; cutoff: number }> =>
        request<{ deleted: number; cutoff: number }>('/api/retention/prune', { method: 'POST', errorMsg: 'Failed to prune' }),
    backupConfig: async (): Promise<void> =>
        request('/api/config/backup', { method: 'POST', parse: 'none', errorMsg: 'Failed to backup config' }),
    restoreConfig: async (): Promise<any> =>
        request<any>('/api/config/restore', { method: 'POST', errorMsg: 'Failed to restore config' }),
    getDashboardData: async (range: string = '24h', start?: string, end?: string): Promise<DashboardData> => {
        const params = new URLSearchParams({ range });
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        return request<DashboardData>(`/api/dashboard?${params.toString()}`, { errorMsg: 'Failed to fetch dashboard data' });
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
        return request<DashboardConsumerChartData>(`/api/dashboard/consumer-chart?${params.toString()}`, { errorMsg: 'Failed to fetch consumer chart' });
    },
    generateRealityKeys: async (): Promise<{ private_key: string; public_key: string; short_id: string[] }> =>
        request('/api/tools/reality-keys', { errorMsg: 'Failed to generate Reality keys' }),
    generateSelfSignedCert: async (payload: { tag?: string; common_name?: string }): Promise<{ cert_path: string; key_path: string }> =>
        request('/api/tools/self-signed-cert', { method: 'POST', json: payload, errorMsg: 'Failed to generate self-signed certificate' }),
    generateRandBase64: async (keyLength: number): Promise<{ value: string }> =>
        request('/api/tools/rand-base64', { method: 'POST', json: { key_length: keyLength }, errorMsg: 'Failed to generate random base64' }),
    applySingboxChanges: async (): Promise<ApplySingboxChangesResponse> =>
        request<ApplySingboxChangesResponse>('/api/singbox/apply', {
            method: 'POST',
            contentType: 'application/json',
            allowStatus: [409],
            errorMsg: 'Failed to apply Sing-box changes',
        }),

    // Panel user management
    getPanelUsers: async (): Promise<PanelUserInfo[]> =>
        request<PanelUserInfo[]>('/api/panel-users', { errorMsg: 'Failed to fetch panel users' }),
    createPanelUser: async (data: CreatePanelUserRequest): Promise<void> =>
        request('/api/panel-users', { method: 'POST', json: data, parse: 'none', errorMsg: 'Failed to create panel user' }),
    updatePanelUserPermissions: async (username: string, permissions: PanelUserPermissions): Promise<void> =>
        request('/api/panel-users/permissions', { method: 'PUT', json: { username, permissions }, parse: 'none', errorMsg: 'Failed to update permissions' }),
    updatePanelUserUsername: async (username: string, new_username: string): Promise<void> =>
        request('/api/panel-users/username', { method: 'PUT', json: { username, new_username }, parse: 'none', errorMsg: 'Failed to update username' }),
    updatePanelUserPassword: async (username: string, new_password: string): Promise<void> =>
        request('/api/panel-users/password', { method: 'PUT', json: { username, new_password }, parse: 'none', errorMsg: 'Failed to update password' }),
    deletePanelUser: async (username: string): Promise<void> =>
        request(`/api/panel-users?username=${encodeURIComponent(username)}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete panel user' }),

    // Subscriptions
    getSubscriptions: async (): Promise<Subscription[]> =>
        request<Subscription[]>('/api/subscriptions', { errorMsg: 'Failed to fetch subscriptions' }),
    getSubscriptionDefaults: async (): Promise<SubscriptionDefaults> =>
        request<SubscriptionDefaults>('/api/subscriptions/defaults', { errorMsg: 'Failed to fetch subscription defaults' }),
    updateSubscriptionDefaults: async (data: SubscriptionDefaults): Promise<SubscriptionDefaults> =>
        request<SubscriptionDefaults>('/api/subscriptions/defaults', { method: 'PUT', json: data, errorMsg: 'Failed to update subscription defaults' }),
    getSubscriptionDefaultDestinations: async (): Promise<SubscriptionDefaultDestinationsResponse> =>
        request<SubscriptionDefaultDestinationsResponse>('/api/subscriptions/default-destinations', { errorMsg: 'Failed to fetch subscription destination suggestions' }),
    getSubscriptionHappConfig: async (): Promise<SubscriptionHappConfig> =>
        request<SubscriptionHappConfig>('/api/subscriptions/happ-config', { errorMsg: 'Failed to fetch Happ config' }),
    updateSubscriptionHappConfig: async (data: SubscriptionHappConfig): Promise<SubscriptionHappConfig> =>
        request<SubscriptionHappConfig>('/api/subscriptions/happ-config', { method: 'PUT', json: data, errorMsg: 'Failed to update Happ config' }),
    encryptHappLink: async (url: string): Promise<{ encrypted_url: string }> =>
        request('/api/happ/encrypt-link', { method: 'POST', json: { url }, errorMsg: 'Failed to encrypt Happ link' }),
    createSubscription: async (data: SubscriptionMutationRequest): Promise<{ id: number; token: string }> =>
        request('/api/subscriptions', { method: 'POST', json: data, errorMsg: 'Failed to create subscription' }),
    updateSubscription: async (id: number, data: SubscriptionMutationRequest): Promise<void> =>
        request(`/api/subscriptions/${id}`, { method: 'PUT', json: data, parse: 'none', errorMsg: 'Failed to update subscription' }),
    deleteSubscription: async (id: number): Promise<void> =>
        request(`/api/subscriptions/${id}`, { method: 'DELETE', parse: 'none', errorMsg: 'Failed to delete subscription' }),
    deleteSubscriptionRequest: async (id: number): Promise<void> =>
        request(`/api/subscription-requests/${id}`, { method: 'DELETE', parse: 'none', errorMode: 'status', errorMsg: 'Failed to delete' }),

    bulkDeleteSubscriptionRequests: async (ids: number[]): Promise<void> =>
        request('/api/subscription-requests', { method: 'DELETE', json: { ids }, parse: 'none', errorMode: 'status', errorMsg: 'Failed to bulk delete' }),

    clearSubscriptionRequestsBySubID: async (subId: number): Promise<void> =>
        request(`/api/subscription-requests?sub_id=${subId}`, { method: 'DELETE', parse: 'none', errorMode: 'status', errorMsg: 'Failed to clear' }),

    regenerateSubscriptionToken: async (id: number): Promise<{ token: string }> =>
        request(`/api/subscriptions/${id}/regenerate`, { method: 'POST', errorMsg: 'Failed to regenerate token' }),
    getSubscriptionDomain: async (): Promise<string> => {
        const data = await request<{ subscription_domain?: string }>('/api/settings/subscription-domain', { errorMsg: 'Failed to fetch subscription domain' });
        return data.subscription_domain || '';
    },
    updateSubscriptionDomain: async (domain: string): Promise<void> =>
        request('/api/settings/subscription-domain', { method: 'PUT', json: { subscription_domain: domain }, parse: 'none', errorMsg: 'Failed to update subscription domain' }),
    getCFWorkerURL: async (): Promise<string> => {
        const data = await request<{ cf_worker_url?: string }>('/api/settings/cf-worker-url', { errorMsg: 'Failed to fetch CF Worker URL' });
        return data.cf_worker_url || '';
    },
    updateCFWorkerURL: async (url: string): Promise<void> =>
        request('/api/settings/cf-worker-url', { method: 'PUT', json: { cf_worker_url: url }, parse: 'none', errorMsg: 'Failed to update CF Worker URL' }),

    getLogStoreStats: async (): Promise<LogStoreStats> =>
        request<LogStoreStats>('/api/settings/logs/stats', { errorMsg: 'Failed to fetch log store stats' }),

    triggerDBBackup: async (): Promise<{ created: string[] }> =>
        request('/api/settings/backup/trigger', { method: 'POST', errorMsg: 'Failed to trigger backup' }),
};

export function downloadDBBackupURL(target: 'main' | 'audit' | 'logs'): string {
    return `/api/settings/backup/download?target=${target}`;
}

export async function downloadDBBackup(target: 'main' | 'audit' | 'logs', includeCold?: string): Promise<void> {
    let url = `/api/settings/backup/download?target=${target}`;
    if (target === 'logs' && includeCold && includeCold !== 'none') url += `&include_cold=${includeCold}`;
    const res = await request<Response>(url, { parse: 'raw', errorMode: 'status', errorMsg: 'Backup download failed' });
    const disposition = res.headers.get('Content-Disposition') ?? '';
    const match = disposition.match(/filename="([^"]+)"/);
    const filename = match ? match[1] : `${target}-backup.tar.gz`;
    const blob = await res.blob();
    const blobUrl = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = blobUrl;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(blobUrl);
}
