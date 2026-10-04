<script lang="ts" module>
    // Icon names are read from the bundled webfont's stylesheet on first use,
    // so the picker always matches the font that is actually shipped.
    let names: Promise<string[]> | null = null;

    function iconNames(): Promise<string[]> {
        names ??= import('@tabler/icons-webfont/dist/tabler-icons.min.css?raw').then((m) => {
            const out = new Set<string>();
            for (const match of m.default.matchAll(/\.ti-([a-z0-9-]+):before/g)) out.add(match[1]);
            return [...out].sort();
        });
        return names;
    }
</script>

<script lang="ts">
    import { onMount } from 'svelte';
    import CategoryIcon from './CategoryIcon.svelte';

    let { value = $bindable(null), inherited = null }: { value: string | null; inherited?: string | null } = $props();

    const LIMIT = 240;
    let all = $state<string[]>([]);
    let query = $state('');

    onMount(() => {
        iconNames().then((n) => (all = n));
    });

    // Exact and prefix matches first, then substring matches.
    let matches = $derived.by(() => {
        const q = query.trim().toLowerCase().replace(/^ti-/, '').replace(/\s+/g, '-');
        if (!q) return all.slice(0, LIMIT);
        const starts = all.filter((n) => n.startsWith(q));
        const contains = all.filter((n) => !n.startsWith(q) && n.includes(q));
        return [...starts, ...contains].slice(0, LIMIT);
    });
</script>

<div class="space-y-2">
    <div class="flex items-center gap-3">
        <span class="flex h-10 w-10 items-center justify-center rounded-md border border-zinc-300 dark:border-zinc-700">
            <CategoryIcon icon={value ?? inherited} size={22} />
        </span>
        <div class="min-w-0 flex-1 text-sm">
            {#if value}
                <code>{value}</code>
            {:else if inherited}
                <span class="muted">None - inherits <code>{inherited}</code></span>
            {:else}
                <span class="muted">None</span>
            {/if}
        </div>
        {#if value}<button type="button" class="btn btn-sm" onclick={() => (value = null)}>Clear</button>{/if}
    </div>
    <input class="input" placeholder="Search {all.length || ''} icons, e.g. bolt, cpu, screw" bind:value={query} aria-label="Search icons" />
    <div class="grid max-h-56 grid-cols-[repeat(auto-fill,minmax(2.5rem,1fr))] gap-1 overflow-y-auto rounded-md border border-zinc-200 p-1 dark:border-zinc-800" role="listbox" aria-label="Icons">
        {#each matches as n (n)}
            <button
                type="button"
                role="option"
                aria-selected={n === value}
                title={n}
                aria-label={n}
                class="flex aspect-square cursor-pointer items-center justify-center rounded hover:bg-zinc-100 dark:hover:bg-zinc-800 {n === value
                    ? 'bg-accent-100 ring-2 ring-accent-500 dark:bg-indigo-500/20'
                    : ''}"
                onclick={() => (value = n)}
            >
                <CategoryIcon icon={n} size={20} />
            </button>
        {:else}
            <p class="muted col-span-full p-2 text-sm">{all.length ? 'No icons match.' : 'Loading icons...'}</p>
        {/each}
    </div>
    {#if matches.length === LIMIT}<p class="muted text-xs">Showing the first {LIMIT}; refine the search to see more.</p>{/if}
</div>
