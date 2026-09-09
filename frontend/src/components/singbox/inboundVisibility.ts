export type InboundType = 'vless' | 'vmess' | 'trojan' | 'hysteria2' | 'shadowsocks' | 'anytls' | 'naive'

/**
 * Protocol capability table. This MIRRORS internal/core/protocol_capability.go
 * (ProtocolCapability / protocolCapabilities). Keep both in sync: any change to
 * a protocol's capabilities must be made in both files in the same commit.
 * `formSection` is frontend-only — it selects which protocol-specific block the
 * inbound editor renders and has no Go counterpart.
 */
export interface ProtocolCapability {
    credential: 'uuid' | 'id_or_uuid' | 'password_as_uuid' | 'password'
    supportsFlow: boolean
    supportsVmessFields: boolean
    supportsTLS: boolean
    tlsAlwaysEnabled: boolean
    supportsReality: boolean
    supportsALPN: boolean
    supportsTransport: boolean
    supportsMultiplex: boolean
    supportsBandwidth: boolean
    supportsObfs: boolean
    supportsMethod: boolean
    supportsServerKey: boolean
    supportsNetwork: boolean
    formSection: 'hysteria2' | 'shadowsocks' | 'anytls' | 'naive' | null
}

export const PROTOCOL_CAPABILITIES: Record<InboundType, ProtocolCapability> = {
    vless: {
        credential: 'uuid',
        supportsFlow: true,
        supportsVmessFields: false,
        supportsTLS: true,
        tlsAlwaysEnabled: false,
        supportsReality: true,
        supportsALPN: true,
        supportsTransport: true,
        supportsMultiplex: true,
        supportsBandwidth: false,
        supportsObfs: false,
        supportsMethod: false,
        supportsServerKey: false,
        supportsNetwork: false,
        formSection: null,
    },
    vmess: {
        credential: 'id_or_uuid',
        supportsFlow: false,
        supportsVmessFields: true,
        supportsTLS: true,
        tlsAlwaysEnabled: false,
        supportsReality: false,
        supportsALPN: true,
        supportsTransport: true,
        supportsMultiplex: true,
        supportsBandwidth: false,
        supportsObfs: false,
        supportsMethod: false,
        supportsServerKey: false,
        supportsNetwork: false,
        formSection: null,
    },
    trojan: {
        credential: 'password_as_uuid',
        supportsFlow: false,
        supportsVmessFields: true,
        supportsTLS: true,
        tlsAlwaysEnabled: false,
        supportsReality: false,
        supportsALPN: true,
        supportsTransport: true,
        supportsMultiplex: true,
        supportsBandwidth: false,
        supportsObfs: false,
        supportsMethod: false,
        supportsServerKey: false,
        supportsNetwork: false,
        formSection: null,
    },
    hysteria2: {
        credential: 'password',
        supportsFlow: false,
        supportsVmessFields: false,
        supportsTLS: true,
        tlsAlwaysEnabled: true,
        supportsReality: false,
        supportsALPN: false,
        supportsTransport: false,
        supportsMultiplex: false,
        supportsBandwidth: true,
        supportsObfs: true,
        supportsMethod: false,
        supportsServerKey: false,
        supportsNetwork: false,
        formSection: 'hysteria2',
    },
    shadowsocks: {
        credential: 'password',
        supportsFlow: false,
        supportsVmessFields: false,
        supportsTLS: false,
        tlsAlwaysEnabled: false,
        supportsReality: false,
        supportsALPN: false,
        supportsTransport: false,
        supportsMultiplex: true,
        supportsBandwidth: false,
        supportsObfs: false,
        supportsMethod: true,
        supportsServerKey: true,
        supportsNetwork: true,
        formSection: 'shadowsocks',
    },
    anytls: {
        credential: 'password',
        supportsFlow: false,
        supportsVmessFields: false,
        supportsTLS: true,
        tlsAlwaysEnabled: true,
        supportsReality: false,
        supportsALPN: true,
        supportsTransport: false,
        supportsMultiplex: false,
        supportsBandwidth: false,
        supportsObfs: false,
        supportsMethod: false,
        supportsServerKey: false,
        supportsNetwork: false,
        formSection: 'anytls',
    },
    naive: {
        credential: 'password',
        supportsFlow: false,
        supportsVmessFields: false,
        supportsTLS: true,
        tlsAlwaysEnabled: true,
        supportsReality: false,
        supportsALPN: false,
        supportsTransport: false,
        supportsMultiplex: false,
        supportsBandwidth: false,
        supportsObfs: false,
        supportsMethod: false,
        supportsServerKey: false,
        supportsNetwork: true,
        formSection: 'naive',
    },
}

type InboundLike = Record<string, any>

export interface InboundVisibility {
    showTlsSection: boolean
    showRealitySection: boolean
    showRealityToggle: boolean
    showLinkTlsVerification: boolean
    showAlpn: boolean
    showTransport: boolean
    showTransportPath: boolean
    showTransportServiceName: boolean
    showMultiplex: boolean
    showHysteria2Password: boolean
    showHysteria2Bandwidth: boolean
    showHysteria2Obfs: boolean
    showShadowsocksSection: boolean
    showAnyTLSSection: boolean
    showNaiveSection: boolean
    showWsHeaders: boolean
    showWsEarlyData: boolean
}

const DEFAULT_VLESS = {
    type: 'vless',
    tag: 'vless-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    override_address: '',
    link_allow_insecure: 'auto',
    tls: {
        enabled: false,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: '',
        reality: {
            enabled: false,
            handshake: {
                server: '',
                server_port: 443,
            },
            private_key: '',
            public_key: '',
            short_id: [''],
        },
    },
    transport: {
        enabled: false,
        type: 'http',
        path: '/',
        service_name: 'grpc-service',
        headers: '',
        max_early_data: '',
        early_data_header_name: '',
    },
    multiplex: {
        enabled: false,
        padding: false,
        brutal: {
            enabled: false,
            up_mbps: 100,
            down_mbps: 100,
        },
    },
}

const DEFAULT_VMESS = {
    type: 'vmess',
    tag: 'vmess-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    override_address: '',
    link_allow_insecure: 'auto',
    tls: {
        enabled: false,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: '',
    },
    transport: {
        enabled: false,
        type: 'ws',
        path: '/',
        service_name: 'grpc-service',
        headers: '',
        max_early_data: '',
        early_data_header_name: '',
    },
    multiplex: {
        enabled: false,
        padding: false,
        brutal: {
            enabled: false,
            up_mbps: 100,
            down_mbps: 100,
        },
    },
}

const DEFAULT_TROJAN = {
    type: 'trojan',
    tag: 'trojan-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    override_address: '',
    link_allow_insecure: 'auto',
    tls: {
        enabled: false,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: '',
    },
    transport: {
        enabled: false,
        type: 'ws',
        path: '/',
        service_name: 'grpc-service',
        headers: '',
        max_early_data: '',
        early_data_header_name: '',
    },
    multiplex: {
        enabled: false,
        padding: false,
        brutal: {
            enabled: false,
            up_mbps: 100,
            down_mbps: 100,
        },
    },
}

const DEFAULT_HYSTERIA2 = {
    type: 'hysteria2',
    tag: 'hysteria2-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    override_address: '',
    link_allow_insecure: 'auto',
    up_mbps: '',
    down_mbps: '',
    ignore_client_bandwidth: false,
    masquerade: '',
    bbr_profile: '',
    users: [],
    obfs: {
        type: '',
        password: '',
    },
    tls: {
        enabled: true,
        server_name: '',
        certificate_path: '',
        key_path: '',
    },
}

const DEFAULT_SHADOWSOCKS = {
    type: 'shadowsocks',
    tag: 'shadowsocks-in',
    listen: '::',
    listen_port: 8080,
    network: ['tcp', 'udp'],
    udp_fragment: false,
    method: '2022-blake3-aes-128-gcm',
    password: '',
    external_port: '',
    override_address: '',
    users: [],
    multiplex: {
        enabled: true,
        padding: false,
        brutal: {
            enabled: false,
            up_mbps: 100,
            down_mbps: 100,
        },
    },
}

const DEFAULT_ANYTLS = {
    type: 'anytls',
    tag: 'anytls-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    override_address: '',
    link_allow_insecure: 'auto',
    users: [],
    padding_scheme: [],
    tls: {
        enabled: true,
        server_name: '',
        alpn: ['h2', 'http/1.1'],
        certificate_path: '',
        key_path: '',
    },
}

const DEFAULT_NAIVE = {
    type: 'naive',
    tag: 'naive-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
    override_address: '',
    link_allow_insecure: 'auto',
    network: 'tcp',
    quic_congestion_control: '',
    users: [],
    tls: {
        enabled: true,
        server_name: '',
        certificate_path: '',
        key_path: '',
    },
}

const DEFAULT_BY_TYPE: Record<InboundType, InboundLike> = {
    vless: DEFAULT_VLESS,
    vmess: DEFAULT_VMESS,
    trojan: DEFAULT_TROJAN,
    hysteria2: DEFAULT_HYSTERIA2,
    shadowsocks: DEFAULT_SHADOWSOCKS,
    anytls: DEFAULT_ANYTLS,
    naive: DEFAULT_NAIVE,
}

function cloneValue<T>(value: T): T {
    return JSON.parse(JSON.stringify(value))
}

function toInboundType(type: unknown): InboundType {
    const raw = String(type || '').toLowerCase()
    if (raw === 'vmess' || raw === 'trojan' || raw === 'hysteria2' || raw === 'shadowsocks' || raw === 'anytls' || raw === 'naive') return raw
    return 'vless'
}

function normalizePortInput(value: unknown): string | number {
    if (value === null || value === undefined || value === '') return ''
    const numeric = Number(value)
    return Number.isFinite(numeric) ? numeric : String(value)
}

function isTransportConfigured(transport: any): boolean {
    if (!transport || typeof transport !== 'object') return false
    if (transport.enabled === false) return false
    if (transport.enabled === true) return true
    const transportType = String(transport.type || '').trim()
    return transportType !== ''
}

function normalizeShortIds(value: unknown): string[] {
    if (Array.isArray(value)) return value.map(item => String(item || '')).slice(0, 1)
    if (typeof value === 'string') return [value]
    return ['']
}

function normalizeHeadersForEditor(headers: unknown): string {
    if (!headers) return ''
    if (typeof headers === 'string') return headers
    if (typeof headers === 'object') {
        try {
            return JSON.stringify(headers, null, 2)
        } catch {
            return ''
        }
    }
    return ''
}

function normalizeRawJsonOrStringForEditor(value: unknown): string {
    if (value === undefined || value === null || value === '') return ''
    if (typeof value === 'string') return value
    if (typeof value === 'object') {
        try {
            return JSON.stringify(value, null, 2)
        } catch {
            return ''
        }
    }
    return String(value)
}

function normalizeTls(type: InboundType, tls: any, fallback: any) {
    const cap = PROTOCOL_CAPABILITIES[type]
    if (!cap.supportsTLS) return undefined
    const normalized: any = {
        ...cloneValue(fallback),
        ...(tls && typeof tls === 'object' ? cloneValue(tls) : {}),
        enabled: cap.tlsAlwaysEnabled ? true : !!tls?.enabled,
    }
    if (cap.supportsReality) {
        normalized.reality = {
            ...cloneValue(DEFAULT_VLESS.tls.reality),
            ...(tls?.reality && typeof tls.reality === 'object' ? cloneValue(tls.reality) : {}),
            enabled: !!tls?.reality?.enabled,
            handshake: {
                ...cloneValue(DEFAULT_VLESS.tls.reality.handshake),
                ...(tls?.reality?.handshake && typeof tls.reality.handshake === 'object' ? cloneValue(tls.reality.handshake) : {}),
                server_port: Number(tls?.reality?.handshake?.server_port) || 443,
            },
            short_id: normalizeShortIds(tls?.reality?.short_id),
        }
    } else {
        delete normalized.reality
    }
    if (!cap.supportsALPN) {
        delete normalized.alpn
    } else if (!Array.isArray(normalized.alpn)) {
        normalized.alpn = cloneValue(fallback.alpn || [])
    }
    return normalized
}

function normalizeTransport(type: InboundType, transport: any, fallback: any) {
    if (!PROTOCOL_CAPABILITIES[type].supportsTransport) return undefined
    const transportEnabled = isTransportConfigured(transport)
    const normalized: any = {
        ...cloneValue(fallback),
        ...(transport && typeof transport === 'object' ? cloneValue(transport) : {}),
        enabled: transportEnabled,
        type: String(transport?.type || fallback.type || 'http'),
    }
    normalized.headers = normalizeHeadersForEditor(transport?.headers)
    normalized.max_early_data = transport?.max_early_data ?? ''
    normalized.early_data_header_name = transport?.early_data_header_name || ''
    if (!transportEnabled) {
        normalized.path = fallback.path || '/'
        normalized.service_name = fallback.service_name || 'grpc-service'
    }
    return normalized
}

function normalizeMultiplex(type: InboundType, multiplex: any, fallback: any) {
    if (!PROTOCOL_CAPABILITIES[type].supportsMultiplex) return undefined
    return {
        ...cloneValue(fallback),
        ...(multiplex && typeof multiplex === 'object' ? cloneValue(multiplex) : {}),
        enabled: !!multiplex?.enabled,
        padding: !!multiplex?.padding,
        brutal: {
            ...cloneValue(fallback.brutal),
            ...(multiplex?.brutal && typeof multiplex.brutal === 'object' ? cloneValue(multiplex.brutal) : {}),
            enabled: !!multiplex?.brutal?.enabled,
            up_mbps: normalizePortInput(multiplex?.brutal?.up_mbps ?? fallback.brutal.up_mbps),
            down_mbps: normalizePortInput(multiplex?.brutal?.down_mbps ?? fallback.brutal.down_mbps),
        },
    }
}

function normalizeHysteria2Users(users: unknown, tag: string) {
    if (!Array.isArray(users) || users.length === 0) {
        return []
    }
    return users.map((user: any, index: number) => ({
        ...cloneValue(user || {}),
        name: String(user?.name || (index === 0 ? tag || 'default' : `user-${index + 1}`)),
        password: String(user?.password || ''),
    }))
}

function normalizeHysteria2Obfs(obfs: unknown) {
    if (!obfs || typeof obfs !== 'object') {
        return { type: '', password: '' }
    }
    return {
        type: String((obfs as any).type || ''),
        password: String((obfs as any).password || ''),
    }
}

function normalizeShadowsocksUsers(users: unknown, tag: string) {
    if (!Array.isArray(users) || users.length === 0) return []
    return users.map((user: any, index: number) => ({
        ...cloneValue(user || {}),
        name: String(user?.name || (index === 0 ? tag || 'default' : `user-${index + 1}`)),
        password: String(user?.password || ''),
    }))
}

function normalizeShadowsocksNetwork(network: unknown) {
    const values = Array.isArray(network) ? network : [network]
    const normalized = values
        .map(value => String(value || '').trim().toLowerCase())
        .filter(value => value === 'tcp' || value === 'udp')
    if (normalized.length === 0) return ['tcp', 'udp']
    return Array.from(new Set(normalized))
}

function normalizeNaiveNetwork(network: unknown) {
    const raw = String(network || '').trim().toLowerCase()
    return raw === 'udp' ? 'udp' : 'tcp'
}

export function getDefaultInbound(type: unknown = 'vless') {
    return cloneValue(DEFAULT_BY_TYPE[toInboundType(type)])
}

export function getInboundTransportType(inbound: InboundLike | null | undefined): string {
    if (!inbound || typeof inbound !== 'object') return ''
    const type = toInboundType(inbound.type)
    // NOTE: intentionally NOT `!PROTOCOL_CAPABILITIES[type].supportsTransport` — shadowsocks
    // does not support the transport block but also does not early-return here, since its
    // network field reuses this function's transport-type-detection to select the network form.
    if (type === 'hysteria2' || type === 'anytls' || type === 'naive') return ''
    const transport = inbound.transport
    if (!isTransportConfigured(transport)) return ''
    return String(transport?.type || '').toLowerCase()
}

export function computeInboundVisibility(inbound: InboundLike | null | undefined): InboundVisibility {
    const type = toInboundType(inbound?.type)
    const cap = PROTOCOL_CAPABILITIES[type]
    const tlsEnabled = !!inbound?.tls?.enabled
    const transportType = getInboundTransportType(inbound)
    const transportEnabled = transportType !== ''
    const showTlsSection = cap.supportsTLS
    const showRealityToggle = cap.supportsReality && tlsEnabled
    const showRealitySection = showRealityToggle && !!inbound?.tls?.reality?.enabled
    const showTransport = cap.supportsTransport
    const showMultiplex = cap.supportsMultiplex
    const showWsFields = showTransport && transportEnabled && transportType === 'ws'
    return {
        showTlsSection,
        showRealitySection,
        showRealityToggle,
        showLinkTlsVerification: cap.supportsTLS,
        showAlpn: tlsEnabled && cap.supportsALPN,
        showTransport,
        showTransportPath: showTransport && transportEnabled && ['http', 'ws', 'httpupgrade'].includes(transportType),
        showTransportServiceName: showTransport && transportEnabled && transportType === 'grpc',
        showMultiplex,
        showHysteria2Password: cap.formSection === 'hysteria2',
        showHysteria2Bandwidth: cap.formSection === 'hysteria2',
        showHysteria2Obfs: cap.formSection === 'hysteria2',
        showShadowsocksSection: cap.formSection === 'shadowsocks',
        showAnyTLSSection: cap.formSection === 'anytls',
        showNaiveSection: cap.formSection === 'naive',
        showWsHeaders: showWsFields,
        showWsEarlyData: showWsFields,
    }
}

function stripHiddenInboundState(inbound: InboundLike) {
    const visibility = computeInboundVisibility(inbound)
    if (!visibility.showTransport) {
        delete inbound.transport
    } else if (!inbound.transport?.enabled) {
        delete inbound.transport.headers
        delete inbound.transport.max_early_data
        delete inbound.transport.early_data_header_name
    } else if (!visibility.showWsHeaders) {
        delete inbound.transport.headers
        delete inbound.transport.max_early_data
        delete inbound.transport.early_data_header_name
    }

    if (!visibility.showMultiplex) {
        delete inbound.multiplex
    }

    if (!visibility.showRealityToggle && inbound.tls) {
        delete inbound.tls.reality
    } else if (!visibility.showRealitySection && inbound.tls?.reality) {
        delete inbound.tls.reality.private_key
        delete inbound.tls.reality.public_key
        delete inbound.tls.reality.short_id
        delete inbound.tls.reality.handshake
    }

    if (!visibility.showAlpn && inbound.tls) {
        delete inbound.tls.alpn
    }

    if (!visibility.showHysteria2Password) {
        delete inbound.up_mbps
        delete inbound.down_mbps
        delete inbound.obfs
        delete inbound.ignore_client_bandwidth
        delete inbound.masquerade
        delete inbound.bbr_profile
    }

    if (!visibility.showShadowsocksSection && !visibility.showNaiveSection) {
        delete inbound.network
    }
    if (!visibility.showShadowsocksSection) {
        delete inbound.udp_fragment
        delete inbound.method
        delete inbound.password
    }

    return inbound
}

export function normalizeInboundForEditor(value: InboundLike | null | undefined) {
    const type = toInboundType(value?.type)
    const fallback = getDefaultInbound(type)
    const source = cloneValue(value || {})
    const normalized: any = {
        ...fallback,
        ...source,
        type,
        external_port: source.external_port ? String(source.external_port) : '',
        override_address: String(source.override_address || ''),
        link_allow_insecure: source.link_allow_insecure || 'auto',
        listen_port: source.listen_port ?? fallback.listen_port,
        tls: normalizeTls(type, source.tls, fallback.tls),
    }

    if (type !== 'hysteria2') {
        normalized.transport = normalizeTransport(type, source.transport, fallback.transport)
        normalized.multiplex = normalizeMultiplex(type, source.multiplex, fallback.multiplex)
    } else {
        normalized.up_mbps = source.up_mbps ?? ''
        normalized.down_mbps = source.down_mbps ?? ''
        normalized.ignore_client_bandwidth = !!source.ignore_client_bandwidth
        normalized.masquerade = normalizeRawJsonOrStringForEditor(source.masquerade)
        normalized.bbr_profile = String(source.bbr_profile || '')
        normalized.obfs = normalizeHysteria2Obfs(source.obfs)
        normalized.users = normalizeHysteria2Users(source.users, String(source.tag || fallback.tag))
    }

    if (type === 'shadowsocks') {
        normalized.network = normalizeShadowsocksNetwork(source.network ?? fallback.network)
        normalized.udp_fragment = !!source.udp_fragment
        normalized.method = String(source.method || fallback.method || '')
        normalized.password = String(source.password || '')
        normalized.users = normalizeShadowsocksUsers(source.users, String(source.tag || fallback.tag))
    }

    if (type === 'anytls') {
        normalized.padding_scheme = Array.isArray(source.padding_scheme) ? source.padding_scheme.map(String) : []
    }

    if (type === 'naive') {
        normalized.network = normalizeNaiveNetwork(source.network ?? fallback.network)
        normalized.quic_congestion_control = String(source.quic_congestion_control || '')
    }

    return stripHiddenInboundState(normalized)
}

function parseOptionalPositiveInteger(value: unknown, fieldName: string) {
    const trimmed = String(value ?? '').trim()
    if (!trimmed) return { value: undefined }
    const parsed = Number.parseInt(trimmed, 10)
    if (Number.isNaN(parsed) || parsed <= 0) {
        return { error: `${fieldName} must be a positive number.` }
    }
    return { value: parsed }
}

function parseHeaders(headers: unknown) {
    const raw = String(headers ?? '').trim()
    if (!raw) return { value: undefined }
    try {
        const parsed = JSON.parse(raw)
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            return { error: 'WebSocket headers must be a JSON object.' }
        }
        return { value: parsed }
    } catch {
        return { error: 'WebSocket headers must be valid JSON.' }
    }
}

function parseMasquerade(masquerade: unknown) {
    const raw = String(masquerade ?? '').trim()
    if (!raw) return { value: undefined }
    if (raw.startsWith('{') || raw.startsWith('[')) {
        try {
            const parsed = JSON.parse(raw)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                return { error: 'Hysteria2 masquerade JSON must be an object.' }
            }
            return { value: parsed }
        } catch {
            return { error: 'Hysteria2 masquerade JSON must be valid.' }
        }
    }
    return { value: raw }
}

export function canSelectInboundUserFlow(userType: string, inbound: InboundLike | null | undefined): boolean {
    const cap = PROTOCOL_CAPABILITIES[String(userType || '').toLowerCase() as InboundType]
    return !!cap?.supportsFlow && getInboundTransportType(inbound) !== 'ws'
}

export function buildInboundSubmission(formData: InboundLike) {
    const normalized = normalizeInboundForEditor(formData)
    const type = toInboundType(normalized.type)
    const submission: any = {
        ...cloneValue(normalized),
        listen_port: Number.parseInt(String(normalized.listen_port), 10),
    }

    if (!submission.tag || Number.isNaN(submission.listen_port)) {
        return { error: 'Tag and Port are required.' }
    }

    const externalPortRaw = String(submission.external_port ?? '').trim()
    if (externalPortRaw) {
        const parsedExternalPort = Number.parseInt(externalPortRaw, 10)
        if (Number.isNaN(parsedExternalPort) || parsedExternalPort <= 0) {
            return { error: 'External Port must be a valid number.' }
        }
        submission.external_port = parsedExternalPort
    } else {
        delete submission.external_port
    }

    const linkAllowInsecure = String(submission.link_allow_insecure || 'auto').trim().toLowerCase()
    if (linkAllowInsecure === '' || linkAllowInsecure === 'auto') {
        delete submission.link_allow_insecure
    } else if (linkAllowInsecure === 'enabled' || linkAllowInsecure === 'disabled') {
        submission.link_allow_insecure = linkAllowInsecure
    } else {
        return { error: 'Link allowInsecure must be auto, enabled, or disabled.' }
    }

    const overrideAddress = String(submission.override_address || '').trim()
    if (overrideAddress) {
        submission.override_address = overrideAddress
    } else {
        delete submission.override_address
    }

    const tlsEnabled = !!submission.tls?.enabled
    const realityEnabled = !!submission.tls?.reality?.enabled
    const hasCert = !!submission.tls?.certificate_path && !!submission.tls?.key_path
    const requiresCert = tlsEnabled && !(type === 'vless' && realityEnabled)
    if (requiresCert && !hasCert) {
        return { error: 'TLS is enabled but certificate/key paths are missing.' }
    }

    if (type === 'vless' && realityEnabled) {
        const handshake = submission.tls?.reality?.handshake || {}
        if (!handshake.server || !submission.tls?.reality?.private_key || !submission.tls?.reality?.short_id?.[0]) {
            return { error: 'Reality is enabled but server, private key, or short ID is missing.' }
        }
    }

    if (type === 'hysteria2') {
        delete submission.transport
        delete submission.multiplex
        if (submission.tls) {
            submission.tls.enabled = true
            delete submission.tls.alpn
            delete submission.tls.reality
        }

        if (!tlsEnabled) {
            return { error: 'Hysteria2 requires TLS and cannot be saved with TLS disabled.' }
        }

        const upMbps = parseOptionalPositiveInteger(submission.up_mbps, 'Hysteria2 up_mbps')
        if (upMbps.error) return { error: upMbps.error }
        const downMbps = parseOptionalPositiveInteger(submission.down_mbps, 'Hysteria2 down_mbps')
        if (downMbps.error) return { error: downMbps.error }

        if (upMbps.value !== undefined) submission.up_mbps = upMbps.value
        else delete submission.up_mbps
        if (downMbps.value !== undefined) submission.down_mbps = downMbps.value
        else delete submission.down_mbps
        submission.ignore_client_bandwidth = !!submission.ignore_client_bandwidth
        if (submission.ignore_client_bandwidth && (submission.up_mbps !== undefined || submission.down_mbps !== undefined)) {
            return { error: 'Hysteria2 ignore_client_bandwidth conflicts with up_mbps/down_mbps.' }
        }
        if (!submission.ignore_client_bandwidth) delete submission.ignore_client_bandwidth

        const masquerade = parseMasquerade(submission.masquerade)
        if (masquerade.error) return { error: masquerade.error }
        if (masquerade.value !== undefined) submission.masquerade = masquerade.value
        else delete submission.masquerade

        const bbrProfile = String(submission.bbr_profile || '').trim()
        if (bbrProfile) {
            if (!['conservative', 'standard', 'aggressive'].includes(bbrProfile)) {
                return { error: 'Hysteria2 bbr_profile must be conservative, standard, or aggressive.' }
            }
            submission.bbr_profile = bbrProfile
        } else {
            delete submission.bbr_profile
        }

        const obfsType = String(submission.obfs?.type || '').trim()
        const obfsPassword = String(submission.obfs?.password || '').trim()
        if (obfsType) {
            if (!obfsPassword) {
                return { error: 'Hysteria2 obfs password is required when obfs is enabled.' }
            }
            submission.obfs = { type: obfsType, password: obfsPassword }
        } else {
            delete submission.obfs
        }

        // Hysteria2 does not support sniff fields

        return { submission }
    }

    if (type === 'shadowsocks') {
        delete submission.tls
        delete submission.transport

        const networkValues = normalizeShadowsocksNetwork(submission.network)
        if (networkValues.length === 0) {
            return { error: 'Shadowsocks network must include tcp or udp.' }
        }
        submission.network = networkValues
        submission.udp_fragment = !!submission.udp_fragment

        const method = String(submission.method || '').trim()
        if (!method) {
            return { error: 'Shadowsocks method is required.' }
        }
        submission.method = method

        const serverPassword = String(submission.password || '').trim()
        if (!serverPassword) {
            return { error: 'Shadowsocks server password is required.' }
        }
        submission.password = serverPassword

        return { submission }
    }

    if (type === 'anytls') {
        delete submission.transport
        delete submission.multiplex
        if (submission.tls) {
            submission.tls.enabled = true
            delete submission.tls.reality
        }
        if (!tlsEnabled) {
            return { error: 'AnyTLS requires TLS and cannot be saved with TLS disabled.' }
        }
        if (!Array.isArray(submission.padding_scheme) || submission.padding_scheme.length === 0) {
            delete submission.padding_scheme
        }
        return { submission }
    }

    if (type === 'naive') {
        delete submission.transport
        delete submission.multiplex
        if (submission.tls) {
            submission.tls.enabled = true
            delete submission.tls.reality
            delete submission.tls.alpn
        }
        if (!tlsEnabled) {
            return { error: 'Naive requires TLS and cannot be saved with TLS disabled.' }
        }
        submission.network = normalizeNaiveNetwork(submission.network)
        const qcc = String(submission.quic_congestion_control || '').trim()
        if (qcc) {
            submission.quic_congestion_control = qcc
        } else {
            delete submission.quic_congestion_control
        }
        return { submission }
    }

    if (submission.transport?.enabled) {
        const transportType = String(submission.transport?.type || 'http')
        const cleanedTransport: any = { type: transportType }
        if (['http', 'ws', 'httpupgrade'].includes(transportType)) {
            if (submission.transport?.path) {
                cleanedTransport.path = submission.transport.path
            }
        }
        if (transportType === 'grpc' && submission.transport?.service_name) {
            cleanedTransport.service_name = submission.transport.service_name
        }
        if (transportType === 'ws') {
            const headers = parseHeaders(submission.transport?.headers)
            if (headers.error) return { error: headers.error }
            if (headers.value) cleanedTransport.headers = headers.value

            const maxEarlyData = parseOptionalPositiveInteger(submission.transport?.max_early_data, 'WebSocket max_early_data')
            if (maxEarlyData.error) return { error: maxEarlyData.error }
            if (maxEarlyData.value !== undefined) {
                cleanedTransport.max_early_data = maxEarlyData.value
            }

            const earlyDataHeaderName = String(submission.transport?.early_data_header_name || '').trim()
            if (earlyDataHeaderName) {
                cleanedTransport.early_data_header_name = earlyDataHeaderName
            }
        }
        submission.transport = cleanedTransport
    } else {
        delete submission.transport
    }

    if (submission.multiplex?.enabled) {
        const cleanedMultiplex: any = { enabled: true, padding: !!submission.multiplex?.padding }
        if (submission.multiplex?.brutal?.enabled) {
            const upMbps = parseOptionalPositiveInteger(submission.multiplex?.brutal?.up_mbps, 'Multiplex brutal up_mbps')
            if (upMbps.error) return { error: upMbps.error }
            const downMbps = parseOptionalPositiveInteger(submission.multiplex?.brutal?.down_mbps, 'Multiplex brutal down_mbps')
            if (downMbps.error) return { error: downMbps.error }
            if (upMbps.value === undefined || downMbps.value === undefined) {
                return { error: 'Multiplex brutal up/down Mbps must be positive numbers.' }
            }
            cleanedMultiplex.brutal = {
                enabled: true,
                up_mbps: upMbps.value,
                down_mbps: downMbps.value,
            }
        }
        submission.multiplex = cleanedMultiplex
    } else {
        delete submission.multiplex
    }

    if (type !== 'vless' && submission.tls) {
        delete submission.tls.reality
    }

    // public_key is display-only (client-side); never include in server inbound config
    delete submission.tls?.reality?.public_key

    return { submission }
}
