<script lang="ts">
    import { untrack } from 'svelte';
    import { api, errorMessage } from '$lib/api';
    import { session } from '$lib/session.svelte';
    import type { CatalogueEntry, Page, Part } from '$lib/types';
    import Autocomplete from './Autocomplete.svelte';
    import TagInput from './TagInput.svelte';
    import LocationPicker from './LocationPicker.svelte';
    import CategoryPicker from './CategoryPicker.svelte';
    import Markdown from './Markdown.svelte';
    import Icon from './Icon.svelte';

    let {
        part = null,
        onsaved,
        oncancel
    }: { part?: Part | null; onsaved: (p: Part) => void; oncancel: () => void } = $props();

    // Copy initial values; the form owns its state thereafter.
    const init = untrack(() => part);
    let name = $state(init?.name ?? '');
    let description = $state(init?.description ?? '');
    let tags = $state<string[]>(init?.tags ?? []);
    let categoryId = $state<number | null>(init?.category?.id ?? null);
    let uom = $state(init?.uom ?? 'pcs');
    let allowFractional = $state(init?.allow_fractional ?? false);
    let mpn = $state(init?.mpn ?? '');
    let manufacturer = $state(init?.manufacturer?.name ?? '');
    let supplier = $state(init?.supplier?.name ?? '');
    let supplierSku = $state(init?.supplier_sku ?? '');
    let barcode = $state(init?.barcode ?? '');
    let cost = $state(init?.cost != null ? String(init.cost) : '');
    let currency = $state(init?.currency ?? session.me?.default_currency ?? 'EUR');
    let minTotal = $state(init?.min_total_quantity != null ? String(init.min_total_quantity) : '');
    let stockRows = $state<{ location_id: number | null; quantity: string }[]>(init ? [] : [{ location_id: null, quantity: '' }]);

    let preview = $state(false);
    let busy = $state(false);
    let error = $state('');

    const named = (kind: string) => async (q: string) => {
        const r = await api.get<Page<CatalogueEntry>>(`/${kind}?limit=8&q=${encodeURIComponent(q)}`);
        return r.items.map((x) => x.name);
    };
    const uoms = async (q: string) => (await api.get<{ items: string[] }>('/uoms?q=' + encodeURIComponent(q))).items;

    function numOrNull(s: string): string | null {
        return s.trim() === '' ? null : s.trim();
    }

    async function submit(e: Event) {
        e.preventDefault();
        busy = true;
        error = '';
        const body: Record<string, unknown> = {
            name,
            description,
            tags,
            category_id: categoryId,
            uom,
            allow_fractional: allowFractional,
            mpn,
            manufacturer: manufacturer.trim() || null,
            supplier: supplier.trim() || null,
            supplier_sku: supplierSku,
            barcode,
            cost: numOrNull(cost),
            currency: numOrNull(cost) ? currency : null,
            min_total_quantity: numOrNull(minTotal)
        };
        try {
            let saved: Part;
            if (init) {
                saved = await api.patch<Part>(`/parts/${init.id}`, { ...body, version: init.version });
            } else {
                body.stock = stockRows
                    .filter((r) => r.location_id !== null)
                    .map((r) => ({ location_id: r.location_id, quantity: r.quantity.trim() || '0' }));
                saved = await api.post<Part>('/parts', body);
            }
            onsaved(saved);
        } catch (err) {
            error = errorMessage(err);
        } finally {
            busy = false;
        }
    }
</script>

<form onsubmit={submit} class="space-y-6">
    <section class="card space-y-4 p-4">
        <h2 class="font-semibold">Details</h2>
        <div>
            <label class="label" for="p-name">Name</label>
            <input id="p-name" class="input" required maxlength="200" bind:value={name} />
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
            <div>
                <label class="label" for="p-cat">Category</label>
                <CategoryPicker id="p-cat" bind:value={categoryId} excludeStructural allowNone noneLabel="Uncategorised" />
            </div>
            <div>
                <label class="label" for="p-tags">Tags</label>
                <TagInput id="p-tags" bind:value={tags} />
            </div>
        </div>
        <div>
            <div class="mb-1 flex items-center justify-between">
                <label class="label mb-0" for="p-desc">Description (Markdown)</label>
                <button type="button" class="link cursor-pointer text-xs" onclick={() => (preview = !preview)}>
                    {preview ? 'Edit' : 'Preview'}
                </button>
            </div>
            {#if preview}
                <div class="min-h-32 rounded-md border border-zinc-300 p-3 dark:border-zinc-700">
                    <Markdown source={description} />
                </div>
            {:else}
                <textarea id="p-desc" class="input min-h-32 font-mono text-[13px]" maxlength="50000" bind:value={description}></textarea>
            {/if}
        </div>
    </section>

    <section class="card grid gap-4 p-4 sm:grid-cols-2">
        <h2 class="font-semibold sm:col-span-2">Specification</h2>
        <div>
            <label class="label" for="p-mpn">Part number (MPN)</label>
            <input id="p-mpn" class="input" maxlength="100" bind:value={mpn} />
        </div>
        <div>
            <label class="label" for="p-mfr">Manufacturer</label>
            <Autocomplete id="p-mfr" bind:value={manufacturer} suggest={named('manufacturers')} maxlength={100} />
        </div>
        <div>
            <label class="label" for="p-sup">Supplier</label>
            <Autocomplete id="p-sup" bind:value={supplier} suggest={named('suppliers')} maxlength={100} />
        </div>
        <div>
            <label class="label" for="p-sku">Supplier order code</label>
            <input id="p-sku" class="input" maxlength="100" bind:value={supplierSku} />
        </div>
        <div>
            <label class="label" for="p-barcode">Barcode</label>
            <input id="p-barcode" class="input" maxlength="100" bind:value={barcode} />
        </div>
        <div class="grid grid-cols-[1fr_6rem] gap-2">
            <div>
                <label class="label" for="p-cost">Cost per {uom || 'unit'}</label>
                <input id="p-cost" class="input" inputmode="decimal" placeholder="0.00" bind:value={cost} />
            </div>
            <div>
                <label class="label" for="p-cur">Currency</label>
                <input id="p-cur" class="input uppercase" maxlength="3" bind:value={currency} />
            </div>
        </div>
        <div>
            <label class="label" for="p-uom">Unit of measure</label>
            <Autocomplete id="p-uom" bind:value={uom} suggest={uoms} maxlength={20} />
        </div>
        <div class="flex flex-col justify-end">
            <label class="flex items-center gap-2 text-sm">
                <input type="checkbox" bind:checked={allowFractional} />
                Allow fractional quantities (e.g. 1.5 {uom || 'm'})
            </label>
        </div>
        <div>
            <label class="label" for="p-min">Low stock below (total)</label>
            <input id="p-min" class="input" inputmode="decimal" placeholder="No threshold" bind:value={minTotal} />
        </div>
        {#if init && init.uom !== uom}
            <p class="flex items-center gap-2 text-sm text-amber-700 sm:col-span-2 dark:text-amber-400">
                <Icon name="alert" size={16} /> Changing the unit does not convert existing quantities.
            </p>
        {/if}
    </section>

    {#if !init}
        <section class="card space-y-3 p-4">
            <h2 class="font-semibold">Initial stock</h2>
            {#each stockRows as row, i (i)}
                <div class="grid grid-cols-[1fr_7rem_auto] items-end gap-2">
                    <div>
                        <label class="label" for="p-st-{i}">Location</label>
                        <LocationPicker id="p-st-{i}" bind:value={row.location_id} excludeStructural />
                    </div>
                    <div>
                        <label class="label" for="p-sq-{i}">Quantity</label>
                        <input id="p-sq-{i}" class="input" inputmode="decimal" placeholder="0" bind:value={row.quantity} />
                    </div>
                    <button
                        type="button"
                        class="btn btn-ghost"
                        aria-label="Remove row"
                        onclick={() => (stockRows = stockRows.filter((_, j) => j !== i))}
                    >
                        <Icon name="x" size={16} />
                    </button>
                </div>
            {/each}
            <button type="button" class="btn btn-sm" onclick={() => stockRows.push({ location_id: null, quantity: '' })}>
                <Icon name="plus" size={14} /> Another location
            </button>
        </section>
    {/if}

    {#if error}
        <p class="flex items-center gap-2 text-sm text-red-600"><Icon name="alert" size={16} />{error}</p>
    {/if}
    <div class="flex justify-end gap-2">
        <button type="button" class="btn" onclick={oncancel}>Cancel</button>
        <button type="submit" class="btn btn-primary" disabled={busy || !name.trim()}>{init ? 'Save changes' : 'Create part'}</button>
    </div>
</form>
