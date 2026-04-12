import type { Subscription, SubscriptionMember, SubscriptionMutationRequest } from '../../services/api'

export type SubscriptionProfileAliasDraft = {
    username: string
    alias: string
}

export type SelectedSubscriptionProfile = SubscriptionProfileAliasDraft

const normalizeUsername = (username: string): string => username.trim()

const normalizeAlias = (alias: string): string => alias.trim()

export const hydrateSubscriptionProfileAliases = (
    usernames: string[],
    members: SubscriptionMember[] = []
): SelectedSubscriptionProfile[] => {
    const byUsername = new Map<string, string>()
    for (const member of members) {
        const username = normalizeUsername(member.username)
        if (!username || byUsername.has(username)) {
            continue
        }
        byUsername.set(username, normalizeAlias(member.alias))
    }

    const seen = new Set<string>()
    const drafts: SelectedSubscriptionProfile[] = []
    for (const username of usernames) {
        const normalizedUsername = normalizeUsername(username)
        if (!normalizedUsername || seen.has(normalizedUsername)) {
            continue
        }
        seen.add(normalizedUsername)
        drafts.push({
            username: normalizedUsername,
            alias: byUsername.get(normalizedUsername) ?? '',
        })
    }
    return drafts
}

export const getSubscriptionProfileDrafts = (subscription: Pick<Subscription, 'users' | 'members'>): SelectedSubscriptionProfile[] => (
    hydrateSubscriptionProfileAliases(subscription.users || [], subscription.members || [])
)

export const serializeSubscriptionProfileAliases = (drafts: SelectedSubscriptionProfile[]): SubscriptionMutationRequest['members'] => {
    const seen = new Set<string>()
    const members: SubscriptionMember[] = []

    for (const draft of drafts) {
        const username = normalizeUsername(draft.username)
        if (!username || seen.has(username)) {
            continue
        }
        seen.add(username)
        members.push({
            username,
            alias: normalizeAlias(draft.alias),
        })
    }

    return members
}

export const getSubscriptionProfileAliasSummary = (drafts: SelectedSubscriptionProfile[]): string[] => (
    drafts.map(draft => {
        const alias = normalizeAlias(draft.alias)
        return alias ? `${alias} (${draft.username})` : draft.username
    })
)
