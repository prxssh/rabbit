<script lang="ts">
    import Modal from './ui/Modal.svelte'
    import Button from './ui/Button.svelte'

    export let show: boolean = false
    export let onClose: () => void
    export let onSelectTorrent: (magnetLink: string) => void

    let searchQuery: string = ''
    let searchResults: Array<{
        name: string
        size: string
        seeds: number
        peers: number
        magnet: string
        source: string
    }> = []
    let isSearching: boolean = false
    let selectedIndex: number = -1

    // Torrent search providers (these would need backend implementation)
    let searchProvider: string = 'piratebay'
    let providers = ['piratebay', '1337x', 'rarbg', 'nyaa']

    async function handleSearch() {
        if (!searchQuery.trim()) return

        isSearching = true
        searchResults = []

        // TODO: Implement actual search via backend
        // This is a placeholder for the UI structure
        setTimeout(() => {
            // Mock results for UI demonstration
            searchResults = [
                {
                    name: `${searchQuery} - Sample Result 1`,
                    size: '1.5 GB',
                    seeds: 123,
                    peers: 45,
                    magnet: 'magnet:?xt=urn:btih:...',
                    source: searchProvider,
                },
                {
                    name: `${searchQuery} - Sample Result 2`,
                    size: '750 MB',
                    seeds: 89,
                    peers: 23,
                    magnet: 'magnet:?xt=urn:btih:...',
                    source: searchProvider,
                },
            ]
            isSearching = false
        }, 500)
    }

    function handleKeydown(event: KeyboardEvent) {
        if (event.key === 'Enter') {
            handleSearch()
        } else if (event.key === 'Escape') {
            onClose()
        } else if (event.key === 'ArrowDown') {
            event.preventDefault()
            selectedIndex = Math.min(selectedIndex + 1, searchResults.length - 1)
        } else if (event.key === 'ArrowUp') {
            event.preventDefault()
            selectedIndex = Math.max(selectedIndex - 1, -1)
        } else if (event.key === 'Enter' && selectedIndex >= 0) {
            selectResult(searchResults[selectedIndex])
        }
    }

    function selectResult(result: (typeof searchResults)[0]) {
        onSelectTorrent(result.magnet)
        onClose()
    }

    function handleModalClose() {
        searchQuery = ''
        searchResults = []
        selectedIndex = -1
        onClose()
    }

    $: if (show) {
        selectedIndex = -1
    }
</script>

<Modal {show} onClose={handleModalClose} title="search torrents" width="800px">
    <div class="search-panel">
        <div class="search-header">
            <div class="search-input-group">
                <input
                    type="text"
                    class="search-input"
                    placeholder="search for torrents..."
                    bind:value={searchQuery}
                    on:keydown={handleKeydown}
                    autofocus
                />
                <button class="search-btn" on:click={handleSearch} disabled={isSearching}>
                    {isSearching ? '...' : '⌕'}
                </button>
            </div>

            <div class="provider-select">
                <label for="provider">provider:</label>
                <select id="provider" bind:value={searchProvider}>
                    {#each providers as provider}
                        <option value={provider}>{provider}</option>
                    {/each}
                </select>
            </div>
        </div>

        <div class="search-results">
            {#if isSearching}
                <div class="search-status">searching...</div>
            {:else if searchResults.length === 0 && searchQuery}
                <div class="search-status">no results found</div>
            {:else if searchResults.length === 0}
                <div class="search-status">enter a search query to begin</div>
            {:else}
                <div class="results-list">
                    <div class="results-header">
                        <span class="col-name">name</span>
                        <span class="col-size">size</span>
                        <span class="col-seeds">seeds</span>
                        <span class="col-peers">peers</span>
                        <span class="col-actions"></span>
                    </div>

                    {#each searchResults as result, index}
                        <div
                            class="result-item"
                            class:selected={index === selectedIndex}
                            on:click={() => selectResult(result)}
                        >
                            <span class="col-name" title={result.name}>
                                {result.name}
                            </span>
                            <span class="col-size">{result.size}</span>
                            <span class="col-seeds">{result.seeds}</span>
                            <span class="col-peers">{result.peers}</span>
                            <button
                                class="download-btn"
                                on:click|stopPropagation={() => selectResult(result)}
                                title="Add torrent"
                            >
                                +
                            </button>
                        </div>
                    {/each}
                </div>
            {/if}
        </div>

        <div class="search-footer">
            <span class="search-note">
                note: torrent search requires backend integration with search providers
            </span>
        </div>
    </div>
</Modal>

<style>
    .search-panel {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-4);
        min-height: 400px;
    }

    .search-header {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-3);
    }

    .search-input-group {
        display: flex;
        gap: var(--spacing-2);
    }

    .search-input {
        flex: 1;
        padding: var(--spacing-3) var(--spacing-4);
        background-color: var(--color-bg-primary);
        border: 1px solid var(--color-border-tertiary);
        border-radius: var(--radius-base);
        color: var(--color-text-primary);
        font-family: var(--font-family-mono);
        font-size: var(--font-size-base);
        outline: none;
        transition: border-color var(--transition-fast);
    }

    .search-input:focus {
        border-color: var(--color-border-active);
    }

    .search-btn {
        padding: var(--spacing-3) var(--spacing-5);
        background-color: var(--color-bg-tertiary);
        border: 1px solid var(--color-border-tertiary);
        border-radius: var(--radius-base);
        color: var(--color-text-primary);
        font-family: var(--font-family-mono);
        font-size: var(--font-size-lg);
        cursor: pointer;
        transition: all var(--transition-fast);
    }

    .search-btn:hover:not(:disabled) {
        background-color: var(--color-bg-elevated);
        border-color: var(--color-border-active);
    }

    .search-btn:disabled {
        opacity: 0.5;
        cursor: not-allowed;
    }

    .provider-select {
        display: flex;
        align-items: center;
        gap: var(--spacing-3);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
    }

    .provider-select label {
        color: var(--color-text-muted);
    }

    .provider-select select {
        padding: var(--spacing-2) var(--spacing-3);
        background-color: var(--color-bg-primary);
        border: 1px solid var(--color-border-tertiary);
        border-radius: var(--radius-sm);
        color: var(--color-text-primary);
        font-family: var(--font-family-mono);
        font-size: var(--font-size-sm);
        cursor: pointer;
    }

    .search-results {
        flex: 1;
        overflow-y: auto;
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-base);
        background-color: var(--color-bg-primary);
    }

    .search-status {
        display: flex;
        align-items: center;
        justify-content: center;
        height: 200px;
        color: var(--color-text-muted);
        font-size: var(--font-size-base);
    }

    .results-list {
        display: flex;
        flex-direction: column;
    }

    .results-header {
        display: grid;
        grid-template-columns: 1fr 100px 80px 80px 40px;
        gap: var(--spacing-3);
        padding: var(--spacing-3) var(--spacing-4);
        background-color: var(--color-bg-secondary);
        border-bottom: 1px solid var(--color-border-primary);
        font-size: var(--font-size-sm);
        color: var(--color-text-muted);
        text-transform: uppercase;
        letter-spacing: var(--letter-spacing-wide);
    }

    .result-item {
        display: grid;
        grid-template-columns: 1fr 100px 80px 80px 40px;
        gap: var(--spacing-3);
        padding: var(--spacing-3) var(--spacing-4);
        border-bottom: 1px solid var(--color-border-primary);
        cursor: pointer;
        transition: background-color var(--transition-fast);
        font-size: var(--font-size-sm);
        align-items: center;
    }

    .result-item:hover {
        background-color: var(--color-bg-hover);
    }

    .result-item.selected {
        background-color: var(--color-bg-tertiary);
    }

    .col-name {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        color: var(--color-text-primary);
    }

    .col-size,
    .col-seeds,
    .col-peers {
        color: var(--color-text-secondary);
        text-align: center;
    }

    .download-btn {
        width: 28px;
        height: 28px;
        background: none;
        border: 1px solid var(--color-border-tertiary);
        border-radius: var(--radius-sm);
        color: var(--color-text-secondary);
        font-size: var(--font-size-lg);
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        transition: all var(--transition-fast);
    }

    .download-btn:hover {
        background-color: var(--color-bg-elevated);
        border-color: var(--color-border-active);
        color: var(--color-text-primary);
    }

    .search-footer {
        padding: var(--spacing-2);
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
        text-align: center;
    }

    .search-note {
        font-style: italic;
    }
</style>
