<script lang="ts">
  import { onMount, onDestroy } from 'svelte'

  export let pieceStates: number[] = []
  export let totalPieces: number = 0

  $: completedCount = pieceStates.filter(s => s === 2).length
  $: inProgressCount = pieceStates.filter(s => s === 1).length
  $: notStartedCount = totalPieces - completedCount - inProgressCount
  $: completionPercentage = totalPieces > 0 ? ((completedCount / totalPieces) * 100).toFixed(1) : 0

  let canvas: HTMLCanvasElement
  let container: HTMLDivElement
  let ctx: CanvasRenderingContext2D
  let animationFrameId: number
  let containerWidth = 800
  let zoomLevel = 1

  function getOptimalCellSize(pieces: number): number {
    if (pieces <= 100) return 12
    if (pieces <= 500) return 8
    if (pieces <= 2000) return 5
    return 3
  }

  $: baseCellSize = getOptimalCellSize(totalPieces)
  $: cellSize = Math.max(2, Math.round(baseCellSize * zoomLevel))
  $: gap = cellSize >= 8 ? 2 : 1
  $: availableWidth = containerWidth - 32
  $: columns = Math.max(1, Math.floor(availableWidth / (cellSize + gap)))
  $: rows = Math.ceil(totalPieces / columns)
  $: canvasWidth = columns * (cellSize + gap) - gap
  $: canvasHeight = rows * (cellSize + gap) - gap

  function handleZoomIn() {
    zoomLevel = Math.min(zoomLevel + 0.25, 3)
  }

  function handleZoomOut() {
    zoomLevel = Math.max(zoomLevel - 0.25, 0.5)
  }

  function handleZoomReset() {
    zoomLevel = 1
  }

  function updateContainerWidth() {
    if (container) {
      containerWidth = container.clientWidth
    }
  }

  $: if (container) {
    updateContainerWidth()
  }

  $: if (canvas) {
    ctx = canvas.getContext('2d')!
  }

  $: if (canvas && pieceStates && totalPieces) {
    startAnimation()
  }

  let pulsePhase = 0

  function startAnimation() {
    if (animationFrameId) {
      cancelAnimationFrame(animationFrameId)
    }
    animate()
  }

  function animate() {
    pulsePhase += 0.04

    drawHeatmap()
    animationFrameId = requestAnimationFrame(animate)
  }

  function drawHeatmap() {
    if (!ctx || !canvas || totalPieces === 0) return

    const style = getComputedStyle(canvas)
    const completedColor = style.getPropertyValue('--color-success') || '#88ff88'
    const inProgressColor = style.getPropertyValue('--color-accent') || '#aaaaaa'
    const notStartedColor = style.getPropertyValue('--color-bg-tertiary') || '#1a1a1a'
    const borderColor = style.getPropertyValue('--color-border-tertiary') || '#333333'

    if (canvas.width !== canvasWidth || canvas.height !== canvasHeight) {
      canvas.width = canvasWidth
      canvas.height = canvasHeight
    }

    ctx.clearRect(0, 0, canvasWidth, canvasHeight)

    for (let i = 0; i < totalPieces; i++) {
      const col = i % columns
      const row = Math.floor(i / columns)
      const x = col * (cellSize + gap)
      const y = row * (cellSize + gap)
      const state = pieceStates[i] || 0

      if (state === 2) {
        // Completed - solid green
        ctx.fillStyle = completedColor
        ctx.fillRect(x, y, cellSize, cellSize)
      } else if (state === 1) {
        // In progress - pulsing cyan with higher minimum opacity
        const pulse = 0.6 + Math.sin(pulsePhase) * 0.4
        ctx.fillStyle = inProgressColor
        ctx.globalAlpha = pulse
        ctx.fillRect(x, y, cellSize, cellSize)
        ctx.globalAlpha = 1
      } else {
        // Not started - dark with subtle border
        ctx.fillStyle = notStartedColor
        ctx.fillRect(x, y, cellSize, cellSize)

        // Add subtle border for better visibility
        if (cellSize >= 4) {
          ctx.strokeStyle = borderColor
          ctx.lineWidth = 1
          ctx.strokeRect(x + 0.5, y + 0.5, cellSize - 1, cellSize - 1)
        }
      }
    }
  }

  function handleResize() {
    updateContainerWidth()
  }

  onMount(() => {
    updateContainerWidth()
    window.addEventListener('resize', handleResize)
  })

  onDestroy(() => {
    if (animationFrameId) {
      cancelAnimationFrame(animationFrameId)
    }
    window.removeEventListener('resize', handleResize)
  })
</script>

<div class="heatmap-wrapper">
  <div class="stats-header">
    <div class="progress-stats">
      <div class="main-percentage">{completionPercentage}%</div>
      <div class="piece-breakdown">
        <div class="stat-item">
          <div class="stat-dot complete"></div>
          <span class="stat-value">{completedCount}</span>
          <span class="stat-label">completed</span>
        </div>
        <div class="stat-divider">/</div>
        <div class="stat-item">
          <div class="stat-dot progress"></div>
          <span class="stat-value">{inProgressCount}</span>
          <span class="stat-label">active</span>
        </div>
        <div class="stat-divider">/</div>
        <div class="stat-item">
          <div class="stat-dot pending"></div>
          <span class="stat-value">{notStartedCount}</span>
          <span class="stat-label">pending</span>
        </div>
      </div>
    </div>

    <div class="zoom-controls">
      <button class="zoom-btn" on:click={handleZoomOut} title="Zoom out" disabled={zoomLevel <= 0.5}>−</button>
      <span class="zoom-text">{Math.round(zoomLevel * 100)}%</span>
      <button class="zoom-btn" on:click={handleZoomIn} title="Zoom in" disabled={zoomLevel >= 3}>+</button>
      <button class="zoom-btn reset" on:click={handleZoomReset} title="Reset zoom">⟲</button>
    </div>
  </div>

  <div class="grid-container" bind:this={container}>
    <canvas bind:this={canvas} class="heatmap-canvas"></canvas>
  </div>
</div>

<style>
  .heatmap-wrapper {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    width: 100%;
  }

  .stats-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    padding: var(--spacing-3) var(--spacing-4);
    gap: var(--spacing-4);
  }

  .progress-stats {
    display: flex;
    align-items: center;
    gap: var(--spacing-5);
    flex: 1;
  }

  .main-percentage {
    font-size: 32px;
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    font-family: var(--font-family-mono);
    line-height: 1;
    letter-spacing: var(--letter-spacing-tight);
  }

  .piece-breakdown {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
  }

  .stat-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
  }

  .stat-dot {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .stat-dot.complete {
    background-color: var(--color-success);
  }

  .stat-dot.progress {
    background-color: var(--color-accent);
  }

  .stat-dot.pending {
    background-color: var(--color-bg-tertiary);
    border: 1px solid var(--color-border-tertiary);
  }

  .stat-value {
    font-size: var(--font-size-md);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
    font-family: var(--font-family-mono);
    min-width: 20px;
    text-align: right;
  }

  .stat-label {
    font-size: var(--font-size-xs);
    color: var(--color-text-muted);
    text-transform: lowercase;
  }

  .stat-divider {
    color: var(--color-text-disabled);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-normal);
    padding: 0 var(--spacing-1);
  }

  .zoom-controls {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    padding: var(--spacing-1);
  }

  .zoom-btn {
    background: none;
    border: none;
    color: var(--color-text-tertiary);
    font-size: var(--font-size-base);
    cursor: pointer;
    padding: var(--spacing-1) var(--spacing-2);
    border-radius: var(--radius-sm);
    transition: all var(--transition-fast);
    min-width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .zoom-btn:hover:not(:disabled) {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .zoom-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .zoom-btn.reset {
    font-size: var(--font-size-lg);
    margin-left: var(--spacing-1);
    border-left: 1px solid var(--color-border-primary);
    padding-left: var(--spacing-2);
  }

  .zoom-text {
    font-size: var(--font-size-xs);
    color: var(--color-text-muted);
    font-family: var(--font-family-mono);
    min-width: 40px;
    text-align: center;
  }

  .grid-container {
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    padding: var(--spacing-4);
    overflow: auto;
    max-height: 240px;
  }

  .heatmap-canvas {
    display: block;
    image-rendering: pixelated;
  }
</style>
