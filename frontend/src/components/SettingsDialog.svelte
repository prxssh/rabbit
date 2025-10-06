<script lang="ts">
  import {SelectDownloadDirectory, GetConfig, UpdateConfig} from '../../wailsjs/go/torrent/Client.js'
  import {onMount} from 'svelte'

  export let show = false
  export let onClose: () => void

  // Config state
  let config = {
    DefaultDownloadDir: '',
    Port: 6969,
    MaxPeers: 50,
    MaxUploadRate: 0,
    MaxDownloadRate: 0,
    AnnounceInterval: 0,
    MinAnnounceInterval: 120000000000,
    MaxAnnounceBackoff: 300000000000,
    EnableIPv6: true,
    EnableDHT: false,
    EnablePEX: false,
    ClientIDPrefix: '-EC0001-',
    PeerManagerConfig: {
      MaxPeers: 50,
      MaxInflightRequestsPerPeer: 5,
      MaxRequestsPerPiece: 4,
      PeerHeartbeatInterval: 120000000000,
      ReadTimeout: 45000000000,
      WriteTimeout: 45000000000,
      DialTimeout: 30000000000,
      KeepAliveInterval: 120000000000,
      PeerOutboundQueueBacklog: 25
    },
    PieceManagerConfig: {
      PickerConfig: {
        DownloadStrategy: 1,
        MaxInflightRequests: 20,
        RequestTimeout: 30000000000,
        EndgameDupPerBlock: 2,
        MaxRequestsPerBlocks: 4
      },
      DownloadDir: ''
    }
  }

  let isSelectingPath = false

  $: if (show) {
    loadConfig()
  }

  async function loadConfig() {
    try {
      const cfg = await GetConfig()
      config = {...cfg}
    } catch (error) {
      console.error('Failed to load config:', error)
    }
  }

  async function selectDirectory() {
    try {
      isSelectingPath = true
      const path = await SelectDownloadDirectory()
      if (path) {
        config.DefaultDownloadDir = path
      }
    } catch (error) {
      console.error('Failed to select directory:', error)
    } finally {
      isSelectingPath = false
    }
  }

  async function handleSave() {
    try {
      await UpdateConfig(config)
      onClose()
    } catch (error) {
      console.error('Failed to save config:', error)
    }
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  // Convert nanoseconds to seconds for display
  function nsToSeconds(ns: number): number {
    return Math.floor(ns / 1000000000)
  }

  // Convert seconds to nanoseconds
  function secondsToNs(seconds: number): number {
    return seconds * 1000000000
  }

  // Convert bytes/sec to MB/sec for display
  function bytesToMB(bytes: number): number {
    return bytes / (1024 * 1024)
  }

  // Convert MB/sec to bytes/sec
  function mbToBytes(mb: number): number {
    return mb * 1024 * 1024
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
        <!-- Download Settings -->
        <div class="setting-section">
          <h3>Download Settings</h3>

          <div class="form-group">
            <label for="download-dir">Default Download Directory</label>
            <p class="description">
              Default directory for NEW torrents. Changing this only affects future downloads; active torrents continue in their current location.
            </p>
            <div class="path-selector">
              <input
                id="download-dir"
                type="text"
                readonly
                bind:value={config.DefaultDownloadDir}
                placeholder="Not set - will ask each time"
                class="path-input"
                class:empty={!config.DefaultDownloadDir}
              />
              <button
                class="browse-btn"
                on:click={selectDirectory}
                disabled={isSelectingPath}
              >
                {isSelectingPath ? 'Selecting...' : 'Browse'}
              </button>
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="max-download">Max Download Rate (MB/s)</label>
              <p class="description">0 = unlimited</p>
              <input
                id="max-download"
                type="number"
                min="0"
                step="0.1"
                value={bytesToMB(config.MaxDownloadRate)}
                on:input={(e) => config.MaxDownloadRate = mbToBytes(parseFloat(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="max-upload">Max Upload Rate (MB/s)</label>
              <p class="description">0 = unlimited</p>
              <input
                id="max-upload"
                type="number"
                min="0"
                step="0.1"
                value={bytesToMB(config.MaxUploadRate)}
                on:input={(e) => config.MaxUploadRate = mbToBytes(parseFloat(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>
          </div>
        </div>

        <!-- Connection Settings -->
        <div class="setting-section">
          <h3>Connection Settings</h3>

          <div class="form-row">
            <div class="form-group">
              <label for="port">Listening Port</label>
              <p class="description">TCP port for incoming connections</p>
              <input
                id="port"
                type="number"
                min="1024"
                max="65535"
                bind:value={config.Port}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="max-peers">Max Peers</label>
              <p class="description">Maximum concurrent connections</p>
              <input
                id="max-peers"
                type="number"
                min="1"
                max="1000"
                bind:value={config.MaxPeers}
                class="input"
              />
            </div>
          </div>

          <div class="form-group">
            <label for="client-id">Client ID Prefix</label>
            <p class="description">8-character peer ID prefix</p>
            <input
              id="client-id"
              type="text"
              maxlength="8"
              bind:value={config.ClientIDPrefix}
              class="input"
            />
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="max-inflight-per-peer">Max Inflight Requests Per Peer</label>
              <p class="description">Outstanding requests per peer connection</p>
              <input
                id="max-inflight-per-peer"
                type="number"
                min="1"
                max="100"
                bind:value={config.PeerManagerConfig.MaxInflightRequestsPerPeer}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="max-requests-per-piece">Max Requests Per Piece</label>
              <p class="description">Duplicate requests across all peers</p>
              <input
                id="max-requests-per-piece"
                type="number"
                min="1"
                max="20"
                bind:value={config.PeerManagerConfig.MaxRequestsPerPiece}
                class="input"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="peer-heartbeat">Peer Heartbeat Interval (seconds)</label>
              <p class="description">Keep-alive message frequency</p>
              <input
                id="peer-heartbeat"
                type="number"
                min="10"
                value={nsToSeconds(config.PeerManagerConfig.PeerHeartbeatInterval)}
                on:input={(e) => config.PeerManagerConfig.PeerHeartbeatInterval = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="keep-alive">Keep Alive Interval (seconds)</label>
              <p class="description">Connection health check frequency</p>
              <input
                id="keep-alive"
                type="number"
                min="10"
                value={nsToSeconds(config.PeerManagerConfig.KeepAliveInterval)}
                on:input={(e) => config.PeerManagerConfig.KeepAliveInterval = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="read-timeout">Read Timeout (seconds)</label>
              <p class="description">Max wait time for data from peer</p>
              <input
                id="read-timeout"
                type="number"
                min="5"
                value={nsToSeconds(config.PeerManagerConfig.ReadTimeout)}
                on:input={(e) => config.PeerManagerConfig.ReadTimeout = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="write-timeout">Write Timeout (seconds)</label>
              <p class="description">Max wait time when sending to peer</p>
              <input
                id="write-timeout"
                type="number"
                min="5"
                value={nsToSeconds(config.PeerManagerConfig.WriteTimeout)}
                on:input={(e) => config.PeerManagerConfig.WriteTimeout = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="dial-timeout">Dial Timeout (seconds)</label>
              <p class="description">Max time for connection establishment</p>
              <input
                id="dial-timeout"
                type="number"
                min="5"
                value={nsToSeconds(config.PeerManagerConfig.DialTimeout)}
                on:input={(e) => config.PeerManagerConfig.DialTimeout = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="peer-queue-backlog">Peer Outbound Queue Backlog</label>
              <p class="description">Max buffered messages per peer</p>
              <input
                id="peer-queue-backlog"
                type="number"
                min="1"
                max="1000"
                bind:value={config.PeerManagerConfig.PeerOutboundQueueBacklog}
                class="input"
              />
            </div>
          </div>
        </div>

        <!-- Tracker Settings -->
        <div class="setting-section">
          <h3>Tracker Settings</h3>

          <div class="form-group">
            <label for="announce-interval">Announce Interval (seconds)</label>
            <p class="description">0 = use tracker default</p>
            <input
              id="announce-interval"
              type="number"
              min="0"
              value={nsToSeconds(config.AnnounceInterval)}
              on:input={(e) => config.AnnounceInterval = secondsToNs(parseInt(e.currentTarget.value) || 0)}
              class="input"
            />
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="min-announce">Min Announce Interval (seconds)</label>
              <input
                id="min-announce"
                type="number"
                min="0"
                value={nsToSeconds(config.MinAnnounceInterval)}
                on:input={(e) => config.MinAnnounceInterval = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="max-backoff">Max Announce Backoff (seconds)</label>
              <input
                id="max-backoff"
                type="number"
                min="0"
                value={nsToSeconds(config.MaxAnnounceBackoff)}
                on:input={(e) => config.MaxAnnounceBackoff = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>
          </div>
        </div>

        <!-- Protocol Settings -->
        <div class="setting-section">
          <h3>Protocol Settings</h3>

          <div class="checkbox-group">
            <label class="checkbox-label">
              <input
                type="checkbox"
                bind:checked={config.EnableIPv6}
              />
              <span>Enable IPv6</span>
              <p class="description">Allow connections to IPv6 peers</p>
            </label>

            <label class="checkbox-label">
              <input
                type="checkbox"
                bind:checked={config.EnableDHT}
              />
              <span>Enable DHT</span>
              <p class="description">Distributed Hash Table for peer discovery (future)</p>
            </label>

            <label class="checkbox-label">
              <input
                type="checkbox"
                bind:checked={config.EnablePEX}
              />
              <span>Enable PEX</span>
              <p class="description">Peer Exchange protocol (future)</p>
            </label>
          </div>
        </div>

        <!-- Piece/Picker Settings -->
        <div class="setting-section">
          <h3>Piece Picker Settings</h3>

          <div class="form-row">
            <div class="form-group">
              <label for="download-strategy">Download Strategy</label>
              <p class="description">Piece selection algorithm</p>
              <select
                id="download-strategy"
                bind:value={config.PieceManagerConfig.PickerConfig.DownloadStrategy}
                class="input"
              >
                <option value={0}>Random</option>
                <option value={1}>Sequential</option>
                <option value={2}>Rarest First</option>
              </select>
            </div>

            <div class="form-group">
              <label for="max-inflight-requests">Max Inflight Requests</label>
              <p class="description">Per-peer pipeline capacity</p>
              <input
                id="max-inflight-requests"
                type="number"
                min="1"
                max="100"
                bind:value={config.PieceManagerConfig.PickerConfig.MaxInflightRequests}
                class="input"
              />
            </div>
          </div>

          <div class="form-row">
            <div class="form-group">
              <label for="request-timeout">Request Timeout (seconds)</label>
              <p class="description">Block request timeout duration</p>
              <input
                id="request-timeout"
                type="number"
                min="5"
                value={nsToSeconds(config.PieceManagerConfig.PickerConfig.RequestTimeout)}
                on:input={(e) => config.PieceManagerConfig.PickerConfig.RequestTimeout = secondsToNs(parseInt(e.currentTarget.value) || 0)}
                class="input"
              />
            </div>

            <div class="form-group">
              <label for="endgame-dup">Endgame Duplicates Per Block</label>
              <p class="description">Concurrent peers for same block</p>
              <input
                id="endgame-dup"
                type="number"
                min="1"
                max="10"
                bind:value={config.PieceManagerConfig.PickerConfig.EndgameDupPerBlock}
                class="input"
              />
            </div>
          </div>

          <div class="form-group">
            <label for="max-requests-per-block">Max Requests Per Block</label>
            <p class="description">Limit duplicate block requests</p>
            <input
              id="max-requests-per-block"
              type="number"
              min="1"
              max="20"
              bind:value={config.PieceManagerConfig.PickerConfig.MaxRequestsPerBlocks}
              class="input"
            />
          </div>
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
    padding-bottom: var(--spacing-6);
    border-bottom: 1px solid var(--color-border);
  }

  .setting-section:last-child {
    border-bottom: none;
    margin-bottom: 0;
    padding-bottom: 0;
  }

  .setting-section h3 {
    margin: 0 0 var(--spacing-4) 0;
    color: var(--color-text-primary);
    font-size: var(--font-size-lg);
    font-weight: 600;
  }

  .form-group {
    margin-bottom: var(--spacing-4);
  }

  .form-group label {
    display: block;
    margin-bottom: var(--spacing-2);
    color: var(--color-text-primary);
    font-size: var(--font-size-sm);
    font-weight: 500;
  }

  .form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--spacing-4);
  }

  .description {
    margin: 0 0 var(--spacing-2) 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-xs);
    line-height: 1.4;
  }

  .input {
    width: 100%;
    padding: var(--spacing-3);
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    color: var(--color-text-primary);
    font-size: var(--font-size-sm);
    transition: all 0.2s;
  }

  .input:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px rgba(99, 102, 241, 0.1);
  }

  .checkbox-group {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .checkbox-label {
    display: flex;
    align-items: flex-start;
    gap: var(--spacing-3);
    cursor: pointer;
    padding: var(--spacing-3);
    border-radius: 4px;
    transition: background-color 0.2s;
  }

  .checkbox-label:hover {
    background-color: var(--color-bg-secondary);
  }

  .checkbox-label input[type="checkbox"] {
    margin-top: 2px;
    cursor: pointer;
  }

  .checkbox-label span {
    flex: 1;
    color: var(--color-text-primary);
    font-size: var(--font-size-sm);
    font-weight: 500;
  }

  .checkbox-label .description {
    margin-top: var(--spacing-1);
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
