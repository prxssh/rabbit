<script lang="ts">
    import {
        SelectDownloadDirectory,
        GetTorrentConfig,
        UpdateTorrentConfig,
    } from '../../wailsjs/go/torrent/Client.js'
    import type { torrent } from '../../wailsjs/go/models'
    import Modal from './ui/Modal.svelte'
    import Button from './ui/Button.svelte'
    import TorrentConfigDialog from './TorrentConfigDialog.svelte'

    export let show = false
    export let torrentName: string = ''
    export let infoHash: string = ''
    export let onConfirm: () => void
    export let onCancel: () => void

    let config: torrent.Config | null = null
    let isSelectingPath = false
    let showConfigDialog = false
    let isLoading = false

    // Load torrent config when dialog opens
    $: if (show && infoHash) {
        loadTorrentConfig()
    }

    async function loadTorrentConfig() {
        try {
            isLoading = true
            config = await GetTorrentConfig(infoHash)
        } catch (error) {
            console.error('Failed to load torrent config:', error)
        } finally {
            isLoading = false
        }
    }

    async function selectDirectory() {
        try {
            isSelectingPath = true
            const path = await SelectDownloadDirectory()
            if (path && config?.Storage) {
                config.Storage.DownloadDir = path
            }
        } catch (error) {
            console.error('Failed to select directory:', error)
        } finally {
            isSelectingPath = false
        }
    }

    function handleConfigure() {
        showConfigDialog = true
    }

    function handleConfigConfirm(updatedConfig: torrent.Config) {
        config = updatedConfig
        showConfigDialog = false
    }

    function handleConfigCancel() {
        showConfigDialog = false
    }

    async function handleConfirm() {
        if (config && infoHash) {
            try {
                await UpdateTorrentConfig(infoHash, config)
                config = null
                onConfirm()
            } catch (error) {
                console.error('Failed to update torrent config:', error)
            }
        }
    }

    function handleCancel() {
        config = null
        onCancel()
    }
</script>

<Modal {show} title="Edit Torrent Settings" onClose={handleCancel} maxWidth="500px">
    <div class="content">
        <div class="field">
            <label for="torrent-name">Torrent Name</label>
            <div id="torrent-name" class="torrent-name">{torrentName}</div>
        </div>

        {#if isLoading}
            <div class="loading">Loading configuration...</div>
        {:else if config}
            <div class="field">
                <label for="download-location">Download Location</label>
                <div class="path-selector">
                    <input
                        id="download-location"
                        type="text"
                        readonly
                        value={config?.Storage?.DownloadDir || 'Not set'}
                        class="path-input"
                    />
                    <Button
                        variant="secondary"
                        disabled={isSelectingPath}
                        on:click={selectDirectory}
                    >
                        {isSelectingPath ? 'Selecting...' : 'Browse'}
                    </Button>
                </div>
                <div class="note">Note: Changing download location requires torrent restart</div>
            </div>

            <div class="field">
                <Button variant="secondary" on:click={handleConfigure} style="width: 100%;">
                    Advanced Configuration...
                </Button>
            </div>
        {/if}
    </div>

    <svelte:fragment slot="footer">
        <Button variant="ghost" on:click={handleCancel}>Cancel</Button>
        <Button variant="primary" disabled={!config || isLoading} on:click={handleConfirm}>
            Save Changes
        </Button>
    </svelte:fragment>
</Modal>

<TorrentConfigDialog
    show={showConfigDialog}
    onConfirm={handleConfigConfirm}
    onCancel={handleConfigCancel}
/>

<style>
    .content {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-4);
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

    .torrent-name {
        padding: var(--spacing-3);
        background: var(--color-bg-secondary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-sm);
        color: var(--color-text-primary);
        font-size: var(--font-size-sm);
        font-family: var(--font-family-mono);
        word-break: break-all;
    }

    .path-selector {
        display: flex;
        gap: var(--spacing-2);
    }

    .path-input {
        flex: 1;
        padding: var(--spacing-3);
        background: var(--color-bg-secondary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-sm);
        color: var(--color-text-muted);
        font-size: var(--font-size-sm);
        font-family: var(--font-family-base);
    }

    .note {
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
        font-style: italic;
    }

    .loading {
        padding: var(--spacing-4);
        text-align: center;
        color: var(--color-text-muted);
    }
</style>
