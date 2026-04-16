export function keyLengthForShadowsocksMethod(method: string): number {
    const normalized = String(method || '').trim().toLowerCase()
    switch (normalized) {
        case '2022-blake3-aes-128-gcm':
        case 'aes-128-gcm':
        case 'aes-128-ctr':
        case 'aes-128-cfb':
        case 'camellia-128-cfb':
            return 16
        case '2022-blake3-aes-256-gcm':
        case '2022-blake3-chacha20-poly1305':
        case 'aes-256-gcm':
        case 'aes-256-ctr':
        case 'aes-256-cfb':
        case 'chacha20-ietf-poly1305':
        case 'xchacha20-ietf-poly1305':
        case 'chacha20-ietf':
        case 'chacha20':
        case 'camellia-256-cfb':
            return 32
        case 'aes-192-gcm':
        case 'aes-192-ctr':
        case 'aes-192-cfb':
        case 'camellia-192-cfb':
            return 24
        default:
            return 32
    }
}
