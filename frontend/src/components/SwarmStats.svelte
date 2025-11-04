<script lang="ts">
    import { formatBytes, formatBytesPerSec } from '../lib/utils'

    export let stats: any | undefined

    $: items = stats
        ? [
              { label: 'Total Peers', value: String(stats.totalPeers ?? 0) },
              { label: 'Connecting', value: String(stats.connectingPeers ?? 0) },
              { label: 'Failed Connections', value: String(stats.failedConnection ?? 0) },
              { label: 'Unchoked Peers', value: String(stats.unchokedPeers ?? 0) },
              { label: 'Interested Peers', value: String(stats.interestedPeers ?? 0) },
              { label: 'Uploading To', value: String(stats.uploadingTo ?? 0) },
              { label: 'Downloading From', value: String(stats.downloadingFrom ?? 0) },
              { label: 'Total Downloaded', value: formatBytes(stats.totalDownloaded ?? 0) },
              { label: 'Total Uploaded', value: formatBytes(stats.totalUploaded ?? 0) },
              { label: 'Download Rate', value: formatBytesPerSec(stats.downloadRate ?? 0) },
              { label: 'Upload Rate', value: formatBytesPerSec(stats.uploadRate ?? 0) },
          ]
        : []
</script>

<div class="swarm-stats">
    {#if stats}
        <div class="stats-grid">
            {#each items as stat}
                <div class="stat-card">
                    <div class="stat-label">{stat.label}</div>
                    <div class="stat-value">{stat.value}</div>
                </div>
            {/each}
        </div>
    {:else}
        <div class="empty-state">No swarm statistics available</div>
    {/if}
</div>

<style>
    .swarm-stats {
        padding: var(--spacing-4);
    }

    .stats-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
        gap: var(--spacing-3);
    }

    .stat-card {
        background-color: var(--color-bg-primary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-base);
        padding: var(--spacing-4);
        display: flex;
        flex-direction: column;
        gap: var(--spacing-2);
    }

    .stat-label {
        font-size: var(--font-size-xs);
        color: var(--color-text-disabled);
        text-transform: uppercase;
        letter-spacing: var(--letter-spacing-wide);
    }

    .stat-value {
        font-size: var(--font-size-lg);
        color: var(--color-text-primary);
        font-weight: var(--font-weight-medium);
        font-family: var(--font-family-mono);
    }

    .empty-state {
        display: flex;
        align-items: center;
        justify-content: center;
        padding: var(--spacing-12);
        color: var(--color-text-disabled);
        font-size: var(--font-size-base);
    }
</style>
