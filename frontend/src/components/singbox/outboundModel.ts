// Pure helpers for creating and editing Sing-box outbounds from the panel.

export type OutboundKind = 'link' | 'selector' | 'urltest' | 'socks' | 'http'

export interface OutboundDraft {
    kind: OutboundKind
    tag: string
    // selector / urltest
    members: string[]
    defaultMember: string
    testUrl: string
    interval: string
    tolerance: string
    // socks / http
    server: string
    port: string
    username: string
    password: string
    // link: the converted outbound, kept as returned by the panel
    converted: Record<string, unknown> | null
}

export const emptyOutboundDraft = (kind: OutboundKind = 'link'): OutboundDraft => ({
    kind,
    tag: '',
    members: [],
    defaultMember: '',
    testUrl: 'https://www.gstatic.com/generate_204',
    interval: '3m',
    tolerance: '',
    server: '',
    port: '',
    username: '',
    password: '',
    converted: null,
})

const TAG_PATTERN = /^[^\s"'\\]+$/
const INTERVAL_PATTERN = /^\d+[smh]$/i

export function serializeOutbound(draft: OutboundDraft): Record<string, unknown> {
    const tag = draft.tag.trim()
    switch (draft.kind) {
        case 'link':
            return { ...(draft.converted || {}), tag }
        case 'selector': {
            const out: Record<string, unknown> = { type: 'selector', tag, outbounds: draft.members }
            if (draft.defaultMember) out.default = draft.defaultMember
            return out
        }
        case 'urltest': {
            const out: Record<string, unknown> = { type: 'urltest', tag, outbounds: draft.members }
            if (draft.testUrl.trim()) out.url = draft.testUrl.trim()
            if (draft.interval.trim()) out.interval = draft.interval.trim()
            if (draft.tolerance.trim()) out.tolerance = Number(draft.tolerance)
            return out
        }
        case 'socks':
        case 'http': {
            const out: Record<string, unknown> = { type: draft.kind, tag, server: draft.server.trim(), server_port: Number(draft.port) }
            if (draft.username.trim()) out.username = draft.username.trim()
            if (draft.password) out.password = draft.password
            return out
        }
    }
}

// parseOutbound reads an outbound the editor can modify, or null when it is
// another type (those stay editable only in the raw config).
export function parseOutbound(entry: unknown): OutboundDraft | null {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return null
    const o = entry as Record<string, unknown>
    if (typeof o.tag !== 'string') return null
    const base = { ...emptyOutboundDraft(), tag: o.tag }
    const strings = (value: unknown) => (Array.isArray(value) && value.every(item => typeof item === 'string') ? (value as string[]) : null)
    if (o.type === 'selector' || o.type === 'urltest') {
        const members = strings(o.outbounds)
        if (!members) return null
        if (o.type === 'selector') return { ...base, kind: 'selector', members, defaultMember: typeof o.default === 'string' ? o.default : '' }
        return {
            ...base,
            kind: 'urltest',
            members,
            testUrl: typeof o.url === 'string' ? o.url : '',
            interval: typeof o.interval === 'string' ? o.interval : '',
            tolerance: typeof o.tolerance === 'number' ? String(o.tolerance) : '',
        }
    }
    if ((o.type === 'socks' || o.type === 'http') && typeof o.server === 'string' && typeof o.server_port === 'number') {
        return {
            ...base,
            kind: o.type,
            server: o.server,
            port: String(o.server_port),
            username: typeof o.username === 'string' ? o.username : '',
            password: typeof o.password === 'string' ? o.password : '',
        }
    }
    return null
}

// validateOutbound returns a message for the first problem, or '' when fine.
// existingTags excludes the outbound being edited.
export function validateOutbound(draft: OutboundDraft, existingTags: string[]): string {
    const tag = draft.tag.trim()
    if (!tag) return 'Tag is required.'
    if (!TAG_PATTERN.test(tag)) return 'Tag cannot contain spaces or quotes.'
    if (existingTags.includes(tag)) return 'Another outbound already uses this tag.'
    switch (draft.kind) {
        case 'link':
            return draft.converted ? '' : 'Paste a share link and convert it first.'
        case 'selector':
        case 'urltest': {
            if (draft.members.length === 0) return 'Pick at least one outbound for the group.'
            if (draft.members.includes(tag)) return 'A group cannot contain itself.'
            if (draft.kind === 'selector' && draft.defaultMember && !draft.members.includes(draft.defaultMember)) {
                return 'The default must be one of the group members.'
            }
            if (draft.kind === 'urltest') {
                if (draft.interval.trim() && !INTERVAL_PATTERN.test(draft.interval.trim())) return 'Interval looks like 30s, 3m or 1h.'
                if (draft.tolerance.trim() && !/^\d+$/.test(draft.tolerance.trim())) return 'Tolerance is a number of milliseconds.'
            }
            return ''
        }
        case 'socks':
        case 'http': {
            if (!draft.server.trim()) return 'Server is required.'
            const port = Number(draft.port)
            if (!Number.isInteger(port) || port < 1 || port > 65535) return 'Port must be between 1 and 65535.'
            return ''
        }
    }
}

export interface OutboundReference {
    where: string
}

// outboundReferences lists what in the config still points at tag, so deleting
// or renaming it does not leave a broken config behind.
export function outboundReferences(config: Record<string, unknown>, tag: string): OutboundReference[] {
    const refs: OutboundReference[] = []
    const route = (config.route && typeof config.route === 'object' ? config.route : {}) as Record<string, unknown>
    if (route.final === tag) refs.push({ where: 'the default route (final)' })
    const outbounds = Array.isArray(config.outbounds) ? config.outbounds : []
    for (const entry of outbounds) {
        const o = entry as Record<string, unknown> | null
        if (!o || o.tag === tag) continue
        if (Array.isArray(o.outbounds) && o.outbounds.includes(tag)) refs.push({ where: `the group "${String(o.tag)}"` })
        if (o.detour === tag) refs.push({ where: `the outbound "${String(o.tag)}" (detour)` })
    }
    const rules = Array.isArray(route.rules) ? route.rules : []
    rules.forEach((rule, index) => {
        if (rule && typeof rule === 'object' && (rule as Record<string, unknown>).outbound === tag) refs.push({ where: `rule ${index + 1}` })
    })
    const ruleSets = Array.isArray(route.rule_set) ? route.rule_set : []
    for (const set of ruleSets) {
        const s = set as Record<string, unknown> | null
        if (s && s.download_detour === tag) refs.push({ where: `the rule set "${String(s.tag)}" (download)` })
    }
    return refs
}

// uniqueTag returns base, or base-2, base-3... when base is taken.
export function uniqueTag(base: string, taken: string[]): string {
    const clean = base.trim() || 'outbound'
    if (!taken.includes(clean)) return clean
    for (let n = 2; ; n++) {
        const candidate = `${clean}-${n}`
        if (!taken.includes(candidate)) return candidate
    }
}
