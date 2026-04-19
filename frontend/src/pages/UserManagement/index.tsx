import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, UserStatus, CreateUserRequest } from '../../services/api'
import { Users, Plus, Trash2, RefreshCw, Edit, ArrowUp, ArrowDown, ArrowUpDown, QrCode as QrCodeIcon } from 'lucide-react'
import { QrLinkModal } from '../../components/ui/QrLinkModal'
import { useToast } from '../../context/ToastContext'
import { useAuth } from '../../context/AuthContext'
import { Button } from '../../components/ui/Button'
import { Badge } from '../../components/ui/Badge'
import { Modal } from '../../components/ui/Modal'
import { ActionIconButton } from '../../components/ui/ActionIconButton'
import { ConfirmModal } from '../../components/ui/ConfirmModal'
import { canSelectInboundUserFlow } from '../../components/singbox/inboundVisibility'
import { keyLengthForShadowsocksMethod } from '../../utils/shadowsocks'
import { formatBytes, formatTimeAgo } from '../../utils/traffic'

const BYTES_PER_GB = 1024 * 1024 * 1024
const DEFAULT_VLESS_FLOW = 'xtls-rprx-vision'
const SUPPORTED_LINK_TYPES = new Set(['vless', 'vmess', 'trojan', 'hysteria2', 'shadowsocks', 'anytls', 'naive'])
type UserType = 'vless' | 'vmess' | 'trojan' | 'hysteria2' | 'shadowsocks' | 'anytls' | 'naive'

function bytesToGbString(bytes?: number) {
    return bytes && bytes > 0 ? (bytes / BYTES_PER_GB).toFixed(2) : ''
}

function parseGbToBytes(input: string) {
    const normalized = input.replace(',', '.').trim()
    if (!normalized) return 0
    const val = parseFloat(normalized)
    if (isNaN(val) || val <= 0) return 0
    return Math.round(val * BYTES_PER_GB)
}

function isPasswordUserType(type: string): boolean {
    return type === 'trojan' || type === 'hysteria2' || type === 'shadowsocks' || type === 'anytls' || type === 'naive'
}

function generateRandomCredential(type: UserType): string {
    if (isPasswordUserType(type)) {
        if (crypto?.getRandomValues) {
            const bytes = new Uint8Array(16)
            crypto.getRandomValues(bytes)
            return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
        }
        return Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2)
    }

    if (crypto.randomUUID) return crypto.randomUUID()
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, c => {
        const r = Math.random() * 16 | 0
        const v = c === 'x' ? r : (r & 0x3 | 0x8)
        return v.toString(16)
    })
}

function formatUserTypeLabel(type: UserType): string {
    if (type === 'shadowsocks') return 'Shadowsocks'
    if (type === 'hysteria2') return 'Hysteria2'
    if (type === 'anytls') return 'AnyTLS'
    if (type === 'naive') return 'Naive'
    return type.toUpperCase()
}

export default function UserManagement() {
    const { success, error: toastError } = useToast()
    const { permissions } = useAuth()
    const canReadConfig = !!permissions?.can_read_config
    const canWriteUsers = !!permissions?.can_write_users
    const canWriteConfig = !!permissions?.can_write_config

    const queryClient = useQueryClient()
    const supportedUserTypes: UserType[] = ['vless', 'vmess', 'trojan', 'hysteria2', 'shadowsocks', 'anytls', 'naive']
    const [userType, setUserType] = useState<UserType>('vless')
    // Modals state
    const [modalState, setModalState] = useState<{
        type: 'create' | 'bulk' | 'qr' | 'usage' | 'select_inbounds' | null,
        data?: any
    }>({ type: null })
    const [selectedInboundsToRemove, setSelectedInboundsToRemove] = useState<Set<string>>(new Set())
    const [confirmDeleteUser, setConfirmDeleteUser] = useState<UserStatus | null>(null)

    const [isEditing, setIsEditing] = useState(false)
    const [sortKey, setSortKey] = useState<'user' | 'quota' | 'usage' | 'status' | 'last_seen'>('user')
    const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
    const [originalName, setOriginalName] = useState<string>('')
    const [filterInbound, setFilterInbound] = useState<string>('')

    // Usage Report State
    const [usageStart, setUsageStart] = useState<string>('')
    const [usageEnd, setUsageEnd] = useState<string>('')
    const [usageLimitGb, setUsageLimitGb] = useState<string>('0')
    const [usageData, setUsageData] = useState<any[]>([])
    const [loadingUsage, setLoadingUsage] = useState(false)
    const [credentialLoading, setCredentialLoading] = useState(false)
    const [singboxApplyLoading, setSingboxApplyLoading] = useState(false)
    const [singboxRestartConfirmOpen, setSingboxRestartConfirmOpen] = useState(false)
    const [singboxRestartLoading, setSingboxRestartLoading] = useState(false)

    // Create/Edit Form State
    const [newUser, setNewUser] = useState<CreateUserRequest>({
        name: '',
        uuid: '',
        flow: 'xtls-rprx-vision',
        vmess_security: 'auto',
        vmess_alter_id: 0,
        quota_limit: 0,
        quota_period: 'monthly',
        reset_day: 1,
        enabled: true,
        inbound_tag: ''
    })
    const [quotaInput, setQuotaInput] = useState<string>('')
    const [inboundRows, setInboundRows] = useState<{ tag: string; flow: string }[]>([])
    const [originalInboundTags, setOriginalInboundTags] = useState<string[]>([])

    // Bulk Form State
    const [bulkConfig, setBulkConfig] = useState({
        prefix: 'user',
        count: 1,
        start_index: 1,
        mode: 'sequential', // 'sequential' | 'random'
        suffix: '', // e.g. @example.com
        flow: 'xtls-rprx-vision',
        quota_limit: 0,
        quota_period: 'monthly',
        inbound_tag: ''
    })
    const [bulkQuotaInput, setBulkQuotaInput] = useState<string>('')
    const [selectedQrInbound, setSelectedQrInbound] = useState<string>('')
    const [qrLink, setQrLink] = useState<string>('')
    const [qrLoading, setQrLoading] = useState(false)
    const [qrError, setQrError] = useState<string>('')
    const [qrLinkCache, setQrLinkCache] = useState<Record<string, string>>({})

    const openQrModal = (user: UserStatus) => {
        setQrLoading(true)
        setQrLink('')
        setQrError('')
        const firstSupportedTag = (user.inbound_tags || []).find(
            tag => SUPPORTED_LINK_TYPES.has(inboundTypeByTag.get(tag) || '')
        ) || ''
        setSelectedQrInbound(firstSupportedTag)
        setQrLinkCache({})
        setModalState({ type: 'qr', data: user })
    }

    const inferInboundsFromUsers = (userList: UserStatus[]) => {
        const uniqueTags = Array.from(
            new Set(
                userList.flatMap(u => (u.inbound_tags || []).map(tag => tag.trim()).filter(Boolean))
            )
        )
        return uniqueTags.map(tag => ({ tag, type: '' }))
    }

    const usersQuery = useQuery({
        queryKey: ['users'],
        queryFn: () => api.getUsers(),
        initialData: () => queryClient.getQueryData<UserStatus[]>(['users']),
        placeholderData: previousData => previousData,
    })

    const users = usersQuery.data || []

    const inboundsQuery = useQuery({
        queryKey: ['singbox-inbounds'],
        queryFn: () => api.getSingboxInbounds(),
        enabled: canReadConfig,
        placeholderData: previousData => previousData,
    })

    useEffect(() => {
        if (usersQuery.error) {
            toastError(`Failed to fetch users: ${usersQuery.error}`)
        }
    }, [usersQuery.error, toastError])

    useEffect(() => {
        if (!canReadConfig || !inboundsQuery.error) return
        const msg = String(inboundsQuery.error || '')
        if (msg.toLowerCase().includes('forbidden') || msg.includes('403')) {
            return
        }
        toastError(`Failed to fetch inbounds: ${inboundsQuery.error}`)
    }, [canReadConfig, inboundsQuery.error, toastError])

    const inbounds = canReadConfig ? (inboundsQuery.data || []) : inferInboundsFromUsers(users)

    const pendingChangesQuery = useQuery({
        queryKey: ['dashboard-pending-changes'],
        queryFn: () => api.getDashboardData(),
        refetchInterval: 10000,
        placeholderData: previousData => previousData,
    })
    const singboxPendingChanges = !!pendingChangesQuery.data?.singbox_pending_changes

    const setSingboxPendingChanges = (pending: boolean) => {
        queryClient.setQueryData(['dashboard-pending-changes'], (old: any) => ({
            ...(old || {}),
            singbox_pending_changes: pending,
        }))
        queryClient.setQueriesData({ queryKey: ['dashboard-data'] }, (old: any) =>
            old ? { ...old, singbox_pending_changes: pending } : old
        )
    }

    const refreshUsersData = async () => {
        await Promise.all([
            queryClient.invalidateQueries({ queryKey: ['users'] }),
            queryClient.invalidateQueries({ queryKey: ['dashboard-pending-changes'] }),
            queryClient.invalidateQueries({ queryKey: ['dashboard-data'] }),
        ])
    }

    const inboundTypeByTag = new Map(
        inbounds.map(inb => [inb.tag, String(inb.type || '').toLowerCase()])
    )

    const getInboundType = (tag: string) => inboundTypeByTag.get(tag) as UserType | undefined
    const getInboundByTag = (tag: string) => inbounds.find(inb => inb.tag === tag)
    const canEditFlowForInbound = (type: string, inboundTag: string) => canSelectInboundUserFlow(type, getInboundByTag(inboundTag) || null)
    const canShowBulkFlow = (inboundTag: string) => getInboundType(inboundTag) === 'vless' && canEditFlowForInbound('vless', inboundTag)
    const normalizeInboundRowsForType = (rows: { tag: string; flow: string }[], type: string) => (
        rows.map(row => ({
            ...row,
            flow: canEditFlowForInbound(type, row.tag) ? (row.flow || DEFAULT_VLESS_FLOW) : '',
        }))
    )

    const getFirstInboundTagForType = (type: UserType) => {
        const match = inbounds.find(inb => String(inb.type || '').toLowerCase() === type)
        return match?.tag || ''
    }

    const getDefaultUserType = () => {
        for (const type of supportedUserTypes) {
            if (getFirstInboundTagForType(type)) return type
        }
        return supportedUserTypes[0] || 'vless'
    }

    const generateCredentialForType = async (type: UserType, inboundTag?: string) => {
        if (type === 'shadowsocks') {
            const inbound = getInboundByTag(inboundTag || '')
            const method = String((inbound as any)?.method || '')
            const { value } = await api.generateRandBase64(keyLengthForShadowsocksMethod(method))
            return value
        }
        return generateRandomCredential(type)
    }

    useEffect(() => {
        if (modalState.type === 'qr' && modalState.data?.inbound_tags?.length > 0) {
            const firstSupported = (modalState.data.inbound_tags as string[]).find(
                (tag: string) => SUPPORTED_LINK_TYPES.has(inboundTypeByTag.get(tag) || '')
            ) || ''
            setSelectedQrInbound(firstSupported)
            setQrLink('')
            setQrError('')
            setQrLinkCache({})
        }
        if (modalState.type !== 'qr') {
            setSelectedQrInbound('')
            setQrLink('')
            setQrError('')
            setQrLinkCache({})
        }
    }, [modalState.type, modalState.data])

    useEffect(() => {
        if (modalState.type !== 'qr' || !modalState.data || !selectedQrInbound) return
        const cached = qrLinkCache[selectedQrInbound]
        if (cached) {
            setQrLink(cached)
            setQrError('')
            return
        }
        setQrLoading(true)
        setQrError('')
        api.getUserLink(modalState.data.name, selectedQrInbound)
            .then(res => {
                setQrLink(res.link || '')
                setQrLinkCache(prev => ({ ...prev, [selectedQrInbound]: res.link }))
            })
            .catch(err => {
                setQrLink('')
                setQrError(err?.message || 'Failed to load link')
            })
            .finally(() => setQrLoading(false))
    }, [modalState.type, modalState.data, selectedQrInbound, qrLinkCache])

    const sortedUsers = users
        .filter(u => !filterInbound || (u.inbound_tags && u.inbound_tags.includes(filterInbound)) || (!u.inbound_tags && !filterInbound))
        .sort((a, b) => {
            const dir = sortDir === 'asc' ? 1 : -1
            switch (sortKey) {
                case 'quota':
                    // Sort by percentage used (Current Consumption / Limit)
                    const aRatio = a.quota_limit ? ((a.total || 0) / a.quota_limit) : 0
                    const bRatio = b.quota_limit ? ((b.total || 0) / b.quota_limit) : 0
                    if (Math.abs(aRatio - bRatio) < 0.0001) {
                        return ((a.total || 0) - (b.total || 0)) * dir
                    }
                    return (aRatio - bRatio) * dir
                case 'usage':
                    return ((a.total || 0) - (b.total || 0)) * dir
                case 'status':
                    return (a.total > (a.quota_limit || Infinity) ? 1 : 0 - (b.total > (b.quota_limit || Infinity) ? 1 : 0)) * dir
                case 'last_seen': {
                    const la = a.last_seen || 0
                    const lb = b.last_seen || 0
                    return (la - lb) * dir
                }
                case 'user':
                default:
                    return a.name.localeCompare(b.name) * dir
            }
        })

    const toggleSort = (key: typeof sortKey) => {
        if (sortKey === key) {
            setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
        } else {
            setSortKey(key)
            // Default to descending for numeric/time metrics
            if (['quota', 'usage', 'last_seen', 'status'].includes(key)) {
                setSortDir('desc')
            } else {
                setSortDir('asc')
            }
        }
    }

    const renderSortIcon = (key: typeof sortKey) => {
        if (sortKey !== key) {
            return <ArrowUpDown size={12} className="inline ml-1 text-slate-500" />
        }
        return sortDir === 'asc'
            ? <ArrowUp size={12} className="inline ml-1 text-white" />
            : <ArrowDown size={12} className="inline ml-1 text-white" />
    }

    const fetchUsage = async () => {
        setLoadingUsage(true)
        try {
            const start = usageStart ? Math.floor(new Date(usageStart).getTime() / 1000).toString() : ''
            const end = usageEnd ? Math.floor(new Date(usageEnd).getTime() / 1000).toString() : ''
            const limitBytes = parseGbToBytes(usageLimitGb)
            const data = await api.getReportSummary(start, end, limitBytes > 0 ? limitBytes : undefined)
            setUsageData(Array.isArray(data) ? data : [])
        } catch (err) {
            toastError('Failed to fetch usage: ' + err)
        } finally {
            setLoadingUsage(false)
        }
    }

    const handleRemoveFromSelectedInbounds = async () => {
        if (!canWriteUsers) {
            toastError('No write permission for sing-box users')
            return
        }
        if (!modalState.data) return

        const user = modalState.data as UserStatus
        const inboundsToRemove = Array.from(selectedInboundsToRemove)

        setModalState({ type: null })
        setSelectedInboundsToRemove(new Set())

        try {
            // If all inbounds selected, delete user completely
            if (inboundsToRemove.length === (user.inbound_tags?.length || 0)) {
                await api.deleteUser(user.name)
                success('User deleted successfully')
            } else {
                // Remove from selected inbounds only
                for (const tag of inboundsToRemove) {
                    await api.removeUserFromInbound(user.name, tag)
                }
                success(`User removed from ${inboundsToRemove.length} inbound(s)`)
            }
            await refreshUsersData()
        } catch (err) {
            toastError('Failed to remove user: ' + err)
        }
    }

    const handleEditClick = (user: UserStatus) => {
        const inboundTags = user.inbound_tags && user.inbound_tags.length > 0 ? user.inbound_tags : []
        const detectedTypes = inboundTags
            .map(tag => getInboundType(tag))
            .filter(Boolean) as UserType[]
        const uniqueTypes = Array.from(new Set(detectedTypes))
        const nextType = (uniqueTypes[0] as UserType) || getDefaultUserType()
        const primaryInboundTag = inboundTags[0] || getFirstInboundTagForType(nextType)
        setUserType(nextType)
        setNewUser({
            name: user.name,
            uuid: user.uuid || '',
            flow: user.flow || '',
            vmess_security: user.vmess_security || 'auto',
            vmess_alter_id: user.vmess_alter_id ?? 0,
            quota_limit: user.quota_limit,
            quota_period: user.quota_period,
            reset_day: 1,
            enabled: user.enabled,
            inbound_tag: primaryInboundTag
        })
        setOriginalInboundTags(inboundTags)
        setInboundRows(
            normalizeInboundRowsForType(
                [{ tag: primaryInboundTag, flow: nextType === 'vless' ? DEFAULT_VLESS_FLOW : '' }],
                nextType
            )
        )
        setQuotaInput(bytesToGbString(user.quota_limit))
        setOriginalName(user.name)
        setIsEditing(true)
        setModalState({ type: 'create' })
        api.getUserInbounds(user.name)
            .then(list => {
                if (!Array.isArray(list) || list.length === 0) return
                if (nextType === 'vless') {
                    setInboundRows(prev => prev.map(row => {
                        const match = list.find(i => i.tag === row.tag)
                        if (!match) return row
                        return {
                            ...row,
                            flow: canEditFlowForInbound(nextType, row.tag) ? (match.flow || '') : '',
                        }
                    }))
                }
                if (isPasswordUserType(nextType)) {
                    const first = list[0]
                    if (first?.password) {
                        setNewUser(prev => ({
                            ...prev,
                            uuid: first.password || prev.uuid,
                        }))
                    }
                }
                if (nextType === 'vmess') {
                    const first = list[0]
                    if (first) {
                        setNewUser(prev => ({
                            ...prev,
                            vmess_security: first.vmess_security || prev.vmess_security || 'auto',
                            vmess_alter_id: typeof first.vmess_alter_id === 'number' && first.vmess_alter_id !== 0
                                ? first.vmess_alter_id
                                : prev.vmess_alter_id || 0
                        }))
                    }
                }
            })
            .catch(err => {
                console.error('Failed to load user inbounds', err)
            })
    }

    const handleDeleteClick = (user: UserStatus) => {
        if (!canWriteUsers) return
        if (user.inbound_tags && user.inbound_tags.length > 1) {
            setSelectedInboundsToRemove(new Set(user.inbound_tags))
            setModalState({ type: 'select_inbounds', data: user })
            return
        }
        setConfirmDeleteUser(user)
    }

    const confirmDeleteSingleUser = async () => {
        if (!canWriteUsers || !confirmDeleteUser) return
        const target = confirmDeleteUser
        setConfirmDeleteUser(null)
        try {
            await api.deleteUser(target.name)
            success('User deleted')
            await refreshUsersData()
        } catch (err) {
            toastError('Failed to delete: ' + err)
        }
    }

    const handleSaveUser = async () => {
        if (!canWriteUsers) {
            toastError('No write permission for sing-box users')
            return
        }
        try {
            const normalizedRows = inboundRows.slice(0, 1).map(row => ({
                tag: row.tag.trim(),
                flow: canEditFlowForInbound(userType, row.tag.trim()) ? row.flow : ''
            }))
            const selectedInbound = normalizedRows[0]
            const emptyInbound = !selectedInbound?.tag

            if (!selectedInbound || emptyInbound) {
                toastError('Please fix inbound entries before saving')
                return
            }

            const inboundTag = selectedInbound.tag
            const selectedFlow = userType === 'vless' ? selectedInbound.flow : ''

            if (isEditing) {
                const vmessSecurity = userType === 'vmess' ? newUser.vmess_security || '' : ''
                const vmessAlterID = userType === 'vmess' ? (newUser.vmess_alter_id || 0) : 0
                const originalTags = originalInboundTags.length > 0
                    ? originalInboundTags
                    : ((modalState.data?.inbound_tags || []) as string[])
                const originalPrimaryTag = originalTags[0] || inboundTag

                const payload = {
                    ...newUser,
                    original_name: originalName || newUser.name,
                    inbound_tag: originalPrimaryTag,
                    flow: userType === 'vless' && originalPrimaryTag === inboundTag ? selectedFlow : '',
                    vmess_security: vmessSecurity,
                    vmess_alter_id: vmessAlterID,
                    reset_day: 1,
                }
                if (!payload.uuid) payload.uuid = await generateCredentialForType(userType, inboundTag)

                await api.updateUser(payload)

                if (newUser.enabled !== false) {
                    const originalSet = new Set(originalTags)

                    for (const tag of originalTags) {
                        if (tag !== inboundTag) {
                            await api.removeUserFromInbound(newUser.name, tag)
                        }
                    }

                    if (!originalSet.has(inboundTag)) {
                        await api.createUser({
                            ...newUser,
                            uuid: payload.uuid,
                            inbound_tag: inboundTag,
                            flow: selectedFlow,
                            vmess_security: vmessSecurity,
                            vmess_alter_id: vmessAlterID,
                        })
                    }
                }

                success(`User updated successfully`)
            } else {
                const payload = {
                    ...newUser,
                    vmess_security: userType === 'vmess' ? newUser.vmess_security || '' : '',
                    vmess_alter_id: userType === 'vmess' ? (newUser.vmess_alter_id || 0) : 0,
                    reset_day: 1,
                    inbound_tag: inboundTag,
                    flow: selectedFlow,
                }
                if (!payload.uuid) payload.uuid = await generateCredentialForType(userType, inboundTag)
                await api.createUser(payload)
                success(`User created successfully`)
            }
            setModalState({ type: null }) // Close modal
            // Reset form
            setNewUser({
                name: '',
                uuid: '',
                flow: 'xtls-rprx-vision',
                vmess_security: 'auto',
                vmess_alter_id: 0,
                quota_limit: 0,
                quota_period: 'monthly',
                reset_day: 1,
                enabled: true,
                inbound_tag: getFirstInboundTagForType(getDefaultUserType())
            })
            setQuotaInput('')
            setOriginalName('')
            setIsEditing(false)
            setInboundRows([])
            setOriginalInboundTags([])
            await refreshUsersData()
        } catch (err) {
            toastError('Failed to save user: ' + err)
        }
    }

    const currentInboundRow = inboundRows[0] || { tag: '', flow: '' }
    const hasEmptyInbound = !currentInboundRow.tag.trim()
    const inboundTypeMismatch = (() => {
        const type = getInboundType(currentInboundRow.tag)
        return type && type !== userType
    })()
    const unsupportedUserType = !supportedUserTypes.includes(userType)
    const hasTypeInbounds = inbounds.some(inb => String(inb.type || '').toLowerCase() === userType)
    const inboundValid = inboundRows.length === 1 && !hasEmptyInbound && !inboundTypeMismatch && !unsupportedUserType && hasTypeInbounds
    const credentialLabel = isPasswordUserType(userType) ? 'Password' : 'UUID'
    const credentialActionLabel = isPasswordUserType(userType) ? 'Generate Random Password' : 'Generate Random UUID'
    const bulkFlowVisible = canShowBulkFlow(bulkConfig.inbound_tag || '')

    const handleBulkCreate = async () => {
        if (!canWriteUsers) {
            toastError('No write permission for sing-box users')
            return
        }
        try {
            const usersToCreate: CreateUserRequest[] = []
            const bulkInboundType = getInboundType(bulkConfig.inbound_tag || '')
            const bulkVmessSecurity = bulkInboundType === 'vmess' ? 'auto' : ''
            const bulkVmessAlterID = bulkInboundType === 'vmess' ? 0 : 0
            const bulkFlow = bulkFlowVisible ? bulkConfig.flow : ''

            for (let i = 0; i < bulkConfig.count; i++) {
                let username = ''
                if (bulkConfig.mode === 'sequential') {
                    username = `${bulkConfig.prefix}-${bulkConfig.start_index + i}`
                } else {
                    const randomSuffix = Math.random().toString(36).substring(2, 6)
                    username = `${bulkConfig.prefix}-${randomSuffix}`
                }

                const fullName = `${username}${bulkConfig.suffix}`

                usersToCreate.push({
                    name: fullName,
                    uuid: await generateCredentialForType((bulkInboundType || 'vless') as UserType, bulkConfig.inbound_tag),
                    flow: bulkFlow,
                    vmess_security: bulkVmessSecurity,
                    vmess_alter_id: bulkVmessAlterID,
                    quota_limit: bulkConfig.quota_limit,
                    quota_period: bulkConfig.quota_period,
                    reset_day: 1,
                    inbound_tag: bulkConfig.inbound_tag
                })
            }

            await api.bulkCreateUsers(usersToCreate)
            success(`Bulk created ${usersToCreate.length} users successfully`)
            setModalState({ type: null })
            await refreshUsersData()
        } catch (err) {
            toastError('Failed to bulk create: ' + err)
        }
    }



    const handleApplySingboxChanges = async () => {
        if (!canWriteConfig) {
            toastError('No write permission for config changes')
            return
        }
        setSingboxApplyLoading(true)
        try {
            const result = await api.applySingboxChanges()
            if (result.restart_required) {
                setSingboxPendingChanges(true)
                setSingboxRestartConfirmOpen(true)
                return
            }

            setSingboxPendingChanges(false)
            await Promise.all([
                pendingChangesQuery.refetch(),
                queryClient.invalidateQueries({ queryKey: ['dashboard-data'] }),
            ])
            success('Sing-box configuration applied successfully')
        } catch (err) {
            setSingboxPendingChanges(true)
            toastError('Failed to apply changes. Please try again.')
        } finally {
            setSingboxApplyLoading(false)
        }
    }

    const handleConfirmSingboxRestart = async () => {
        setSingboxRestartLoading(true)
        try {
            await api.restartService('sing-box')
            setSingboxPendingChanges(false)
            setSingboxRestartConfirmOpen(false)
            success('Sing-box restart started')
        } catch (err) {
            setSingboxPendingChanges(true)
            toastError('Failed to restart Sing-box: ' + err)
        } finally {
            setSingboxRestartLoading(false)
        }
    }


    return (
        <div className="space-y-4 sm:space-y-6 pb-4 sm:pb-0">
            {/* Pending Changes Banner */}
            {singboxPendingChanges && (
                <div className="bg-yellow-900/20 border border-yellow-600/50 rounded-lg p-4 flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <Users className="text-yellow-500" size={20} />
                        <div>
                            <p className="text-sm font-medium text-yellow-200">Sing-box Configuration Changes Pending</p>
                            <p className="text-xs text-yellow-300/70 mt-0.5">User changes have been saved but not yet applied. Click "Apply Changes" to apply them.</p>
                        </div>
                    </div>
                    <Button
                        onClick={handleApplySingboxChanges}
                        variant="primary"
                        size="sm"
                        disabled={!canWriteConfig}
                        isLoading={singboxApplyLoading}
                        className="whitespace-nowrap bg-yellow-600 hover:bg-yellow-700 text-white"
                    >
                        Apply Changes
                    </Button>
                </div>
            )}

            <div className="flex flex-col sm:flex-row sm:items-center justify-end gap-0 sm:gap-4">

                <div className="grid grid-cols-2 gap-3 w-full md:w-auto md:flex md:flex-wrap">
                    <Button
                        onClick={() => {
                            if (!canWriteUsers) return
                            setIsEditing(false)
                            const nextType = getDefaultUserType()
                            setUserType(nextType)
                            setInboundRows(normalizeInboundRowsForType(
                                [{ tag: getFirstInboundTagForType(nextType), flow: nextType === 'vless' ? DEFAULT_VLESS_FLOW : '' }],
                                nextType
                            ))
                            setOriginalInboundTags([])
                            setNewUser({
                                name: '',
                                uuid: '',
                                flow: DEFAULT_VLESS_FLOW,
                                vmess_security: 'auto',
                                vmess_alter_id: 0,
                                quota_limit: 0,
                                quota_period: 'monthly',
                                reset_day: 1,
                                enabled: true,
                                inbound_tag: getFirstInboundTagForType(getDefaultUserType())
                            })
                            setQuotaInput('')
                            setModalState({ type: 'create' })
                        }}
                        icon={<Plus size={16} />}
                        variant="primary"
                        disabled={!canWriteUsers}
                        className="w-full md:w-auto justify-center"
                    >
                        Create User
                    </Button>
                    <Button
                        onClick={() => canWriteUsers && setModalState({ type: 'bulk' })}
                        variant="secondary"
                        icon={<Users size={16} />}
                        disabled={!canWriteUsers}
                        className="w-full md:w-auto justify-center"
                    >
                        Bulk Create
                    </Button>
                    <div className="flex bg-slate-800 rounded-lg p-1 border border-slate-700 w-full md:w-auto">
                        <select
                            value={filterInbound}
                            onChange={(e) => setFilterInbound(e.target.value)}
                            className="select-field bg-transparent text-slate-300 text-sm px-3 py-1 outline-none w-full text-center md:text-left"
                        >
                            <option value="">All Inbounds</option>
                            {inbounds.map((inb) => (
                                <option key={inb.tag} value={inb.tag}>{inb.tag}</option>
                            ))}
                        </select>
                    </div>
                    <Button
                        onClick={() => setModalState({ type: 'usage' })}
                        variant="secondary"
                        icon={<RefreshCw size={16} />}
                        className="w-full md:w-auto justify-center"
                    >
                        Usage Report
                    </Button>
                </div>
            </div>

            {/* Users Table Container - Matching WireGuard Style */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden shadow-sm min-h-[220px]">
                <div className="overflow-x-auto hidden md:block">
                    <table className={`w-full ${sortedUsers.length > 0 ? 'min-w-[1100px]' : 'min-w-full'} text-left border-collapse table-fixed`}>
                        <thead>
                            <tr className="bg-slate-950/50 border-b border-slate-800 text-slate-400 text-xs uppercase tracking-wider">
                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('last_seen')}>
                                    Last Seen {renderSortIcon('last_seen')}
                                </th>
                                <th className="w-[260px] p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('user')}>
                                    Name/Alias {renderSortIcon('user')}
                                </th>
                                <th className="p-4 min-w-[140px] font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('quota')}>
                                    Quota {renderSortIcon('quota')}
                                </th>

                                <th className="p-4 font-semibold cursor-pointer select-none hover:text-slate-200 transition-colors" onClick={() => toggleSort('usage')}>
                                    Data Usage {renderSortIcon('usage')}
                                </th>
                                <th className="p-4 font-semibold text-left">Inbound</th>
                                <th className="p-4 font-semibold text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {usersQuery.isSuccess && sortedUsers.length === 0 ? (
                                <tr>
                                    <td colSpan={6} className="p-12 text-center text-slate-500">
                                        <Users size={48} className="mx-auto mb-4 opacity-20" />
                                        <p>No users found.</p>
                                        <button
                                            onClick={() => canWriteUsers && setModalState({ type: 'create' })}
                                            disabled={!canWriteUsers}
                                            className="mt-4 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                        >
                                            Create user
                                        </button>
                                    </td>
                                </tr>
                            ) : (
                                sortedUsers.map(user => {
                                    const isExceeded = user.quota_limit ? user.total > user.quota_limit : false

                                    // Status Logic
                                    let statusColor = "bg-slate-600" // Default offline
                                    let statusText = "Never"
                                    let isOnline = false

                                    if (user.enabled) {
                                        if (user.last_seen) {
                                            const diff = Math.floor(Date.now() / 1000) - user.last_seen
                                            statusText = formatTimeAgo(user.last_seen)
                                            if (diff < 300) { // 5 mins
                                                statusColor = "bg-emerald-500"
                                                isOnline = true
                                            } else {
                                                statusColor = "bg-slate-700"
                                            }
                                        } else {
                                            statusColor = "bg-slate-700"
                                            statusText = "Never"
                                        }
                                    } else {
                                        statusColor = "bg-red-500"
                                        statusText = "Disabled"
                                    }

                                    if (isExceeded) {
                                        statusColor = "bg-amber-500"
                                        statusText = "Exceeded"
                                    }

                                    return (
                                        <tr
                                            key={user.name}
                                            className="border-b border-slate-800 hover:bg-slate-800/30 transition-colors"
                                        >
                                            <td className="p-4">
                                                <div className="flex items-center gap-3">
                                                    <div className={`w-2.5 h-2.5 rounded-full ring-2 ring-slate-900 ${statusColor} ${isOnline ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                                    <span className={`text-xs font-medium ${isOnline ? 'text-emerald-400' : 'text-slate-500'}`}>
                                                        {statusText}
                                                    </span>
                                                </div>
                                            </td>
                                            <td className="w-[260px] p-4">
                                                <div className="max-w-[260px] font-semibold text-slate-200 truncate" title={user.name}>{user.name}</div>
                                            </td>
                                            <td className="p-4 align-middle">
                                                <div className="w-1/2 min-w-[140px]">
                                                    <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center text-[10px] mb-1 px-1.5 font-mono text-slate-400">
                                                        <span className="truncate whitespace-nowrap">{formatBytes(user.total)}</span>
                                                        <span className="px-2 text-center text-slate-300 whitespace-nowrap">
                                                            {user.quota_limit ? `${Math.round((user.total / user.quota_limit) * 100)}%` : ''}
                                                        </span>
                                                        <span className="truncate whitespace-nowrap text-right">{user.quota_limit ? formatBytes(user.quota_limit) : '∞'}</span>
                                                    </div>
                                                    <div className="h-2.5 bg-slate-800 rounded-full overflow-hidden">
                                                        <div
                                                            className={`h-full rounded-full transition-all duration-500 ${user.quota_limit
                                                                ? ((user.total / user.quota_limit) > 1 ? 'bg-red-500' :
                                                                    (user.total / user.quota_limit) > 0.8 ? 'bg-amber-500' :
                                                                        'bg-blue-500')
                                                                : 'bg-slate-700/50'
                                                                }`}
                                                            style={{ width: `${user.quota_limit ? Math.min((user.total / user.quota_limit) * 100, 100) : 100}%` }}
                                                        />
                                                    </div>
                                                </div>
                                            </td>

                                            <td className="p-4">
                                                <div className="flex flex-col gap-1 text-[11px] font-mono">
                                                    <div className="flex items-center gap-1.5 text-emerald-400">
                                                        <ArrowUp size={12} strokeWidth={3} />
                                                        {formatBytes(user.uplink)}
                                                    </div>
                                                    <div className="flex items-center gap-1.5 text-blue-400">
                                                        <ArrowDown size={12} strokeWidth={3} />
                                                        {formatBytes(user.downlink)}
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="p-2">
                                                <div className="flex flex-wrap gap-2 max-w-full">
                                                    {(user.inbound_tags && user.inbound_tags.length > 0) ? (
                                                        user.inbound_tags.map(tag => (
                                                            <Badge key={tag} variant="info" className="max-w-[160px] truncate">
                                                                {tag}
                                                            </Badge>
                                                        ))
                                                    ) : (
                                                        <Badge variant="neutral" className="max-w-[160px] truncate">All</Badge>
                                                    )}
                                                </div>
                                            </td>
                                            <td className="p-4 text-right">
                                                <div className="flex items-center justify-end gap-2">
                                                    <ActionIconButton
                                                        onClick={() => handleEditClick(user)}
                                                        disabled={!canWriteUsers}
                                                        tone="primary"
                                                        title="Edit User"
                                                    >
                                                        <Edit size={16} />
                                                    </ActionIconButton>
                                                    <ActionIconButton
                                                        onClick={() => openQrModal(user)}
                                                        disabled={!canWriteUsers}
                                                        title="Show QR / Link"
                                                    >
                                                        <QrCodeIcon size={16} />
                                                    </ActionIconButton>
                                                    <ActionIconButton
                                                        onClick={() => handleDeleteClick(user)}
                                                        disabled={!canWriteUsers}
                                                        tone="danger"
                                                        title="Delete User"
                                                    >
                                                        <Trash2 size={16} />
                                                    </ActionIconButton>
                                                </div>
                                            </td>
                                        </tr>
                                    )
                                })
                            )}
                        </tbody>
                    </table>
                </div>

                {/* Mobile List */}
                <div className="md:hidden space-y-3">
                    {(sortedUsers || []).map(user => {
                        const isExceeded = user.quota_limit ? user.total > user.quota_limit : false
                        const quotaRatio = user.quota_limit ? (user.total / user.quota_limit) : 0
                        const quotaPercent = user.quota_limit ? `${Math.round(quotaRatio * 100)}%` : ''

                        let statusColor = "bg-slate-600"
                        let statusText = "Never"
                        let isOnline = false

                        if (user.enabled) {
                            if (user.last_seen) {
                                const diff = Math.floor(Date.now() / 1000) - user.last_seen
                                statusText = formatTimeAgo(user.last_seen)
                                if (diff < 300) {
                                    statusColor = "bg-emerald-500"
                                    isOnline = true
                                } else {
                                    statusColor = "bg-slate-700"
                                }
                            } else {
                                statusColor = "bg-slate-700"
                            }
                        } else {
                            statusColor = "bg-red-500"
                            statusText = "Disabled"
                        }

                        if (isExceeded) {
                            statusColor = "bg-amber-500"
                            statusText = "Exceeded"
                        }

                        return (
                            <div key={user.name} className="bg-slate-900 border border-slate-800 rounded-xl">
                                <div className="p-4 space-y-3">
                                    <div className="flex items-center justify-between gap-3">
                                        <div className="min-w-0 flex-1">
                                            <div className="font-semibold text-white truncate">{user.name}</div>
                                        </div>
                                        <div className="flex gap-2 shrink-0">
                                            <ActionIconButton
                                                onClick={() => handleEditClick(user)}
                                                disabled={!canWriteUsers}
                                                tone="primary"
                                            >
                                                <Edit size={16} />
                                            </ActionIconButton>
                                            <ActionIconButton
                                                onClick={() => openQrModal(user)}
                                                disabled={!canWriteUsers}
                                                title="Show QR / Link"
                                            >
                                                <QrCodeIcon size={16} />
                                            </ActionIconButton>
                                            <ActionIconButton
                                                onClick={() => handleDeleteClick(user)}
                                                disabled={!canWriteUsers}
                                                tone="danger"
                                            >
                                                <Trash2 size={16} />
                                            </ActionIconButton>
                                        </div>
                                    </div>

                                    <div className="flex items-center justify-between gap-3">
                                        <div className="flex items-center gap-2 shrink-0">
                                            <div className={`w-2 h-2 rounded-full ${statusColor} ${isOnline ? 'shadow-[0_0_8px_rgba(16,185,129,0.4)]' : ''}`}></div>
                                            <span className={`text-xs ${isOnline ? 'text-emerald-400' : 'text-slate-500'}`}>{statusText}</span>
                                        </div>
                                        <div className="flex flex-wrap justify-end gap-1.5">
                                            {(user.inbound_tags && user.inbound_tags.length > 0) ? (
                                                user.inbound_tags.map(tag => (
                                                    <Badge key={tag} variant="info" className="max-w-[200px]" title={tag}>{tag}</Badge>
                                                ))
                                            ) : (
                                                <Badge variant="neutral">All</Badge>
                                            )}
                                        </div>
                                    </div>

                                    <div className="bg-slate-950/50 rounded-lg p-3">
                                        <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center text-[10px] mb-1 px-1.5 font-mono text-slate-400">
                                            <span className="truncate whitespace-nowrap">{formatBytes(user.total)}</span>
                                            <span className="px-2 text-center text-slate-300 whitespace-nowrap">{quotaPercent}</span>
                                            <span className="truncate whitespace-nowrap text-right">{user.quota_limit ? formatBytes(user.quota_limit) : '∞'}</span>
                                        </div>
                                        <div className="h-2.5 bg-slate-800 rounded-full overflow-hidden">
                                            <div
                                                className={`h-full rounded-full transition-all duration-500 ${user.quota_limit ? (quotaRatio > 1 ? 'bg-red-500' : quotaRatio > 0.8 ? 'bg-amber-500' : 'bg-blue-500') : 'bg-slate-700/50'}`}
                                                style={{ width: `${user.quota_limit ? Math.min(quotaRatio * 100, 100) : 100}%` }}
                                            />
                                        </div>
                                    </div>
                                </div>
                            </div>
                        )
                    })}
                    {usersQuery.isSuccess && sortedUsers.length === 0 && (
                        <div className="p-8 text-center text-slate-500">
                            <Users size={48} className="mx-auto mb-4 opacity-20" />
                            <p>No users found.</p>
                            <button
                                onClick={() => canWriteUsers && setModalState({ type: 'create' })}
                                disabled={!canWriteUsers}
                                className="mt-4 px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                Create user
                            </button>
                        </div>
                    )}
                </div>
            </div>

            {/* Create/Edit User Modal */}
            <Modal
                isOpen={modalState.type === 'create'}
                onClose={() => setModalState({ type: null })}
                title={isEditing ? 'Edit User' : 'Create New User'}
                footer={
                    <>
                        <Button variant="ghost" onClick={() => setModalState({ type: null })}>Cancel</Button>
                        <Button
                            variant="primary"
                            onClick={handleSaveUser}
                            disabled={!canWriteUsers || !inboundValid}
                        >
                            {isEditing ? 'Save Changes' : 'Create User'}
                        </Button>
                    </>
                }
            >
                <div className="space-y-6 modal-form-uniform">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Username</label>
                            <input
                                type="text"
                                value={newUser.name}
                                onChange={e => setNewUser({ ...newUser, name: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                                placeholder="e.g. john_doe"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">{credentialLabel}</label>
                            <div className="flex gap-2">
                                <input
                                    type="text"
                                    value={newUser.uuid || ''}
                                    onChange={e => setNewUser({ ...newUser, uuid: e.target.value })}
                                    className="flex-1 min-w-0 bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors placeholder:text-slate-600"
                                    placeholder={isPasswordUserType(userType) ? 'Enter password or leave empty to generate one' : 'Auto-generated if empty'}
                                />
                                <Button
                                    type="button"
                                    variant="icon"
                                    size="icon"
                                    className="h-[2.625rem] w-[2.625rem] shrink-0 p-0"
                                    onClick={async () => {
                                        setCredentialLoading(true)
                                        try {
                                            const value = await generateCredentialForType(userType, currentInboundRow.tag)
                                            setNewUser(prev => ({ ...prev, uuid: value }))
                                        } catch (err) {
                                            toastError('Failed to generate credential: ' + err)
                                        } finally {
                                            setCredentialLoading(false)
                                        }
                                    }}
                                    disabled={credentialLoading}
                                    title={credentialActionLabel}
                                >
                                    <RefreshCw size={16} className={credentialLoading ? 'animate-spin' : ''} />
                                </Button>
                            </div>
                        </div>
                    </div>
                    <div className="grid grid-cols-2 gap-4 items-end">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">User Type</label>
                            <select
                                value={userType}
                                onChange={e => {
                                    const nextType = e.target.value as UserType
                                    setUserType(nextType)
                                    setInboundRows(normalizeInboundRowsForType(
                                        [{ tag: getFirstInboundTagForType(nextType), flow: nextType === 'vless' ? DEFAULT_VLESS_FLOW : '' }],
                                        nextType
                                    ))
                                }}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                            >
                                {supportedUserTypes.map(type => {
                                    const hasType = inbounds.some(inb => String(inb.type || '').toLowerCase() === type)
                                    const supported = supportedUserTypes.includes(type)
                                    return (
                                        <option key={type} value={type} disabled={!hasType || !supported}>
                                            {formatUserTypeLabel(type)}
                                        </option>
                                    )
                                })}
                            </select>
                            {!hasTypeInbounds && (
                                <p className="text-xs text-amber-400 mt-1">No inbounds available for this type.</p>
                            )}
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Account Status</label>
                            <select
                                value={(newUser.enabled ?? true) ? 'enabled' : 'disabled'}
                                onChange={e => setNewUser({ ...newUser, enabled: e.target.value === 'enabled' })}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                            >
                                <option value="enabled">Enabled</option>
                                <option value="disabled">Disabled</option>
                            </select>
                        </div>
                    </div>
                    {userType === 'vmess' && (
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">VMess Security</label>
                                <select
                                    value={newUser.vmess_security || 'auto'}
                                    onChange={e => setNewUser({ ...newUser, vmess_security: e.target.value })}
                                    className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                                >
                                    <option value="auto">auto</option>
                                    <option value="aes-128-gcm">aes-128-gcm</option>
                                    <option value="chacha20-poly1305">chacha20-poly1305</option>
                                    <option value="none">none</option>
                                </select>
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Alter ID</label>
                                <input
                                    type="number"
                                    min="0"
                                    value={newUser.vmess_alter_id ?? 0}
                                    onChange={e => setNewUser({ ...newUser, vmess_alter_id: Number(e.target.value) || 0 })}
                                    className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50 transition-colors"
                                />
                            </div>
                        </div>
                    )}
                    {/* Inbound List */}
                    <div className="space-y-3">
                        <div className={`grid gap-3 items-end ${canEditFlowForInbound(userType, currentInboundRow.tag) ? 'grid-cols-2' : 'grid-cols-1'}`}>
                            <div className="space-y-1">
                                <label className="block text-sm font-medium text-slate-400">Inbound</label>
                                <select
                                    value={currentInboundRow.tag}
                                    onChange={e => {
                                        const value = e.target.value
                                        const nextFlow = canEditFlowForInbound(userType, value)
                                            ? (currentInboundRow.flow || DEFAULT_VLESS_FLOW)
                                            : ''
                                        setInboundRows([{ tag: value, flow: nextFlow }])
                                    }}
                                    className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                                >
                                    <option value="" disabled>Select an Inbound</option>
                                    {inbounds
                                        .filter(inb => String(inb.type || '').toLowerCase() === userType)
                                        .map(inb => (
                                            <option key={inb.tag} value={inb.tag}>{inb.tag} ({inb.type})</option>
                                        ))}
                                </select>
                            </div>
                            {canEditFlowForInbound(userType, currentInboundRow.tag) && (
                                <div className="space-y-1">
                                    <label className="block text-sm font-medium text-slate-400">Flow</label>
                                    <select
                                        value={currentInboundRow.flow}
                                        onChange={e => {
                                            const value = e.target.value
                                            setInboundRows([{ ...currentInboundRow, flow: value }])
                                        }}
                                        className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors"
                                    >
                                        <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                                        <option value="">none</option>
                                    </select>
                                </div>
                            )}
                        </div>
                        {isEditing && originalInboundTags.length > 1 && (
                            <p className="text-xs text-amber-400">Legacy multi-inbound assignment detected. Saving will keep only the selected inbound.</p>
                        )}
                        {hasEmptyInbound && (
                            <p className="text-xs text-amber-400">Each inbound row must have a selected inbound.</p>
                        )}
                        {inboundTypeMismatch && (
                            <p className="text-xs text-amber-400">All inbounds must match the selected user type.</p>
                        )}
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Quota (GB)</label>
                            <input
                                type="text"
                                inputMode="decimal"
                                value={quotaInput}
                                onChange={e => {
                                    const val = e.target.value
                                    setQuotaInput(val)
                                    setNewUser({ ...newUser, quota_limit: parseGbToBytes(val) })
                                }}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                placeholder="0 for unlimited"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Quota Period</label>
                            <select
                                value={newUser.quota_period}
                                onChange={e => setNewUser({ ...newUser, quota_period: e.target.value })}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            >
                                <option value="monthly">Monthly</option>
                                <option value="total">Total (One-time)</option>
                            </select>
                        </div>
                    </div>
                </div>
            </Modal>

            <ConfirmModal
                isOpen={!!confirmDeleteUser}
                onClose={() => setConfirmDeleteUser(null)}
                onConfirm={confirmDeleteSingleUser}
                title="Delete user?"
                message={confirmDeleteUser ? `This will delete "${confirmDeleteUser.name}".` : 'This action cannot be undone.'}
                confirmLabel="Delete"
                confirmTone="danger"
            />

            <ConfirmModal
                isOpen={singboxRestartConfirmOpen}
                onClose={() => {
                    if (singboxRestartLoading) return
                    setSingboxRestartConfirmOpen(false)
                }}
                onConfirm={handleConfirmSingboxRestart}
                title="Restart Sing-box?"
                message="These configuration changes cannot be hot-reloaded through Clash API. Restart Sing-box to apply them."
                confirmLabel="Restart"
                confirmTone="primary"
                isLoading={singboxRestartLoading}
            />

            {/* Bulk Create Modal */}
            <Modal
                isOpen={modalState.type === 'bulk'}
                onClose={() => setModalState({ type: null })}
                title="Bulk Generate Users"
                footer={
                    <>
                        <Button variant="ghost" onClick={() => setModalState({ type: null })}>Cancel</Button>
                        <Button variant="primary" onClick={handleBulkCreate} disabled={!canWriteUsers}>Generate Users</Button>
                    </>
                }
            >
                <div className="space-y-4 modal-form-uniform">
                    <div className="grid grid-cols-2 sm:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Inbound (Required)</label>
                            <select
                                value={bulkConfig.inbound_tag || ''}
                                onChange={e => {
                                    const nextTag = e.target.value
                                    setBulkConfig(prev => ({
                                        ...prev,
                                        inbound_tag: nextTag,
                                        flow: canShowBulkFlow(nextTag) ? (prev.flow || DEFAULT_VLESS_FLOW) : '',
                                    }))
                                }}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            >
                                <option value="">Select Inbound</option>
                                {inbounds.map((inb: any) => (
                                    <option key={inb.tag} value={inb.tag}>{inb.tag} ({inb.type})</option>
                                ))}
                            </select>
                        </div>
                        {bulkFlowVisible ? (
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Flow</label>
                                <select
                                    value={bulkConfig.flow}
                                    onChange={e => setBulkConfig({ ...bulkConfig, flow: e.target.value })}
                                    className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                >
                                    <option value="xtls-rprx-vision">xtls-rprx-vision</option>
                                    <option value="">none</option>
                                </select>
                            </div>
                        ) : (
                            <div>
                                <label className="block text-sm font-medium text-slate-400 mb-1">Flow</label>
                                <div className="w-full rounded-lg border border-slate-800 bg-slate-950 p-2.5 text-sm text-slate-500">
                                    Not used for this inbound type
                                </div>
                            </div>
                        )}
                    </div>
                    <div className="grid grid-cols-2 sm:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Prefix</label>
                            <input
                                type="text"
                                value={bulkConfig.prefix}
                                onChange={e => setBulkConfig({ ...bulkConfig, prefix: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                placeholder="user"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Suffix (Optional)</label>
                            <input
                                type="text"
                                value={bulkConfig.suffix}
                                onChange={e => setBulkConfig({ ...bulkConfig, suffix: e.target.value })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                placeholder="@example.com"
                            />
                        </div>
                    </div>
                    <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
                        <div className="col-span-1 sm:col-span-1">
                            <label className="block text-sm font-medium text-slate-400 mb-1">Count</label>
                            <input
                                type="number"
                                min="1"
                                value={bulkConfig.count}
                                onChange={e => setBulkConfig({ ...bulkConfig, count: parseInt(e.target.value) })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            />
                        </div>
                        <div className="col-span-1 sm:col-span-1">
                            <label className="block text-sm font-medium text-slate-400 mb-1">Start Index</label>
                            <input
                                type="number"
                                min="0"
                                value={bulkConfig.start_index}
                                onChange={e => setBulkConfig({ ...bulkConfig, start_index: parseInt(e.target.value) })}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                disabled={bulkConfig.mode !== 'sequential'}
                            />
                        </div>
                        <div className="col-span-2 sm:col-span-2">
                            <label className="block text-sm font-medium text-slate-400 mb-1">Pattern</label>
                            <select
                                value={bulkConfig.mode}
                                onChange={e => setBulkConfig({ ...bulkConfig, mode: e.target.value })}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            >
                                <option value="sequential">Sequential (prefix-1...)</option>
                                <option value="random">Random Suffix (prefix-xyz...)</option>
                            </select>
                        </div>
                    </div>
                    <div className="grid grid-cols-2 sm:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Quota (GB)</label>
                            <input
                                type="text"
                                inputMode="decimal"
                                value={bulkQuotaInput}
                                onChange={e => {
                                    const val = e.target.value
                                    setBulkQuotaInput(val)
                                    setBulkConfig({ ...bulkConfig, quota_limit: parseGbToBytes(val) })
                                }}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                                placeholder="0 for unlimited"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-slate-400 mb-1">Quota Period</label>
                            <select
                                value={bulkConfig.quota_period}
                                onChange={e => setBulkConfig({ ...bulkConfig, quota_period: e.target.value as 'monthly' | 'daily' | 'none' })}
                                className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg p-2.5 text-white outline-none focus:border-blue-500/50"
                            >
                                <option value="monthly">Monthly</option>
                                <option value="daily">Daily</option>
                                <option value="none">None</option>
                            </select>
                        </div>
                    </div>
                </div>
            </Modal >

            {/* Usage Report Modal */}
            < Modal
                isOpen={modalState.type === 'usage'}
                onClose={() => setModalState({ type: null })
                }
                title="Usage Report"
                size="lg"
                footer={
                    < Button variant="ghost" onClick={() => setModalState({ type: null })}> Close</Button >
                }
            >
                <div className="space-y-6">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                            <label className="block text-sm text-slate-400 mb-1">Start Date</label>
                            <input
                                type="datetime-local"
                                value={usageStart}
                                onChange={e => setUsageStart(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white text-sm"
                            />
                        </div>
                        <div>
                            <label className="block text-sm text-slate-400 mb-1">End Date</label>
                            <input
                                type="datetime-local"
                                value={usageEnd}
                                onChange={e => setUsageEnd(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white text-sm"
                            />
                        </div>
                        <div>
                            <label className="block text-sm text-slate-400 mb-1">Filter Limit (GB)</label>
                            <input
                                type="text"
                                value={usageLimitGb}
                                onChange={e => setUsageLimitGb(e.target.value)}
                                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-2 text-white text-sm"
                                placeholder="0 = show all"
                            />
                        </div>
                        <div className="flex items-end">
                            <Button
                                onClick={fetchUsage}
                                isLoading={loadingUsage}
                                variant="primary"
                                className="w-full"
                                icon={<RefreshCw size={16} />}
                            >
                                Generate Report
                            </Button>
                        </div>
                    </div>

                    <div className="border border-slate-800 rounded-lg overflow-hidden">
                        <table className="w-full text-sm text-left">
                            <thead className="bg-slate-900 text-slate-400 font-semibold text-xs uppercase">
                                <tr>
                                    <th className="p-3">User</th>
                                    <th className="p-3 text-right">Uplink</th>
                                    <th className="p-3 text-right">Downlink</th>
                                    <th className="p-3 text-right">Total</th>
                                    <th className="p-3 text-center">Status</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-slate-800 bg-slate-950/50">
                                {usageData.length === 0 ? (
                                    <tr><td className="p-4 text-center text-slate-500 italic" colSpan={5}>No data available</td></tr>
                                ) : (
                                    usageData.map((row, idx) => (
                                        <tr key={idx} className="hover:bg-slate-800/20">
                                            <td className="p-3 font-medium text-white">{row.name}</td>
                                            <td className="p-3 text-right font-mono text-slate-300">{formatBytes(row.uplink)}</td>
                                            <td className="p-3 text-right font-mono text-slate-300">{formatBytes(row.downlink)}</td>
                                            <td className="p-3 text-right font-mono text-blue-300">{formatBytes(row.total)}</td>
                                            <td className="p-3 text-center">
                                                {row.exceeded ? (
                                                    <Badge variant="error">Exceeded</Badge>
                                                ) : (
                                                    <Badge variant="success">OK</Badge>
                                                )}
                                            </td>
                                        </tr>
                                    ))
                                )}
                            </tbody>
                        </table>
                    </div>
                </div>
            </Modal >

            {/* QR Code Modal */}
            <QrLinkModal
                isOpen={modalState.type === 'qr'}
                onClose={() => setModalState({ type: null })}
                title={modalState.data ? `${modalState.data.name}` : 'User Configuration'}
                link={qrLoading ? '' : qrLink}
                loading={qrLoading}
                error={qrError && !qrLoading ? qrError : undefined}
            />

            {/* Inbound Selection Modal */}
            <Modal
                isOpen={modalState.type === 'select_inbounds'}
                onClose={() => {
                    setModalState({ type: null })
                    setSelectedInboundsToRemove(new Set())
                }}
                title="Remove User from Inbounds"
                size="sm"
                footer={
                    <>
                        <Button variant="ghost" onClick={() => {
                            setModalState({ type: null })
                            setSelectedInboundsToRemove(new Set())
                        }}>Cancel</Button>
                        <Button
                            variant="danger"
                            onClick={handleRemoveFromSelectedInbounds}
                            disabled={!canWriteUsers || selectedInboundsToRemove.size === 0}
                        >
                            Remove from {selectedInboundsToRemove.size} Inbound(s)
                        </Button>
                    </>
                }
            >
                <div className="space-y-4">
                    <p className="text-slate-400 text-sm">
                        Select which inbound(s) to remove user <span className="text-white font-mono">{modalState.data?.name}</span> from:
                    </p>
                    <div className="space-y-2">
                        {modalState.data?.inbound_tags?.map((tag: string) => (
                            <label key={tag} className="flex items-center gap-3 p-3 bg-slate-950 border border-slate-800 rounded-lg cursor-pointer hover:border-slate-700 transition-colors">
                                <input
                                    type="checkbox"
                                    checked={selectedInboundsToRemove.has(tag)}
                                    onChange={e => {
                                        const newSet = new Set(selectedInboundsToRemove)
                                        if (e.target.checked) {
                                            newSet.add(tag)
                                        } else {
                                            newSet.delete(tag)
                                        }
                                        setSelectedInboundsToRemove(newSet)
                                    }}
                                    className="w-4 h-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900"
                                />
                                <span className="text-white font-mono text-sm">{tag}</span>
                            </label>
                        ))}
                    </div>
                    {selectedInboundsToRemove.size === modalState.data?.inbound_tags?.length && (
                        <div className="bg-amber-500/10 border border-amber-500/20 rounded-lg p-3 text-amber-400 text-xs">
                            ⚠️ All inbounds selected. User will be completely deleted.
                        </div>
                    )}
                </div>
            </Modal>
        </div >
    )
}
