<script lang="ts">
  import type {torrent} from '../../wailsjs/go/models'
  import ProgressBar from './ProgressBar.svelte'
  import { formatBytes } from '../lib/utils'

  export let id: number
  export let torrentData: torrent.Torrent | undefined
  export let fileName: string
  export let progress: number
  export let downloadSpeed: string
  export let uploadSpeed: string
  export let selected: boolean = false
  export let onSelect: () => void
  export let onRemove: () => void
  export let onSettings: () => void
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
  <div class="action-buttons">
    <button class="settings-btn" on:click|stopPropagation={onSettings} aria-label="Torrent settings" title="Settings">
      ⚙
    </button>
    <button class="remove-btn" on:click|stopPropagation={onRemove} aria-label="Remove torrent" title="Remove">
      ×
    </button>
  </div>
</div>

<style>
  .torrent-item {
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    margin-bottom: var(--spacing-2);
    padding: var(--spacing-4) var(--spacing-5);
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--spacing-4);
    cursor: pointer;
    transition: all var(--transition-base);
  }

  .torrent-item:hover {
    background-color: var(--color-bg-hover);
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

  .action-buttons {
    display: flex;
    gap: var(--spacing-2);
  }

  .settings-btn {
    background-color: transparent;
    border: 1px solid var(--color-border-tertiary);
    color: var(--color-text-disabled);
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: var(--font-family-base);
    font-size: 16px;
    line-height: 1;
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: all var(--transition-base);
    opacity: 0.6;
  }

  .settings-btn:hover {
    background-color: var(--color-bg-hover);
    border-color: var(--color-border-active);
    color: var(--color-text-secondary);
    opacity: 1;
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
