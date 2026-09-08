import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, Save } from 'lucide-react'
import { Card } from '../../../components/ui/Card'
import { Button } from '../../../components/ui/Button'
import { api, type NtfySettingsRequest } from '../../../services/api'

interface NotificationsTabProps {
    canWriteSettings: boolean
    success: (msg: string) => void
    toastError: (msg: string) => void
}

type AuthMode = 'none' | 'bearer' | 'basic'

export default function NotificationsTab({ canWriteSettings, success, toastError }: NotificationsTabProps) {
    const queryClient = useQueryClient()

    const [serverURL, setServerURL] = useState('')
    const [topic, setTopic] = useState('')
    const [authMode, setAuthMode] = useState<AuthMode>('none')
    const [bearerToken, setBearerToken] = useState('')
    const [basicUser, setBasicUser] = useState('')
    const [basicPass, setBasicPass] = useState('')
    const [enableSingboxDown, setEnableSingboxDown] = useState(false)
    const [enableWireguardDown, setEnableWireguardDown] = useState(false)
    const [enableHighTraffic, setEnableHighTraffic] = useState(false)
    const [enableConfigErrors, setEnableConfigErrors] = useState(false)
    const [trafficThresholdBytes, setTrafficThresholdBytes] = useState(0)

    const settingsQuery = useQuery({
        queryKey: ['ntfy-settings'],
        queryFn: () => api.getNtfySettings(),
        placeholderData: previousData => previousData,
    })

    useEffect(() => {
        const data = settingsQuery.data
        if (!data) return
        setServerURL(data.server_url || '')
        setTopic(data.topic || '')
        setAuthMode(data.auth_mode || 'none')
        setBearerToken('')
        setBasicUser(data.basic_user || '')
        setBasicPass('')
        setEnableSingboxDown(!!data.enable_singbox_down)
        setEnableWireguardDown(!!data.enable_wireguard_down)
        setEnableHighTraffic(!!data.enable_high_traffic)
        setEnableConfigErrors(!!data.enable_config_errors)
        setTrafficThresholdBytes(data.traffic_threshold_bytes || 0)
    }, [settingsQuery.data])

    const buildPayload = (): NtfySettingsRequest => ({
        server_url: serverURL.trim(),
        topic: topic.trim(),
        auth_mode: authMode,
        bearer_token: bearerToken,
        basic_user: basicUser.trim(),
        basic_pass: basicPass,
        enable_singbox_down: enableSingboxDown,
        enable_wireguard_down: enableWireguardDown,
        enable_high_traffic: enableHighTraffic,
        enable_config_errors: enableConfigErrors,
        traffic_threshold_bytes: trafficThresholdBytes,
    })

    const saveMutation = useMutation({
        mutationFn: async () => api.updateNtfySettings(buildPayload()),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ['ntfy-settings'] })
            success('Notification settings saved')
        },
        onError: err => {
            toastError('Failed to save notification settings: ' + err)
        },
    })

    const testMutation = useMutation({
        mutationFn: async () => api.sendTestNtfyNotification(buildPayload()),
        onSuccess: () => {
            success('Test notification sent')
        },
        onError: err => {
            toastError('Test notification failed: ' + err)
        },
    })

    const handleSave = () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        saveMutation.mutate()
    }

    const handleTest = () => {
        if (!canWriteSettings) {
            toastError('No write permission for settings')
            return
        }
        testMutation.mutate()
    }

    const data = settingsQuery.data

    return (
        <div className="space-y-4 sm:space-y-6">
            <Card title="ntfy Server">
                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Server URL
                        </label>
                        <input
                            type="text"
                            value={serverURL}
                            onChange={e => setServerURL(e.target.value)}
                            disabled={!canWriteSettings}
                            placeholder="https://ntfy.example.com"
                            className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono disabled:opacity-60"
                        />
                        <p className="text-xs text-slate-500 mt-1">
                            Self-hosted instance or https://ntfy.sh
                        </p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Topic
                        </label>
                        <input
                            type="text"
                            value={topic}
                            onChange={e => setTopic(e.target.value)}
                            disabled={!canWriteSettings}
                            placeholder="ogs-panel-alerts"
                            className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono disabled:opacity-60"
                        />
                        <p className="text-xs text-slate-500 mt-1">
                            Alphanumeric, dash and underscore, max 64 characters.
                        </p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Authentication
                        </label>
                        <select
                            value={authMode}
                            onChange={e => setAuthMode(e.target.value as AuthMode)}
                            disabled={!canWriteSettings}
                            className="select-field w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                        >
                            <option value="none">None (public topic)</option>
                            <option value="bearer">Access token</option>
                            <option value="basic">Username / password</option>
                        </select>
                    </div>

                    {authMode === 'bearer' && (
                        <div>
                            <label className="block text-sm font-medium text-slate-300 mb-2">
                                Access token
                            </label>
                            <input
                                type="password"
                                autoComplete="off"
                                value={bearerToken}
                                onChange={e => setBearerToken(e.target.value)}
                                disabled={!canWriteSettings}
                                placeholder={data?.has_bearer_token ? 'Saved — leave blank to keep' : 'Paste your ntfy access token'}
                                className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono disabled:opacity-60"
                            />
                        </div>
                    )}

                    {authMode === 'basic' && (
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-2">
                                    Username
                                </label>
                                <input
                                    type="text"
                                    value={basicUser}
                                    onChange={e => setBasicUser(e.target.value)}
                                    disabled={!canWriteSettings}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono disabled:opacity-60"
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-slate-300 mb-2">
                                    Password
                                </label>
                                <input
                                    type="password"
                                    autoComplete="off"
                                    value={basicPass}
                                    onChange={e => setBasicPass(e.target.value)}
                                    disabled={!canWriteSettings}
                                    placeholder={data?.has_basic_pass ? 'Saved — leave blank to keep' : 'Password'}
                                    className="w-full bg-slate-950 border border-slate-800 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors font-mono disabled:opacity-60"
                                />
                            </div>
                        </div>
                    )}

                    <div className="flex justify-end gap-2">
                        <Button
                            onClick={handleTest}
                            variant="secondary"
                            size="sm"
                            icon={<Bell size={16} />}
                            isLoading={testMutation.isPending}
                            disabled={!canWriteSettings || !serverURL.trim() || !topic.trim()}
                        >
                            Send test notification
                        </Button>
                        <Button onClick={handleSave} size="sm" icon={<Save size={16} />} isLoading={saveMutation.isPending} disabled={!canWriteSettings}>
                            Save
                        </Button>
                    </div>
                </div>
            </Card>

            <Card title="Event Notifications">
                <div className="space-y-4">
                    <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                        <input
                            type="checkbox"
                            checked={enableSingboxDown}
                            onChange={e => setEnableSingboxDown(e.target.checked)}
                            disabled={!canWriteSettings}
                            className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 disabled:opacity-60"
                        />
                        <div>
                            <div className="font-semibold text-white">Sing-box service down / recovered</div>
                        </div>
                    </label>

                    <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                        <input
                            type="checkbox"
                            checked={enableWireguardDown}
                            onChange={e => setEnableWireguardDown(e.target.checked)}
                            disabled={!canWriteSettings}
                            className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 disabled:opacity-60"
                        />
                        <div>
                            <div className="font-semibold text-white">WireGuard service down / recovered</div>
                        </div>
                    </label>

                    <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                        <input
                            type="checkbox"
                            checked={enableConfigErrors}
                            onChange={e => setEnableConfigErrors(e.target.checked)}
                            disabled={!canWriteSettings}
                            className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 disabled:opacity-60"
                        />
                        <div>
                            <div className="font-semibold text-white">Sing-box config apply failure or crash after reload</div>
                        </div>
                    </label>

                    <label className="flex items-start gap-4 p-4 bg-slate-950 border border-slate-800 rounded-xl cursor-pointer hover:border-slate-700 transition-colors">
                        <input
                            type="checkbox"
                            checked={enableHighTraffic}
                            onChange={e => setEnableHighTraffic(e.target.checked)}
                            disabled={!canWriteSettings}
                            className="mt-1 h-4 w-4 rounded border-slate-700 bg-slate-900 text-blue-600 focus:ring-offset-slate-900 disabled:opacity-60"
                        />
                        <div>
                            <div className="font-semibold text-white">High total traffic threshold reached</div>
                        </div>
                    </label>

                    <div>
                        <label className="block text-sm font-medium text-slate-300 mb-2">
                            Traffic threshold (bytes)
                        </label>
                        <input
                            type="number"
                            min={0}
                            value={trafficThresholdBytes}
                            onChange={e => setTrafficThresholdBytes(Math.max(0, Number(e.target.value) || 0))}
                            disabled={!canWriteSettings}
                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-blue-500/50 transition-colors disabled:opacity-60"
                        />
                        <p className="text-xs text-slate-500 mt-1">
                            Total sing-box + WireGuard bytes measured over each sampler interval. 0 disables this alert.
                        </p>
                    </div>

                    <div className="flex justify-end">
                        <Button onClick={handleSave} size="sm" icon={<Save size={16} />} isLoading={saveMutation.isPending} disabled={!canWriteSettings}>
                            Save
                        </Button>
                    </div>
                </div>
            </Card>
        </div>
    )
}
