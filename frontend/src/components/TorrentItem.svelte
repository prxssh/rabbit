<script lang="ts">
  import type {torrent} from '../../wailsjs/go/models'
  import ProgressBar from './ProgressBar.svelte'

  export let id: number
  export let torrentData: torrent.Torrent | undefined
  export let fileName: string
  export let progress: number
  export let downloadSpeed: string
  export let uploadSpeed: string
  export let selected: boolean = false
  export let onSelect: () => void
  export let onRemove: () => void

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
  }
</script>

<div class="torrent-item" class:selected on:click={onSelect}>
  <div class="torrent-main">
    <div class="torrent-header">
      <div class="torrent-name">{torrentData?.metainfo?.info?.name || fileName}</div>
      <span class="torrent-progress">{progress.toFixed(1)}%</span>
    </div>
    <ProgressBar progress={progress} height="6px" />
    <div class="torrent-info">
      <span class="info-item">{formatBytes(torrentData?.size || 0)}</span>
      <span class="info-item">↓ {downloadSpeed}</span>
      <span class="info-item">↑ {uploadSpeed}</span>
    </div>
  </div>
  <button class="remove-btn" on:click|stopPropagation={onRemove} aria-label="Remove torrent">
    ×
  </button>
</div>

<style>
  .torrent-item {
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border-primary);
    padding: var(--spacing-4) var(--spacing-5);
    margin-bottom: var(--spacing-2);
    border-radius: var(--radius-base);
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--spacing-4);
    transition: all var(--transition-base);
    cursor: pointer;
  }

  .torrent-item:hover {
    background-color: var(--color-bg-hover);
    border-color: var(--color-border-secondary);
  }

  .torrent-item.selected {
    background-color: var(--color-bg-tertiary);
    border-color: var(--color-border-tertiary);
  }

  .torrent-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .torrent-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--spacing-3);
  }

  .torrent-name {
    font-size: var(--font-size-md);
    color: var(--color-text-primary);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .torrent-progress {
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
    font-weight: var(--font-weight-medium);
    white-space: nowrap;
  }

  .torrent-info {
    display: flex;
    gap: var(--spacing-5);
  }

  .info-item {
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }

  .remove-btn {
    background-color: transparent;
    border: 1px solid var(--color-border-tertiary);
    color: var(--color-text-disabled);
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: var(--font-family-base);
    font-size: 24px;
    line-height: 1;
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: all var(--transition-base);
    opacity: 0.6;
  }

  .remove-btn:hover {
    background-color: var(--color-error-bg);
    border-color: var(--color-error-border);
    color: var(--color-error);
    opacity: 1;
  }
</style>
