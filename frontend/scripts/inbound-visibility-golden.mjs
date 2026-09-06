#!/usr/bin/env node
// Golden fixture harness for frontend/src/components/singbox/inboundVisibility.ts.
//
// Usage (from the frontend/ directory):
//   node scripts/inbound-visibility-golden.mjs --update   # (re)generate the golden file
//   node scripts/inbound-visibility-golden.mjs --check    # compare against the golden file
//   node scripts/inbound-visibility-golden.mjs            # --check if the file exists, else --update
//
// This script bundles inboundVisibility.ts with esbuild (no TS project build
// required) and exercises every exported function across a representative
// matrix of inbound types, TLS states, and transport variants, producing a
// deterministic JSON snapshot of pre-refactor behavior.

import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync, mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const FRONTEND_ROOT = join(__dirname, '..')
const GOLDEN_PATH = join(__dirname, 'inbound-visibility-golden.json')
const SOURCE_PATH = 'src/components/singbox/inboundVisibility.ts'

const TYPES = ['vless', 'vmess', 'trojan', 'hysteria2', 'shadowsocks', 'anytls', 'naive']

const TRANSPORT_VARIANTS = [
    { label: 'none', transport: null },
    { label: 'tcp', transport: { enabled: true, type: 'tcp' } },
    { label: 'ws', transport: { enabled: true, type: 'ws', path: '/ws' } },
    { label: 'grpc', transport: { enabled: true, type: 'grpc', service_name: 'svc' } },
    { label: 'http', transport: { enabled: true, type: 'http' } },
    { label: 'httpupgrade', transport: { enabled: true, type: 'httpupgrade' } },
    { label: 'ws-disabled', transport: { enabled: false, type: 'ws' } },
]

async function bundleModule() {
    const esbuildBin = join(FRONTEND_ROOT, 'node_modules', '.bin', 'esbuild')
    const outDir = mkdtempSync(join(tmpdir(), 'inbound-visibility-golden-'))
    const outfile = join(outDir, 'inbound-visibility-golden.mjs')
    execFileSync(
        esbuildBin,
        [SOURCE_PATH, '--bundle', '--format=esm', `--outfile=${outfile}`],
        { cwd: FRONTEND_ROOT, stdio: 'inherit' }
    )
    return import(pathToFileURL(outfile).href)
}

function buildGolden(mod) {
    const {
        computeInboundVisibility,
        getInboundTransportType,
        canSelectInboundUserFlow,
        getDefaultInbound,
        normalizeInboundForEditor,
        buildInboundSubmission,
    } = mod

    const entries = []

    for (const type of TYPES) {
        entries.push([`defaultInbound:${type}`, getDefaultInbound(type)])

        for (const tlsEnabled of [false, true]) {
            for (const variant of TRANSPORT_VARIANTS) {
                const inbound = { type, tls: { enabled: tlsEnabled }, transport: variant.transport }
                entries.push([
                    `visibility:${type}:tls=${tlsEnabled}:transport=${variant.label}`,
                    computeInboundVisibility(inbound),
                ])
                entries.push([
                    `transportType:${type}:tls=${tlsEnabled}:transport=${variant.label}`,
                    getInboundTransportType(inbound),
                ])
            }
        }

        entries.push([
            `visibility:${type}:reality`,
            computeInboundVisibility({ type, tls: { enabled: true, reality: { enabled: true } } }),
        ])

        entries.push([`normalizeForEditor:${type}`, normalizeInboundForEditor(getDefaultInbound(type))])

        try {
            entries.push([
                `buildSubmission:${type}`,
                buildInboundSubmission(normalizeInboundForEditor(getDefaultInbound(type))),
            ])
        } catch (err) {
            entries.push([`buildSubmission:${type}`, { error: String((err && err.message) || err) }])
        }

        for (const userType of TYPES) {
            entries.push([
                `canSelectFlow:${userType}@${type}`,
                canSelectInboundUserFlow(userType, getDefaultInbound(type)),
            ])
        }
    }

    entries.sort(([a], [b]) => a.localeCompare(b))
    return Object.fromEntries(entries)
}

function serialize(obj) {
    return JSON.stringify(obj, null, 2) + '\n'
}

async function main() {
    const args = process.argv.slice(2)
    let mode = args.includes('--update') ? 'update' : args.includes('--check') ? 'check' : null
    if (!mode) {
        mode = existsSync(GOLDEN_PATH) ? 'check' : 'update'
    }

    const mod = await bundleModule()
    const golden = buildGolden(mod)
    const serialized = serialize(golden)

    if (mode === 'update') {
        writeFileSync(GOLDEN_PATH, serialized)
        console.log(`inbound-visibility golden written (${Object.keys(golden).length} keys)`)
        return
    }

    if (!existsSync(GOLDEN_PATH)) {
        console.error(`golden file not found: ${GOLDEN_PATH} (run with --update first)`)
        process.exit(1)
    }
    const want = readFileSync(GOLDEN_PATH, 'utf8')
    if (serialized === want) {
        console.log(`inbound-visibility golden OK (${Object.keys(golden).length} keys)`)
        return
    }

    const wantObj = JSON.parse(want)
    const gotKeys = new Set(Object.keys(golden))
    const wantKeys = new Set(Object.keys(wantObj))
    const diffKeys = []
    for (const key of new Set([...gotKeys, ...wantKeys])) {
        const gotVal = JSON.stringify(golden[key])
        const wantVal = JSON.stringify(wantObj[key])
        if (gotVal !== wantVal) diffKeys.push(key)
    }
    console.error(`inbound-visibility golden MISMATCH (${diffKeys.length} differing keys)`)
    for (const key of diffKeys.slice(0, 5)) {
        console.error(`  key: ${key}`)
        console.error(`    got:  ${JSON.stringify(golden[key])}`)
        console.error(`    want: ${JSON.stringify(wantObj[key])}`)
    }
    process.exit(1)
}

main().catch(err => {
    console.error(err)
    process.exit(1)
})
