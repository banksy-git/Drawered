<script lang="ts">
    import { goto } from '$app/navigation';
    import { page } from '$app/state';
    import { api, errorMessage, qs } from '$lib/api';
    import { can } from '$lib/session.svelte';
    import { toast, toastError } from '$lib/toast.svelte';
    import { qty } from '$lib/format';
    import { invalidateLocations, loadLocations, locationCache } from '$lib/locations.svelte';
    import type { Location, LocationStockItem, Page } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import Swatch from '$lib/components/Swatch.svelte';
    import Thumb from '$lib/components/Thumb.svelte';
    import Pager from '$lib/components/Pager.svelte';
    import EventList from '$lib/components/EventList.svelte';
    import LocationForm from '$lib/components/LocationForm.svelte';

    const limit = 50;

    let loc = $state<Location | null>(null);
    let error = $state('');
    let stock = $state<Page<LocationStockItem> | null>(null);
    let descendants = $state(false);
    let offset = $state(0);
    let editOpen = $state(false);
    let childOpen = $state(false);
    let historyKey = $state(0);

    let id = $derived(Number(page.params.id));

    function load() {
        error = '';
        api.get<Location>(`/locations/${id}`)
            .then((l) => (loc = l))
            .catch((e) => (error = errorMessage(e)));
        loadLocations();
    }

    $effect(() => {
        id;
        offset = 0;
        load();
    });

    $effect(() => {
        if (!can('parts:read')) return;
        api.get<Page<LocationStockItem>>(`/locations/${id}/stock` + qs({ descendants, limit, offset }))
            .then((s) => (stock = s))
            .catch(toastError);
    });

    let childLocations = $derived(locationCache.items.filter((l) => l.parent_id === id));

    async function remove() {
        if (!loc || !confirm(`Delete ${loc.path}?`)) return;
        try {
            await api.del(`/locations/${loc.id}`);
            invalidateLocations();
            toast(`Deleted ${loc.name}`);
            goto(loc.parent_id ? `/locations/${loc.parent_id}` : '/locations');
        } catch (e) {
            toastError(e);
        }
    }
</script>

<svelte:head><title>{loc?.name ?? 'Location'} - Drawered</title></svelte:head>

{#if error}
    <div class="card p-6 text-center">
        <p class="font-medium">{error}</p>
        <a class="link text-sm" href="/locations">Back to locations</a>
    </div>
{:else if loc}
    <nav class="muted mb-2 flex flex-wrap items-center gap-1 text-sm" aria-label="Breadcrumb">
        <a class="hover:underline" href="/locations">Locations</a>
        {#each loc.ancestors as a (a.id)}
            <Icon name="chevronRight" size={14} />
            <a class="hover:underline" href="/locations/{a.id}">{a.name}</a>
        {/each}
    </nav>

    <header class="mb-4 flex flex-wrap items-start gap-3">
        <div class="min-w-0 flex-1">
            <h1 class="flex items-center gap-2 text-xl font-semibold">
                <Swatch colour={loc.effective_colour} size={16} />
                {loc.name}
                {#if loc.structural}<span class="badge">structural</span>{/if}
            </h1>
            {#if loc.description}<p class="muted mt-1 text-sm whitespace-pre-line">{loc.description}</p>{/if}
        </div>
        <div class="flex flex-wrap gap-2">
            {#if can('locations:create')}
                <button class="btn" onclick={() => (childOpen = true)}><Icon name="plus" size={16} /> Add inside</button>
            {/if}
            {#if can('locations:edit')}
                <button class="btn" onclick={() => (editOpen = true)}><Icon name="edit" size={16} /> Edit</button>
            {/if}
            {#if can('locations:delete')}
                <button class="btn" onclick={remove} aria-label="Delete location"><Icon name="trash" size={16} /></button>
            {/if}
        </div>
    </header>

    <div class="grid gap-4 lg:grid-cols-[1fr_18rem]">
        <section class="card">
            <div class="flex flex-wrap items-center gap-3 border-b border-zinc-100 px-4 py-3 dark:border-zinc-800">
                <h2 class="mr-auto font-semibold">Stock</h2>
                {#if loc.child_count}
                    <label class="flex items-center gap-2 text-sm">
                        <input type="checkbox" bind:checked={descendants} onchange={() => (offset = 0)} />
                        Include locations inside
                    </label>
                {/if}
            </div>
            {#if loc.structural && !descendants}
                <p class="muted p-4 text-sm">Structural locations hold other locations, not parts.</p>
            {:else if stock && stock.items.length}
                <ul class="divide-y divide-zinc-100 dark:divide-zinc-800">
                    {#each stock.items as s (s.part.id + '-' + s.location_id)}
                        <li>
                            <a href="/parts/{s.part.id}?tab=stock" class="flex items-center gap-3 px-4 py-2 hover:bg-zinc-50 dark:hover:bg-zinc-800/50">
                                <Thumb fileId={s.thumbnail_file_id} size={40} />
                                <div class="min-w-0 flex-1">
                                    <p class="truncate text-sm font-medium">{s.part.name}</p>
                                    <p class="muted truncate text-xs">
                                        {s.mpn}{descendants && s.location_id !== loc.id ? `${s.mpn ? ' | ' : ''}${s.path}` : ''}
                                        {s.note ? ` | ${s.note}` : ''}
                                    </p>
                                </div>
                                <span class="text-sm font-semibold tabular-nums">{qty(s.quantity, s.uom)}</span>
                                {#if s.low}<span class="badge bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">low</span>{/if}
                            </a>
                        </li>
                    {/each}
                </ul>
                <div class="px-4"><Pager total={stock.total} {limit} bind:offset /></div>
            {:else if stock}
                <p class="muted p-4 text-sm">Nothing stored here.</p>
            {/if}
        </section>

        <section class="card p-4">
            <h2 class="mb-2 font-semibold">Inside</h2>
            {#if childLocations.length}
                <ul class="space-y-1">
                    {#each childLocations as c (c.id)}
                        <li>
                            <a class="flex items-center gap-2 text-sm hover:underline" href="/locations/{c.id}">
                                <Swatch colour={c.effective_colour} />{c.name}
                                <span class="muted ml-auto text-xs">{c.total_part_count}</span>
                            </a>
                        </li>
                    {/each}
                </ul>
            {:else}
                <p class="muted text-sm">No locations inside.</p>
            {/if}
        </section>
    </div>

    {#if can('events:read')}
        <section class="card mt-4 p-4">
            <h2 class="mb-2 font-semibold">History</h2>
            {#key historyKey}
                <EventList endpoint="/locations/{loc.id}/events" linkSubjects />
            {/key}
        </section>
    {/if}

    <LocationForm
        bind:open={editOpen}
        location={loc}
        onsaved={(l) => {
            loc = l;
            historyKey++;
            toast('Saved');
        }}
    />
    <LocationForm bind:open={childOpen} parentId={loc.id} onsaved={(l) => goto(`/locations/${l.id}`)} />
{:else}
    <p class="muted text-sm">Loading...</p>
{/if}
