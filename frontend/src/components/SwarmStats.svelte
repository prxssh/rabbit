<script lang="ts">
  import type {torrent} from '../../wailsjs/go/models'
  import { formatBytes, formatBytesPerSec } from '../lib/utils'

  export let swarm: torrent.SwarmMetrics | undefined

  $: stats = swarm ? [
    { label: 'Total Peers', value: swarm.totalPeers.toString() },
    { label: 'Connecting', value: swarm.connectingPeers.toString() },
    { label: 'Failed Connections', value: swarm.failedConnection.toString() },
    { label: 'Unchoked Peers', value: swarm.unchokedPeers.toString() },
    { label: 'Interested Peers', value: swarm.interestedPeers.toString() },
    { label: 'Uploading To', value: swarm.uploadingTo.toString() },
    { label: 'Downloading From', value: swarm.downloadingFrom.toString() },
    { label: 'Total Downloaded', value: formatBytes(swarm.totalDownloaded) },
    { label: 'Total Uploaded', value: formatBytes(swarm.totalUploaded) },
    { label: 'Download Rate', value: formatBytesPerSec(swarm.downloadRate) },
    { label: 'Upload Rate', value: formatBytesPerSec(swarm.uploadRate) }
  ] : []
</script>

<div class="swarm-stats">
  {#if swarm}
    <div class="stats-grid">
      {#each stats as stat}
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
