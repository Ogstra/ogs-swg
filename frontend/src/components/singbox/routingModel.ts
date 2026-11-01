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
