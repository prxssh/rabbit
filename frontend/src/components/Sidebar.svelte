<script lang="ts">
    export let selectedFilter: string = 'all'
    export let counts: {
        all: number
        downloading: number
        seeding: number
        completed: number
        paused: number
        error: number
    } = {
        all: 0,
        downloading: 0,
        seeding: 0,
        completed: 0,
        paused: 0,
        error: 0,
    }
    export let onFilterChange: (filter: string) => void

    interface FilterItem {
        id: string
        label: string
        icon: string
        count: number
    }

    $: filters = [
        { id: 'all', label: 'all', icon: '■', count: counts.all },
        { id: 'downloading', label: 'downloading', icon: '↓', count: counts.downloading },
        { id: 'seeding', label: 'seeding', icon: '↑', count: counts.seeding },
        { id: 'completed', label: 'completed', icon: '✓', count: counts.completed },
        { id: 'paused', label: 'paused', icon: '‖', count: counts.paused },
        { id: 'error', label: 'error', icon: '!', count: counts.error },
    ] as FilterItem[]

    function selectFilter(id: string) {
        onFilterChange(id)
    }
</script>

<aside class="sidebar">
    <div class="sidebar-header">
        <span class="sidebar-title">filters</span>
    </div>

    <nav class="filter-list">
        {#each filters as filter}
            <button
                class="filter-item"
                class:active={selectedFilter === filter.id}
                on:click={() => selectFilter(filter.id)}
            >
                <span class="filter-icon">{filter.icon}</span>
                <span class="filter-label">{filter.label}</span>
                <span class="filter-count">{filter.count}</span>
            </button>
        {/each}
    </nav>
</aside>

<style>
    .sidebar {
        width: 180px;
        background-color: var(--color-bg-primary);
        border-right: 1px solid var(--color-border-primary);
        display: flex;
        flex-direction: column;
        overflow-y: auto;
    }

    .sidebar-header {
        padding: var(--spacing-4) var(--spacing-5);
        border-bottom: 1px solid var(--color-border-primary);
    }

    .sidebar-title {
        font-size: var(--font-size-sm);
        color: var(--color-text-muted);
        text-transform: uppercase;
        letter-spacing: var(--letter-spacing-wider);
    }

    .filter-list {
        display: flex;
        flex-direction: column;
        padding: var(--spacing-2);
    }

    .filter-item {
        display: flex;
        align-items: center;
        gap: var(--spacing-3);
        padding: var(--spacing-2) var(--spacing-3);
        background: none;
        border: none;
        color: var(--color-text-secondary);
        font-family: var(--font-family-mono);
        font-size: var(--font-size-base);
        cursor: pointer;
        border-radius: var(--radius-sm);
        transition: all var(--transition-fast);
        text-align: left;
    }

    .filter-item:hover {
        background-color: var(--color-bg-hover);
        color: var(--color-text-primary);
    }

    .filter-item.active {
        background-color: var(--color-bg-tertiary);
        color: var(--color-text-primary);
        border: 1px solid var(--color-border-tertiary);
    }

    .filter-icon {
        width: 16px;
        text-align: center;
        font-size: var(--font-size-md);
    }

    .filter-label {
        flex: 1;
    }

    .filter-count {
        font-size: var(--font-size-sm);
        color: var(--color-text-muted);
        font-weight: var(--font-weight-medium);
    }

    .filter-item.active .filter-count {
        color: var(--color-text-secondary);
    }
</style>
