<script lang="ts">
  import type {torrent, peer} from '../../wailsjs/go/models'
  import PeersList from './PeersList.svelte'
  import { formatBytes, formatHash } from '../lib/utils'

  export let torrentData: torrent.Torrent | undefined
  export let peers: peer.PeerMetrics[]
  export let activeTab: 'info' | 'peers' = 'info'

  $: meta = torrentData?.metainfo
  $: info = meta?.info
  $: infoHash = info?.hash ? formatHash(info.hash) : ''
</script>

<div class="detail-view">
  <div class="tabs">
    <button class="tab" class:active={activeTab === 'info'} on:click={() => activeTab = 'info'}>
      Info
    </button>
    <button class="tab" class:active={activeTab === 'peers'} on:click={() => activeTab = 'peers'}>
      Peers ({peers.length})
    </button>
  </div>

  <div class="content">
    {#if activeTab === 'info' && meta}
      <div class="info-grid">
        <div class="info-card">
          <div class="info-label">Name</div>
          <div class="info-value">{info?.name || 'N/A'}</div>
        </div>

        <div class="info-card">
          <div class="info-label">Info Hash</div>
          <div class="info-value hash">{info?.hash ? formatHash(info.hash) : 'N/A'}</div>
        </div>

        <div class="info-card">
          <div class="info-label">Size</div>
          <div class="info-value">{formatBytes(torrentData?.size || 0)}</div>
        </div>

        <div class="info-card">
          <div class="info-label">Piece Length</div>
          <div class="info-value">{info?.pieceLength ? formatBytes(info.pieceLength) : 'N/A'}</div>
        </div>

        <div class="info-card">
          <div class="info-label">Total Pieces</div>
          <div class="info-value">{info?.pieces?.length || 0}</div>
        </div>

        <div class="info-card">
          <div class="info-label">Private</div>
          <div class="info-value">{info?.private ? 'Yes' : 'No'}</div>
        </div>
      </div>

      {#if meta.announceList && meta.announceList.length > 0}
        <div class="section">
          <div class="section-title">Trackers</div>
          <div class="tracker-grid">
            {#each meta.announceList as tier, i}
              {#if tier && tier.length > 0}
                <div class="tracker-tier">
                  <div class="tier-label">Tier {i + 1}</div>
                  {#each tier as tracker}
                    <div class="tracker-url">{tracker}</div>
                  {/each}
                </div>
              {/if}
            {/each}
          </div>
        </div>
      {:else if meta.announce}
        <div class="section">
          <div class="section-title">Tracker</div>
          <div class="tracker-single">{meta.announce}</div>
        </div>
      {/if}

      {#if meta.createdBy || meta.comment}
        <div class="section">
          {#if meta.createdBy}
            <div class="meta-item">
              <span class="meta-label">Created By:</span>
              <span class="meta-value">{meta.createdBy}</span>
            </div>
          {/if}
          {#if meta.comment}
            <div class="meta-item">
              <span class="meta-label">Comment:</span>
              <span class="meta-value">{meta.comment}</span>
            </div>
          {/if}
        </div>
      {/if}

      {#if info?.files && info.files.length > 0}
        <div class="section">
          <div class="section-title">Files ({info.files.length})</div>
          <div class="files-grid">
            {#each info.files as file}
              <div class="file-item">
                <span class="file-path">{file.path.join('/')}</span>
                <span class="file-size">{formatBytes(file.length)}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}
    {:else if activeTab === 'peers'}
      <PeersList {peers} />
    {/if}
  </div>
</div>

<style>
  .detail-view {
    margin-top: var(--spacing-3);
    border-top: 1px solid var(--color-border-secondary);
    padding-top: var(--spacing-4);
  }

  .tabs {
    display: flex;
    gap: var(--spacing-2);
    margin-bottom: var(--spacing-4);
  }

  .tab {
    background-color: transparent;
    border: 1px solid var(--color-border-tertiary);
    color: var(--color-text-muted);
    padding: var(--spacing-2) var(--spacing-5);
    font-family: var(--font-family-base);
    font-size: var(--font-size-sm);
    cursor: pointer;
    border-radius: var(--radius-base);
    transition: all var(--transition-base);
  }

  .tab:hover {
    color: var(--color-text-tertiary);
    background-color: var(--color-bg-hover);
  }

  .tab.active {
    background-color: var(--color-bg-tertiary);
    border-color: var(--color-border-hover);
    color: var(--color-text-primary);
  }

  .content {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-5);
  }

  .info-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
    gap: var(--spacing-4);
  }

  .info-card {
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    padding: var(--spacing-4);
  }

  .info-label {
    font-size: var(--font-size-xs);
    color: var(--color-text-disabled);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
    margin-bottom: var(--spacing-2);
  }

  .info-value {
    font-size: var(--font-size-base);
    color: var(--color-text-primary);
    word-break: break-all;
  }

  .info-value.hash {
    font-size: var(--font-size-sm);
    font-family: var(--font-family-mono);
    color: var(--color-text-muted);
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .section-title {
    font-size: var(--font-size-base);
    color: var(--color-text-primary);
    font-weight: var(--font-weight-medium);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
  }

  .tracker-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: var(--spacing-3);
  }

  .tracker-tier {
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    padding: var(--spacing-3);
  }

  .tier-label {
    font-size: var(--font-size-xs);
    color: var(--color-text-disabled);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
    margin-bottom: var(--spacing-2);
  }

  .tracker-url {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
    word-break: break-all;
    padding: var(--spacing-1) 0;
  }

  .tracker-url:not(:last-child) {
    border-bottom: 1px solid var(--color-bg-hover);
    margin-bottom: var(--spacing-1);
  }

  .tracker-single {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
    padding: var(--spacing-3);
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    word-break: break-all;
  }

  .meta-item {
    display: flex;
    gap: var(--spacing-2);
    padding: var(--spacing-3);
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
  }

  .meta-label {
    font-size: var(--font-size-sm);
    color: var(--color-text-disabled);
    font-weight: var(--font-weight-medium);
  }

  .meta-value {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
  }

  .files-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(400px, 1fr));
    gap: var(--spacing-2);
  }

  .file-item {
    display: flex;
    justify-content: space-between;
    gap: var(--spacing-3);
    padding: var(--spacing-3);
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-sm);
  }

  .file-path {
    font-size: var(--font-size-sm);
    color: var(--color-text-tertiary);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-size {
    font-size: var(--font-size-sm);
    color: var(--color-text-disabled);
    white-space: nowrap;
  }
</style>
