<script lang="ts">
    let { colour, size = 12 }: { colour: string | null | undefined; size?: number } = $props();
</script>

{#if colour}
    <span
        class="inline-block shrink-0 rounded-full ring-1 ring-zinc-900/15 dark:ring-white/20"
        style="background-color: {colour}; width: {size}px; height: {size}px"
        aria-hidden="true"
    ></span>
{/if}
