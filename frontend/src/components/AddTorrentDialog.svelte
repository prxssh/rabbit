<script lang="ts">
    import { SelectDownloadDirectory, GetDefaultConfig, AddMagnetTorrent } from '../../wailsjs/go/ui/Client.js'
    import type { torrent } from '../../wailsjs/go/models'
    import Modal from './ui/Modal.svelte'
    import Button from './ui/Button.svelte'
    import TorrentConfigDialog from './TorrentConfigDialog.svelte'

    export let show = false
    export let onConfirm: (data: {
        config: torrent.Config
        remember: boolean
        magnetURL?: string
        file?: File
    }) => void
    export let onCancel: () => void
    export let defaultPath = ''

    let internalSelectedFile: File | null = null

    let config: torrent.Config | null = null
    let isSelectingPath = false
    let rememberLocation = false
    let showConfigDialog = false
    let mode: 'file' | 'magnet' = 'file'
    let magnetURL = ''

    // Load default config when dialog opens
    $: if (show && !config) {
        loadDefaultConfig()
    }

    async function loadDefaultConfig() {
        try {
            config = await GetDefaultConfig()
            if (defaultPath && config?.Storage) {
                config.Storage.DownloadDir = defaultPath
            }
        } catch (error) {
            console.error('Failed to load default config:', error)
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

    function handleConfirm() {
        if (mode === 'magnet') {
            if (config && config.Storage?.DownloadDir && magnetURL) {
                onConfirm({ config, remember: rememberLocation, magnetURL })
                resetState()
            }
            return
        }

        if (config && config.Storage?.DownloadDir && internalSelectedFile) {
            onConfirm({ config, remember: rememberLocation, file: internalSelectedFile })
            resetState()
        }
    }

    function handleCancel() {
        resetState()
        onCancel()
    }

    function resetState() {
        config = null
        rememberLocation = false
        internalSelectedFile = null
        magnetURL = ''
    }

    function handleFileSelect(e: Event) {
        const target = e.target as HTMLInputElement
        const files = target.files
        if (files && files.length > 0) {
            internalSelectedFile = files[0]
        }
    }

    $: isConfirmDisabled =
        !config?.Storage?.DownloadDir ||
        (mode === 'file' && !internalSelectedFile) ||
        (mode === 'magnet' && !magnetURL)
</script>

<Modal {show} title="Add Torrent" onClose={handleCancel} maxWidth="500px">
    <div class="content">
        <div class="mode-toggle">
            <button
                class="mode-btn"
                class:active={mode === 'file'}
                on:click={() => (mode = 'file')}
            >
                Torrent File
            </button>
            <button
                class="mode-btn"
                class:active={mode === 'magnet'}
                on:click={() => (mode = 'magnet')}
            >
                Magnet Link
            </button>
        </div>

        {#if mode === 'file'}
            <div class="field">
                <label for="torrent-file">Torrent File</label>
                <div class="file-input-group">
                    <div class="file-name-display">
                        {internalSelectedFile?.name || 'No file selected'}
                    </div>
                    <input
                        id="torrent-file"
                        type="file"
                        accept=".torrent"
                        on:change={handleFileSelect}
                        class="hidden-file-input"
                    />
                    <Button variant="secondary" on:click={() => document.getElementById('torrent-file')?.click()}>
                        Browse
                    </Button>
                </div>
            </div>
        {:else}
            <div class="field">
                <label for="magnet-link">Magnet Link</label>
                <textarea
                    id="magnet-link"
                    bind:value={magnetURL}
                    placeholder="magnet:?xt=urn:btih:..."
                    rows="3"
                    class="magnet-input"
                />
                <span class="hint">Paste a magnet link to add the torrent</span>
            </div>
        {/if}

        <div class="field">
            <label for="download-location">Download Location</label>
            <div class="path-selector">
                <input
                    id="download-location"
                    type="text"
                    readonly
                    value={config?.Storage?.DownloadDir || 'Click browse to select...'}
                    class="path-input"
                />
                <Button variant="secondary" disabled={isSelectingPath} on:click={selectDirectory}>
                    {isSelectingPath ? 'Selecting...' : 'Browse'}
                </Button>
            </div>
            <label class="checkbox-label">
                <input type="checkbox" bind:checked={rememberLocation} />
                <span>Remember this location</span>
            </label>
        </div>

        <div class="field">
            <Button variant="secondary" on:click={handleConfigure} style="width: 100%;">
                Advanced Configuration...
            </Button>
        </div>
    </div>

    <svelte:fragment slot="footer">
        <Button variant="ghost" on:click={handleCancel}>Cancel</Button>
        <Button variant="primary" disabled={isConfirmDisabled} on:click={handleConfirm}>
            Add Torrent
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

    .mode-toggle {
        display: flex;
        gap: var(--spacing-1);
        padding: var(--spacing-1);
        background: var(--color-bg-secondary);
        border-radius: var(--radius-base);
    }

    .mode-btn {
        flex: 1;
        padding: var(--spacing-2) var(--spacing-3);
        background: transparent;
        border: none;
        color: var(--color-text-secondary);
        font-size: var(--font-size-sm);
        font-weight: var(--font-weight-medium);
        cursor: pointer;
        border-radius: var(--radius-sm);
        transition: all var(--transition-base);
    }

    .mode-btn:hover {
        background: var(--color-bg-hover);
        color: var(--color-text-primary);
    }

    .mode-btn.active {
        background: var(--color-bg-tertiary);
        color: var(--color-text-primary);
        border: 1px solid var(--color-border-tertiary);
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

    .hint {
        font-size: var(--font-size-xs);
        color: var(--color-text-tertiary);
        font-style: italic;
    }

    .file-input-group {
        display: flex;
        gap: var(--spacing-2);
        align-items: center;
    }

    .hidden-file-input {
        display: none;
    }

    .file-name-display {
        flex: 1;
        padding: var(--spacing-3);
        background: var(--color-bg-secondary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-sm);
        color: var(--color-text-muted);
        font-size: var(--font-size-sm);
        font-family: var(--font-family-base); /* Changed to base font for consistency */
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .magnet-input {
        padding: var(--spacing-3);
        background: var(--color-bg-secondary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-sm);
        color: var(--color-text-primary);
        font-size: var(--font-size-sm);
        font-family: var(--font-family-mono);
        resize: vertical;
        line-height: 1.5;
    }

    .magnet-input:focus {
        outline: none;
        border-color: var(--color-accent);
    }

    .magnet-input::placeholder {
        color: var(--color-text-disabled);
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

    .checkbox-label {
        display: flex;
        align-items: center;
        gap: var(--spacing-2);
        margin-top: var(--spacing-1);
        cursor: pointer;
        font-size: var(--font-size-sm);
    }

    .checkbox-label input[type='checkbox'] {
        cursor: pointer;
        width: 16px;
        height: 16px;
    }

    .checkbox-label span {
        color: var(--color-text-secondary);
    }
</style>
