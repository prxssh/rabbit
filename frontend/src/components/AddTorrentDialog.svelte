<script lang="ts">
  import {SelectDownloadDirectory, GetDefaultConfig} from '../../wailsjs/go/torrent/Client.js'
  import type {torrent} from '../../wailsjs/go/models'
  import Modal from './ui/Modal.svelte'
  import Button from './ui/Button.svelte'
  import TorrentConfigDialog from './TorrentConfigDialog.svelte'

  export let show = false
  export let selectedFile: File | null = null
  export let onConfirm: (config: torrent.Config, remember: boolean) => void
  export let onCancel: () => void
  export let defaultPath = ''

  let config: torrent.Config | null = null
  let isSelectingPath = false
  let rememberLocation = false
  let showConfigDialog = false

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
    if (config && config.Storage?.DownloadDir) {
      onConfirm(config, rememberLocation)
      config = null
      rememberLocation = false
    }
  }

  function handleCancel() {
    config = null
    rememberLocation = false
    onCancel()
  }
</script>

<Modal {show} title="Add Torrent" onClose={handleCancel} maxWidth="500px">
  <div class="content">
    <div class="field">
      <label>Torrent File</label>
      <div class="file-name">{selectedFile?.name || 'No file selected'}</div>
    </div>

    <div class="field">
      <label>Download Location</label>
      <div class="path-selector">
        <input
          type="text"
          readonly
          value={config?.Storage?.DownloadDir || 'Click browse to select...'}
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
    <Button variant="primary" disabled={!config?.Storage?.DownloadDir} on:click={handleConfirm}>
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

  .file-name {
    padding: var(--spacing-3);
    background: var(--color-bg-secondary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-sm);
    color: var(--color-text-primary);
    font-size: var(--font-size-sm);
    font-family: var(--font-family-mono);
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

  .checkbox-label input[type="checkbox"] {
    cursor: pointer;
    width: 16px;
    height: 16px;
  }

  .checkbox-label span {
    color: var(--color-text-secondary);
  }
</style>
