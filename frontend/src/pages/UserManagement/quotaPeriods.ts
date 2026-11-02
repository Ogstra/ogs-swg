// The quota periods every form offers, in one place so they never drift apart.
export const QUOTA_PERIODS = [
    { value: 'daily', label: 'Daily' },
    { value: 'weekly', label: 'Weekly (from Monday)' },
    { value: 'monthly', label: 'Monthly' },
    { value: 'yearly', label: 'Yearly' },
    { value: 'total', label: 'Total (one-time)' },
] as const

export type QuotaPeriod = (typeof QUOTA_PERIODS)[number]['value']

// Older users may carry "none", which has always meant a one-time total.
export const normalizeQuotaPeriod = (period?: string): QuotaPeriod => {
    const value = (period || '').trim().toLowerCase()
    if (value === 'none') return 'total'
    return (QUOTA_PERIODS.find(item => item.value === value)?.value ?? 'monthly') as QuotaPeriod
}

export const quotaPeriodLabel = (period?: string): string => QUOTA_PERIODS.find(item => item.value === normalizeQuotaPeriod(period))?.label ?? 'Monthly'
