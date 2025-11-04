<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import type { peer } from '../../wailsjs/go/models'
  import { GetPeerMessageHistory } from '../../wailsjs/go/torrent/Client.js'

  export let infoHash: string
  export let peers: peer.PeerMetrics[]
  export let selectedPeer: string = 'all'

  let messages: peer.Event[] = []
  let pollInterval: number | null = null
  let isLoading = false

  interface MessageWithPeer extends peer.Event {
    peerAddr?: string
  }

  $: filteredMessages = selectedPeer === 'all'
    ? messages
    : messages.filter(msg => (msg as MessageWithPeer).peerAddr === selectedPeer)

  function getPeerAddr(msg: peer.Event): string {
    return (msg as MessageWithPeer).peerAddr || ''
  }

  async function loadMessages() {
    if (isLoading) return
    isLoading = true

    try {
      if (selectedPeer === 'all') {
        // Load messages from all peers
        const allMessages: peer.Event[] = []
        for (const p of peers) {
          const peerMessages = await GetPeerMessageHistory(infoHash, p.Addr, 100)
          if (peerMessages) {
            // Add peer address to each message for filtering
            peerMessages.forEach(msg => {
              (msg as any).peerAddr = p.Addr
            })
            allMessages.push(...peerMessages)
          }
        }
        // Sort by timestamp (newest first)
        messages = allMessages.sort((a, b) =>
          new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
        )
      } else {
        const peerMessages = await GetPeerMessageHistory(infoHash, selectedPeer, 100)
        if (peerMessages) {
          peerMessages.forEach(msg => {
            (msg as any).peerAddr = selectedPeer
          })
          messages = peerMessages.reverse() // Newest first
        } else {
          messages = []
        }
      }
    } catch (error) {
      console.error('Failed to load message history:', error)
      messages = []
    } finally {
      isLoading = false
    }
  }

  function formatTimestamp(timestamp: any): string {
    const date = new Date(timestamp)
    return date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3
    })
  }

  function formatDetails(msg: peer.Event): string {
    const parts: string[] = []
    if (msg.pieceIndex !== undefined && msg.pieceIndex !== null) {
      parts.push(`Piece: ${msg.pieceIndex}`)
    }
    if (msg.blockOffset !== undefined && msg.blockOffset !== null) {
      parts.push(`Offset: ${msg.blockOffset}`)
    }
    if (msg.payloadSize > 0) {
      parts.push(`${msg.payloadSize}B`)
    }
    return parts.join(', ') || '-'
  }

  onMount(() => {
    loadMessages()
    // Poll every 2 seconds
    pollInterval = window.setInterval(loadMessages, 2000)
  })

  onDestroy(() => {
    if (pollInterval !== null) {
      clearInterval(pollInterval)
    }
  })

  $: if (selectedPeer) {
    loadMessages()
  }
</script>

<div class="message-history">
  <div class="controls">
    <label for="peer-filter">Filter by peer:</label>
    <select id="peer-filter" bind:value={selectedPeer}>
      <option value="all">All Peers ({peers.length})</option>
      {#each peers as peer}
        <option value={peer.Addr}>{peer.Addr}</option>
      {/each}
    </select>
  </div>

  {#if isLoading && messages.length === 0}
    <div class="empty-state">Loading message history...</div>
  {:else if messages.length === 0}
    <div class="empty-state">No messages yet</div>
  {:else}
    <div class="terminal">
      <div class="terminal-header">
        <span class="timestamp">TIME</span>
        {#if selectedPeer === 'all'}
          <span class="peer-addr">PEER</span>
        {/if}
        <span class="direction"></span>
        <span class="message-type">TYPE</span>
        <span class="details">DETAILS</span>
      </div>
      <div class="terminal-body">
        {#each filteredMessages as msg}
          <div class="log-line">
            <span class="timestamp">{formatTimestamp(msg.timestamp)}</span>
            {#if selectedPeer === 'all'}
              <span class="peer-addr">{getPeerAddr(msg)}</span>
            {/if}
            <span class="direction {msg.direction === 'received' ? 'received' : 'sent'}">
              {msg.direction === 'received' ? '↓' : '↑'}
            </span>
            <span class="message-type">{msg.messageType}</span>
            <span class="details">{formatDetails(msg)}</span>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .message-history {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
  }

  .controls label {
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
    font-weight: var(--font-weight-medium);
  }

  .controls select {
    padding: var(--spacing-2) var(--spacing-3);
    font-size: var(--font-size-sm);
    background-color: var(--color-bg-secondary);
    color: var(--color-text-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-family: var(--font-family-mono);
  }

  .controls select:hover {
    border-color: var(--color-border-secondary);
  }

  .controls select:focus {
    outline: none;
    border-color: var(--color-accent);
  }

  .empty-state {
    text-align: center;
    padding: var(--spacing-10) var(--spacing-5);
    color: var(--color-text-disabled);
    font-size: var(--font-size-base);
  }

  .terminal {
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-lg);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--color-text-primary);
    max-height: 500px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .terminal-header {
    display: flex;
    gap: var(--spacing-3);
    padding: var(--spacing-2) var(--spacing-4);
    border-bottom: 1px solid var(--color-border-secondary);
    color: var(--color-text-disabled);
    font-size: var(--font-size-xs);
    text-transform: uppercase;
    letter-spacing: var(--letter-spacing-wider);
    font-weight: var(--font-weight-semibold);
    flex-shrink: 0;
  }

  .terminal-body {
    padding: var(--spacing-2) var(--spacing-4) var(--spacing-4);
    overflow-y: auto;
    overflow-x: auto;
  }

  .log-line {
    display: flex;
    gap: var(--spacing-3);
    white-space: nowrap;
    padding: var(--spacing-1) 0;
    transition: background-color var(--transition-fast);
  }

  .log-line:hover {
    background-color: var(--color-bg-hover);
  }

  .terminal-header .timestamp,
  .log-line .timestamp {
    width: 100px;
    flex-shrink: 0;
  }

  .terminal-header .peer-addr,
  .log-line .peer-addr {
    width: 150px;
    flex-shrink: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .terminal-header .direction,
  .log-line .direction {
    width: 20px;
    flex-shrink: 0;
    text-align: center;
  }

  .terminal-header .message-type,
  .log-line .message-type {
    width: 100px;
    flex-shrink: 0;
    font-weight: var(--font-weight-medium);
  }

  .terminal-header .details,
  .log-line .details {
    flex-grow: 1;
    color: var(--color-text-disabled);
  }

  .log-line .timestamp {
    color: var(--color-text-secondary);
  }

  .log-line .peer-addr {
    color: var(--color-text-secondary);
  }

  .log-line .direction.received {
    color: var(--color-success);
  }

  .log-line .direction.sent {
    color: var(--color-error);
  }
</style>
