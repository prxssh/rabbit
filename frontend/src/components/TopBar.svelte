<script lang="ts">
    export let torrentCount: number = 0
    export let downloadSpeed: string = '0 KB/s'
    export let uploadSpeed: string = '0 KB/s'
    export let onAddTorrent: () => void
</script>

<div class="topbar">
    <div class="logo">
        <span class="logo-text">rabbit</span>
    </div>
    <div class="topbar-actions">
        <div class="stats">
            <span class="stat-item">↓ {downloadSpeed}</span>
            <span class="stat-item">↑ {uploadSpeed}</span>
            <span class="stat-item">{torrentCount} torrents</span>
        </div>
        <button class="icon-button add-button" on:click={onAddTorrent} title="Add Torrent">
            +
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

    .icon-button {
        background: none;
        border: none;
        color: var(--color-text-secondary);
        font-size: 20px;
        cursor: pointer;
        padding: var(--spacing-2);
        border-radius: var(--radius-base);
        transition: all var(--transition-base);
        display: flex;
        align-items: center;
        justify-content: center;
        width: 36px;
        height: 36px;
    }

    .icon-button:hover {
        background-color: var(--color-bg-hover);
        color: var(--color-text-primary);
    }

    .icon-button.add-button {
        font-size: 24px;
        font-weight: 300;
    }
</style>
