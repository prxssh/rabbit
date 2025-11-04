<script lang="ts">
    export let variant: 'primary' | 'secondary' | 'danger' | 'ghost' = 'secondary'
    export let size: 'sm' | 'md' | 'lg' = 'md'
    export let disabled = false
    export let type: 'button' | 'submit' = 'button'
</script>

<button {type} {disabled} class="btn btn-{variant} btn-{size}" on:click>
    <slot />
</button>

<style>
    .btn {
        font-family: var(--font-family-base);
        font-weight: var(--font-weight-normal);
        border-radius: var(--radius-sm);
        cursor: pointer;
        transition: all var(--transition-base);
        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: var(--spacing-2);
        border: 1px solid transparent;
        white-space: nowrap;
    }

    .btn:disabled {
        opacity: 0.5;
        cursor: not-allowed;
    }

    /* Sizes */
    .btn-sm {
        padding: var(--spacing-1) var(--spacing-3);
        font-size: var(--font-size-xs);
    }

    .btn-md {
        padding: var(--spacing-2) var(--spacing-4);
        font-size: var(--font-size-sm);
    }

    .btn-lg {
        padding: var(--spacing-3) var(--spacing-5);
        font-size: var(--font-size-base);
    }

    /* Variants */
    .btn-primary {
        background: var(--color-primary);
        color: white;
        border-color: var(--color-primary);
    }

    .btn-primary:hover:not(:disabled) {
        background: var(--color-primary-dark);
        border-color: var(--color-primary-dark);
    }

    .btn-secondary {
        background: var(--color-bg-tertiary);
        color: var(--color-text-primary);
        border-color: var(--color-border-tertiary);
    }

    .btn-secondary:hover:not(:disabled) {
        background: var(--color-bg-hover);
        border-color: var(--color-border-hover);
    }

    .btn-danger {
        background: var(--color-error-bg);
        color: var(--color-error);
        border-color: var(--color-error-border);
    }

    .btn-danger:hover:not(:disabled) {
        background: var(--color-danger-bg);
        border-color: var(--color-danger-border);
        color: var(--color-danger);
    }

    .btn-ghost {
        background: transparent;
        color: var(--color-text-secondary);
        border-color: var(--color-border-tertiary);
    }

    .btn-ghost:hover:not(:disabled) {
        background: var(--color-bg-hover);
        border-color: var(--color-border-hover);
        color: var(--color-text-primary);
    }
</style>
