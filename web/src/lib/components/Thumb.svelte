<script lang="ts">
    import Icon from './Icon.svelte';
    import { fileUrl } from '$lib/format';

    let { fileId, size = 48, alt = '' }: { fileId: number | null | undefined; size?: number; alt?: string } = $props();
</script>

<div
    class="flex shrink-0 items-center justify-center overflow-hidden rounded-md border border-zinc-200 bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-800"
    style="width: {size}px; height: {size}px"
>
    {#if fileId}
        <img src={fileUrl(fileId, 'thumb')} {alt} loading="lazy" class="h-full w-full object-cover" />
    {:else}
        <Icon name="box" size={Math.round(size / 2.4)} class="text-zinc-400" />
    {/if}
</div>
