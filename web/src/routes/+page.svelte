<script lang="ts">
    import { goto } from '$app/navigation';
    import { page } from '$app/state';
    import { api, errorMessage, qs } from '$lib/api';
    import { can } from '$lib/session.svelte';
    import { qty } from '$lib/format';
    import type { CatalogueEntry, Page, PartSummary } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import Thumb from '$lib/components/Thumb.svelte';
    import Pager from '$lib/components/Pager.svelte';
    import LocationPicker from '$lib/components/LocationPicker.svelte';
    import CategoryPicker from '$lib/components/CategoryPicker.svelte';
    import CategoryIcon from '$lib/components/CategoryIcon.svelte';
    import { categoryCache, loadCategories } from '$lib/categories.svelte';
    import Autocomplete from '$lib/components/Autocomplete.svelte';

    const limit = 50;

    let params = $derived(page.url.searchParams);
    let q = $derived(params.get('q') ?? '');
    let tags = $derived(params.getAll('tag'));
    let manufacturer = $derived(params.get('manufacturer'));
    let supplier = $derived(params.get('supplier'));
    let locationId = $derived(params.get('location') ? Number(params.get('location')) : null);
    let uncategorised = $derived(params.get('category') === 'none');
    let categoryId = $derived(params.get('category') && !uncategorised ? Number(params.get('category')) : null);
    let categoryName = $derived(categoryCache.items.find((c) => c.id === categoryId)?.path ?? '');
    let stock = $derived(params.get('stock') ?? '');
    let sort = $derived(params.get('sort') ?? '');
    let deleted = $derived(params.get('deleted') === 'only');
    let offset = $derived(Number(params.get('offset') ?? 0));

    let result = $state<Page<PartSummary> | null>(null);
    let error = $state('');
    let loading = $state(false);
    let showFilters = $state(false);
    let tagText = $state('');
    let manufacturers = $state<CatalogueEntry[]>([]);
    let suppliers = $state<CatalogueEntry[]>([]);

    let filterCount = $derived(
        tags.length + (categoryId || uncategorised ? 1 : 0) + (manufacturer ? 1 : 0) + (supplier ? 1 : 0) + (locationId ? 1 : 0) + (stock ? 1 : 0) + (deleted ? 1 : 0)
    );

    $effect(() => {
        const query = qs({
            q,
            tag: tags,
            manufacturer,
            supplier,
            location: locationId,
            category: uncategorised ? 'none' : categoryId,
            stock,
            sort,
            deleted: deleted ? 'only' : null,
            limit,
            offset
        });
        loading = true;
        error = '';
        let cancelled = false;
        api.get<Page<PartSummary>>('/parts' + query)
            .then((r) => {
                if (!cancelled) result = r;
            })
            .catch((e) => {
                if (!cancelled) error = errorMessage(e);
            })
            .finally(() => {
                if (!cancelled) loading = false;
            });
        return () => (cancelled = true);
    });

    $effect(() => {
        if (categoryId) loadCategories();
    });

    $effect(() => {
        if (showFilters && manufacturers.length === 0) {
            api.get<Page<CatalogueEntry>>('/manufacturers?limit=200').then((r) => (manufacturers = r.items));
            api.get<Page<CatalogueEntry>>('/suppliers?limit=200').then((r) => (suppliers = r.items));
        }
    });

    // update changes URL parameters, resetting paging unless offset is set.
    function update(changes: Record<string, string | string[] | number | null>) {
        const p = new URLSearchParams(page.url.search);
        if (!('offset' in changes)) p.delete('offset');
        for (const [k, v] of Object.entries(changes)) {
            p.delete(k);
            if (Array.isArray(v)) v.forEach((x) => p.append(k, x));
            else if (v !== null && v !== '' && v !== 0) p.set(k, String(v));
        }
        const s = p.toString();
        goto('/' + (s ? '?' + s : ''), { replaceState: true, keepFocus: true, noScroll: !('offset' in changes) });
    }

    function addTag(t: string) {
        t = t.trim().toLowerCase();
        if (t && !tags.includes(t)) update({ tag: [...tags, t] });
        tagText = '';
    }

    async function suggestTags(text: string): Promise<string[]> {
        const r = await api.get<Page<CatalogueEntry>>('/tags?limit=8&q=' + encodeURIComponent(text));
        return r.items.map((t) => t.name);
    }
</script>

<svelte:head><title>{q ? `${q} - ` : ''}Parts - Drawered</title></svelte:head>

<div class="mb-4 flex flex-wrap items-center gap-2">
    <h1 class="mr-auto text-xl font-semibold">
        {#if deleted}Deleted parts{:else if q}Results for "{q}"{:else if uncategorised}Uncategorised parts{:else if categoryName}{categoryName}{:else}Parts{/if}
    </h1>
    <button class="btn" onclick={() => (showFilters = !showFilters)} aria-expanded={showFilters}>
        <Icon name="layers" size={16} /> Filters{filterCount ? ` (${filterCount})` : ''}
    </button>
    <select class="input w-auto" value={sort} onchange={(e) => update({ sort: e.currentTarget.value })} aria-label="Sort">
        <option value="">{q ? 'Relevance' : 'Name'}</option>
        <option value="name">Name</option>
        <option value="updated">Recently updated</option>
        <option value="quantity">Quantity</option>
    </select>
    {#if can('parts:create')}
        <a class="btn btn-primary" href="/parts/new"><Icon name="plus" size={16} /> New part</a>
    {/if}
</div>

{#if showFilters}
    <section class="card mb-4 grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-3" aria-label="Filters">
        <div>
            <label class="label" for="f-tag">Tags</label>
            <div class="flex gap-2">
                <div class="flex-1">
                    <Autocomplete id="f-tag" bind:value={tagText} suggest={suggestTags} placeholder="Add a tag filter" />
                </div>
                <button class="btn" onclick={() => addTag(tagText)} disabled={!tagText.trim()}>Add</button>
            </div>
            {#if tags.length}
                <div class="mt-2 flex flex-wrap gap-1">
                    {#each tags as t (t)}
                        <button class="badge cursor-pointer" onclick={() => update({ tag: tags.filter((x) => x !== t) })}>
                            {t} <Icon name="x" size={12} />
                        </button>
                    {/each}
                </div>
            {/if}
        </div>
        <div>
            <label class="label" for="f-cat">Category (including inside)</label>
            <CategoryPicker id="f-cat" allowNone noneLabel="Any category" bind:value={() => categoryId, (v) => update({ category: v })} />
            <label class="mt-1.5 flex items-center gap-2 text-sm">
                <input type="checkbox" checked={uncategorised} onchange={(e) => update({ category: e.currentTarget.checked ? 'none' : null })} />
                Uncategorised only
            </label>
        </div>
        <div>
            <label class="label" for="f-loc">Location (including inside)</label>
            <LocationPicker
                id="f-loc"
                allowNone
                noneLabel="Any location"
                bind:value={() => locationId, (v) => update({ location: v })}
            />
        </div>
        <div>
            <label class="label" for="f-stock">Stock</label>
            <select id="f-stock" class="input" value={stock} onchange={(e) => update({ stock: e.currentTarget.value })}>
                <option value="">Any</option>
                <option value="in">In stock</option>
                <option value="out">Out of stock</option>
                <option value="low">Low stock</option>
                <option value="none">Not in any location</option>
            </select>
        </div>
        <div>
            <label class="label" for="f-mfr">Manufacturer</label>
            <select id="f-mfr" class="input" value={manufacturer ?? ''} onchange={(e) => update({ manufacturer: e.currentTarget.value })}>
                <option value="">Any</option>
                {#each manufacturers as m (m.id)}<option value={String(m.id)}>{m.name}</option>{/each}
            </select>
        </div>
        <div>
            <label class="label" for="f-sup">Supplier</label>
            <select id="f-sup" class="input" value={supplier ?? ''} onchange={(e) => update({ supplier: e.currentTarget.value })}>
                <option value="">Any</option>
                {#each suppliers as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
            </select>
        </div>
        <div class="flex flex-col justify-end gap-2">
            {#if can('parts:delete')}
                <label class="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={deleted} onchange={(e) => update({ deleted: e.currentTarget.checked ? 'only' : null })} />
                    Show deleted parts only
                </label>
            {/if}
            {#if filterCount}
                <button
                    class="btn btn-sm self-start"
                    onclick={() => update({ tag: [], category: null, manufacturer: null, supplier: null, location: null, stock: null, deleted: null })}
                >
                    Clear filters
                </button>
            {/if}
        </div>
    </section>
{/if}

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if result}
    {#if result.items.length === 0}
        <div class="card flex flex-col items-center gap-2 p-10 text-center">
            <Icon name="search" size={32} class="text-zinc-400" />
            <p class="font-medium">No parts found</p>
            <p class="muted text-sm">
                {#if q || filterCount}Try fewer words or clear some filters.{:else}Add your first part to get started.{/if}
            </p>
        </div>
    {:else}
        <ul class="card divide-y divide-zinc-100 dark:divide-zinc-800 {loading ? 'opacity-60' : ''}">
            {#each result.items as p (p.id)}
                <li>
                    <a href="/parts/{p.id}" class="flex items-center gap-3 px-3 py-2.5 hover:bg-zinc-50 dark:hover:bg-zinc-800/50">
                        <Thumb fileId={p.thumbnail_file_id} size={48} />
                        <div class="min-w-0 flex-1">
                            <p class="truncate font-medium">{p.name}</p>
                            {#if p.category}
                                <p class="muted flex items-center gap-1 truncate text-xs">
                                    <CategoryIcon icon={p.category.effective_icon} size={13} />{p.category.name}
                                </p>
                            {/if}
                            <p class="muted truncate text-xs">
                                {[p.mpn, p.manufacturer].filter(Boolean).join(' - ')}
                                {#if p.locations.length}
                                    <span class="hidden sm:inline">
                                        {p.mpn || p.manufacturer ? ' | ' : ''}{p.locations.slice(0, 2).join('; ')}{p.locations.length > 2
                                            ? ` +${p.locations.length - 2}`
                                            : ''}
                                    </span>
                                {/if}
                            </p>
                            {#if p.tags.length}
                                <div class="mt-1 hidden flex-wrap gap-1 sm:flex">
                                    {#each p.tags.slice(0, 5) as t (t)}<span class="badge">{t}</span>{/each}
                                </div>
                            {/if}
                        </div>
                        <div class="shrink-0 text-right">
                            <p class="font-semibold tabular-nums">{qty(p.total_quantity, p.uom)}</p>
                            {#if p.low}<span class="badge bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">low</span>{/if}
                            {#if p.deleted_at}<span class="badge bg-red-100 text-red-800 dark:bg-red-500/20 dark:text-red-300">deleted</span>{/if}
                        </div>
                    </a>
                </li>
            {/each}
        </ul>
        <Pager total={result.total} {limit} bind:offset={() => offset, (v) => update({ offset: v })} />
    {/if}
{:else}
    <p class="muted text-sm">Loading...</p>
{/if}
