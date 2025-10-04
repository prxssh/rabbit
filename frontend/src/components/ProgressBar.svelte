<script lang="ts">
  export let progress: number = 0 // 0 to 100
  export let height: string = '4px'
  export let showPercentage: boolean = false

  $: clampedProgress = Math.min(Math.max(progress, 0), 100)
</script>

<div class="progress-container" style="height: {height}">
  <div class="progress-bar" style="width: {clampedProgress}%">
    {#if showPercentage && clampedProgress > 10}
      <span class="progress-text">{clampedProgress.toFixed(1)}%</span>
    {/if}
  </div>
</div>

<style>
  .progress-container {
    width: 100%;
    background-color: var(--color-bg-primary);
    border: 1px solid var(--color-border-primary);
    border-radius: var(--radius-sm);
    overflow: hidden;
    position: relative;
  }

  .progress-bar {
    height: 100%;
    background: linear-gradient(
      90deg,
      var(--color-border-hover) 0%,
      var(--color-text-muted) 100%
    );
    transition: width var(--transition-slow);
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding-right: var(--spacing-2);
  }

  .progress-text {
    font-size: 10px;
    color: var(--color-text-primary);
    font-weight: var(--font-weight-medium);
    text-shadow: 0 1px 2px rgba(0, 0, 0, 0.5);
  }
</style>
