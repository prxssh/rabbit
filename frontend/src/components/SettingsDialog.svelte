<script lang="ts">
  import {GetConfig, UpdateConfig, SelectDownloadDirectory} from '../../wailsjs/go/torrent/Client.js'
  import type {config} from '../../wailsjs/go/models'
  import {onMount} from 'svelte'

  export let show = false
  export let onClose: () => void

  let cfg: config.Config | null = null
  let loading = true
  let saveStatus = ''

  // Form values
  let downloadDir = ''
  let port = 6969
  let numWant = 50
  let maxPeers = 50
  let maxUploadRate = 0
  let maxDownloadRate = 0
  let pieceStrategy = 1 // 0=Random, 1=RarestFirst, 2=Sequential

  onMount(async () => {
    await loadConfig()
  })

  async function loadConfig() {
    try {
      loading = true
      cfg = await GetConfig()
      if (cfg) {
        downloadDir = cfg.DefaultDownloadDir
        port = cfg.Port
        numWant = cfg.NumWant
        maxPeers = cfg.MaxPeers
        maxUploadRate = Number(cfg.MaxUploadRate)
        maxDownloadRate = Number(cfg.MaxDownloadRate)
        pieceStrategy = cfg.PieceDownloadStrategy
      }
    } catch (error) {
      console.error('Failed to load config:', error)
      saveStatus = 'Failed to load settings'
    } finally {
      loading = false
    }
  }

  async function selectDirectory() {
    try {
      const path = await SelectDownloadDirectory()
      if (path) {
        downloadDir = path
      }
    } catch (error) {
      console.error('Failed to select directory:', error)
    }
  }

  function validateSettings(): string | null {
    // Validate download directory
    if (!downloadDir || downloadDir.trim() === '') {
      return 'Download directory cannot be empty'
    }

    // Validate max peers
    if (maxPeers < 1 || maxPeers > 100) {
      return 'Max peers must be between 1 and 100'
    }

    // Validate num want
    if (numWant < 1 || numWant > 200) {
      return 'Peers per tracker request must be between 1 and 200'
    }

    // Validate upload/download rates
    if (maxUploadRate < 0) {
      return 'Max upload rate cannot be negative'
    }

    if (maxDownloadRate < 0) {
      return 'Max download rate cannot be negative'
    }

    // Validate piece strategy
    if (pieceStrategy < 0 || pieceStrategy > 2) {
      return 'Invalid piece download strategy'
    }

    return null
  }

  async function saveSettings() {
    if (!cfg) return

    // Validate before saving
    const validationError = validateSettings()
    if (validationError) {
      saveStatus = validationError
      setTimeout(() => {
        saveStatus = ''
      }, 3000)
      return
    }

    try {
      saveStatus = 'Saving...'

      const updatedConfig: config.Config = {
        ...cfg,
        DefaultDownloadDir: downloadDir,
        Port: port,
        NumWant: numWant,
        MaxPeers: maxPeers,
        MaxUploadRate: maxUploadRate,
        MaxDownloadRate: maxDownloadRate,
        PieceDownloadStrategy: pieceStrategy
      }

      await UpdateConfig(updatedConfig)
      saveStatus = 'Settings saved successfully!'

      setTimeout(() => {
        saveStatus = ''
        onClose()
      }, 1500)
    } catch (error) {
      console.error('Failed to save settings:', error)
      saveStatus = 'Failed to save settings'
      setTimeout(() => {
        saveStatus = ''
      }, 3000)
    }
  }

  function handleCancel() {
    saveStatus = ''
    onClose()
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B/s (Unlimited)'
    const k = 1024
    const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
  }

  $: if (show) {
    loadConfig()
  }
</script>

{#if show}
  <div class="modal-overlay" on:click={handleCancel}>
    <div class="modal-content" on:click|stopPropagation>
      <div class="modal-header">
        <h2>Settings</h2>
        <button class="close-btn" on:click={handleCancel}>&times;</button>
      </div>

      {#if loading}
        <div class="loading">Loading settings...</div>
      {:else}
        <div class="modal-body">
          <div class="settings-section">
            <h3>General</h3>

            <div class="form-group">
              <label for="downloadDir">Default Download Directory</label>
              <div class="input-with-button">
                <input
                  type="text"
                  id="downloadDir"
                  bind:value={downloadDir}
                  readonly
                  placeholder="Select a directory..."
                />
                <button class="browse-btn" on:click={selectDirectory}>
                  Browse
                </button>
              </div>
            </div>
          </div>

          <div class="settings-section">
            <h3>Network</h3>

            <div class="form-group">
              <label for="maxPeers">Max Peers</label>
              <input
                type="number"
                id="maxPeers"
                bind:value={maxPeers}
                min="1"
                max="100"
              />
              <span class="hint">Maximum concurrent peer connections per torrent (1-100)</span>
            </div>

            <div class="form-group">
              <label for="numWant">Peers Per Tracker Request</label>
              <input
                type="number"
                id="numWant"
                bind:value={numWant}
                min="1"
                max="200"
              />
              <span class="hint">Number of peers to request from tracker</span>
            </div>
          </div>

          <div class="settings-section">
            <h3>Bandwidth</h3>

            <div class="form-group">
              <label for="maxDownloadRate">
                Max Download Rate (bytes/s)
                <span class="current-value">{formatBytes(maxDownloadRate)}</span>
              </label>
              <input
                type="number"
                id="maxDownloadRate"
                bind:value={maxDownloadRate}
                min="0"
                step="1024"
              />
              <span class="hint">0 = unlimited</span>
            </div>

            <div class="form-group">
              <label for="maxUploadRate">
                Max Upload Rate (bytes/s)
                <span class="current-value">{formatBytes(maxUploadRate)}</span>
              </label>
              <input
                type="number"
                id="maxUploadRate"
                bind:value={maxUploadRate}
                min="0"
                step="1024"
              />
              <span class="hint">0 = unlimited</span>
            </div>
          </div>

          <div class="settings-section">
            <h3>Advanced</h3>

            <div class="form-group">
              <label>Piece Download Strategy</label>
              <div class="strategy-buttons">
                <button
                  type="button"
                  class="strategy-btn"
                  class:active={pieceStrategy === 0}
                  on:click={() => pieceStrategy = 0}
                >
                  Random
                </button>
                <button
                  type="button"
                  class="strategy-btn"
                  class:active={pieceStrategy === 1}
                  on:click={() => pieceStrategy = 1}
                >
                  Rarest First
                </button>
                <button
                  type="button"
                  class="strategy-btn"
                  class:active={pieceStrategy === 2}
                  on:click={() => pieceStrategy = 2}
                >
                  Sequential
                </button>
              </div>
              <span class="hint">
                {#if pieceStrategy === 0}
                  Download pieces in random order
                {:else if pieceStrategy === 1}
                  Prioritize rare pieces (recommended for swarm health)
                {:else}
                  Download pieces in order (good for streaming)
                {/if}
              </span>
            </div>
          </div>

          {#if saveStatus}
            <div class="save-status" class:error={saveStatus !== 'Saving...' && saveStatus !== 'Settings saved successfully!'}>
              {saveStatus}
            </div>
          {/if}
        </div>

        <div class="modal-footer">
          <button class="cancel-btn" on:click={handleCancel}>Cancel</button>
          <button class="save-btn" on:click={saveSettings}>Save</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal-content {
    background: var(--color-bg-primary);
    border: 1px solid var(--color-border-secondary);
    border-radius: var(--radius-base);
    width: 90%;
    max-width: 600px;
    max-height: 85vh;
    display: flex;
    flex-direction: column;
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--spacing-4) var(--spacing-5);
    border-bottom: 1px solid var(--color-border-primary);
  }

  .modal-header h2 {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }

  .close-btn {
    background: transparent;
    border: 1px solid var(--color-border-tertiary);
    font-size: 20px;
    color: var(--color-text-disabled);
    cursor: pointer;
    line-height: 1;
    padding: 0;
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--radius-sm);
    transition: all var(--transition-base);
  }

  .close-btn:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-hover);
    color: var(--color-text-secondary);
  }

  .loading {
    padding: var(--spacing-8);
    text-align: center;
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }

  .modal-body {
    flex: 1;
    overflow-y: auto;
    padding: var(--spacing-5);
  }

  .settings-section {
    margin-bottom: var(--spacing-5);
  }

  .settings-section:last-child {
    margin-bottom: 0;
  }

  .settings-section h3 {
    margin: 0 0 var(--spacing-3) 0;
    font-size: var(--font-size-base);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wide);
  }

  .form-group {
    margin-bottom: var(--spacing-3);
  }

  .form-group label {
    display: block;
    margin-bottom: var(--spacing-1);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-normal);
    color: var(--color-text-secondary);
  }

  .form-group input[type="text"],
  .form-group input[type="number"],
  .form-group select {
    width: 100%;
    padding: var(--spacing-2);
    border: 1px solid var(--color-border-secondary);
    border-radius: var(--radius-sm);
    background: var(--color-bg-secondary);
    color: var(--color-text-primary);
    font-size: var(--font-size-sm);
    font-family: var(--font-family-base);
    transition: all var(--transition-base);
  }

  .form-group input[type="text"]:focus,
  .form-group input[type="number"]:focus,
  .form-group select:focus {
    outline: none;
    border-color: var(--color-border-tertiary);
    background: var(--color-bg-tertiary);
  }

  .form-group input[type="text"]:read-only {
    color: var(--color-text-muted);
    cursor: default;
  }

  .input-with-button {
    display: flex;
    gap: var(--spacing-2);
  }

  .input-with-button input {
    flex: 1;
  }

  .browse-btn {
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--color-bg-tertiary);
    color: var(--color-text-primary);
    border: 1px solid var(--color-border-tertiary);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--font-size-sm);
    font-family: var(--font-family-base);
    white-space: nowrap;
    transition: all var(--transition-base);
  }

  .browse-btn:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-hover);
  }

  .hint {
    display: block;
    margin-top: var(--spacing-1);
    font-size: var(--font-size-xs);
    color: var(--color-text-muted);
  }

  .current-value {
    float: right;
    color: var(--color-text-muted);
    font-weight: var(--font-weight-normal);
    font-size: var(--font-size-xs);
  }

  .form-group.checkbox {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .form-group.checkbox input[type="checkbox"] {
    width: auto;
    margin: 0;
    cursor: pointer;
  }

  .form-group.checkbox label {
    margin: 0;
    cursor: pointer;
  }

  .strategy-buttons {
    display: flex;
    gap: var(--spacing-2);
  }

  .strategy-btn {
    flex: 1;
    padding: var(--spacing-2) var(--spacing-3);
    background: transparent;
    color: var(--color-text-secondary);
    border: 1px solid var(--color-border-secondary);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--font-size-sm);
    font-family: var(--font-family-base);
    transition: all var(--transition-base);
  }

  .strategy-btn:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-tertiary);
  }

  .strategy-btn.active {
    background: var(--color-bg-tertiary);
    color: var(--color-text-primary);
    border-color: var(--color-border-tertiary);
  }

  .save-status {
    margin-top: var(--spacing-3);
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--color-bg-tertiary);
    color: var(--color-text-secondary);
    border: 1px solid var(--color-border-secondary);
    border-radius: var(--radius-sm);
    text-align: center;
    font-size: var(--font-size-xs);
  }

  .save-status.error {
    background: var(--color-error-bg);
    border-color: var(--color-error-border);
    color: var(--color-error);
  }

  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-2);
    padding: var(--spacing-4) var(--spacing-5);
    border-top: 1px solid var(--color-border-primary);
  }

  .cancel-btn,
  .save-btn {
    padding: var(--spacing-2) var(--spacing-4);
    border: 1px solid var(--color-border-tertiary);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--font-size-sm);
    font-family: var(--font-family-base);
    transition: all var(--transition-base);
  }

  .cancel-btn {
    background: transparent;
    color: var(--color-text-secondary);
  }

  .cancel-btn:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-hover);
  }

  .save-btn {
    background: var(--color-bg-tertiary);
    color: var(--color-text-primary);
  }

  .save-btn:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-hover);
  }
</style>
