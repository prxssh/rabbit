<script lang="ts">
  export let torrentCount: number = 0
  export let downloadSpeed: string = '0 KB/s'
  export let uploadSpeed: string = '0 KB/s'
  export let onAddTorrent: () => void
</script>

<div class="topbar">
  <div class="logo">
    <span class="logo-text">rabbit</span>
    <span class="logo-subtitle">BitTorrent Client</span>
  </div>
  <div class="topbar-actions">
    <div class="stats">
      <span class="stat-item">↓ {downloadSpeed}</span>
      <span class="stat-item">↑ {uploadSpeed}</span>
      <span class="stat-item">{torrentCount} torrents</span>
    </div>
    <button class="topbar-add-button" on:click={onAddTorrent}>
      + Add Torrent
    </button>
  </div>
</div>

<style>
  .topbar {
    height: var(--size-topbar);
    border-bottom: 1px solid var(--color-border-primary);
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 var(--spacing-6);
    background-color: var(--color-bg-primary);
  }

  .logo {
    display: flex;
    align-items: baseline;
    gap: var(--spacing-3);
  }

  .logo-text {
    font-size: var(--font-size-xl);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
    letter-spacing: var(--letter-spacing-wider);
  }

  .logo-subtitle {
    font-size: var(--font-size-base);
    color: var(--color-text-muted);
    letter-spacing: var(--letter-spacing-wide);
    text-transform: uppercase;
  }

  .topbar-actions {
    display: flex;
    align-items: center;
    gap: var(--spacing-8);
  }

  .stats {
    display: flex;
    gap: var(--spacing-6);
  }

  .stat-item {
    font-size: var(--font-size-base);
    color: var(--color-text-secondary);
  }

  .topbar-add-button {
    background-color: var(--color-bg-tertiary);
    border: 1px solid var(--color-border-active);
    color: var(--color-text-primary);
    padding: var(--spacing-2) var(--spacing-5);
    font-family: var(--font-family-base);
    font-size: var(--font-size-base);
    cursor: pointer;
    border-radius: var(--radius-base);
    transition: all var(--transition-base);
  }

  .topbar-add-button:hover {
    background-color: var(--color-bg-elevated);
    border-color: var(--color-border-hover);
    color: var(--color-text-primary);
  }
</style>
