<script lang="ts">
    export let label: string
    export let value: string
    export let mono: boolean = false
</script>

<div class="info-card">
    <div class="info-label">{label}</div>
    <div class="info-value" class:mono>{value}</div>
</div>

<style>
    .info-card {
        background-color: var(--color-bg-primary);
        border: 1px solid var(--color-border-primary);
        border-radius: var(--radius-base);
        padding: var(--spacing-4);
    }

    .info-label {
        font-size: var(--font-size-xs);
        color: var(--color-text-disabled);
        text-transform: uppercase;
        letter-spacing: var(--letter-spacing-wide);
        margin-bottom: var(--spacing-2);
    }

    .info-value {
        font-size: var(--font-size-base);
        color: var(--color-text-primary);
        word-break: break-all;
    }

    .info-value.mono {
        font-size: var(--font-size-sm);
        font-family: var(--font-family-mono);
        color: var(--color-text-muted);
    }
</style>
