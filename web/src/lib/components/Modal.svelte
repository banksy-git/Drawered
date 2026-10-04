<script lang="ts">
    import type { Snippet } from 'svelte';
    import Icon from './Icon.svelte';

    let {
        open = $bindable(false),
        title,
        wide = false,
        children,
        footer
    }: { open: boolean; title: string; wide?: boolean; children: Snippet; footer?: Snippet } = $props();

    let dialog: HTMLDialogElement | undefined = $state();

    $effect(() => {
        if (!dialog) return;
        if (open && !dialog.open) dialog.showModal();
        if (!open && dialog.open) dialog.close();
    });
</script>

<dialog
    bind:this={dialog}
    onclose={() => (open = false)}
    class="m-auto w-[calc(100%-2rem)] {wide
        ? 'max-w-2xl'
        : 'max-w-md'} rounded-xl border border-zinc-200 bg-white p-0 text-zinc-900 shadow-2xl backdrop:bg-zinc-950/50 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-100"
>
    {#if open}
        <div class="flex items-center justify-between border-b border-zinc-200 px-4 py-3 dark:border-zinc-800">
            <h2 class="text-base font-semibold">{title}</h2>
            <button class="btn btn-ghost btn-sm" onclick={() => (open = false)} aria-label="Close">
                <Icon name="x" size={16} />
            </button>
        </div>
        <div class="max-h-[70vh] overflow-y-auto px-4 py-4">
            {@render children()}
        </div>
        {#if footer}
            <div class="flex justify-end gap-2 border-t border-zinc-200 px-4 py-3 dark:border-zinc-800">
                {@render footer()}
            </div>
        {/if}
    {/if}
</dialog>
