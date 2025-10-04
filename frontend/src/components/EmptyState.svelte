<script lang="ts">
  export let onAddTorrent: () => void
</script>

<div class="empty-state">
  <div class="empty-icon">
    <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
      <polyline points="17 8 12 3 7 8"/>
      <line x1="12" y1="3" x2="12" y2="15"/>
    </svg>
  </div>
  <p class="empty-text">No torrents yet</p>
  <p class="empty-hint">Drop a .torrent file anywhere or click the button below</p>
  <button class="add-button" on:click={onAddTorrent}>
    + Add Torrent
  </button>
</div>

<style>
  .empty-state {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--spacing-4);
  }

  .empty-icon {
    color: var(--color-text-disabled);
    margin-bottom: var(--spacing-2);
  }

  .empty-text {
    font-size: var(--font-size-2xl);
    color: var(--color-text-tertiary);
  }

  .empty-hint {
    font-size: var(--font-size-md);
    color: var(--color-text-muted);
    margin-bottom: var(--spacing-4);
  }

  .add-button {
    background-color: var(--color-bg-tertiary);
    border: 1px solid var(--color-border-active);
    color: var(--color-text-primary);
    padding: var(--spacing-3) var(--spacing-8);
    font-family: var(--font-family-base);
    font-size: var(--font-size-md);
    cursor: pointer;
    border-radius: var(--radius-base);
    transition: all var(--transition-base);
  }

  .add-button:hover {
    background-color: var(--color-bg-elevated);
    border-color: var(--color-border-hover);
    color: var(--color-text-primary);
  }
</style>
