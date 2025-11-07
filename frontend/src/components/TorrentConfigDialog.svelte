<script lang="ts">
    import { GetDefaultConfig } from '../../wailsjs/go/ui/Client.js'
    import type { torrent, scheduler, storage, peer, tracker } from '../../wailsjs/go/models'
    import Modal from './ui/Modal.svelte'
    import Button from './ui/Button.svelte'
    import { onMount } from 'svelte'

    export let show = false
    export let onConfirm: (config: torrent.Config) => void
    export let onCancel: () => void

    let config: torrent.Config | null = null
    let activeTab: 'storage' | 'scheduler' | 'peer' | 'tracker' = 'storage'

    // Download strategy options
    const downloadStrategyOptions = [
        { value: 0, label: 'Random' },
        { value: 1, label: 'Rarest First' },
        { value: 2, label: 'Sequential' },
    ]

    onMount(async () => {
        await loadDefaultConfig()
    })

    async function loadDefaultConfig() {
        try {
            config = await GetDefaultConfig()
        } catch (error) {
            console.error('Failed to load default config:', error)
        }
    }

    function handleConfirm() {
        if (config) {
            onConfirm(config)
        }
    }

    function handleCancel() {
        onCancel()
    }

    $: if (show && !config) {
        loadDefaultConfig()
    }
</script>

<Modal {show} title="Torrent Configuration" onClose={handleCancel} maxWidth="700px">
    {#if config}
        <div class="content">
            <div class="tabs">
                <button
                    class="tab"
                    class:active={activeTab === 'storage'}
                    on:click={() => (activeTab = 'storage')}
                >
                    Storage
                </button>
                <button
                    class="tab"
                    class:active={activeTab === 'scheduler'}
                    on:click={() => (activeTab = 'scheduler')}
                >
                    Scheduler
                </button>
                <button
                    class="tab"
                    class:active={activeTab === 'peer'}
                    on:click={() => (activeTab = 'peer')}
                >
                    Peer
                </button>
                <button
                    class="tab"
                    class:active={activeTab === 'tracker'}
                    on:click={() => (activeTab = 'tracker')}
                >
                    Tracker
                </button>
            </div>

            <div class="tab-content">
                {#if activeTab === 'storage' && config.Storage}
                    <div class="section">
                        <h3>Storage Settings</h3>

                        <div class="field">
                            <label for="downloadDir">Download Directory</label>
                            <input
                                id="downloadDir"
                                type="text"
                                bind:value={config.Storage.DownloadDir}
                                placeholder="/path/to/downloads"
                            />
                            <span class="hint">Where downloaded files will be saved</span>
                        </div>

                        <div class="field">
                            <label for="pieceQueueSize">Piece Queue Size</label>
                            <input
                                id="pieceQueueSize"
                                type="number"
                                bind:value={config.Storage.PieceQueueSize}
                                min="10"
                                max="1000"
                            />
                            <span class="hint">Buffer size for piece processing (default: 200)</span
                            >
                        </div>

                        <div class="field">
                            <label for="diskQueueSize">Disk Queue Size</label>
                            <input
                                id="diskQueueSize"
                                type="number"
                                bind:value={config.Storage.DiskQueueSize}
                                min="10"
                                max="500"
                            />
                            <span class="hint">Buffer size for disk writes (default: 100)</span>
                        </div>
                    </div>
                {/if}

                {#if activeTab === 'scheduler' && config.Scheduler}
                    <div class="section">
                        <h3>Scheduler Settings</h3>

                        <div class="field">
                            <label for="downloadStrategy">Download Strategy</label>
                            <select
                                id="downloadStrategy"
                                bind:value={config.Scheduler.DownloadStrategy}
                            >
                                {#each downloadStrategyOptions as option}
                                    <option value={option.value}>{option.label}</option>
                                {/each}
                            </select>
                            <span class="hint">How pieces are selected for download</span>
                        </div>

                        <div class="field">
                            <label for="maxInflight">Max Inflight Requests Per Peer</label>
                            <input
                                id="maxInflight"
                                type="number"
                                bind:value={config.Scheduler.MaxInflightRequestsPerPeer}
                                min="1"
                                max="100"
                            />
                            <span class="hint"
                                >Maximum concurrent requests per peer (default: 32)</span
                            >
                        </div>

                        <div class="field">
                            <label for="minInflight">Min Inflight Requests Per Peer</label>
                            <input
                                id="minInflight"
                                type="number"
                                bind:value={config.Scheduler.MinInflightRequestsPerPeer}
                                min="1"
                                max="50"
                            />
                            <span class="hint"
                                >Minimum requests to keep pipeline full (default: 4)</span
                            >
                        </div>

                        <div class="field">
                            <label for="endgameThreshold">Endgame Threshold</label>
                            <input
                                id="endgameThreshold"
                                type="number"
                                bind:value={config.Scheduler.EndgameThreshold}
                                min="1"
                                max="100"
                            />
                            <span class="hint"
                                >Remaining blocks to trigger endgame mode (default: 30)</span
                            >
                        </div>

                        <div class="field">
                            <label for="maxRequestBacklog">Max Request Backlog</label>
                            <input
                                id="maxRequestBacklog"
                                type="number"
                                bind:value={config.Scheduler.MaxRequestBacklog}
                                min="10"
                                max="500"
                            />
                            <span class="hint">Work queue size per peer (default: 100)</span>
                        </div>
                    </div>
                {/if}

                {#if activeTab === 'peer' && config.Peer}
                    <div class="section">
                        <h3>Peer Settings</h3>

                        <div class="field">
                            <label for="maxPeers">Max Peers</label>
                            <input
                                id="maxPeers"
                                type="number"
                                bind:value={config.Peer.MaxPeers}
                                min="1"
                                max="200"
                            />
                            <span class="hint"
                                >Maximum concurrent peer connections (default: 50)</span
                            >
                        </div>

                        <div class="field">
                            <label for="uploadSlots">Upload Slots</label>
                            <input
                                id="uploadSlots"
                                type="number"
                                bind:value={config.Peer.UploadSlots}
                                min="1"
                                max="20"
                            />
                            <span class="hint"
                                >Number of peers to upload to simultaneously (default: 4)</span
                            >
                        </div>

                        <div class="field">
                            <label for="peerOutboxBacklog">Peer Outbox Backlog</label>
                            <input
                                id="peerOutboxBacklog"
                                type="number"
                                bind:value={config.Peer.PeerOutboxBacklog}
                                min="10"
                                max="200"
                            />
                            <span class="hint">Message buffer size per peer (default: 50)</span>
                        </div>
                    </div>
                {/if}

                {#if activeTab === 'tracker' && config.Tracker}
                    <div class="section">
                        <h3>Tracker Settings</h3>

                        <div class="field">
                            <label for="port">Port</label>
                            <input
                                id="port"
                                type="number"
                                bind:value={config.Tracker.Port}
                                min="1024"
                                max="65535"
                            />
                            <span class="hint"
                                >Port for incoming peer connections (default: 6969)</span
                            >
                        </div>

                        <div class="field">
                            <label for="numWant">Num Want</label>
                            <input
                                id="numWant"
                                type="number"
                                bind:value={config.Tracker.NumWant}
                                min="10"
                                max="200"
                            />
                            <span class="hint"
                                >Number of peers to request from tracker (default: 50)</span
                            >
                        </div>
                    </div>
                {/if}
            </div>
        </div>
    {:else}
        <div class="loading">Loading configuration...</div>
    {/if}

    <svelte:fragment slot="footer">
        <Button variant="ghost" on:click={handleCancel}>Cancel</Button>
        <Button variant="primary" on:click={handleConfirm} disabled={!config}>OK</Button>
    </svelte:fragment>
</Modal>

<style>
    .content {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-4);
        min-height: 400px;
    }

    .tabs {
        display: flex;
        gap: var(--spacing-1);
        border-bottom: 1px solid var(--color-border-primary);
    }

    .tab {
        padding: var(--spacing-3) var(--spacing-4);
        background: transparent;
        border: none;
        border-bottom: 2px solid transparent;
        color: var(--color-text-secondary);
        font-size: var(--font-size-sm);
        font-weight: var(--font-weight-medium);
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .tab:hover {
        color: var(--color-text-primary);
        background: var(--color-bg-hover);
    }

    .tab.active {
        color: var(--color-accent);
        border-bottom-color: var(--color-accent);
    }

    .tab-content {
        flex: 1;
        overflow-y: auto;
        padding: var(--spacing-4) 0;
    }

    .section {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-4);
    }

    .section h3 {
        font-size: var(--font-size-base);
        font-weight: var(--font-weight-semibold);
        color: var(--color-text-primary);
        margin: 0 0 var(--spacing-2) 0;
    }

    .field {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-2);
    }

    .field label {
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
        font-weight: var(--font-weight-medium);
    }

    .field input,
    .field select {
        padding: var(--spacing-3);
        background: var(--color-bg-secondary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-sm);
        color: var(--color-text-primary);
        font-size: var(--font-size-sm);
        font-family: var(--font-family-base);
    }

    .field input:focus,
    .field select:focus {
        outline: none;
        border-color: var(--color-accent);
    }

    .field .hint {
        font-size: var(--font-size-xs);
        color: var(--color-text-tertiary);
        font-style: italic;
    }

    .loading {
        display: flex;
        align-items: center;
        justify-content: center;
        min-height: 400px;
        color: var(--color-text-secondary);
    }
</style>
