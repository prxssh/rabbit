<script lang="ts">
  import {AddTorrent, GetTorrentStats, RemoveTorrent, GetConfig, UpdateConfig} from '../wailsjs/go/torrent/Client.js'
  import type {torrent, peer} from '../wailsjs/go/models'
  import {onDestroy, onMount} from 'svelte'
  import TopBar from './components/TopBar.svelte'
  import StatusBar from './components/StatusBar.svelte'
  import TorrentItem from './components/TorrentItem.svelte'
  import EmptyState from './components/EmptyState.svelte'
  import DetailPanel from './components/DetailPanel.svelte'
  import AddTorrentDialog from './components/AddTorrentDialog.svelte'
  import SettingsDialog from './components/SettingsDialog.svelte'

  let fileInput: HTMLInputElement
  let isDragging = false
  let selectedFile: File | null = null
  let uploadStatus = ''
  let torrents: any[] = []
  let selectedTorrentId: number | null = null
  let peers: peer.PeerStats[] = []
  let pieceStates: number[] = []
  let statsUpdateInterval: number | null = null
  let showAddDialog = false
  let showSettingsDialog = false
  let pendingFile: File | null = null
  let defaultDownloadPath = ''
  let totalDownloadRate = 0
  let totalUploadRate = 0

  onMount(async () => {
    // Load default download path from config
    try {
      const cfg = await GetConfig()
      if (cfg) {
        defaultDownloadPath = cfg.DefaultDownloadDir
      }
    } catch (error) {
      console.error('Failed to load config:', error)
    }
  })

  function handleDragOver(e: DragEvent) {
    e.preventDefault()
    isDragging = true
  }

  function handleDragLeave(e: DragEvent) {
    e.preventDefault()
    isDragging = false
  }

  async function handleDrop(e: DragEvent) {
    e.preventDefault()
    isDragging = false

    const files = e.dataTransfer?.files
    if (files && files.length > 0) {
      const file = files[0]
      if (file.name.endsWith('.torrent')) {
        // If default path exists, use it directly
        if (defaultDownloadPath) {
          await uploadTorrent(file, defaultDownloadPath)
        } else {
          pendingFile = file
          showAddDialog = true
        }
      } else {
        uploadStatus = 'Error: Please select a .torrent file'
      }
    }
  }

  function handleFileSelect(e: Event) {
    const target = e.target as HTMLInputElement
    const files = target.files
    if (files && files.length > 0) {
      const file = files[0]
      if (file.name.endsWith('.torrent')) {
        // If default path exists, use it directly
        if (defaultDownloadPath) {
          uploadTorrent(file, defaultDownloadPath)
        } else {
          pendingFile = file
          showAddDialog = true
        }
      } else {
        uploadStatus = 'Error: Please select a .torrent file'
      }
    }
    // Reset input so the same file can be selected again
    target.value = ''
  }

  function formatHash(hash: number[]): string {
    return hash.map(b => b.toString(16).padStart(2, '0')).join('')
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
  }

  function formatBytesPerSec(bytes: number): string {
    return formatBytes(bytes) + '/s'
  }

  async function updateAllTorrentsStats() {
    let aggDownload = 0
    let aggUpload = 0
    const updatedTorrents = await Promise.all(
      torrents.map(async (torrent) => {
        if (torrent.torrentData?.metainfo?.info?.hash) {
          const infoHash = formatHash(torrent.torrentData.metainfo.info.hash)
          try {
            const stats = await GetTorrentStats(infoHash)
            if (stats) {
              // Update peers and piece states if this is the selected torrent
              if (selectedTorrentId === torrent.id) {
                peers = stats.peers || []
                // Force a new array reference to trigger Svelte reactivity
                pieceStates = (stats.pieceStates || []).slice()
              }

              // Track aggregate rates
              aggDownload += stats.downloadRate || 0
              aggUpload += stats.uploadRate || 0

              // Align progress with PieceHeatmap: percent of completed pieces
              let progressPercent = stats.progress
              const totalPieces = torrent.torrentData?.metainfo?.info?.pieces?.length || 0
              if (Array.isArray(stats.pieceStates) && totalPieces > 0) {
                const completed = stats.pieceStates.filter((s: number) => s === 2).length
                progressPercent = (completed / totalPieces) * 100
              }

              return {
                ...torrent,
                progress: progressPercent,
                downloadSpeed: formatBytesPerSec(stats.downloadRate),
                uploadSpeed: formatBytesPerSec(stats.uploadRate)
              }
            }
          } catch (error) {
            console.error('Failed to load stats:', error)
          }
        }
        return torrent
      })
    )
    torrents = updatedTorrents
    totalDownloadRate = aggDownload
    totalUploadRate = aggUpload
  }

  async function uploadTorrent(file: File, downloadPath: string) {
    try {
      uploadStatus = `Uploading ${file.name}...`
      const arrayBuffer = await file.arrayBuffer()
      const bytes = new Uint8Array(arrayBuffer)

      const result: torrent.Torrent = await AddTorrent(Array.from(bytes), downloadPath)
      uploadStatus = `Success: ${file.name} added`
      selectedFile = null

      const newTorrent = {
        id: Date.now(),
        fileName: file.name,
        torrentData: result,
        status: 'downloading',
        progress: 0,
        downloadSpeed: '0 KB/s',
        uploadSpeed: '0 KB/s'
      }

      torrents = [...torrents, newTorrent]
      selectedTorrentId = newTorrent.id
    } catch (error) {
      uploadStatus = `Error: ${error}`
    }
  }

  async function handleAddDialogConfirm(downloadPath: string, remember: boolean) {
    if (pendingFile) {
      uploadTorrent(pendingFile, downloadPath)
      pendingFile = null

      // Update config if user checked "Remember this location"
      if (remember) {
        defaultDownloadPath = downloadPath
        try {
          const cfg = await GetConfig()
          if (cfg) {
            cfg.DefaultDownloadDir = downloadPath
            await UpdateConfig(cfg)
          }
        } catch (error) {
          console.error('Failed to update config:', error)
        }
      }
    }
    showAddDialog = false
  }

  function handleAddDialogCancel() {
    pendingFile = null
    showAddDialog = false
  }

  async function removeTorrent(id: number) {
    const torrent = torrents.find(t => t.id === id)
    if (torrent && torrent.torrentData?.metainfo?.info?.hash) {
      const infoHash = formatHash(torrent.torrentData.metainfo.info.hash)
      try {
        await RemoveTorrent(infoHash)
        uploadStatus = 'Torrent removed'
      } catch (error) {
        console.error('Failed to remove torrent:', error)
        uploadStatus = `Error: Failed to remove torrent`
        return
      }
    }

    torrents = torrents.filter(t => t.id !== id)
    if (selectedTorrentId === id) {
      selectedTorrentId = null
    }
  }

  function selectTorrent(id: number) {
    selectedTorrentId = selectedTorrentId === id ? null : id
    if (selectedTorrentId) {
      const torrent = torrents.find(t => t.id === selectedTorrentId)
      if (torrent) {
        pieceStates = new Array(torrent.torrentData.metainfo.info.pieces.length).fill(0)
      }
    }
  }

  function openFileDialog() {
    fileInput.click()
  }

  function openSettingsDialog() {
    showSettingsDialog = true
  }

  async function closeSettingsDialog() {
    showSettingsDialog = false

    // Reload config to get updated default download path
    try {
      const cfg = await GetConfig()
      if (cfg) {
        defaultDownloadPath = cfg.DefaultDownloadDir
      }
    } catch (error) {
      console.error('Failed to reload config:', error)
    }
  }

  $: selectedTorrent = torrents.find(t => t.id === selectedTorrentId)

  // Clear peers and piece states when selection changes to null
  $: if (!selectedTorrent) {
    peers = []
    pieceStates = []
  }

  // Start stats update interval when we have torrents
  $: if (torrents.length > 0) {
    if (!statsUpdateInterval) {
      updateAllTorrentsStats() // Update immediately
      statsUpdateInterval = setInterval(updateAllTorrentsStats, 2000)
    }
  } else {
    if (statsUpdateInterval) {
      clearInterval(statsUpdateInterval)
      statsUpdateInterval = null
    }
  }

  onDestroy(() => {
    if (statsUpdateInterval) {
      clearInterval(statsUpdateInterval)
    }
  })
</script>

<main class="{isDragging ? 'dragging' : ''}"
      on:dragover={handleDragOver}
      on:dragleave={handleDragLeave}
      on:drop={handleDrop}>

  <TopBar
    torrentCount={torrents.length}
    onAddTorrent={openFileDialog}
    onOpenSettings={openSettingsDialog}
    downloadSpeed={formatBytesPerSec(totalDownloadRate)}
    uploadSpeed={formatBytesPerSec(totalUploadRate)}
  />

  <div class="content">
    <div class="torrent-list">
      {#if torrents.length === 0}
        <EmptyState onAddTorrent={openFileDialog} />
      {:else}
        {#each torrents as torrent (torrent.id)}
          <TorrentItem
            id={torrent.id}
            torrentData={torrent.torrentData}
            fileName={torrent.fileName}
            progress={torrent.progress}
            downloadSpeed={torrent.downloadSpeed}
            uploadSpeed={torrent.uploadSpeed}
            selected={selectedTorrentId === torrent.id}
            onSelect={() => selectTorrent(torrent.id)}
            onRemove={() => removeTorrent(torrent.id)}
          />
        {/each}
      {/if}
    </div>

    {#if selectedTorrent}
      <div class="detail-panel-container">
        <DetailPanel
          torrentData={selectedTorrent.torrentData}
          {peers}
          {pieceStates}
        />
      </div>
    {/if}
  </div>

  <StatusBar
    status={uploadStatus || 'Ready'}
    isError={uploadStatus.startsWith('Error')}
    isSuccess={uploadStatus.startsWith('Success')}
  />

  <input
    type="file"
    accept=".torrent"
    bind:this={fileInput}
    on:change={handleFileSelect}
    style="display: none"
  />

  <AddTorrentDialog
    show={showAddDialog}
    selectedFile={pendingFile}
    defaultPath={defaultDownloadPath}
    onConfirm={handleAddDialogConfirm}
    onCancel={handleAddDialogCancel}
  />

  <SettingsDialog
    show={showSettingsDialog}
    onClose={closeSettingsDialog}
  />
</main>

<style>
  main {
    width: 100vw;
    height: 100vh;
    display: flex;
    flex-direction: column;
    background-color: var(--color-bg-primary);
  }

  main.dragging {
    background-color: var(--color-bg-hover);
  }

  main.dragging::after {
    content: 'Drop .torrent file to add';
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    font-size: var(--font-size-3xl);
    color: var(--color-text-tertiary);
    pointer-events: none;
  }

  .content {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    padding: var(--spacing-6);
    gap: var(--spacing-4);
  }

  .torrent-list {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .detail-panel-container {
    height: 50%;
    min-height: 300px;
    overflow: hidden;
  }
</style>
