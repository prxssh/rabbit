<script lang="ts">
  import type {torrent} from '../../wailsjs/go/models'

  export let tracker: torrent.TrackerMetrics | undefined

  $: announceSuccessRate = tracker && tracker.totalAnnounces > 0
    ? ((tracker.successfulAnnounces / tracker.totalAnnounces) * 100).toFixed(2) + '%'
    : 'N/A'

  $: stats = tracker ? [
    { label: 'Total Announces', value: tracker.totalAnnounces.toString() },
    { label: 'Successful Announces', value: tracker.successfulAnnounces.toString() },
    { label: 'Failed Announces', value: tracker.failedAnnounces.toString() },
    { label: 'Success Rate', value: announceSuccessRate },
    { label: 'Total Peers Received', value: tracker.totalPeersReceived.toString() },
    { label: 'Current Seeders', value: tracker.currentSeeders.toString() },
    { label: 'Current Leechers', value: tracker.currentLeechers.toString() },
    { label: 'Last Announce', value: tracker.lastAnnounce || 'Never' },
    { label: 'Last Success', value: tracker.lastSuccess || 'Never' }
  ] : []
</script>

<div class="tracker-stats">
  {#if tracker}
    <div class="stats-grid">
      {#each stats as stat}
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
