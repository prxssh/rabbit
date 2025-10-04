<script lang="ts">
  export let status: string = 'Ready'
  export let isError: boolean = false
  export let isSuccess: boolean = false
</script>

<div class="statusbar">
  <span class="status-text" class:error={isError} class:success={isSuccess}>
    {status}
  </span>
</div>

<style>
  .statusbar {
    height: var(--size-statusbar);
    border-top: 1px solid var(--color-border-primary);
    display: flex;
    align-items: center;
    padding: 0 var(--spacing-6);
    background-color: var(--color-bg-primary);
  }

  .status-text {
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }

  .status-text.error {
    color: var(--color-error);
  }

  .status-text.success {
    color: var(--color-success);
  }
</style>
