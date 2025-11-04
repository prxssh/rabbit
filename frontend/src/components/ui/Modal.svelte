<script lang="ts">
  export let show = false
  export let title: string
  export let onClose: () => void
  export let maxWidth = '600px'

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (show && e.key === 'Escape') {
      onClose()
    }
  }

  function handleBackdropKeydown(e: KeyboardEvent) {
    if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
      onClose()
    }
  }
</script>

<svelte:window on:keydown={handleKeydown} />

{#if show}
  <div
    class="modal-overlay"
    on:click={handleBackdropClick}
    on:keydown={handleBackdropKeydown}
    tabindex="0"
    role="button"
    aria-label="Close modal"
  >
    <div class="modal-content" style="max-width: {maxWidth}" role="document">
      <div class="modal-header">
        <h2>{title}</h2>
        <button class="close-btn" on:click={onClose} aria-label="Close">&times;</button>
      </div>

      <div class="modal-body">
        <slot />
      </div>

      <div class="modal-footer">
        <slot name="footer" />
      </div>
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
    max-height: 85vh;
    display: flex;
    flex-direction: column;
    box-shadow: var(--shadow-lg);
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

  .modal-body {
    flex: 1;
    overflow-y: auto;
    padding: var(--spacing-5);
  }

  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--spacing-2);
    padding: var(--spacing-4) var(--spacing-5);
    border-top: 1px solid var(--color-border-primary);
  }
</style>
