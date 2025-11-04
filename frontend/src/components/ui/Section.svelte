<script lang="ts">
    export let title: string
</script>

<div class="section">
    <div class="section-title">{title}</div>
    <slot />
</div>

<style>
    .section {
        display: flex;
        flex-direction: column;
        gap: var(--spacing-4);
    }

    .section-title {
        font-size: var(--font-size-lg);
        color: var(--color-text-primary);
        font-weight: var(--font-weight-semibold);
        text-transform: uppercase;
        letter-spacing: var(--letter-spacing-wide);
    }
</style>
