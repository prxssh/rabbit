<script lang="ts">
  import type {torrent, peer} from '../wailsjs/go/models'
  import PeersList from './PeersList.svelte'
  import PiecesHeatmap from './PiecesHeatmap.svelte'
  import InfoCard from './ui/InfoCard.svelte'
  import TabGroup from './ui/TabGroup.svelte'
  import Section from './ui/Section.svelte'
  import SwarmStats from './SwarmStats.svelte'
  import TrackerStats from './TrackerStats.svelte'
  import { formatBytes, formatHash } from '../lib/utils'

  export let torrentData: torrent.Torrent | undefined
  export let peers: peer.PeerMetrics[]
  export let pieceStates: number[] = []
  export let stats: any | undefined = undefined

  let activeTab: 'details' | 'peers' = 'details'

  $: meta = torrentData?.metainfo
  $: info = meta?.info
  $: infoHash = info?.hash ? formatHash(info.hash) : ''
  $: totalPieces = info?.pieces?.length || 0
  $: effectivePieceStates = pieceStates.length > 0 ? pieceStates : new Array(totalPieces).fill(0)

  $: tabs = [
    { id: 'details', label: 'Details' },
    { id: 'peers', label: 'Peers', count: peers.length }
  ]

  function handleTabChange(tabId: string) {
    activeTab = tabId as 'details' | 'peers'
  }

  // TrackerStats renders its own computed metrics
</script>

<div class="detail-panel">
  <TabGroup {tabs} {activeTab} onTabChange={handleTabChange} />

  <div class="panel-content">
    {#if meta}
      {#if activeTab === 'details'}
        <Section title="General Information">
          <div class="info-grid">
            <InfoCard label="Name" value={info?.name || 'N/A'} />
            <InfoCard label="Info Hash" value={info?.hash ? formatHash(info.hash) : 'N/A'} mono />
            <InfoCard label="Size" value={formatBytes(torrentData?.size || 0)} />
            <InfoCard label="Piece Length" value={info?.pieceLength ? formatBytes(info.pieceLength) : 'N/A'} />
            <InfoCard label="Total Pieces" value={String(info?.pieces?.length || 0)} />
            <InfoCard label="Private" value={info?.private ? 'Yes' : 'No'} />
          </div>
        </Section>

        {#if meta.announceList && meta.announceList.length > 0}
          <Section title="Trackers">
            <div class="tracker-details">
              <TrackerStats {stats} />
            </div>
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
          </Section>
        {:else if meta.announce}
          <Section title="Tracker">
            <div class="tracker-details">
              <TrackerStats {stats} />
            </div>
            <div class="tracker-single">{meta.announce}</div>
          </Section>
        {/if}

        {#if info?.files && info.files.length > 0}
          <Section title="Files ({info.files.length})">
            <div class="files-grid">
              {#each info.files as file}
                <div class="file-item">
                  <span class="file-path">{file.path.join('/')}</span>
                  <span class="file-size">{formatBytes(file.length)}</span>
                </div>
              {/each}
            </div>
          </Section>
        {/if}
      {:else if activeTab === 'peers'}
        <Section title="Swarm Statistics">
          <SwarmStats {stats} />
        </Section>

        <Section title="Pieces">
          <PiecesHeatmap pieceStates={effectivePieceStates} totalPieces={totalPieces} />
        </Section>

        <Section title="Peers ({peers.length})">
          {#if peers.length > 0}
            <PeersList {peers} />
          {:else}
            <div class="empty-state">No peers connected</div>
          {/if}
        </Section>
      {/if}
    {/if}
  </div>
</div>

<style>
  .detail-panel {
    height: 100%;
    background-color: var(--color-bg-secondary);
    border-top: 1px solid var(--color-border-primary);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .panel-content {
    flex: 1;
    overflow-y: auto;
    padding: var(--spacing-6);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-6);
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--color-text-disabled);
    font-size: var(--font-size-base);
  }

  .info-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
    gap: var(--spacing-4);
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

  

  

  .tracker-details {
    margin: 0 0 var(--spacing-3) 0;
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
