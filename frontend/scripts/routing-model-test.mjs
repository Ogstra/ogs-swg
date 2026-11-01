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
