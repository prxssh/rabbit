<script lang="ts">
  // TODO: Fix SettingsDialog to use new config system
  import {SelectDownloadDirectory} from '../../wailsjs/go/torrent/Client.js'
  // import type {config} from '../../wailsjs/go/models'
  import {onMount} from 'svelte'
  import Modal from './ui/Modal.svelte'
  import Button from './ui/Button.svelte'

  export let show = false
  export let onClose: () => void

  // let cfg: config.Config | null = null
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
    // await loadConfig()
    loading = false
  })

  // async function loadConfig() {
  //   try {
  //     loading = true
  //     cfg = await GetConfig()
  //     if (cfg) {
  //       downloadDir = cfg.DefaultDownloadDir
  //       port = cfg.Port
  //       numWant = cfg.NumWant
  //       maxPeers = cfg.MaxPeers
  //       maxUploadRate = Number(cfg.MaxUploadRate)
  //       maxDownloadRate = Number(cfg.MaxDownloadRate)
  //       pieceStrategy = cfg.PieceDownloadStrategy
  //     }
  //   } catch (error) {
  //     console.error('Failed to load config:', error)
  //     saveStatus = 'Failed to load settings'
  //   } finally {
  //     loading = false
  //   }
  // }

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

  // async function saveSettings() {
  //   if (!cfg) return
  //   // Validate before saving
  //   const validationError = validateSettings()
  //   if (validationError) {
  //     saveStatus = validationError
  //     setTimeout(() => {
  //       saveStatus = ''
  //     }, 3000)
  //     return
  //   }
  //   try {
  //     saveStatus = 'Saving...'
  //     const updatedConfig: config.Config = {
  //       ...cfg,
  //       DefaultDownloadDir: downloadDir,
  //       Port: port,
  //       NumWant: numWant,
  //       MaxPeers: maxPeers,
  //       MaxUploadRate: maxUploadRate,
  //       MaxDownloadRate: maxDownloadRate,
  //       PieceDownloadStrategy: pieceStrategy
  //     }
  //     await UpdateConfig(updatedConfig)
  //     saveStatus = 'Settings saved successfully!'
  //     setTimeout(() => {
  //       saveStatus = ''
  //       onClose()
  //     }, 1500)
  //   } catch (error) {
  //     console.error('Failed to save settings:', error)
  //     saveStatus = 'Failed to save settings'
  //     setTimeout(() => {
  //       saveStatus = ''
  //     }, 3000)
  //   }
  // }

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

<Modal {show} title="Settings" {onClose}>
  {#if loading}
    <div class="loading">Loading settings...</div>
  {:else}
    <div class="settings-content">
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
                <Button variant="secondary" on:click={selectDirectory}>
                  Browse
                </Button>
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
  {/if}

  <svelte:fragment slot="footer">
    <Button variant="ghost" on:click={handleCancel}>Cancel</Button>
    <Button variant="primary" on:click={saveSettings}>Save</Button>
  </svelte:fragment>
</Modal>

<style>
  .loading {
    padding: var(--spacing-8);
    text-align: center;
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }

  .settings-content {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-5);
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
</style>
