<script lang="ts">
    import { dismiss, toasts } from '$lib/toast.svelte';
    import Icon from './Icon.svelte';
</script>

<div class="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-4 sm:items-end" aria-live="polite">
    {#each toasts as t (t.id)}
        <div
            class="pointer-events-auto flex w-full max-w-sm items-start gap-2 rounded-lg px-4 py-3 text-sm shadow-lg {t.kind === 'error'
                ? 'bg-red-600 text-white'
                : 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900'}"
            role={t.kind === 'error' ? 'alert' : 'status'}
        >
            <span class="flex-1">{t.text}</span>
            <button class="cursor-pointer opacity-70 hover:opacity-100" onclick={() => dismiss(t.id)} aria-label="Dismiss">
                <Icon name="x" size={16} />
            </button>
        </div>
    {/each}
</div>
