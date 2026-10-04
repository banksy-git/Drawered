<script lang="ts">
    import type { Snippet } from 'svelte';
    import { page } from '$app/state';
    import { can } from '$lib/session.svelte';
    import { adminSections } from './sections';

    let { children }: { children: Snippet } = $props();
    let sections = $derived(adminSections.filter((s) => s.perms.some((p) => can(p))));
</script>

<div class="mb-4 flex gap-1 overflow-x-auto border-b border-zinc-200 dark:border-zinc-800" role="navigation" aria-label="Admin">
    {#each sections as s (s.href)}
        {@const on = page.url.pathname.startsWith(s.href)}
        <a
            href={s.href}
            class="-mb-px border-b-2 px-3 py-2 text-sm font-medium whitespace-nowrap {on
                ? 'border-accent-600 text-accent-700 dark:border-indigo-400 dark:text-indigo-300'
                : 'border-transparent text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'}"
            aria-current={on ? 'page' : undefined}>{s.label}</a
        >
    {/each}
</div>

{@render children()}
