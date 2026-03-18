export type InboundType = 'vless' | 'vmess' | 'trojan' | 'hysteria2'

type InboundLike = Record<string, any>

export interface InboundVisibility {
    showRealitySection: boolean
    showRealityToggle: boolean
    showAlpn: boolean
    showTransport: boolean
    showTransportPath: boolean
    showTransportServiceName: boolean
    showMultiplex: boolean
    showHysteria2Password: boolean
    showHysteria2Bandwidth: boolean
    showHysteria2Obfs: boolean
    showWsHeaders: boolean
    showWsEarlyData: boolean
}

const DEFAULT_VLESS = {
    type: 'vless',
    tag: 'vless-in',
    listen: '0.0.0.0',
    listen_port: 443,
    external_port: '',
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
    up_mbps: '',
    down_mbps: '',
    users: [
        {
            name: 'default',
            password: '',
        },
    ],
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

const DEFAULT_BY_TYPE: Record<InboundType, InboundLike> = {
    vless: DEFAULT_VLESS,
    vmess: DEFAULT_VMESS,
    trojan: DEFAULT_TROJAN,
    hysteria2: DEFAULT_HYSTERIA2,
}

function cloneValue<T>(value: T): T {
    return JSON.parse(JSON.stringify(value))
}

function toInboundType(type: unknown): InboundType {
    const raw = String(type || '').toLowerCase()
    if (raw === 'vmess' || raw === 'trojan' || raw === 'hysteria2') return raw
    return 'vless'
}

function normalizePortInput(value: unknown): string | number {
    if (value === null || value === undefined || value === '') return ''
    const numeric = Number(value)
    return Number.isFinite(numeric) ? numeric : String(value)
}

function isTransportConfigured(transport: any): boolean {
    if (!transport || typeof transport !== 'object') return false
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

function normalizeTls(type: InboundType, tls: any, fallback: any) {
    const normalized: any = {
        ...cloneValue(fallback),
        ...(tls && typeof tls === 'object' ? cloneValue(tls) : {}),
        enabled: !!tls?.enabled,
    }
    if (type === 'vless') {
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
    if (type === 'hysteria2') {
        delete normalized.alpn
    } else if (!Array.isArray(normalized.alpn)) {
        normalized.alpn = cloneValue(fallback.alpn || [])
    }
    return normalized
}

function normalizeTransport(type: InboundType, transport: any, fallback: any) {
    if (type === 'hysteria2') return undefined
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
    if (type === 'hysteria2') return undefined
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
        return [{ name: tag || 'default', password: '' }]
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

export function getDefaultInbound(type: unknown = 'vless') {
    return cloneValue(DEFAULT_BY_TYPE[toInboundType(type)])
}

export function getInboundTransportType(inbound: InboundLike | null | undefined): string {
    if (!inbound || typeof inbound !== 'object') return ''
    const type = toInboundType(inbound.type)
    if (type === 'hysteria2') return ''
    const transport = inbound.transport
    if (!isTransportConfigured(transport)) return ''
    return String(transport?.type || '').toLowerCase()
}

export function computeInboundVisibility(inbound: InboundLike | null | undefined): InboundVisibility {
    const type = toInboundType(inbound?.type)
    const tlsEnabled = !!inbound?.tls?.enabled
    const transportType = getInboundTransportType(inbound)
    const transportEnabled = transportType !== ''
    const showRealityToggle = type === 'vless' && tlsEnabled
    const showRealitySection = showRealityToggle && !!inbound?.tls?.reality?.enabled
    const showTransport = type !== 'hysteria2'
    const showMultiplex = type !== 'hysteria2'
    const showWsFields = showTransport && transportEnabled && transportType === 'ws'
    return {
        showRealitySection,
        showRealityToggle,
        showAlpn: tlsEnabled && type !== 'hysteria2',
        showTransport,
        showTransportPath: showTransport && transportEnabled && ['http', 'ws', 'httpupgrade'].includes(transportType),
        showTransportServiceName: showTransport && transportEnabled && transportType === 'grpc',
        showMultiplex,
        showHysteria2Password: type === 'hysteria2',
        showHysteria2Bandwidth: type === 'hysteria2',
        showHysteria2Obfs: type === 'hysteria2',
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
        listen_port: source.listen_port ?? fallback.listen_port,
        tls: normalizeTls(type, source.tls, fallback.tls),
    }

    if (type !== 'hysteria2') {
        normalized.transport = normalizeTransport(type, source.transport, fallback.transport)
        normalized.multiplex = normalizeMultiplex(type, source.multiplex, fallback.multiplex)
    } else {
        normalized.up_mbps = source.up_mbps ?? ''
        normalized.down_mbps = source.down_mbps ?? ''
        normalized.obfs = normalizeHysteria2Obfs(source.obfs)
        normalized.users = normalizeHysteria2Users(source.users, String(source.tag || fallback.tag))
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

export function getPrimaryHysteria2Password(inbound: InboundLike | null | undefined): string {
    return String(inbound?.users?.[0]?.password || '')
}

export function setPrimaryHysteria2Password(inbound: InboundLike, password: string) {
    const next = normalizeInboundForEditor(inbound)
    const currentTag = String(next.tag || 'default')
    next.users = normalizeHysteria2Users(next.users, currentTag)
    next.users[0].password = password
    if (!next.users[0].name) {
        next.users[0].name = currentTag
    }
    return next
}

export function canSelectInboundUserFlow(userType: string, inbound: InboundLike | null | undefined): boolean {
    return String(userType || '').toLowerCase() === 'vless' && getInboundTransportType(inbound) !== 'ws'
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
            delete submission.tls.alpn
            delete submission.tls.reality
        }

        const password = getPrimaryHysteria2Password(submission).trim()
        if (!password) {
            return { error: 'Hysteria2 password is required.' }
        }

        const userName = String(submission.users?.[0]?.name || submission.tag || 'default')
        submission.users = [{ name: userName, password }]

        const upMbps = parseOptionalPositiveInteger(submission.up_mbps, 'Hysteria2 up_mbps')
        if (upMbps.error) return { error: upMbps.error }
        const downMbps = parseOptionalPositiveInteger(submission.down_mbps, 'Hysteria2 down_mbps')
        if (downMbps.error) return { error: downMbps.error }

        if (upMbps.value !== undefined) submission.up_mbps = upMbps.value
        else delete submission.up_mbps
        if (downMbps.value !== undefined) submission.down_mbps = downMbps.value
        else delete submission.down_mbps

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

    if (type !== 'vless' || !submission.tls?.reality?.enabled) {
        delete submission.tls?.reality?.public_key
    }

    return { submission }
}
