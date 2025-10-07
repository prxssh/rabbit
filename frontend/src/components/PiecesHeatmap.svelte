<script lang="ts">
  import { onMount, onDestroy } from 'svelte'

  // pieceStates: 0 = not started, 1 = in progress, 2 = completed
  export let pieceStates: number[] = []
  export let totalPieces: number = 0

  // Calculate piece statistics
  $: completedCount = pieceStates.filter(s => s === 2).length
  $: inProgressCount = pieceStates.filter(s => s === 1).length
  $: completionPercentage = totalPieces > 0 ? ((completedCount / totalPieces) * 100).toFixed(1) : 0

  let canvas: HTMLCanvasElement
  let container: HTMLDivElement
  let ctx: CanvasRenderingContext2D
  let animationFrameId: number

  // Zoom and pan state
  let zoomLevel = 1
  let panX = 0
  let panY = 0
  let isPanning = false
  let startPanX = 0
  let startPanY = 0

  // Container width tracking
  let containerWidth = 800

  // Calculate smart cell size based on piece count
  function getSmartCellSize(pieces: number): number {
    if (pieces <= 50) return 24
    if (pieces <= 200) return 16
    if (pieces <= 500) return 12
    if (pieces <= 2000) return 8
    return 6
  }

  // Calculate grid dimensions - fill width
  $: baseCellSize = getSmartCellSize(totalPieces)
  $: cellSize = baseCellSize * zoomLevel
  $: gap = cellSize > 4 ? 1 : 0
  $: availableWidth = containerWidth - 24 // subtract padding
  $: columns = Math.max(1, Math.floor(availableWidth / (cellSize + gap)))
  $: rows = Math.ceil(totalPieces / columns)
  $: canvasWidth = columns * (cellSize + gap) - gap
  $: canvasHeight = rows * (cellSize + gap) - gap

  // Update container width on resize
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

  let blinkOpacity = 1
  let blinkDirection = -1

  function startAnimation() {
    if (animationFrameId) {
      cancelAnimationFrame(animationFrameId)
    }
    animate()
  }

  function animate() {
    blinkOpacity += blinkDirection * 0.05
    if (blinkOpacity <= 0.3) {
      blinkOpacity = 0.3
      blinkDirection = 1
    } else if (blinkOpacity >= 1) {
      blinkOpacity = 1
      blinkDirection = -1
    }

    drawHeatmap()
    animationFrameId = requestAnimationFrame(animate)
  }

  function drawHeatmap() {
    if (!ctx || !canvas || totalPieces === 0) return

    const style = getComputedStyle(canvas)
    const completedColor = style.getPropertyValue('--color-success') || '#10b981'
    const notStartedColor = style.getPropertyValue('--color-bg-secondary') || '#374151'
    const inProgressColor = style.getPropertyValue('--color-accent') || '#3b82f6'

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
        // completed - solid green
        ctx.fillStyle = completedColor
      } else if (state === 1) {
        // in progress - distinct blinking color (amber/orange)
        const rgb = hexToRgb(inProgressColor)
        ctx.fillStyle = `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, ${blinkOpacity})`
      } else {
        // not started - gray
        ctx.fillStyle = notStartedColor
      }

      ctx.fillRect(x, y, cellSize, cellSize)
    }
  }

  function hexToRgb(hex: string): { r: number; g: number; b: number } {
    const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex)
    return result ? {
      r: parseInt(result[1], 16),
      g: parseInt(result[2], 16),
      b: parseInt(result[3], 16)
    } : { r: 16, g: 185, b: 129 }
  }

  function handleZoomIn() {
    zoomLevel = Math.min(zoomLevel * 1.5, 5)
  }

  function handleZoomOut() {
    zoomLevel = Math.max(zoomLevel / 1.5, 0.5)
  }

  function handleZoomReset() {
    zoomLevel = 1
    panX = 0
    panY = 0
    if (container) {
      container.scrollLeft = 0
      container.scrollTop = 0
    }
  }

  function handleMouseDown(e: MouseEvent) {
    if (zoomLevel > 1) {
      isPanning = true
      startPanX = e.clientX - panX
      startPanY = e.clientY - panY
    }
  }

  function handleMouseMove(e: MouseEvent) {
    if (isPanning) {
      panX = e.clientX - startPanX
      panY = e.clientY - startPanY
    }
  }

  function handleMouseUp() {
    isPanning = false
  }

  // Handle window resize
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

<div class="heatmap-container">
  <div class="heatmap-header">
    <div class="heatmap-stats">
      <span class="heatmap-title">Pieces ({completedCount} / {totalPieces})</span>
      <span class="heatmap-percentage">{completionPercentage}% complete</span>
      <span class="cell-size-indicator">{baseCellSize}px per piece</span>
    </div>
    <div class="controls">
      <div class="zoom-controls">
        <button class="zoom-btn" on:click={handleZoomOut} title="Zoom out">−</button>
        <span class="zoom-level">{Math.round(zoomLevel * 100)}%</span>
        <button class="zoom-btn" on:click={handleZoomIn} title="Zoom in">+</button>
        <button class="zoom-btn reset" on:click={handleZoomReset} title="Reset zoom">Reset</button>
      </div>
      <div class="legend">
        <div class="legend-item">
          <div class="legend-color completed"></div>
          <span>Complete</span>
        </div>
        <div class="legend-item">
          <div class="legend-color in-progress"></div>
          <span>In Progress</span>
        </div>
        <div class="legend-item">
          <div class="legend-color not-started"></div>
          <span>Not Started</span>
        </div>
      </div>
    </div>
  </div>
  <div
    class="heatmap-canvas-container"
    bind:this={container}
    class:panning={isPanning}
  >
    <canvas
      bind:this={canvas}
      class="heatmap-canvas"
      on:mousedown={handleMouseDown}
      on:mousemove={handleMouseMove}
      on:mouseup={handleMouseUp}
      on:mouseleave={handleMouseUp}
    ></canvas>
  </div>
</div>

<style>
  .heatmap-container {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    width: 100%;
    height: auto;
    min-height: 100%;
  }

  .heatmap-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    flex-wrap: wrap;
    gap: var(--spacing-3);
  }

  .heatmap-stats {
    display: flex;
    gap: var(--spacing-4);
    align-items: center;
    flex-wrap: wrap;
  }

  .heatmap-title {
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
    font-weight: var(--font-weight-medium);
  }

  .heatmap-percentage {
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }

  .cell-size-indicator {
    font-size: var(--font-size-xs);
    color: var(--color-text-disabled);
  }

  .controls {
    display: flex;
    gap: var(--spacing-4);
    align-items: center;
    flex-wrap: wrap;
  }

  .zoom-controls {
    display: flex;
    gap: var(--spacing-2);
    align-items: center;
    background-color: var(--color-bg-tertiary);
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
    transition: all var(--transition-base);
    min-width: 24px;
  }

  .zoom-btn:hover {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .zoom-btn.reset {
    font-size: var(--font-size-xs);
  }

  .zoom-level {
    font-size: var(--font-size-xs);
    color: var(--color-text-muted);
    min-width: 45px;
    text-align: center;
  }

  .legend {
    display: flex;
    gap: var(--spacing-4);
  }

  .legend-item {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
    color: var(--color-text-muted);
  }

  .legend-color {
    width: 12px;
    height: 12px;
    border-radius: 2px;
  }

  .legend-color.completed {
    background-color: var(--color-success);
  }

  .legend-color.in-progress {
    background-color: var(--color-accent, #3b82f6);
    animation: blink 1s ease-in-out infinite;
  }

  @keyframes blink {
    0%, 100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }

  .legend-color.not-started {
    background-color: var(--color-bg-secondary);
    border: 1px solid var(--color-border-primary);
  }

  .heatmap-canvas-container {
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-base);
    overflow: auto;
    min-height: 150px;
    max-height: 300px;
    padding: var(--spacing-3);
  }

  .heatmap-canvas-container.panning {
    cursor: grabbing;
  }

  .heatmap-canvas {
    image-rendering: pixelated;
    display: block;
  }
</style>
