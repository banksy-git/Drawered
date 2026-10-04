<script lang="ts">
    import { goto } from '$app/navigation';
    import { onMount } from 'svelte';
    import { loadLocations, locationCache } from '$lib/locations.svelte';
    import { can } from '$lib/session.svelte';
    import { pathMatches } from '$lib/format';
    import type { Location } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import Swatch from '$lib/components/Swatch.svelte';
    import LocationForm from '$lib/components/LocationForm.svelte';

    const KEY = 'drawered-expanded';

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
        loadLocations(true).catch((e) => (error = String(e)));
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
        const m = new Map<number | null, Location[]>();
        for (const l of locationCache.items) {
            const k = l.parent_id;
            if (!m.has(k)) m.set(k, []);
            m.get(k)!.push(l);
        }
        return m;
    });

    let matches = $derived(filter.trim() ? locationCache.items.filter((l) => pathMatches(l.path, filter)) : []);

    function setAll(open: boolean) {
        const next: Record<number, boolean> = {};
        if (open) for (const l of locationCache.items) if (l.child_count) next[l.id] = true;
        expanded = next;
    }
</script>

<svelte:head><title>Locations - Drawered</title></svelte:head>

<div class="mb-4 flex flex-wrap items-center gap-2">
    <h1 class="mr-auto text-xl font-semibold">Locations</h1>
    <input class="input w-full sm:w-64" placeholder="Find a location" bind:value={filter} aria-label="Find a location" />
    {#if can('locations:create')}
        <button class="btn btn-primary" onclick={() => (formOpen = true)}><Icon name="plus" size={16} /> New location</button>
    {/if}
</div>

{#snippet node(l: Location, depth: number)}
    <li>
        <div class="flex items-center gap-2 py-1.5 pr-3 hover:bg-zinc-50 dark:hover:bg-zinc-800/50" style="padding-left: {depth * 1.25 + 0.5}rem">
            {#if l.child_count}
                <button
                    class="btn btn-ghost btn-sm h-6 min-h-6 w-6 p-0"
                    onclick={() => (expanded[l.id] = !expanded[l.id])}
                    aria-expanded={!!expanded[l.id]}
                    aria-label="{expanded[l.id] ? 'Collapse' : 'Expand'} {l.name}"
                >
                    <Icon name={expanded[l.id] ? 'chevronDown' : 'chevronRight'} size={14} />
                </button>
            {:else}
                <span class="w-6"></span>
            {/if}
            <Swatch colour={l.effective_colour} />
            <a class="truncate text-sm font-medium hover:underline" href="/locations/{l.id}">{l.name}</a>
            {#if l.structural}<span class="badge">structural</span>{/if}
            <span class="muted ml-auto shrink-0 text-xs tabular-nums" title="Stock entries here / including inside">
                {l.part_count}{l.total_part_count !== l.part_count ? ` / ${l.total_part_count}` : ''}
            </span>
        </div>
        {#if expanded[l.id] && children.get(l.id)}
            <ul>
                {#each children.get(l.id)! as c (c.id)}{@render node(c, depth + 1)}{/each}
            </ul>
        {/if}
    </li>
{/snippet}

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if !locationCache.loaded}
    <p class="muted text-sm">Loading...</p>
{:else if filter.trim()}
    <ul class="card divide-y divide-zinc-100 dark:divide-zinc-800">
        {#each matches as l (l.id)}
            <li>
                <a class="flex items-center gap-2 px-3 py-2 text-sm hover:bg-zinc-50 dark:hover:bg-zinc-800/50" href="/locations/{l.id}">
                    <Swatch colour={l.effective_colour} />{l.path}
                    {#if l.structural}<span class="badge">structural</span>{/if}
                </a>
            </li>
        {:else}
            <li class="muted p-4 text-sm">No matching locations.</li>
        {/each}
    </ul>
{:else if locationCache.items.length === 0}
    <div class="card flex flex-col items-center gap-2 p-10 text-center">
        <Icon name="map" size={32} class="text-zinc-400" />
        <p class="font-medium">No locations yet</p>
        <p class="muted text-sm">Create a top-level location such as a room, then build the hierarchy inside it.</p>
    </div>
{:else}
    <div class="mb-2 flex gap-2 text-xs">
        <button class="link cursor-pointer" onclick={() => setAll(true)}>Expand all</button>
        <button class="link cursor-pointer" onclick={() => setAll(false)}>Collapse all</button>
    </div>
    <ul class="card py-1">
        {#each children.get(null) ?? [] as l (l.id)}{@render node(l, 0)}{/each}
    </ul>
{/if}

<LocationForm bind:open={formOpen} onsaved={(l) => goto(`/locations/${l.id}`)} />
