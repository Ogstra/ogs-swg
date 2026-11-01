// Pure helpers for the routing editor: they turn route.rule_set entries and
// route.rules entries into editable shapes and back, and leave anything they
// do not understand untouched so saving never drops hand-written config.

export type RuleSetFormat = 'binary' | 'source'

export interface RuleSetDraft {
    type: 'remote' | 'local'
    tag: string
    format: RuleSetFormat
    url: string
    path: string
    downloadDetour: string
    updateInterval: string
}

export type RuleSetItem =
    | { kind: 'editable'; draft: RuleSetDraft }
    | { kind: 'raw'; raw: unknown }

const REMOTE_KEYS = new Set(['type', 'tag', 'format', 'url', 'download_detour', 'update_interval'])
const LOCAL_KEYS = new Set(['type', 'tag', 'format', 'path'])

export const emptyRuleSetDraft = (): RuleSetDraft => ({
    type: 'remote',
    tag: '',
    format: 'binary',
    url: '',
    path: '',
    downloadDetour: '',
    updateInterval: '',
})

const isFormat = (value: unknown): value is RuleSetFormat => value === 'binary' || value === 'source'

export function parseRuleSet(entry: unknown): RuleSetItem {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return { kind: 'raw', raw: entry }
    const obj = entry as Record<string, unknown>
    const keys = Object.keys(obj)
    const tag = typeof obj.tag === 'string' ? obj.tag : ''
    if (!tag || !isFormat(obj.format)) return { kind: 'raw', raw: entry }

    if (obj.type === 'remote' && keys.every(k => REMOTE_KEYS.has(k)) && typeof obj.url === 'string') {
        const detour = obj.download_detour === undefined ? '' : obj.download_detour
        const interval = obj.update_interval === undefined ? '' : obj.update_interval
        if (typeof detour !== 'string' || typeof interval !== 'string') return { kind: 'raw', raw: entry }
        return {
            kind: 'editable',
            draft: { type: 'remote', tag, format: obj.format, url: obj.url, path: '', downloadDetour: detour, updateInterval: interval },
        }
    }
    if (obj.type === 'local' && keys.every(k => LOCAL_KEYS.has(k)) && typeof obj.path === 'string') {
        return {
            kind: 'editable',
            draft: { type: 'local', tag, format: obj.format, url: '', path: obj.path, downloadDetour: '', updateInterval: '' },
        }
    }
    return { kind: 'raw', raw: entry }
}

export function serializeRuleSet(draft: RuleSetDraft): Record<string, unknown> {
    if (draft.type === 'local') {
        return { type: 'local', tag: draft.tag.trim(), format: draft.format, path: draft.path.trim() }
    }
    const out: Record<string, unknown> = { type: 'remote', tag: draft.tag.trim(), format: draft.format, url: draft.url.trim() }
    if (draft.downloadDetour.trim()) out.download_detour = draft.downloadDetour.trim()
    if (draft.updateInterval.trim()) out.update_interval = draft.updateInterval.trim()
    return out
}

const TAG_PATTERN = /^[A-Za-z0-9._-]+$/
const INTERVAL_PATTERN = /^\d+[smhd]$/i

// validateRuleSet returns a message for the first problem, or '' when the
// draft is fine. existingTags are the other rule sets' tags.
export function validateRuleSet(draft: RuleSetDraft, existingTags: string[]): string {
    const tag = draft.tag.trim()
    if (!tag) return 'Tag is required.'
    if (!TAG_PATTERN.test(tag)) return 'Tag may only use letters, digits, dot, dash and underscore.'
    if (existingTags.includes(tag)) return 'Another rule set already uses this tag.'
    if (draft.type === 'remote') {
        let parsed: URL
        try {
            parsed = new URL(draft.url.trim())
        } catch {
            return 'URL must be a valid http(s) address.'
        }
        if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return 'URL must start with http:// or https://.'
        if (draft.updateInterval.trim() && !INTERVAL_PATTERN.test(draft.updateInterval.trim())) {
            return 'Update interval looks like 1d, 12h or 30m.'
        }
    } else if (!draft.path.trim()) {
        return 'Path is required.'
    }
    return ''
}

export type RuleSetPreset = 'geosite' | 'geoip'

// presetRuleSetDraft builds a remote rule set from the SagerNet rule-set
// repositories, e.g. ('geosite', 'netflix') -> geosite-netflix.srs.
export function presetRuleSetDraft(preset: RuleSetPreset, name: string): RuleSetDraft {
    const clean = name.trim().toLowerCase()
    const repo = preset === 'geosite' ? 'sing-geosite' : 'sing-geoip'
    return {
        ...emptyRuleSetDraft(),
        tag: `${preset}-${clean}`,
        url: `https://raw.githubusercontent.com/SagerNet/${repo}/rule-set/${preset}-${clean}.srs`,
        updateInterval: '1d',
    }
}

// ruleSetUsage lists which rule indexes reference tag in a rule's rule_set.
export function ruleSetUsage(rules: unknown[], tag: string): number[] {
    const used: number[] = []
    rules.forEach((rule, index) => {
        const value = rule && typeof rule === 'object' ? (rule as Record<string, unknown>).rule_set : undefined
        const tags = Array.isArray(value) ? value : value === undefined ? [] : [value]
        if (tags.includes(tag)) used.push(index)
    })
    return used
}

// ---- Routing rules ----

export type RuleAction = 'route' | 'reject'
export type RuleIpVersion = '' | '4' | '6'

export interface RuleMatch {
    inbound: string[]
    auth_user: string[]
    rule_set: string[]
    domain: string[]
    domain_suffix: string[]
    domain_keyword: string[]
    ip_cidr: string[]
    port: string[]
    port_range: string[]
    protocol: string[]
    ip_version: RuleIpVersion
}

export interface RuleDraft {
    match: RuleMatch
    action: RuleAction
    outbound: string
    explicitRoute: boolean
}

export type RuleItem =
    | { kind: 'editable'; draft: RuleDraft; source?: Record<string, unknown> }
    | { kind: 'raw'; raw: unknown }

export const LIST_MATCH_KEYS = ['inbound', 'auth_user', 'rule_set', 'domain', 'domain_suffix', 'domain_keyword', 'ip_cidr', 'port', 'port_range', 'protocol'] as const
type ListMatchKey = (typeof LIST_MATCH_KEYS)[number]

const RULE_KEYS = new Set<string>([...LIST_MATCH_KEYS, 'ip_version', 'outbound', 'action'])

export const emptyRuleMatch = (): RuleMatch => ({
    inbound: [], auth_user: [], rule_set: [], domain: [], domain_suffix: [], domain_keyword: [],
    ip_cidr: [], port: [], port_range: [], protocol: [], ip_version: '',
})

export const emptyRuleDraft = (): RuleDraft => ({ match: emptyRuleMatch(), action: 'route', outbound: '', explicitRoute: false })

const toStringList = (value: unknown): string[] | null => {
    const values = Array.isArray(value) ? value : [value]
    const out: string[] = []
    for (const entry of values) {
        if (typeof entry === 'number' && Number.isFinite(entry)) out.push(String(entry))
        else if (typeof entry === 'string' && entry.trim() !== '') out.push(entry.trim())
        else return null
    }
    return out.length > 0 ? out : null
}

export function matchIsEmpty(match: RuleMatch): boolean {
    return LIST_MATCH_KEYS.every(key => match[key].length === 0) && match.ip_version === ''
}

export function parseRule(entry: unknown): RuleItem {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return { kind: 'raw', raw: entry }
    const obj = entry as Record<string, unknown>
    if (!Object.keys(obj).every(key => RULE_KEYS.has(key))) return { kind: 'raw', raw: entry }

    const match = emptyRuleMatch()
    for (const key of LIST_MATCH_KEYS) {
        if (!(key in obj)) continue
        const list = toStringList(obj[key])
        if (!list) return { kind: 'raw', raw: entry }
        match[key] = list
    }
    if ('ip_version' in obj) {
        if (obj.ip_version !== 4 && obj.ip_version !== 6) return { kind: 'raw', raw: entry }
        match.ip_version = String(obj.ip_version) as RuleIpVersion
    }
    if (matchIsEmpty(match)) return { kind: 'raw', raw: entry }

    const action = obj.action
    if (action === undefined || action === 'route') {
        if (typeof obj.outbound !== 'string' || obj.outbound.trim() === '') return { kind: 'raw', raw: entry }
        return { kind: 'editable', source: obj, draft: { match, action: 'route', outbound: obj.outbound.trim(), explicitRoute: action === 'route' } }
    }
    if (action === 'reject' && !('outbound' in obj)) {
        return { kind: 'editable', source: obj, draft: { match, action: 'reject', outbound: '', explicitRoute: false } }
    }
    return { kind: 'raw', raw: entry }
}

function serializeDraft(draft: RuleDraft): Record<string, unknown> {
    const out: Record<string, unknown> = {}
    for (const key of LIST_MATCH_KEYS) {
        const values = draft.match[key]
        if (values.length === 0) continue
        out[key] = key === 'port' ? values.map(Number) : values
    }
    if (draft.match.ip_version) out.ip_version = Number(draft.match.ip_version)
    if (draft.action === 'reject') {
        out.action = 'reject'
    } else {
        if (draft.explicitRoute) out.action = 'route'
        out.outbound = draft.outbound.trim()
    }
    return out
}

// sameRule compares what a draft means, ignoring the order of list values.
function sameRule(a: RuleDraft, b: RuleDraft): boolean {
    const normal = (draft: RuleDraft) => JSON.stringify([
        LIST_MATCH_KEYS.map(key => [...draft.match[key]].sort()),
        draft.match.ip_version, draft.action, draft.action === 'route' ? draft.outbound : '',
    ])
    return normal(a) === normal(b)
}

// serializeRule writes a rule back. An edited-nothing rule keeps the exact
// shape it was read in, so saving never rewrites rules the user did not touch
// (route tags identify rules by their content).
export function serializeRule(item: RuleItem): unknown {
    if (item.kind === 'raw') return item.raw
    if (item.source) {
        const original = parseRule(item.source)
        if (original.kind === 'editable' && sameRule(original.draft, item.draft)) return item.source
    }
    return serializeDraft(item.draft)
}

const CIDR_V4 = /^(\d{1,3})(\.\d{1,3}){3}(\/(\d|[12]\d|3[0-2]))?$/
const CIDR_V6 = /^[0-9a-fA-F:]+(\/(\d{1,2}|1[01]\d|12[0-8]))?$/

export function isValidCidr(value: string): boolean {
    const v = value.trim()
    if (CIDR_V4.test(v)) return v.split('/')[0].split('.').every(part => Number(part) <= 255)
    return v.includes(':') && CIDR_V6.test(v)
}

const isPort = (value: string) => /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535
const isPortRange = (value: string) => {
    const [from, to, extra] = value.split(':')
    if (extra !== undefined) return false
    const ok = (part: string | undefined) => part === undefined || part === '' || isPort(part)
    return ok(from) && ok(to) && (from !== '' || to !== '') && to !== undefined
}

export interface KnownTags {
    inbounds: string[]
    outbounds: string[]
    ruleSets: string[]
}

// validateRule returns a message for the first problem, or '' when fine.
export function validateRule(draft: RuleDraft, known: KnownTags): string {
    if (matchIsEmpty(draft.match)) return 'Add at least one condition.'
    if (draft.action === 'route') {
        if (!draft.outbound.trim()) return 'Pick an outbound.'
        if (!known.outbounds.includes(draft.outbound.trim())) return `Outbound "${draft.outbound}" does not exist.`
    }
    const missingInbound = draft.match.inbound.find(tag => !known.inbounds.includes(tag))
    if (missingInbound) return `Inbound "${missingInbound}" does not exist.`
    const missingSet = draft.match.rule_set.find(tag => !known.ruleSets.includes(tag))
    if (missingSet) return `Rule set "${missingSet}" does not exist.`
    const badCidr = draft.match.ip_cidr.find(value => !isValidCidr(value))
    if (badCidr) return `"${badCidr}" is not a valid IP or CIDR.`
    const badPort = draft.match.port.find(value => !isPort(value))
    if (badPort) return `"${badPort}" is not a valid port.`
    const badRange = draft.match.port_range.find(value => !isPortRange(value))
    if (badRange) return `"${badRange}" is not a valid port range (use 1000:2000).`
    return ''
}

const SUMMARY_LABELS: Record<ListMatchKey, string> = {
    inbound: 'inbound', auth_user: 'user', rule_set: 'rule set', domain: 'domain', domain_suffix: 'suffix',
    domain_keyword: 'keyword', ip_cidr: 'ip', port: 'port', port_range: 'ports', protocol: 'protocol',
}

// summarizeRule is a one-line description of a rule for collapsed lists.
export function summarizeRule(draft: RuleDraft): string {
    const parts = LIST_MATCH_KEYS
        .filter(key => draft.match[key].length > 0)
        .map(key => {
            const values = draft.match[key]
            const shown = values.slice(0, 2).join(', ') + (values.length > 2 ? ` +${values.length - 2}` : '')
            return `${SUMMARY_LABELS[key]}: ${shown}`
        })
    if (draft.match.ip_version) parts.push(`IPv${draft.match.ip_version}`)
    const target = draft.action === 'reject' ? 'reject' : draft.outbound || '(no outbound)'
    return `${parts.join(' · ')} → ${target}`
}

export function moveItem<T>(items: T[], from: number, to: number): T[] {
    if (from === to || from < 0 || to < 0 || from >= items.length || to >= items.length) return items
    const next = [...items]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    return next
}

// splitListInput turns pasted text (commas, spaces, new lines) into values.
export function splitListInput(text: string): string[] {
    return text.split(/[\s,]+/).map(part => part.trim()).filter(Boolean)
}
