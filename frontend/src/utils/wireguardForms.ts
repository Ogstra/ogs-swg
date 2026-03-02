export const WG_INTERFACE_DEFAULTS = {
    listenPort: 51820,
    mtu: 1420,
    dns: '',
} as const

export interface WireGuardInterfaceCreateInput {
    name: string
    subnet: string
    listenPort: string
}

export interface WireGuardInterfaceEditInput {
    address: string
    listenPort: string
    mtu?: string
}

const WG_INTERFACE_NAME_PATTERN = /^wg\d+$/

const isValidIPv4 = (value: string) => {
    const parts = value.split('.')
    if (parts.length !== 4) {
        return false
    }
    return parts.every(part => {
        if (!/^\d+$/.test(part)) {
            return false
        }
        const num = Number.parseInt(part, 10)
        return num >= 0 && num <= 255
    })
}

const isValidCIDR = (value: string) => {
    const [ipPart, maskPart, ...rest] = value.trim().split('/')
    if (rest.length > 0) {
        return false
    }
    if (!ipPart || !maskPart) {
        return false
    }
    if (!isValidIPv4(ipPart)) {
        return false
    }
    if (!/^\d+$/.test(maskPart)) {
        return false
    }
    const mask = Number.parseInt(maskPart, 10)
    return mask >= 0 && mask <= 32
}

const parsePort = (raw: string) => Number.parseInt(raw.trim(), 10)

const parseMtu = (raw: string) => Number.parseInt(raw.trim(), 10)

export const normalizeWireGuardInterfaceCreateInput = (input: WireGuardInterfaceCreateInput) => ({
    name: input.name.trim(),
    subnet: input.subnet.trim(),
    listenPort: parsePort(input.listenPort),
})

export const normalizeWireGuardInterfaceEditInput = (input: WireGuardInterfaceEditInput) => ({
    address: input.address.trim(),
    listenPort: parsePort(input.listenPort),
    mtu: input.mtu !== undefined && input.mtu.trim() !== '' ? parseMtu(input.mtu) : undefined,
})

export function validateWireGuardInterfaceCreate(input: WireGuardInterfaceCreateInput): Record<string, string> {
    const normalized = normalizeWireGuardInterfaceCreateInput(input)
    const errors: Record<string, string> = {}

    if (!WG_INTERFACE_NAME_PATTERN.test(normalized.name)) {
        errors.name = 'Name must match wg<number> (e.g. wg1)'
    }
    if (!isValidCIDR(normalized.subnet)) {
        errors.subnet = 'Subnet must be a valid CIDR (e.g. 10.20.0.0/24)'
    }
    if (!Number.isFinite(normalized.listenPort) || normalized.listenPort < 1 || normalized.listenPort > 65535) {
        errors.listen_port = 'Listen port must be between 1 and 65535'
    }

    return errors
}

export function validateWireGuardInterfaceEdit(input: WireGuardInterfaceEditInput): Record<string, string> {
    const normalized = normalizeWireGuardInterfaceEditInput(input)
    const errors: Record<string, string> = {}
    const addresses = normalized.address
        .split(',')
        .map(item => item.trim())
        .filter(Boolean)

    if (addresses.length === 0 || addresses.some(item => !isValidCIDR(item))) {
        errors.address = 'Address is required and must include CIDR mask'
    }
    if (!Number.isFinite(normalized.listenPort) || normalized.listenPort < 1 || normalized.listenPort > 65535) {
        errors.listen_port = 'Listen port must be between 1 and 65535'
    }
    if (input.mtu !== undefined && input.mtu.trim() !== '') {
        if (!Number.isFinite(normalized.mtu) || (normalized.mtu ?? 0) <= 0) {
            errors.mtu = 'MTU must be a positive number'
        }
    }

    return errors
}
