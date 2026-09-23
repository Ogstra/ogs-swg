// Several places reload the page to recover from a transient failure (a
// stale chunk after a deploy, a 401 in demo mode caused by a brief container
// restart). Without a guard, a failure that doesn't actually clear reloads
// forever instead of surfacing to the user. Each call site uses its own key
// so unrelated reload loops don't interfere with each other.
const KEY_PREFIX = 'ogs-swg:reload-attempted:'

export function attemptReloadOnce(key: string): boolean {
    if (typeof window === 'undefined') return false
    const storageKey = KEY_PREFIX + key
    if (sessionStorage.getItem(storageKey)) return false
    sessionStorage.setItem(storageKey, '1')
    window.location.reload()
    return true
}

export function clearReloadGuard(key: string): void {
    if (typeof window === 'undefined') return
    sessionStorage.removeItem(KEY_PREFIX + key)
}

// For failures that can be transient and self-heal on their own (e.g. the
// backend container briefly restarting) a one-shot guard permanently blocks
// recovery once it fires. This instead rate-limits: reload at most once per
// minIntervalMs, so a still-down backend doesn't hot-loop reloads, but a
// recovered one is picked up on the next attempt after the interval.
export function attemptReloadThrottled(key: string, minIntervalMs: number): boolean {
    if (typeof window === 'undefined') return false
    const storageKey = KEY_PREFIX + key
    const last = Number(sessionStorage.getItem(storageKey) || '0')
    const now = Date.now()
    if (now - last < minIntervalMs) return false
    sessionStorage.setItem(storageKey, String(now))
    window.location.reload()
    return true
}
