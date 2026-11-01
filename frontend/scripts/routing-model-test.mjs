#!/usr/bin/env node
// Unit checks for frontend/src/components/singbox/routingModel.ts.
//
// Usage (from the frontend/ directory): npm run test:routing
//
// The module is bundled with esbuild (no TS project build needed), then every
// check runs under node:test.

import { execFileSync } from 'node:child_process'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import test from 'node:test'
import assert from 'node:assert/strict'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const outDir = mkdtempSync(join(tmpdir(), 'routing-model-'))
const outfile = join(outDir, 'routingModel.mjs')
execFileSync(join(root, 'node_modules', '.bin', 'esbuild'), [
    join(root, 'src/components/singbox/routingModel.ts'),
    '--bundle', '--format=esm', '--platform=node', `--outfile=${outfile}`, '--log-level=error',
])
const m = await import(pathToFileURL(outfile).href)

test('remote rule set round-trips through the editor shape', () => {
    const entry = { type: 'remote', tag: 'geosite-netflix', format: 'binary', url: 'https://example.test/netflix.srs', download_detour: 'proxy', update_interval: '1d' }
    const item = m.parseRuleSet(entry)
    assert.equal(item.kind, 'editable')
    assert.deepEqual(m.serializeRuleSet(item.draft), entry)
})

test('local rule set round-trips', () => {
    const entry = { type: 'local', tag: 'mine', format: 'source', path: '/etc/sing-box/mine.json' }
    const item = m.parseRuleSet(entry)
    assert.equal(item.kind, 'editable')
    assert.deepEqual(m.serializeRuleSet(item.draft), entry)
})

test('optional remote fields are not written when empty', () => {
    const draft = { ...m.emptyRuleSetDraft(), tag: 'a', url: 'https://example.test/a.srs' }
    assert.deepEqual(m.serializeRuleSet(draft), { type: 'remote', tag: 'a', format: 'binary', url: 'https://example.test/a.srs' })
})

test('entries the editor does not understand are kept raw', () => {
    for (const entry of [
        { type: 'inline', tag: 'x', rules: [{ domain: ['a.test'] }] },
        { type: 'remote', tag: 'x', format: 'binary', url: 'https://example.test/x.srs', headers: { a: 'b' } },
        'legacy-string',
        null,
    ]) {
        const item = m.parseRuleSet(entry)
        assert.equal(item.kind, 'raw')
        assert.deepEqual(item.raw, entry)
    }
})

test('validation rejects bad tags, urls, intervals and duplicates', () => {
    const ok = { ...m.emptyRuleSetDraft(), tag: 'geosite-cn', url: 'https://example.test/cn.srs', updateInterval: '1d' }
    assert.equal(m.validateRuleSet(ok, []), '')
    assert.match(m.validateRuleSet({ ...ok, tag: '' }, []), /Tag is required/)
    assert.match(m.validateRuleSet({ ...ok, tag: 'has space' }, []), /letters, digits/)
    assert.match(m.validateRuleSet(ok, ['geosite-cn']), /already uses/)
    assert.match(m.validateRuleSet({ ...ok, url: 'not a url' }, []), /valid http/)
    assert.match(m.validateRuleSet({ ...ok, url: 'ftp://example.test/x' }, []), /http:\/\//)
    assert.match(m.validateRuleSet({ ...ok, updateInterval: 'daily' }, []), /Update interval/)
    assert.match(m.validateRuleSet({ ...m.emptyRuleSetDraft(), type: 'local', tag: 'p' }, []), /Path is required/)
})

test('presets point at the SagerNet rule-set branches', () => {
    const geosite = m.presetRuleSetDraft('geosite', ' Netflix ')
    assert.equal(geosite.tag, 'geosite-netflix')
    assert.equal(geosite.url, 'https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-netflix.srs')
    const geoip = m.presetRuleSetDraft('geoip', 'cn')
    assert.equal(geoip.url, 'https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs')
})

test('usage finds rules that reference a rule set', () => {
    const rules = [{ rule_set: ['a', 'b'] }, { rule_set: 'b' }, { domain: ['x'] }, null]
    assert.deepEqual(m.ruleSetUsage(rules, 'b'), [0, 1])
    assert.deepEqual(m.ruleSetUsage(rules, 'zzz'), [])
})

// ---- routing rules ----

const known = { inbounds: ['in-vless'], outbounds: ['direct', 'proxy'], ruleSets: ['geosite-cn'] }

test('legacy rules stay byte-identical when nothing is edited', () => {
    for (const rule of [
        { inbound: ['in-vless'], outbound: 'direct' },
        { inbound: 'in-vless', outbound: 'direct' },
        { auth_user: ['bob', 'alice'], rule_set: ['geosite-cn'], action: 'route', outbound: 'proxy' },
        { rule_set: 'geosite-cn', outbound: 'proxy', ip_version: 4 },
    ]) {
        const item = m.parseRule(rule)
        assert.equal(item.kind, 'editable')
        assert.deepEqual(m.serializeRule(item), rule)
    }
})

test('editing a rule rewrites it from the draft', () => {
    const item = m.parseRule({ inbound: ['in-vless'], outbound: 'direct' })
    item.draft.outbound = 'proxy'
    item.draft.match.domain_suffix = ['example.test']
    assert.deepEqual(m.serializeRule(item), { inbound: ['in-vless'], domain_suffix: ['example.test'], outbound: 'proxy' })
})

test('reordering the values of a list does not count as an edit', () => {
    const rule = { auth_user: ['bob', 'alice'], rule_set: ['geosite-cn'], outbound: 'proxy' }
    const item = m.parseRule(rule)
    item.draft.match.auth_user = ['alice', 'bob']
    assert.deepEqual(m.serializeRule(item), rule)
})

test('ports are written as numbers and reject rules carry no outbound', () => {
    const draft = m.emptyRuleDraft()
    draft.action = 'reject'
    draft.match.port = ['80', '443']
    draft.match.port_range = ['1000:2000']
    draft.match.ip_version = '6'
    assert.deepEqual(m.serializeRule({ kind: 'editable', draft }), { port: [80, 443], port_range: ['1000:2000'], ip_version: 6, action: 'reject' })
})

test('reject rules read back as editable', () => {
    const rule = { domain_suffix: ['ads.example.test'], action: 'reject' }
    const item = m.parseRule(rule)
    assert.equal(item.kind, 'editable')
    assert.equal(item.draft.action, 'reject')
    assert.deepEqual(m.serializeRule(item), rule)
})

test('rules the editor cannot express are kept raw and unchanged', () => {
    for (const rule of [
        { type: 'logical', mode: 'and', rules: [{ domain: ['a.test'] }] },
        { domain: ['a.test'], action: 'sniff' },
        { clash_mode: 'direct', outbound: 'direct' },
        { domain: ['a.test'], action: 'reject', method: 'drop' },
        { outbound: 'direct' },
        { inbound: ['in-vless'] },
        { port: [80, {}], outbound: 'direct' },
        'string',
    ]) {
        const item = m.parseRule(rule)
        assert.equal(item.kind, 'raw', JSON.stringify(rule))
        assert.deepEqual(m.serializeRule(item), rule)
    }
})

test('rule validation catches the common mistakes', () => {
    const base = () => ({ ...m.emptyRuleDraft(), outbound: 'direct', match: { ...m.emptyRuleMatch(), domain_suffix: ['example.test'] } })
    assert.equal(m.validateRule(base(), known), '')
    assert.match(m.validateRule({ ...base(), match: m.emptyRuleMatch() }, known), /at least one condition/)
    assert.match(m.validateRule({ ...base(), outbound: '' }, known), /Pick an outbound/)
    assert.match(m.validateRule({ ...base(), outbound: 'nowhere' }, known), /does not exist/)
    assert.equal(m.validateRule({ ...base(), action: 'reject', outbound: '' }, known), '')
    const withMatch = patch => ({ ...base(), match: { ...base().match, ...patch } })
    assert.match(m.validateRule(withMatch({ inbound: ['nope'] }), known), /Inbound "nope"/)
    assert.match(m.validateRule(withMatch({ rule_set: ['nope'] }), known), /Rule set "nope"/)
    assert.match(m.validateRule(withMatch({ ip_cidr: ['300.1.1.1'] }), known), /valid IP or CIDR/)
    assert.equal(m.validateRule(withMatch({ ip_cidr: ['10.0.0.0/8', '2001:db8::/32', '192.0.2.1'] }), known), '')
    assert.match(m.validateRule(withMatch({ port: ['70000'] }), known), /valid port/)
    assert.match(m.validateRule(withMatch({ port_range: ['1000-2000'] }), known), /port range/)
    assert.equal(m.validateRule(withMatch({ port_range: ['1000:2000', ':443'] }), known), '')
})

test('summaries stay short and name the target', () => {
    const draft = { ...m.emptyRuleDraft(), outbound: 'proxy', match: { ...m.emptyRuleMatch(), domain_suffix: ['a.test', 'b.test', 'c.test'], ip_version: '4' } }
    assert.equal(m.summarizeRule(draft), 'suffix: a.test, b.test +1 · IPv4 → proxy')
    assert.equal(m.summarizeRule({ ...draft, action: 'reject' }), 'suffix: a.test, b.test +1 · IPv4 → reject')
})

test('moveItem reorders without mutating and ignores bad indexes', () => {
    const items = ['a', 'b', 'c']
    assert.deepEqual(m.moveItem(items, 0, 2), ['b', 'c', 'a'])
    assert.deepEqual(items, ['a', 'b', 'c'])
    assert.equal(m.moveItem(items, 0, 5), items)
    assert.equal(m.moveItem(items, -1, 1), items)
})

test('pasted list text is split on commas, spaces and new lines', () => {
    assert.deepEqual(m.splitListInput('a.test, b.test\nc.test  d.test,,'), ['a.test', 'b.test', 'c.test', 'd.test'])
})

// ---- outbounds ----

const outboundOut = join(outDir, 'outboundModel.mjs')
execFileSync(join(root, 'node_modules', '.bin', 'esbuild'), [
    join(root, 'src/components/singbox/outboundModel.ts'),
    '--bundle', '--format=esm', '--platform=node', `--outfile=${outboundOut}`, '--log-level=error',
])
const o = await import(pathToFileURL(outboundOut).href)

test('groups serialize and read back', () => {
    const selector = { type: 'selector', tag: 'pick', outbounds: ['a', 'b'], default: 'b' }
    const draft = o.parseOutbound(selector)
    assert.equal(draft.kind, 'selector')
    assert.deepEqual(o.serializeOutbound(draft), selector)

    const urltest = { type: 'urltest', tag: 'auto', outbounds: ['a'], url: 'https://example.test/204', interval: '3m', tolerance: 50 }
    assert.deepEqual(o.serializeOutbound(o.parseOutbound(urltest)), urltest)
})

test('proxy outbounds serialize with numeric ports and optional auth', () => {
    const entry = { type: 'socks', tag: 'upstream', server: 'proxy.example.test', server_port: 1080, username: 'u', password: 'p' }
    assert.deepEqual(o.serializeOutbound(o.parseOutbound(entry)), entry)
    const draft = { ...o.emptyOutboundDraft('http'), tag: 'h', server: 'proxy.example.test', port: '8080' }
    assert.deepEqual(o.serializeOutbound(draft), { type: 'http', tag: 'h', server: 'proxy.example.test', server_port: 8080 })
})

test('a converted link keeps its fields and takes the chosen tag', () => {
    const draft = { ...o.emptyOutboundDraft('link'), tag: 'my-vless', converted: { type: 'vless', tag: 'from-link', server: 's.example.test', server_port: 443, uuid: 'x' } }
    assert.deepEqual(o.serializeOutbound(draft), { type: 'vless', tag: 'my-vless', server: 's.example.test', server_port: 443, uuid: 'x' })
})

test('types the editor does not manage are not parsed', () => {
    for (const entry of [{ type: 'direct', tag: 'direct' }, { type: 'vless', tag: 'x', server: 's', server_port: 1 }, { type: 'selector', tag: 'g', outbounds: [1] }, null, 'x']) {
        assert.equal(o.parseOutbound(entry), null)
    }
})

test('outbound validation', () => {
    const group = (patch = {}) => ({ ...o.emptyOutboundDraft('selector'), tag: 'g', members: ['a'], ...patch })
    assert.equal(o.validateOutbound(group(), ['a']), '')
    assert.match(o.validateOutbound(group({ tag: '' }), []), /Tag is required/)
    assert.match(o.validateOutbound(group({ tag: 'has space' }), []), /spaces/)
    assert.match(o.validateOutbound(group(), ['g']), /already uses/)
    assert.match(o.validateOutbound(group({ members: [] }), []), /at least one/)
    assert.match(o.validateOutbound(group({ members: ['g'] }), []), /itself/)
    assert.match(o.validateOutbound(group({ defaultMember: 'zzz' }), []), /default/)
    assert.match(o.validateOutbound({ ...group({ kind: 'urltest' }), interval: 'soon' }, []), /Interval/)
    assert.match(o.validateOutbound({ ...group({ kind: 'urltest' }), tolerance: 'x' }, []), /Tolerance/)
    assert.match(o.validateOutbound({ ...o.emptyOutboundDraft('socks'), tag: 's' }, []), /Server is required/)
    assert.match(o.validateOutbound({ ...o.emptyOutboundDraft('socks'), tag: 's', server: 'h', port: '70000' }, []), /Port/)
    assert.match(o.validateOutbound({ ...o.emptyOutboundDraft('link'), tag: 'l' }, []), /convert/)
})

test('references show everything that still points at an outbound', () => {
    const config = {
        outbounds: [
            { type: 'direct', tag: 'direct' },
            { type: 'selector', tag: 'pick', outbounds: ['direct', 'up'] },
            { type: 'vless', tag: 'chained', detour: 'up' },
            { type: 'socks', tag: 'up', server: 's', server_port: 1 },
        ],
        route: { final: 'up', rules: [{ domain: ['a'], outbound: 'direct' }, { domain: ['b'], outbound: 'up' }], rule_set: [{ tag: 'rs', download_detour: 'up' }] },
    }
    const wheres = o.outboundReferences(config, 'up').map(ref => ref.where)
    assert.deepEqual(wheres, ['the default route (final)', 'the group "pick"', 'the outbound "chained" (detour)', 'rule 2', 'the rule set "rs" (download)'])
    assert.deepEqual(o.outboundReferences(config, 'unused'), [])
})

test('unique tags get a numeric suffix', () => {
    assert.equal(o.uniqueTag('new', ['a']), 'new')
    assert.equal(o.uniqueTag('a', ['a', 'a-2']), 'a-3')
    assert.equal(o.uniqueTag('  ', []), 'outbound')
})
