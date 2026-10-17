// Case-insensitive match where every whitespace-separated term in the query
// must appear in at least one of the fields (order and field don't matter).
export function matchesSearch(
    query: string,
    fields: ReadonlyArray<string | number | null | undefined>,
): boolean {
    const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
    if (terms.length === 0) return true
    const haystack = fields
        .filter((field): field is string | number => field !== null && field !== undefined)
        .map(field => String(field).toLowerCase())
        .join('\n')
    return terms.every(term => haystack.includes(term))
}
