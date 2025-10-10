<script lang="ts">
  import type {peer} from '../../wailsjs/go/models'
  import Badge from './ui/Badge.svelte'
  import { formatBytes, formatBytesPerSec } from '../lib/utils'

  export let peers: peer.PeerMetrics[]

  // Sort peers by connection time (descending - longest connected first)
  $: sortedPeers = [...peers].sort((a, b) => b.ConnectedFor - a.ConnectedFor)
</script>

{#if peers.length === 0}
  <div class="peers-empty">No connected peers</div>
{:else}
  <div class="table-wrapper">
    <table class="peers-table">
      <thead>
        <tr>
          <th>Address</th>
          <th>Status</th>
          <th>Downloaded</th>
          <th>Uploaded</th>
          <th>Down Rate</th>
          <th>Up Rate</th>
        </tr>
      </thead>
      <tbody>
        {#each sortedPeers as peer}
          <tr>
            <td class="peer-addr">{peer.Addr}</td>
            <td class="peer-status">
              <div class="status-badges">
                {#if peer.IsChoked}
                  <Badge variant="error" text="Choked" />
                {:else}
                  <Badge variant="success" text="Unchoked" />
                {/if}
                {#if peer.IsInterested}
                  <Badge variant="success" text="Interested" />
                {/if}
              </div>
            </td>
            <td>{formatBytes(peer.Downloaded)}</td>
            <td>{formatBytes(peer.Uploaded)}</td>
            <td>{formatBytesPerSec(peer.DownloadRate)}</td>
            <td>{formatBytesPerSec(peer.UploadRate)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .peers-empty {
    text-align: center;
    padding: var(--spacing-10) var(--spacing-5);
    color: var(--color-text-disabled);
    font-size: var(--font-size-base);
  }

  .table-wrapper {
    overflow-x: auto;
  }

  .peers-table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--font-size-sm);
  }

  .peers-table thead {
    background-color: var(--color-bg-primary);
    border-bottom: 1px solid var(--color-border-primary);
  }

  .peers-table th {
    padding: var(--spacing-3) var(--spacing-4);
    text-align: left;
    font-size: var(--font-size-xs);
    color: var(--color-text-disabled);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
    font-weight: var(--font-weight-semibold);
  }

  .peers-table tbody tr {
    border-bottom: 1px solid var(--color-border-primary);
    transition: background-color 0.15s ease;
  }

  .peers-table tbody tr:hover {
    background-color: var(--color-bg-hover);
  }

  .peers-table tbody tr:last-child {
    border-bottom: none;
  }

  .peers-table td {
    padding: var(--spacing-3) var(--spacing-4);
    color: var(--color-text-tertiary);
  }

  .peer-addr {
    font-family: var(--font-family-mono);
    color: var(--color-text-secondary);
  }

  .status-badges {
    display: flex;
    gap: var(--spacing-2);
    flex-wrap: wrap;
  }
</style>
