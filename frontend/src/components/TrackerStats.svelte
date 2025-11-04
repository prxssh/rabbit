<script lang="ts">
    export let stats: any | undefined

    $: announceSuccessRate =
        stats && stats.totalAnnounces > 0
            ? ((stats.successfulAnnounces / stats.totalAnnounces) * 100).toFixed(2) + '%'
            : 'N/A'

    $: items = stats
        ? [
              { label: 'Total Announces', value: String(stats.totalAnnounces ?? 0) },
              { label: 'Successful Announces', value: String(stats.successfulAnnounces ?? 0) },
              { label: 'Failed Announces', value: String(stats.failedAnnounces ?? 0) },
              { label: 'Success Rate', value: announceSuccessRate },
              { label: 'Total Peers Received', value: String(stats.totalPeersReceived ?? 0) },
              { label: 'Current Seeders', value: String(stats.currentSeeders ?? 0) },
              { label: 'Current Leechers', value: String(stats.currentLeechers ?? 0) },
              { label: 'Last Announce', value: stats.lastAnnounce || 'Never' },
              { label: 'Last Success', value: stats.lastSuccess || 'Never' },
          ]
        : []
</script>

<div class="tracker-stats">
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
        <div class="empty-state">No tracker statistics available</div>
    {/if}
</div>

<style>
    .tracker-stats {
        padding: 0;
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
