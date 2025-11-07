<script lang="ts">
    import {
        AddTorrent,
        GetTorrentStats,
        RemoveTorrent,
        GetDefaultConfig,
    } from '../wailsjs/go/ui/Client.js'
    import type { torrent, peer } from '../wailsjs/go/models'
    import { onDestroy, onMount } from 'svelte'
    import TopBar from './components/TopBar.svelte'
    import StatusBar from './components/StatusBar.svelte'
    import TorrentItem from './components/TorrentItem.svelte'
    import EmptyState from './components/EmptyState.svelte'
    import DetailPanel from './components/DetailPanel.svelte'
    import AddTorrentDialog from './components/AddTorrentDialog.svelte'
    import EditTorrentDialog from './components/EditTorrentDialog.svelte'
    import { formatBytes, formatBytesPerSec, formatHash } from './lib/utils'

    interface TorrentItemData {
        id: number
        fileName: string
        size: number
        torrentData: torrent.Torrent
        status: string
        progress: number
        downloadSpeed: string
        uploadSpeed: string
    }

    let fileInput: HTMLInputElement
    let isDragging = false
    let selectedFile: File | null = null
    let uploadStatus = ''
    let torrents: TorrentItemData[] = []
    let selectedTorrentId: number | null = null
    let peers: peer.PeerMetrics[] = []
    let pieceStates: number[] = []
    let selectedStats: any = null
    let statsUpdateInterval: number | null = null
    let showAddDialog = false
    let showEditDialog = false
    let editingTorrentId: number | null = null
    let pendingFile: File | null = null
    let defaultDownloadPath = ''
    let totalDownloadRate = 0
    let totalUploadRate = 0

    onMount(async () => {
        // Load default download path from default config
        try {
            const cfg = await GetDefaultConfig()
            if (cfg?.Storage) {
                defaultDownloadPath = cfg.Storage.DownloadDir
            }
        } catch (error) {
            console.error('Failed to load default config:', error)
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
                // Always show dialog for configuration
                pendingFile = file
                showAddDialog = true
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
                // Always show dialog for configuration
                pendingFile = file
                showAddDialog = true
            } else {
                uploadStatus = 'Error: Please select a .torrent file'
            }
        }
        // Reset input so the same file can be selected again
        target.value = ''
    }

    async function updateAllTorrentsStats() {
        let aggDownload = 0
        let aggUpload = 0
        const updatedTorrents = await Promise.all(
            torrents.map(async (torrent) => {
                if (torrent.torrentData?.metainfo?.hash) {
                    const infoHash = formatHash(torrent.torrentData.metainfo.hash)
                    try {
                        const stats = await GetTorrentStats(infoHash)
                        if (stats) {
                            // Update peers and piece states if this is the selected torrent
                            if (selectedTorrentId === torrent.id) {
                                peers = stats.peers || []
                                // Force a new array reference to trigger Svelte reactivity
                                pieceStates = (stats.pieceStates || []).slice()
                                selectedStats = stats
                            }

                            // Track aggregate rates
                            aggDownload += stats.downloadRate || 0
                            aggUpload += stats.uploadRate || 0

                            // Align progress with PieceHeatmap: percent of completed pieces
                            let progressPercent = stats.progress
                            const totalPieces =
                                torrent.torrentData?.metainfo?.info?.pieces?.length || 0
                            if (Array.isArray(stats.pieceStates) && totalPieces > 0) {
                                const completed = stats.pieceStates.filter(
                                    (s: number) => s === 2
                                ).length
                                progressPercent = (completed / totalPieces) * 100
                            }

                            return {
                                ...torrent,
                                progress: progressPercent,
                                downloadSpeed: formatBytesPerSec(stats.downloadRate),
                                uploadSpeed: formatBytesPerSec(stats.uploadRate),
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

    async function uploadTorrent(file: File, config: torrent.Config) {
        try {
            uploadStatus = `Uploading ${file.name}...`
            const arrayBuffer = await file.arrayBuffer()
            const bytes = new Uint8Array(arrayBuffer)

            const result: torrent.Torrent = await AddTorrent(Array.from(bytes), config)
            uploadStatus = `Success: ${file.name} added`
            selectedFile = null

            const newTorrent = {
                id: Date.now(),
                fileName: file.name,
                torrentData: result,
                status: 'downloading',
                progress: 0,
                downloadSpeed: '0 KB/s',
                uploadSpeed: '0 KB/s',
            }

            torrents = [...torrents, newTorrent]
            selectedTorrentId = newTorrent.id
        } catch (error) {
            uploadStatus = `Error: ${error}`
        }
    }

    async function handleAddDialogConfirm(config: torrent.Config, remember: boolean) {
        if (pendingFile) {
            uploadTorrent(pendingFile, config)
            pendingFile = null

            // Update default download path if user checked "Remember this location"
            if (remember && config.Storage?.DownloadDir) {
                defaultDownloadPath = config.Storage.DownloadDir
                // TODO: Persist this to app config
            }
        }
        showAddDialog = false
    }

    function handleAddDialogCancel() {
        pendingFile = null
        showAddDialog = false
    }

    async function removeTorrent(id: number) {
        const torrent = torrents.find((t) => t.id === id)
        if (torrent && torrent.torrentData?.metainfo?.hash) {
            const infoHash = formatHash(torrent.torrentData.metainfo.hash)
            try {
                await RemoveTorrent(infoHash)
                uploadStatus = 'Torrent removed'
            } catch (error) {
                console.error('Failed to remove torrent:', error)
                uploadStatus = `Error: Failed to remove torrent`
                return
            }
        }

        torrents = torrents.filter((t) => t.id !== id)
        if (selectedTorrentId === id) {
            selectedTorrentId = null
        }
    }

    function selectTorrent(id: number) {
        selectedTorrentId = selectedTorrentId === id ? null : id
        if (selectedTorrentId) {
            const torrent = torrents.find((t) => t.id === selectedTorrentId)
            if (torrent) {
                pieceStates = new Array(torrent.torrentData.metainfo.info.pieces.length).fill(0)
            }
        }
    }

    function openFileDialog() {
        fileInput.click()
    }

    function openEditDialog(id: number) {
        editingTorrentId = id
        showEditDialog = true
    }

    function handleEditDialogConfirm() {
        showEditDialog = false
        editingTorrentId = null
        uploadStatus = 'Torrent configuration updated'
    }

    function handleEditDialogCancel() {
        showEditDialog = false
        editingTorrentId = null
    }

    $: selectedTorrent = torrents.find((t) => t.id === selectedTorrentId)

    // Clear peers and piece states when selection changes to null
    $: if (!selectedTorrent) {
        peers = []
        pieceStates = []
        selectedStats = null
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

<main
    class={isDragging ? 'dragging' : ''}
    on:dragover={handleDragOver}
    on:dragleave={handleDragLeave}
    on:drop={handleDrop}
>
    <TopBar
        torrentCount={torrents.length}
        onAddTorrent={openFileDialog}
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
                        onSettings={() => openEditDialog(torrent.id)}
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
                    stats={selectedStats}
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

    <EditTorrentDialog
        show={showEditDialog}
        torrentName={torrents.find((t) => t.id === editingTorrentId)?.torrentData?.metainfo?.info
            ?.name || ''}
        infoHash={editingTorrentId
            ? formatHash(
                  torrents.find((t) => t.id === editingTorrentId)?.torrentData?.metainfo?.hash || []
              )
            : ''}
        onConfirm={handleEditDialogConfirm}
        onCancel={handleEditDialogCancel}
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
