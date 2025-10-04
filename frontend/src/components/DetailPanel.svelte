<script lang="ts">
  import type {torrent, peer} from '../../wailsjs/go/models'
  import PeersList from './PeersList.svelte'

  export let torrentData: torrent.Torrent | undefined
  export let peers: peer.PeerStats[]
  export let activeTab: 'details' | 'peers' = 'details'

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
  }

  function formatHash(hash: number[]): string {
    return hash.map(b => b.toString(16).padStart(2, '0')).join('')
  }

  $: meta = torrentData?.metainfo
  $: info = meta?.info
</script>

<div class="details-panel">
  <div class="details-header">
    <div class="tabs">
      <button class="tab" class:active={activeTab === 'details'} on:click={() => activeTab = 'details'}>
        Details
      </button>
      <button class="tab" class:active={activeTab === 'peers'} on:click={() => activeTab = 'peers'}>
        Peers ({peers.length})
      </button>
    </div>
  </div>
  <div class="details-content">
    {#if activeTab === 'details' && meta}
      <div class="detail-section">
        <div class="detail-label">Name</div>
        <div class="detail-value">{info?.name || 'N/A'}</div>
      </div>

      <div class="detail-section">
        <div class="detail-label">Info Hash</div>
        <div class="detail-value hash">{info?.hash ? formatHash(info.hash) : 'N/A'}</div>
      </div>

      <div class="detail-section">
        <div class="detail-label">Size</div>
        <div class="detail-value">{formatBytes(torrentData?.size || 0)}</div>
      </div>

      <div class="detail-section">
        <div class="detail-label">Piece Length</div>
        <div class="detail-value">{info?.pieceLength ? formatBytes(info.pieceLength) : 'N/A'}</div>
      </div>

      <div class="detail-section">
        <div class="detail-label">Pieces</div>
        <div class="detail-value">{info?.pieces?.length || 0} pieces</div>
      </div>

      <div class="detail-section">
        <div class="detail-label">Private</div>
        <div class="detail-value">{info?.private ? 'Yes' : 'No'}</div>
      </div>

      {#if meta.announceList && meta.announceList.length > 0}
        <div class="detail-section">
          <div class="detail-label">Trackers</div>
          <div class="detail-value">
            <div class="tracker-list">
              {#each meta.announceList as tier, i}
                {#if tier && tier.length > 0}
                  <div class="tracker-tier">
                    <div class="tracker-tier-label">Tier {i + 1}</div>
                    {#each tier as tracker}
                      <div class="tracker-url">{tracker}</div>
                    {/each}
                  </div>
                {/if}
              {/each}
            </div>
          </div>
        </div>
      {:else if meta.announce}
        <div class="detail-section">
          <div class="detail-label">Tracker</div>
          <div class="detail-value">{meta.announce}</div>
        </div>
      {/if}

      {#if meta.createdBy}
        <div class="detail-section">
          <div class="detail-label">Created By</div>
          <div class="detail-value">{meta.createdBy}</div>
        </div>
      {/if}

      {#if meta.comment}
        <div class="detail-section">
          <div class="detail-label">Comment</div>
          <div class="detail-value">{meta.comment}</div>
        </div>
      {/if}

      {#if info?.files && info.files.length > 0}
        <div class="detail-section">
          <div class="detail-label">Files</div>
          <div class="detail-value">
            <div class="file-list">
              {#each info.files as file}
                <div class="file-item">
                  <span class="file-path">{file.path.join('/')}</span>
                  <span class="file-size">{formatBytes(file.length)}</span>
                </div>
              {/each}
            </div>
          </div>
        </div>
      {/if}
    {:else if activeTab === 'peers'}
      <PeersList {peers} />
    {/if}
  </div>
</div>

<style>
  .details-panel {
    width: var(--size-panel-width);
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .details-header {
    border-bottom: 1px solid var(--color-border-primary);
  }

  .tabs {
    display: flex;
    gap: 0;
  }

  .tab {
    background-color: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--color-text-muted);
    padding: var(--spacing-4) var(--spacing-5);
    font-family: var(--font-family-base);
    font-size: var(--font-size-base);
    cursor: pointer;
    transition: all var(--transition-base);
  }

  .tab:hover {
    color: var(--color-text-tertiary);
    background-color: var(--color-bg-hover);
  }

  .tab.active {
    color: var(--color-text-primary);
    border-bottom-color: var(--color-border-hover);
  }

  .details-content {
    flex: 1;
    overflow-y: auto;
    padding: var(--spacing-5);
  }

  .detail-section {
    margin-bottom: var(--spacing-5);
  }

  .detail-section:last-child {
    margin-bottom: 0;
  }

  .detail-label {
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
    margin-bottom: 6px;
  }

  .detail-value {
    font-size: var(--font-size-base);
    color: var(--color-text-secondary);
    word-break: break-all;
  }

  .detail-value.hash {
    font-size: var(--font-size-sm);
    font-family: var(--font-family-mono);
    color: var(--color-text-muted);
  }

  .file-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .file-item {
    display: flex;
    justify-content: space-between;
    gap: var(--spacing-3);
    padding: var(--spacing-2) var(--spacing-3);
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-sm);
  }

  .file-path {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
    flex: 1;
    min-width: 0;
    word-break: break-all;
  }

  .file-size {
    font-size: var(--font-size-sm);
    color: var(--color-text-disabled);
    white-space: nowrap;
  }

  .tracker-list {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .tracker-tier {
    padding: 10px var(--spacing-3);
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-sm);
  }

  .tracker-tier-label {
    font-size: var(--font-size-xs);
    color: var(--color-text-disabled);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
    margin-bottom: 6px;
  }

  .tracker-url {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
    word-break: break-all;
    padding: var(--spacing-1) 0;
  }

  .tracker-url:not(:last-child) {
    border-bottom: 1px solid var(--color-bg-hover);
  }
</style>
