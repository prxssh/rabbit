<script lang="ts">
  import {SelectDownloadDirectory} from '../../wailsjs/go/torrent/Client.js'
  import Modal from './ui/Modal.svelte'
  import Button from './ui/Button.svelte'

  export let show = false
  export let selectedFile: File | null = null
  export let onConfirm: (downloadPath: string, remember: boolean) => void
  export let onCancel: () => void
  export let defaultPath = ''

  let downloadPath = ''
  let isSelectingPath = false
  let rememberLocation = false

  // Set download path when dialog opens and default exists
  $: if (show && defaultPath && !downloadPath) {
    downloadPath = defaultPath
  }

  async function selectDirectory() {
    try {
      isSelectingPath = true
      const path = await SelectDownloadDirectory()
      if (path) {
        downloadPath = path
      }
    } catch (error) {
      console.error('Failed to select directory:', error)
    } finally {
      isSelectingPath = false
    }
  }

  function handleConfirm() {
    if (downloadPath) {
      onConfirm(downloadPath, rememberLocation)
      downloadPath = ''
      rememberLocation = false
    }
  }

  function handleCancel() {
    downloadPath = ''
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
          value={downloadPath || 'Click browse to select...'}
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
  </div>

  <svelte:fragment slot="footer">
    <Button variant="ghost" on:click={handleCancel}>Cancel</Button>
    <Button variant="primary" disabled={!downloadPath} on:click={handleConfirm}>
      Add Torrent
    </Button>
  </svelte:fragment>
</Modal>

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
