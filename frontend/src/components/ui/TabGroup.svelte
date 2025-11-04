<script lang="ts">
    export let tabs: Array<{ id: string; label: string; count?: number }>
    export let activeTab: string
    export let onTabChange: (tabId: string) => void
</script>

<div class="tabs">
    {#each tabs as tab}
        <button
            class="tab"
            class:active={activeTab === tab.id}
            on:click={() => onTabChange(tab.id)}
        >
            {tab.label}
            {#if tab.count !== undefined && tab.count > 0}
                ({tab.count})
            {/if}
        </button>
    {/each}
</div>

<style>
    .tabs {
        display: flex;
        gap: var(--spacing-2);
        padding: var(--spacing-4) var(--spacing-6) 0;
        background-color: var(--color-bg-secondary);
        border-bottom: 1px solid var(--color-border-primary);
    }

    .tab {
        padding: var(--spacing-3) var(--spacing-5);
        background: none;
        border: none;
        border-bottom: 2px solid transparent;
        color: var(--color-text-disabled);
        font-size: var(--font-size-base);
        font-weight: var(--font-weight-medium);
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .tab:hover {
        color: var(--color-text-tertiary);
        background-color: var(--color-bg-hover);
    }

    .tab.active {
        color: var(--color-text-primary);
        border-bottom-color: var(--color-accent-primary);
    }
</style>
