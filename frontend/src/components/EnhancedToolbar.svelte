<script lang="ts">
    export let onAddTorrent: () => void
    export let onPauseTorrent: () => void
    export let onResumeTorrent: () => void
    export let onRemoveTorrent: () => void
    export let onOpenSearch: () => void
    export let onOpenSettings: () => void
    export let selectedCount: number = 0
    export let downloadSpeed: string = '0 KB/s'
    export let uploadSpeed: string = '0 KB/s'

    $: hasSelection = selectedCount > 0
</script>

<div class="toolbar">
    <div class="toolbar-section">
        <button
            class="toolbar-btn primary"
            on:click={onAddTorrent}
            title="Add Torrent (Ctrl+O)"
        >
            <span class="btn-icon">+</span>
            <span class="btn-label">add</span>
        </button>

        <div class="toolbar-divider"></div>

        <button
            class="toolbar-btn"
            disabled={!hasSelection}
            on:click={onPauseTorrent}
            title="Pause (Space)"
        >
            <span class="btn-icon">‖</span>
        </button>

        <button
            class="toolbar-btn"
            disabled={!hasSelection}
            on:click={onResumeTorrent}
            title="Resume (Space)"
        >
            <span class="btn-icon">▶</span>
        </button>

        <button
            class="toolbar-btn danger"
            disabled={!hasSelection}
            on:click={onRemoveTorrent}
            title="Remove (Delete)"
        >
            <span class="btn-icon">×</span>
        </button>

        <div class="toolbar-divider"></div>

        <button class="toolbar-btn" on:click={onOpenSearch} title="Search Torrents (Ctrl+F)">
            <span class="btn-icon">⌕</span>
            <span class="btn-label">search</span>
        </button>
    </div>

    <div class="toolbar-section">
        <div class="speed-stats">
            <span class="speed-item download">
                <span class="speed-icon">↓</span>
                {downloadSpeed}
            </span>
            <span class="speed-item upload">
                <span class="speed-icon">↑</span>
                {uploadSpeed}
            </span>
        </div>

        <div class="toolbar-divider"></div>

        <button class="toolbar-btn" on:click={onOpenSettings} title="Settings (Ctrl+,)">
            <span class="btn-icon">⚙</span>
        </button>
    </div>
</div>

<style>
    .toolbar {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: var(--spacing-2) var(--spacing-4);
        background-color: var(--color-bg-secondary);
        border-bottom: 1px solid var(--color-border-primary);
        gap: var(--spacing-4);
    }

    .toolbar-section {
        display: flex;
        align-items: center;
        gap: var(--spacing-2);
    }

    .toolbar-btn {
        display: flex;
        align-items: center;
        gap: var(--spacing-2);
        padding: var(--spacing-2) var(--spacing-3);
        background: none;
        border: 1px solid var(--color-border-tertiary);
        color: var(--color-text-secondary);
        font-family: var(--font-family-mono);
        font-size: var(--font-size-sm);
        cursor: pointer;
        border-radius: var(--radius-sm);
        transition: all var(--transition-fast);
        height: 32px;
    }

    .toolbar-btn:hover:not(:disabled) {
        background-color: var(--color-bg-hover);
        border-color: var(--color-border-active);
        color: var(--color-text-primary);
    }

    .toolbar-btn:disabled {
        opacity: 0.3;
        cursor: not-allowed;
    }

    .toolbar-btn.primary {
        background-color: var(--color-bg-tertiary);
        border-color: var(--color-border-active);
    }

    .toolbar-btn.primary:hover:not(:disabled) {
        background-color: var(--color-bg-elevated);
    }

    .toolbar-btn.danger:hover:not(:disabled) {
        background-color: var(--color-error-bg);
        border-color: var(--color-error-border);
        color: var(--color-error);
    }

    .btn-icon {
        font-size: var(--font-size-lg);
        line-height: 1;
    }

    .btn-label {
        font-size: var(--font-size-sm);
    }

    .toolbar-divider {
        width: 1px;
        height: 24px;
        background-color: var(--color-border-tertiary);
        margin: 0 var(--spacing-1);
    }

    .speed-stats {
        display: flex;
        gap: var(--spacing-4);
        padding: 0 var(--spacing-2);
    }

    .speed-item {
        display: flex;
        align-items: center;
        gap: var(--spacing-1);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
        font-family: var(--font-family-mono);
    }

    .speed-icon {
        font-size: var(--font-size-base);
    }
</style>
