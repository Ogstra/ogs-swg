// usePaginatedHistory: the single implementation of Settings history pagination (RFAC-04).
// The `offsetMode` / `mergeRefresh` / `swallowErrors` / `token` options exist solely to
// preserve per-surface legacy behavior (sampler history, subscription request history,
// audit log) while sharing one state machine.
import { useCallback, useEffect, useRef, useState } from 'react'

export interface HistoryPage<T> {
    items: T[]
    next_offset: number
    has_more: boolean
}

export interface UsePaginatedHistoryOptions<T> {
    pageSize: number
    fetchPage: (limit: number, offset: number) => Promise<HistoryPage<T>>
    getKey?: (item: T) => string | number
    mergeRefresh?: (incoming: T[], existing: T[]) => T[]
    offsetMode?: 'fetched' | 'merged'
    swallowErrors?: boolean
    token?: unknown
}

export interface UsePaginatedHistoryResult<T> {
    items: T[]
    hasMore: boolean
    refreshing: boolean
    loadingMore: boolean
    refresh: () => Promise<void>
    hardRefresh: () => Promise<void>
    loadMore: () => Promise<void>
    reset: (options?: { keepItems?: boolean }) => void
    removeItems: (predicate: (item: T) => boolean) => void
}

export function usePaginatedHistory<T>(options: UsePaginatedHistoryOptions<T>): UsePaginatedHistoryResult<T> {
    const { pageSize, offsetMode = 'fetched', swallowErrors = false } = options

    const [items, setItems] = useState<T[]>([])
    const itemsRef = useRef<T[]>([])
    const [, setNextOffsetState] = useState(0)
    const nextOffsetRef = useRef(0)
    const [hasMore, setHasMore] = useState(false)
    const hasMoreRef = useRef(false)
    const [refreshing, setRefreshing] = useState(false)
    const refreshingRef = useRef(false)
    const [loadingMore, setLoadingMore] = useState(false)
    const loadingMoreRef = useRef(false)

    const fetchPageRef = useRef(options.fetchPage)
    const getKeyRef = useRef(options.getKey)
    const mergeRefreshRef = useRef(options.mergeRefresh)
    const tokenRef = useRef(options.token)

    useEffect(() => {
        fetchPageRef.current = options.fetchPage
        getKeyRef.current = options.getKey
        mergeRefreshRef.current = options.mergeRefresh
        tokenRef.current = options.token
    })

    const commitItems = useCallback((next: T[]) => {
        itemsRef.current = next
        setItems(next)
    }, [])

    const commitNextOffset = useCallback((next: number) => {
        nextOffsetRef.current = next
        setNextOffsetState(next)
    }, [])

    const commitHasMore = useCallback((next: boolean) => {
        hasMoreRef.current = next
        setHasMore(next)
    }, [])

    const runRefresh = useCallback(async (replace: boolean) => {
        const t = tokenRef.current
        try {
            const page = await fetchPageRef.current(pageSize, 0)
            if (tokenRef.current !== t) return
            const merged = !replace && mergeRefreshRef.current
                ? mergeRefreshRef.current(page.items, itemsRef.current)
                : page.items
            commitItems(merged)
            commitNextOffset(offsetMode === 'merged' ? Math.max(page.next_offset, merged.length) : page.next_offset)
            commitHasMore(page.has_more)
        } catch {
            if (!swallowErrors) throw new Error('usePaginatedHistory: refresh failed')
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pageSize, offsetMode, swallowErrors, commitItems, commitNextOffset, commitHasMore])

    const refresh = useCallback(async () => {
        if (refreshingRef.current) return
        refreshingRef.current = true
        setRefreshing(true)
        try {
            await runRefresh(false)
        } finally {
            refreshingRef.current = false
            setRefreshing(false)
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pageSize, offsetMode, swallowErrors, options.token])

    const hardRefresh = useCallback(async () => {
        commitItems([])
        commitNextOffset(0)
        commitHasMore(false)
        try {
            await runRefresh(true)
        } finally {
            refreshingRef.current = false
            setRefreshing(false)
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pageSize, offsetMode, swallowErrors, options.token])

    const loadMore = useCallback(async () => {
        if (loadingMoreRef.current || !hasMoreRef.current) return
        const t = tokenRef.current
        const offset = nextOffsetRef.current
        loadingMoreRef.current = true
        setLoadingMore(true)
        try {
            const page = await fetchPageRef.current(pageSize, offset)
            if (tokenRef.current !== t) return
            const existing = itemsRef.current
            let incoming = page.items
            if (getKeyRef.current) {
                const getKey = getKeyRef.current
                const existingKeys = new Set(existing.map(item => getKey(item)))
                incoming = page.items.filter(item => !existingKeys.has(getKey(item)))
            }
            const merged = [...existing, ...incoming]
            commitItems(merged)
            commitNextOffset(Math.max(page.next_offset, offsetMode === 'merged' ? merged.length : offset + page.items.length))
            commitHasMore(page.has_more)
        } catch {
            if (!swallowErrors) throw new Error('usePaginatedHistory: loadMore failed')
        } finally {
            loadingMoreRef.current = false
            setLoadingMore(false)
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pageSize, offsetMode, swallowErrors, options.token])

    const reset = useCallback((resetOptions?: { keepItems?: boolean }) => {
        commitNextOffset(0)
        commitHasMore(false)
        if (!resetOptions?.keepItems) {
            commitItems([])
        }
    }, [commitItems, commitNextOffset, commitHasMore])

    const removeItems = useCallback((predicate: (item: T) => boolean) => {
        const next = itemsRef.current.filter(item => !predicate(item))
        const removed = itemsRef.current.length - next.length
        commitItems(next)
        commitNextOffset(Math.max(0, nextOffsetRef.current - removed))
    }, [commitItems, commitNextOffset])

    return {
        items,
        hasMore,
        refreshing,
        loadingMore,
        refresh,
        hardRefresh,
        loadMore,
        reset,
        removeItems,
    }
}
