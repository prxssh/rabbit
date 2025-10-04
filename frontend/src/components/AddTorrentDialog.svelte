<script lang="ts">
  import {SelectDownloadDirectory} from '../../wailsjs/go/torrent/Client.js'

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

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      handleCancel()
    }
  }
</script>

{#if show}
  <div class="modal-backdrop" on:click={handleBackdropClick}>
    <div class="modal">
      <div class="modal-header">
        <h2>Add Torrent</h2>
        <button class="close-btn" on:click={handleCancel}>×</button>
      </div>

      <div class="modal-body">
        <div class="field">
          <label>Torrent File:</label>
          <div class="file-name">{selectedFile?.name || 'No file selected'}</div>
        </div>

        <div class="field">
          <label>Download Location:</label>
          <div class="path-selector">
            <input
              type="text"
              readonly
              value={downloadPath || 'Click browse to select...'}
              class="path-input"
            />
            <button
              class="browse-btn"
              on:click={selectDirectory}
              disabled={isSelectingPath}
            >
              {isSelectingPath ? 'Selecting...' : 'Browse'}
            </button>
          </div>
          <label class="checkbox-label">
            <input type="checkbox" bind:checked={rememberLocation} />
            <span>Remember this location</span>
          </label>
        </div>
      </div>

      <div class="modal-footer">
        <button class="btn btn-secondary" on:click={handleCancel}>Cancel</button>
        <button
          class="btn btn-primary"
          on:click={handleConfirm}
          disabled={!downloadPath}
        >
          Add Torrent
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal {
    background-color: var(--color-bg-primary);
    border-radius: 8px;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.3);
    width: 90%;
    max-width: 500px;
    max-height: 90vh;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--spacing-4);
    border-bottom: 1px solid var(--color-border);
  }

  .modal-header h2 {
    margin: 0;
    font-size: var(--font-size-xl);
    color: var(--color-text-primary);
  }

  .close-btn {
    background: none;
    border: none;
    color: var(--color-text-tertiary);
    font-size: 28px;
    cursor: pointer;
    padding: 0;
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition: all 0.2s;
  }

  .close-btn:hover {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .modal-body {
    padding: var(--spacing-6);
    overflow-y: auto;
    flex: 1;
  }

  .field {
    margin-bottom: var(--spacing-4);
  }

  .field label {
    display: block;
    margin-bottom: var(--spacing-2);
    color: var(--color-text-secondary);
    font-size: var(--font-size-sm);
    font-weight: 500;
  }

  .file-name {
    padding: var(--spacing-3);
    background-color: var(--color-bg-secondary);
    border-radius: 4px;
    color: var(--color-text-primary);
    font-family: monospace;
  }

  .path-selector {
    display: flex;
    gap: var(--spacing-2);
  }

  .path-input {
    flex: 1;
    padding: var(--spacing-3);
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-text-primary);
    font-size: var(--font-size-sm);
  }

  .browse-btn {
    padding: var(--spacing-3) var(--spacing-4);
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-text-primary);
    cursor: pointer;
    font-size: var(--font-size-sm);
    transition: all 0.2s;
    white-space: nowrap;
  }

  .browse-btn:hover:not(:disabled) {
    background-color: var(--color-bg-hover);
    border-color: var(--color-primary);
  }

  .browse-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    margin-top: var(--spacing-3);
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

  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-3);
    padding: var(--spacing-4);
    border-top: 1px solid var(--color-border);
  }

  .btn {
    padding: var(--spacing-3) var(--spacing-5);
    border-radius: 4px;
    font-size: var(--font-size-sm);
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    border: none;
  }

  .btn-secondary {
    background-color: var(--color-bg-secondary);
    color: var(--color-text-primary);
  }

  .btn-secondary:hover {
    background-color: var(--color-bg-hover);
  }

  .btn-primary {
    background-color: var(--color-primary);
    color: white;
  }

  .btn-primary:hover:not(:disabled) {
    background-color: var(--color-primary-hover);
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
