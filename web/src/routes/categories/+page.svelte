<script lang="ts">
    import { goto } from '$app/navigation';
    import { onMount } from 'svelte';
    import { categoryCache, loadCategories } from '$lib/categories.svelte';
    import { can } from '$lib/session.svelte';
    import { pathMatches } from '$lib/format';
    import type { Category } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import CategoryIcon from '$lib/components/CategoryIcon.svelte';
    import CategoryForm from '$lib/components/CategoryForm.svelte';

    const KEY = 'drawered-category-expanded';

    let expanded = $state<Record<number, boolean>>({});
    let filter = $state('');
    let formOpen = $state(false);
    let error = $state('');

    onMount(() => {
        try {
            expanded = JSON.parse(localStorage.getItem(KEY) ?? '{}');
        } catch {
            expanded = {};
        }
        loadCategories(true).catch((e) => (error = String(e)));
    });

    $effect(() => {
        const snapshot = JSON.stringify(expanded);
        try {
            localStorage.setItem(KEY, snapshot);
        } catch {
            // Storage unavailable; expansion state lasts for this page only.
        }
    });

    let children = $derived.by(() => {
        const m = new Map<number | null, Category[]>();
        for (const c of categoryCache.items) {
            if (!m.has(c.parent_id)) m.set(c.parent_id, []);
            m.get(c.parent_id)!.push(c);
        }
        return m;
    });

    let matches = $derived(filter.trim() ? categoryCache.items.filter((c) => pathMatches(c.path, filter)) : []);

    function setAll(open: boolean) {
        const next: Record<number, boolean> = {};
        if (open) for (const c of categoryCache.items) if (c.child_count) next[c.id] = true;
        expanded = next;
    }
</script>

<svelte:head><title>Categories - Drawered</title></svelte:head>

<div class="mb-4 flex flex-wrap items-center gap-2">
    <h1 class="mr-auto text-xl font-semibold">Categories</h1>
    <input class="input w-full sm:w-64" placeholder="Find a category" bind:value={filter} aria-label="Find a category" />
    {#if can('categories:manage')}
        <button class="btn btn-primary" onclick={() => (formOpen = true)}><Icon name="plus" size={16} /> New category</button>
    {/if}
</div>

{#snippet node(c: Category, depth: number)}
    <li>
        <div class="flex items-center gap-2 py-1.5 pr-3 hover:bg-zinc-50 dark:hover:bg-zinc-800/50" style="padding-left: {depth * 1.25 + 0.5}rem">
            {#if c.child_count}
                <button
                    class="btn btn-ghost btn-sm h-6 min-h-6 w-6 p-0"
                    onclick={() => (expanded[c.id] = !expanded[c.id])}
                    aria-expanded={!!expanded[c.id]}
                    aria-label="{expanded[c.id] ? 'Collapse' : 'Expand'} {c.name}"
                >
                    <Icon name={expanded[c.id] ? 'chevronDown' : 'chevronRight'} size={14} />
                </button>
            {:else}
                <span class="w-6"></span>
            {/if}
            <CategoryIcon icon={c.effective_icon} size={18} class="text-accent-600 dark:text-indigo-400" />
            <a class="truncate text-sm font-medium hover:underline" href="/categories/{c.id}">{c.name}</a>
            {#if c.structural}<span class="badge">structural</span>{/if}
            <span class="muted ml-auto shrink-0 text-xs tabular-nums" title="Parts here / including inside">
                {c.part_count}{c.total_part_count !== c.part_count ? ` / ${c.total_part_count}` : ''}
            </span>
        </div>
        {#if expanded[c.id] && children.get(c.id)}
            <ul>
                {#each children.get(c.id)! as ch (ch.id)}{@render node(ch, depth + 1)}{/each}
            </ul>
        {/if}
    </li>
{/snippet}

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if !categoryCache.loaded}
    <p class="muted text-sm">Loading...</p>
{:else if filter.trim()}
    <ul class="card divide-y divide-zinc-100 dark:divide-zinc-800">
        {#each matches as c (c.id)}
            <li>
                <a class="flex items-center gap-2 px-3 py-2 text-sm hover:bg-zinc-50 dark:hover:bg-zinc-800/50" href="/categories/{c.id}">
                    <CategoryIcon icon={c.effective_icon} />{c.path}
                    {#if c.structural}<span class="badge">structural</span>{/if}
                </a>
            </li>
        {:else}
            <li class="muted p-4 text-sm">No matching categories.</li>
        {/each}
    </ul>
{:else if categoryCache.items.length === 0}
    <div class="card flex flex-col items-center gap-2 p-10 text-center">
        <Icon name="tag" size={32} class="text-zinc-400" />
        <p class="font-medium">No categories yet</p>
        <p class="muted text-sm">Categories classify parts, e.g. Electrical / Switches / Twoway.</p>
    </div>
{:else}
    <div class="mb-2 flex gap-2 text-xs">
        <button class="link cursor-pointer" onclick={() => setAll(true)}>Expand all</button>
        <button class="link cursor-pointer" onclick={() => setAll(false)}>Collapse all</button>
        <a class="link ml-auto" href="/?category=none">Uncategorised parts</a>
    </div>
    <ul class="card py-1">
        {#each children.get(null) ?? [] as c (c.id)}{@render node(c, 0)}{/each}
    </ul>
{/if}

<CategoryForm bind:open={formOpen} onsaved={(c) => goto(`/categories/${c.id}`)} />
