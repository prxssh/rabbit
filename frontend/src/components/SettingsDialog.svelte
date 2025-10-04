<script lang="ts">
  import {SelectDownloadDirectory} from '../../wailsjs/go/torrent/Client.js'

  export let show = false
  export let defaultPath = ''
  export let onSave: (path: string) => void
  export let onClose: () => void

  let newPath = defaultPath
  let isSelectingPath = false

  $: if (show) {
    newPath = defaultPath
  }

  async function selectDirectory() {
    try {
      isSelectingPath = true
      const path = await SelectDownloadDirectory()
      if (path) {
        newPath = path
      }
    } catch (error) {
      console.error('Failed to select directory:', error)
    } finally {
      isSelectingPath = false
    }
  }

  function handleSave() {
    onSave(newPath)
    onClose()
  }

  function handleClear() {
    newPath = ''
    onSave('')
    onClose()
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }
</script>

{#if show}
  <div class="modal-backdrop" on:click={handleBackdropClick}>
    <div class="modal">
      <div class="modal-header">
        <h2>Settings</h2>
        <button class="close-btn" on:click={onClose}>×</button>
      </div>

      <div class="modal-body">
        <div class="setting-section">
          <h3>Default Download Location</h3>
          <p class="description">
            Set a default directory for torrent downloads. When set, torrents will be saved here automatically.
          </p>

          <div class="path-selector">
            <input
              type="text"
              readonly
              value={newPath || 'Not set - will ask each time'}
              class="path-input"
              class:empty={!newPath}
            />
            <button
              class="browse-btn"
              on:click={selectDirectory}
              disabled={isSelectingPath}
            >
              {isSelectingPath ? 'Selecting...' : 'Browse'}
            </button>
          </div>

          {#if defaultPath}
            <button class="clear-btn" on:click={handleClear}>
              Clear Default Location
            </button>
          {/if}
        </div>
      </div>

      <div class="modal-footer">
        <button class="btn btn-secondary" on:click={onClose}>Cancel</button>
        <button class="btn btn-primary" on:click={handleSave}>
          Save
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
    max-width: 600px;
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

  .setting-section {
    margin-bottom: var(--spacing-6);
  }

  .setting-section h3 {
    margin: 0 0 var(--spacing-2) 0;
    color: var(--color-text-primary);
    font-size: var(--font-size-lg);
  }

  .description {
    margin: 0 0 var(--spacing-4) 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-sm);
    line-height: 1.5;
  }

  .path-selector {
    display: flex;
    gap: var(--spacing-2);
    margin-bottom: var(--spacing-3);
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

  .path-input.empty {
    color: var(--color-text-tertiary);
    font-style: italic;
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

  .clear-btn {
    padding: var(--spacing-2) var(--spacing-3);
    background: none;
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-text-secondary);
    cursor: pointer;
    font-size: var(--font-size-sm);
    transition: all 0.2s;
  }

  .clear-btn:hover {
    background-color: var(--color-bg-hover);
    border-color: var(--color-text-secondary);
    color: var(--color-text-primary);
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

  .btn-primary:hover {
    background-color: var(--color-primary-hover);
  }
</style>
