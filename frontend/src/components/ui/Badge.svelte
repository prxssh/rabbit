<script lang="ts">
    export let variant: 'success' | 'error' | 'info' = 'info'
    export let text: string
</script>

<span
    class="badge"
    class:success={variant === 'success'}
    class:error={variant === 'error'}
    class:info={variant === 'info'}
>
    {text}
</span>

<style>
    .badge {
        font-size: var(--font-size-xs);
        padding: 2px var(--spacing-2);
        border-radius: var(--radius-sm);
        text-transform: uppercase;
        letter-spacing: var(--letter-spacing-wide);
        white-space: nowrap;
    }

    .badge.error {
        background-color: var(--color-error-bg);
        color: var(--color-error);
        border: 1px solid var(--color-error-border);
    }

    .badge.success {
        background-color: var(--color-success-bg);
        color: var(--color-success);
        border: 1px solid var(--color-success-border);
    }

    .badge.info {
        background-color: var(--color-bg-tertiary);
        color: var(--color-text-tertiary);
        border: 1px solid var(--color-border-tertiary);
    }
</style>
