<script lang="ts">
  import type {peer} from '../../wailsjs/go/models'

  export let peers: peer.PeerStats[]

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
  }

  function formatBytesPerSec(bytes: number): string {
    return formatBytes(bytes) + '/s'
  }
</script>

{#if peers.length === 0}
  <div class="peers-empty">No connected peers</div>
{:else}
  <div class="peers-list">
    {#each peers as peer}
      <div class="peer-item">
        <div class="peer-header">
          <span class="peer-addr">{peer.Addr}</span>
          <div class="peer-status">
            {#if peer.IsChoked}
              <span class="peer-badge choked">Choked</span>
            {/if}
            {#if peer.IsInterested}
              <span class="peer-badge interested">Interested</span>
            {/if}
          </div>
        </div>
        <div class="peer-stats">
          <div class="peer-stat">
            <span class="peer-stat-label">↓ Downloaded</span>
            <span class="peer-stat-value">{formatBytes(peer.Downloaded)}</span>
          </div>
          <div class="peer-stat">
            <span class="peer-stat-label">↑ Uploaded</span>
            <span class="peer-stat-value">{formatBytes(peer.Uploaded)}</span>
          </div>
          <div class="peer-stat">
            <span class="peer-stat-label">↓ Rate</span>
            <span class="peer-stat-value">{formatBytesPerSec(peer.DownloadRate)}</span>
          </div>
          <div class="peer-stat">
            <span class="peer-stat-label">Connected</span>
            <span class="peer-stat-value">{Math.floor(peer.ConnectedFor / 1e9)}s</span>
          </div>
          <div class="peer-stat">
            <span class="peer-stat-label">Blocks</span>
            <span class="peer-stat-value">{peer.BlocksReceived} / {peer.BlocksFailed} failed</span>
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

<style>
  .peers-empty {
    text-align: center;
    padding: var(--spacing-10) var(--spacing-5);
    color: var(--color-text-disabled);
    font-size: var(--font-size-base);
  }

  .peers-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .peer-item {
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    padding: 14px var(--spacing-4);
  }

  .peer-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: var(--spacing-3);
    padding-bottom: 10px;
    border-bottom: 1px solid var(--color-bg-hover);
  }

  .peer-addr {
    font-size: var(--font-size-base);
    color: var(--color-text-secondary);
    font-family: var(--font-family-mono);
  }

  .peer-status {
    display: flex;
    gap: 6px;
  }

  .peer-badge {
    font-size: var(--font-size-xs);
    padding: 3px var(--spacing-2);
    border-radius: var(--radius-sm);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
  }

  .peer-badge.choked {
    background-color: var(--color-error-bg);
    color: var(--color-error);
    border: 1px solid var(--color-error-border);
  }

  .peer-badge.interested {
    background-color: var(--color-success-bg);
    color: var(--color-success);
    border: 1px solid var(--color-success-border);
  }

  .peer-stats {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 10px;
  }

  .peer-stat {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .peer-stat-label {
    font-size: var(--font-size-xs);
    color: var(--color-text-disabled);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
  }

  .peer-stat-value {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
  }
</style>
