<script lang="ts">
    export let variant: 'success' | 'error' | 'info' | 'primary' | 'default' = 'info'
    export let text: string
</script>

<span
    class="badge"
    class:success={variant === 'success'}
    class:error={variant === 'error'}
    class:info={variant === 'info'}
    class:primary={variant === 'primary'}
    class:default={variant === 'default'}
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
        background-color: rgba(59, 130, 246, 0.1);
        color: rgb(59, 130, 246);
        border: 1px solid rgba(59, 130, 246, 0.3);
    }

    .badge.primary {
        background-color: rgba(139, 92, 246, 0.1);
        color: rgb(139, 92, 246);
        border: 1px solid rgba(139, 92, 246, 0.3);
    }

    .badge.default {
        background-color: var(--color-bg-tertiary);
        color: var(--color-text-tertiary);
        border: 1px solid var(--color-border-tertiary);
    }
</style>
