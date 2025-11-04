/**
 * Format bytes to human-readable string
 */
export function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
}

/**
 * Format bytes per second
 */
export function formatBytesPerSec(bytes: number): string {
    return formatBytes(bytes) + '/s'
}

/**
 * Format hash array to hex string
 */
export function formatHash(hash: number[]): string {
    return hash.map((b) => b.toString(16).padStart(2, '0')).join('')
}
