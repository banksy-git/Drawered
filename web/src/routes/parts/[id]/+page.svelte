<script lang="ts">
    import { goto } from '$app/navigation';
    import { page } from '$app/state';
    import { api, errorMessage } from '$lib/api';
    import { can, session } from '$lib/session.svelte';
    import { toast, toastError } from '$lib/toast.svelte';
    import { bytes, dateTime, fileUrl, money, qty } from '$lib/format';
    import { invalidateLocations } from '$lib/locations.svelte';
    import type { Document, Image, Link, Part, StockEntry } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import Thumb from '$lib/components/Thumb.svelte';
    import Swatch from '$lib/components/Swatch.svelte';
    import CategoryIcon from '$lib/components/CategoryIcon.svelte';
    import Markdown from '$lib/components/Markdown.svelte';
    import Modal from '$lib/components/Modal.svelte';
    import EventList from '$lib/components/EventList.svelte';
    import LocationPicker from '$lib/components/LocationPicker.svelte';
    import StockDialog, { type StockMode } from '$lib/components/StockDialog.svelte';

    type Tab = 'details' | 'specification' | 'stock' | 'resources' | 'history';

    let part = $state<Part | null>(null);
    let error = $state('');
    let id = $derived(page.params.id);
    let tab = $derived<Tab>((page.url.searchParams.get('tab') as Tab) || 'details');

    $effect(() => {
        error = '';
        api.get<Part>(`/parts/${id}`)
            .then((p) => (part = p))
            .catch((e) => (error = errorMessage(e)));
    });

    let editable = $derived(!!part && !part.deleted_at);
    let tabs = $derived(
        [
            { id: 'details', label: 'Details' },
            { id: 'specification', label: 'Specification' },
            { id: 'stock', label: `Stock (${part?.stock.length ?? 0})` },
            { id: 'resources', label: `Resources (${(part?.links.length ?? 0) + (part?.documents.length ?? 0)})` },
            { id: 'history', label: 'History', show: can('events:read') }
        ].filter((t) => t.show !== false) as { id: Tab; label: string }[]
    );

    function setTab(t: Tab) {
        const u = new URL(page.url);
        if (t === 'details') u.searchParams.delete('tab');
        else u.searchParams.set('tab', t);
        goto(u.pathname + u.search, { replaceState: true, noScroll: true, keepFocus: true });
    }

    function updated(p: Part) {
        part = p;
        historyKey++;
    }

    // Stock dialogs.
    let stockOpen = $state(false);
    let stockMode = $state<StockMode>('add');
    let stockLoc = $state<number | null>(null);
    function openStock(mode: StockMode, loc: number | null = null) {
        stockMode = mode;
        stockLoc = loc;
        stockOpen = true;
    }

    // Stock entry editing.
    let entryOpen = $state(false);
    let entry = $state<StockEntry | null>(null);
    let entryMin = $state('');
    let entryNote = $state('');
    function editEntry(s: StockEntry) {
        entry = s;
        entryMin = s.min_quantity != null ? String(s.min_quantity) : '';
        entryNote = s.note;
        entryOpen = true;
    }
    async function saveEntry() {
        if (!part || !entry) return;
        try {
            updated(
                await api.patch<Part>(`/stock/${part.id}/${entry.location_id}`, {
                    min_quantity: entryMin.trim() === '' ? null : entryMin.trim(),
                    note: entryNote
                })
            );
            entryOpen = false;
        } catch (e) {
            toastError(e);
        }
    }
    async function removeEntry(s: StockEntry) {
        if (!part || !confirm(`Remove ${s.path} as a location for this part?`)) return;
        try {
            updated(await api.del<Part>(`/stock/${part.id}/${s.location_id}`));
            invalidateLocations();
        } catch (e) {
            toastError(e);
        }
    }

    let addLocOpen = $state(false);
    let addLocId = $state<number | null>(null);
    async function addLocation() {
        if (!part || addLocId === null) return;
        try {
            updated(await api.post<Part>('/stock/entries', { part_id: part.id, location_id: addLocId }));
            invalidateLocations();
            addLocOpen = false;
            addLocId = null;
        } catch (e) {
            toastError(e);
        }
    }

    // Images.
    let uploading = $state(0);
    let lightbox = $state<Image | null>(null);
    let lightboxOpen = $state(false);

    async function uploadImages(files: FileList | File[]) {
        if (!part) return;
        for (const f of Array.from(files)) {
            if (!f.type.startsWith('image/')) continue;
            const form = new FormData();
            form.append('file', f, f.name || 'pasted.png');
            uploading++;
            try {
                updated(await api.upload<Part>(`/parts/${part.id}/images`, form));
            } catch (e) {
                toastError(e);
            } finally {
                uploading--;
            }
        }
    }

    function onpaste(e: ClipboardEvent) {
        if (!editable || !can('parts:edit')) return;
        const target = e.target as HTMLElement;
        if (['INPUT', 'TEXTAREA'].includes(target.tagName)) return;
        const files = Array.from(e.clipboardData?.files ?? []).filter((f) => f.type.startsWith('image/'));
        if (files.length) {
            e.preventDefault();
            uploadImages(files);
        }
    }

    async function setThumbnail(img: Image) {
        if (!part) return;
        try {
            updated(await api.put<Part>(`/parts/${part.id}/thumbnail`, { image_id: img.id }));
            toast('Thumbnail updated');
        } catch (e) {
            toastError(e);
        }
    }

    async function moveItem(kind: 'images' | 'links' | 'documents', list: { id: number; sort_order: number }[], i: number, d: number) {
        if (!part) return;
        const j = i + d;
        if (j < 0 || j >= list.length) return;
        // Renumber everything so equal sort orders cannot stall a swap.
        const order = list.map((x) => x.id);
        [order[i], order[j]] = [order[j], order[i]];
        try {
            let p: Part = part;
            for (let k = 0; k < order.length; k++) {
                const item = list.find((x) => x.id === order[k])!;
                if (item.sort_order !== k) p = await api.patch<Part>(`/parts/${part.id}/${kind}/${item.id}`, { sort_order: k });
            }
            updated(p);
        } catch (e) {
            toastError(e);
        }
    }

    async function deleteImage(img: Image) {
        if (!part || !confirm('Remove this image?')) return;
        try {
            updated(await api.del<Part>(`/parts/${part.id}/images/${img.id}`));
            lightboxOpen = false;
        } catch (e) {
            toastError(e);
        }
    }

    async function editCaption(img: Image) {
        if (!part) return;
        const caption = prompt('Caption', img.caption);
        if (caption === null) return;
        try {
            updated(await api.patch<Part>(`/parts/${part.id}/images/${img.id}`, { caption }));
        } catch (e) {
            toastError(e);
        }
    }

    // Links.
    let linkUrl = $state('');
    let linkDesc = $state('');
    async function addLink(e: Event) {
        e.preventDefault();
        if (!part) return;
        try {
            updated(await api.post<Part>(`/parts/${part.id}/links`, { url: linkUrl, description: linkDesc }));
            linkUrl = '';
            linkDesc = '';
        } catch (err) {
            toastError(err);
        }
    }
    async function editLink(l: Link) {
        if (!part) return;
        const description = prompt('Description', l.description);
        if (description === null) return;
        try {
            updated(await api.patch<Part>(`/parts/${part.id}/links/${l.id}`, { description }));
        } catch (e) {
            toastError(e);
        }
    }
    async function deleteLink(l: Link) {
        if (!part || !confirm('Remove this link?')) return;
        try {
            updated(await api.del<Part>(`/parts/${part.id}/links/${l.id}`));
        } catch (e) {
            toastError(e);
        }
    }

    // Documents.
    let docDesc = $state('');
    let docInput: HTMLInputElement | undefined = $state();
    async function uploadDocument(e: Event) {
        e.preventDefault();
        const f = docInput?.files?.[0];
        if (!part || !f) return;
        if (session.me && f.size > session.me.max_document_bytes) {
            toast(`That file is larger than ${bytes(session.me.max_document_bytes)}`, 'error');
            return;
        }
        const form = new FormData();
        form.append('description', docDesc);
        form.append('file', f, f.name);
        uploading++;
        try {
            updated(await api.upload<Part>(`/parts/${part.id}/documents`, form));
            docDesc = '';
            if (docInput) docInput.value = '';
        } catch (err) {
            toastError(err);
        } finally {
            uploading--;
        }
    }
    async function editDocument(d: Document) {
        if (!part) return;
        const description = prompt('Description', d.description);
        if (description === null) return;
        try {
            updated(await api.patch<Part>(`/parts/${part.id}/documents/${d.id}`, { description }));
        } catch (e) {
            toastError(e);
        }
    }
    async function deleteDocument(d: Document) {
        if (!part || !confirm(`Remove ${d.original_name}?`)) return;
        try {
            updated(await api.del<Part>(`/parts/${part.id}/documents/${d.id}`));
        } catch (e) {
            toastError(e);
        }
    }

    // Part-level actions.
    async function duplicate() {
        if (!part) return;
        const images = part.images.length > 0 && confirm('Copy the images too?');
        try {
            const p = await api.post<Part>(`/parts/${part.id}/duplicate`, { images });
            toast('Duplicated - edit the copy below');
            goto(`/parts/${p.id}/edit`);
        } catch (e) {
            toastError(e);
        }
    }

    async function remove() {
        if (!part) return;
        let force = false;
        if (part.total_quantity > 0) {
            if (!confirm(`${part.name} still has ${qty(part.total_quantity, part.uom)} in stock. Delete it anyway?`)) return;
            force = true;
        } else if (!confirm(`Delete ${part.name}?`)) {
            return;
        }
        try {
            await api.del(`/parts/${part.id}${force ? '?force=true' : ''}`);
            invalidateLocations();
            toast(`Deleted ${part.name}`);
            goto('/');
        } catch (e) {
            toastError(e);
        }
    }

    async function restore() {
        if (!part) return;
        try {
            updated(await api.post<Part>(`/parts/${part.id}/restore`));
            toast('Restored');
        } catch (e) {
            toastError(e);
        }
    }

    async function purge() {
        if (!part || !confirm(`Permanently purge ${part.name}? This cannot be undone. Its history is kept.`)) return;
        try {
            await api.post(`/parts/${part.id}/purge`);
            toast('Purged');
            goto('/?deleted=only');
        } catch (e) {
            toastError(e);
        }
    }

    let historyKey = $state(0);
</script>

<svelte:window {onpaste} />
<svelte:head><title>{part?.name ?? 'Part'} - Drawered</title></svelte:head>

{#if error}
    <div class="card p-6 text-center">
        <p class="font-medium">{error}</p>
        <a class="link text-sm" href="/">Back to parts</a>
    </div>
{:else if !part}
    <p class="muted text-sm">Loading...</p>
{:else}
    {#if part.deleted_at}
        <div class="mb-4 flex flex-wrap items-center gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
            <Icon name="trash" size={16} />
            <span class="flex-1">This part was deleted on {dateTime(part.deleted_at)}.</span>
            {#if can('parts:delete')}<button class="btn btn-sm" onclick={restore}><Icon name="restore" size={14} /> Restore</button>{/if}
            {#if can('system:admin')}<button class="btn btn-danger btn-sm" onclick={purge}>Purge</button>{/if}
        </div>
    {/if}

    <header class="mb-4 flex flex-wrap items-start gap-4">
        <Thumb fileId={part.thumbnail_file_id} size={72} alt={part.name} />
        <div class="min-w-0 flex-1">
            {#if part.category}
                <a class="muted mb-0.5 inline-flex items-center gap-1.5 text-xs hover:underline" href="/categories/{part.category.id}">
                    <CategoryIcon icon={part.category.effective_icon} size={14} />{part.category.path}
                </a>
            {/if}
            <h1 class="text-xl font-semibold break-words">{part.name}</h1>
            <p class="muted text-sm">
                {[part.mpn, part.manufacturer?.name].filter(Boolean).join(' - ')}
            </p>
            {#if part.tags.length}
                <div class="mt-1.5 flex flex-wrap gap-1">
                    {#each part.tags as t (t)}<a class="badge hover:underline" href="/?tag={encodeURIComponent(t)}">{t}</a>{/each}
                </div>
            {/if}
        </div>
        <div class="text-right">
            <p class="text-2xl font-semibold tabular-nums">{qty(part.total_quantity, part.uom)}</p>
            <p class="muted text-xs">
                in {part.stock.length} {part.stock.length === 1 ? 'location' : 'locations'}
                {#if part.low}<span class="badge ml-1 bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300">low</span>{/if}
            </p>
        </div>
    </header>

    {#if editable}
        <div class="mb-4 flex flex-wrap gap-2">
            {#if can('stock:move')}
                <button class="btn btn-primary" onclick={() => openStock('add')}><Icon name="plus" size={16} /> Add</button>
                <button class="btn" onclick={() => openStock('remove')} disabled={part.stock.length === 0}>
                    <Icon name="minus" size={16} /> Remove
                </button>
                <button class="btn" onclick={() => openStock('move')} disabled={part.stock.length === 0}>
                    <Icon name="move" size={16} /> Move
                </button>
            {/if}
            <div class="ml-auto flex flex-wrap gap-2">
                {#if can('parts:edit')}<a class="btn" href="/parts/{part.id}/edit"><Icon name="edit" size={16} /> Edit</a>{/if}
                {#if can('parts:create')}<button class="btn" onclick={duplicate}><Icon name="copy" size={16} /> Duplicate</button>{/if}
                {#if can('parts:delete')}
                    <button class="btn" onclick={remove} aria-label="Delete part"><Icon name="trash" size={16} /></button>
                {/if}
            </div>
        </div>
    {/if}

    <div class="mb-4 flex gap-1 overflow-x-auto border-b border-zinc-200 dark:border-zinc-800" role="tablist">
        {#each tabs as t (t.id)}
            <button
                role="tab"
                aria-selected={tab === t.id}
                class="-mb-px cursor-pointer border-b-2 px-3 py-2 text-sm font-medium whitespace-nowrap {tab === t.id
                    ? 'border-accent-600 text-accent-700 dark:border-indigo-400 dark:text-indigo-300'
                    : 'border-transparent text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'}"
                onclick={() => setTab(t.id)}>{t.label}</button
            >
        {/each}
    </div>

    {#if tab === 'details'}
        <div class="grid gap-4 lg:grid-cols-[1fr_20rem]">
            <section class="card p-4">
                {#if part.description}
                    <Markdown source={part.description} />
                {:else}
                    <p class="muted text-sm">No description.</p>
                {/if}
            </section>
            <section class="card p-4">
                <div class="mb-3 flex items-center justify-between">
                    <h2 class="font-semibold">Images</h2>
                    {#if uploading}<span class="muted text-xs">Uploading...</span>{/if}
                </div>
                {#if part.images.length}
                    <ul class="grid grid-cols-3 gap-2">
                        {#each part.images as img, i (img.id)}
                            <li class="group relative">
                                <button
                                    class="block aspect-square w-full cursor-zoom-in overflow-hidden rounded-md border-2 {img.file_id ===
                                    part.thumbnail_file_id
                                        ? 'border-accent-500'
                                        : 'border-transparent'}"
                                    onclick={() => {
                                        lightbox = img;
                                        lightboxOpen = true;
                                    }}
                                    aria-label="View image {i + 1}"
                                >
                                    <img src={fileUrl(img.file_id, 'small')} alt={img.caption} class="h-full w-full object-cover" loading="lazy" />
                                </button>
                                {#if img.file_id === part.thumbnail_file_id}
                                    <span class="absolute top-1 left-1 rounded bg-accent-600 p-0.5 text-white" title="Thumbnail">
                                        <Icon name="star" size={12} />
                                    </span>
                                {/if}
                            </li>
                        {/each}
                    </ul>
                {:else}
                    <p class="muted text-sm">No images.</p>
                {/if}
                {#if editable && can('parts:edit')}
                    <div class="mt-3 flex flex-wrap gap-2">
                        <label class="btn btn-sm">
                            <Icon name="upload" size={14} /> Upload
                            <input type="file" accept="image/*" multiple class="sr-only" onchange={(e) => uploadImages(e.currentTarget.files ?? [])} />
                        </label>
                        <label class="btn btn-sm sm:hidden">
                            <Icon name="camera" size={14} /> Camera
                            <input type="file" accept="image/*" capture="environment" class="sr-only" onchange={(e) => uploadImages(e.currentTarget.files ?? [])} />
                        </label>
                    </div>
                    <p class="muted mt-2 hidden text-xs sm:block">Tip: paste an image anywhere on this page to add it.</p>
                {/if}
            </section>
        </div>
    {:else if tab === 'specification'}
        <section class="card p-4">
            <dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[12rem_1fr]">
                <dt class="muted">Category</dt>
                <dd>
                    {#if part.category}
                        <a class="link inline-flex items-center gap-1.5" href="/categories/{part.category.id}">
                            <CategoryIcon icon={part.category.effective_icon} />{part.category.path}
                        </a>
                    {:else}-{/if}
                </dd>
                <dt class="muted">Part number (MPN)</dt>
                <dd>{part.mpn || '-'}</dd>
                <dt class="muted">Manufacturer</dt>
                <dd>{#if part.manufacturer}<a class="link" href="/?manufacturer={part.manufacturer.id}">{part.manufacturer.name}</a>{:else}-{/if}</dd>
                <dt class="muted">Supplier</dt>
                <dd>{#if part.supplier}<a class="link" href="/?supplier={part.supplier.id}">{part.supplier.name}</a>{:else}-{/if}</dd>
                <dt class="muted">Supplier order code</dt>
                <dd>{part.supplier_sku || '-'}</dd>
                <dt class="muted">Barcode</dt>
                <dd class="font-mono">{part.barcode || '-'}</dd>
                <dt class="muted">Unit of measure</dt>
                <dd>{part.uom}{part.allow_fractional ? ' (fractional quantities allowed)' : ''}</dd>
                <dt class="muted">Cost per {part.uom}</dt>
                <dd>{part.cost != null ? money(part.cost, part.currency) : '-'}</dd>
                <dt class="muted">Stock value</dt>
                <dd>{part.total_value != null ? money(part.total_value, part.currency) : '-'}</dd>
                <dt class="muted">Low stock below</dt>
                <dd>{part.min_total_quantity != null ? qty(part.min_total_quantity, part.uom) : '-'}</dd>
                <dt class="muted">Created</dt>
                <dd>{dateTime(part.created_at)}</dd>
                <dt class="muted">Updated</dt>
                <dd>{dateTime(part.updated_at)}</dd>
            </dl>
        </section>
    {:else if tab === 'stock'}
        <section class="card overflow-x-auto">
            {#if part.stock.length}
                <table class="table">
                    <thead>
                        <tr><th>Location</th><th class="text-right">Quantity</th><th class="hidden sm:table-cell">Note</th><th></th></tr>
                    </thead>
                    <tbody>
                        {#each part.stock as s (s.location_id)}
                            <tr>
                                <td>
                                    <a href="/locations/{s.location_id}" class="link inline-flex items-center gap-2">
                                        <Swatch colour={s.effective_colour} />{s.path}
                                    </a>
                                </td>
                                <td class="text-right whitespace-nowrap tabular-nums">
                                    <span class="font-semibold">{qty(s.quantity, part.uom)}</span>
                                    {#if s.min_quantity != null}
                                        <span class="muted block text-xs {s.low ? 'text-amber-600 dark:text-amber-400' : ''}">
                                            min {qty(s.min_quantity)}
                                        </span>
                                    {/if}
                                </td>
                                <td class="muted hidden sm:table-cell">{s.note}</td>
                                <td class="text-right whitespace-nowrap">
                                    {#if editable && can('stock:move')}
                                        <button class="btn btn-ghost btn-sm" onclick={() => openStock('add', s.location_id)} aria-label="Add here">
                                            <Icon name="plus" size={14} />
                                        </button>
                                        <button class="btn btn-ghost btn-sm" onclick={() => openStock('remove', s.location_id)} aria-label="Remove from here" disabled={s.quantity === 0}>
                                            <Icon name="minus" size={14} />
                                        </button>
                                        <button class="btn btn-ghost btn-sm" onclick={() => openStock('move', s.location_id)} aria-label="Move from here" disabled={s.quantity === 0}>
                                            <Icon name="move" size={14} />
                                        </button>
                                        {#if can('stock:adjust')}
                                            <button class="btn btn-ghost btn-sm" onclick={() => openStock('set', s.location_id)}>Set</button>
                                        {/if}
                                        <button class="btn btn-ghost btn-sm" onclick={() => editEntry(s)} aria-label="Stock settings">
                                            <Icon name="settings" size={14} />
                                        </button>
                                        {#if s.quantity === 0}
                                            <button class="btn btn-ghost btn-sm" onclick={() => removeEntry(s)} aria-label="Remove location">
                                                <Icon name="trash" size={14} />
                                            </button>
                                        {/if}
                                    {/if}
                                </td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            {:else}
                <p class="muted p-4 text-sm">This part is not stocked anywhere yet.</p>
            {/if}
            {#if editable && can('stock:move')}
                <div class="border-t border-zinc-100 p-3 dark:border-zinc-800">
                    <button class="btn btn-sm" onclick={() => (addLocOpen = true)}><Icon name="map" size={14} /> Add to a location</button>
                </div>
            {/if}
        </section>
    {:else if tab === 'resources'}
        <div class="grid gap-4 lg:grid-cols-2">
            <section class="card p-4">
                <h2 class="mb-3 font-semibold">Links</h2>
                {#if part.links.length}
                    <ul class="space-y-2">
                        {#each part.links as l, i (l.id)}
                            <li class="flex items-start gap-2 text-sm">
                                <Icon name="link" size={16} class="mt-0.5 text-zinc-400" />
                                <div class="min-w-0 flex-1">
                                    <a class="link break-all" href={l.url} target="_blank" rel="noopener noreferrer">
                                        {l.description || l.url}
                                    </a>
                                    {#if l.description}<p class="muted truncate text-xs">{l.url}</p>{/if}
                                </div>
                                {#if editable && can('parts:edit')}
                                    <div class="flex shrink-0">
                                        <button class="btn btn-ghost btn-sm" disabled={i === 0} onclick={() => moveItem('links', part!.links, i, -1)} aria-label="Move up">&uarr;</button>
                                        <button class="btn btn-ghost btn-sm" onclick={() => editLink(l)} aria-label="Edit"><Icon name="edit" size={14} /></button>
                                        <button class="btn btn-ghost btn-sm" onclick={() => deleteLink(l)} aria-label="Delete"><Icon name="trash" size={14} /></button>
                                    </div>
                                {/if}
                            </li>
                        {/each}
                    </ul>
                {:else}
                    <p class="muted text-sm">No links.</p>
                {/if}
                {#if editable && can('parts:edit')}
                    <form class="mt-4 space-y-2 border-t border-zinc-100 pt-3 dark:border-zinc-800" onsubmit={addLink}>
                        <input class="input" type="url" placeholder="https://..." required bind:value={linkUrl} aria-label="Link URL" />
                        <div class="flex gap-2">
                            <input class="input" placeholder="Description, e.g. Datasheet" maxlength="200" bind:value={linkDesc} aria-label="Link description" />
                            <button class="btn" type="submit">Add</button>
                        </div>
                    </form>
                {/if}
            </section>
            <section class="card p-4">
                <h2 class="mb-3 font-semibold">Documents</h2>
                {#if part.documents.length}
                    <ul class="space-y-2">
                        {#each part.documents as d, i (d.id)}
                            <li class="flex items-start gap-2 text-sm">
                                <Icon name="file" size={16} class="mt-0.5 text-zinc-400" />
                                <div class="min-w-0 flex-1">
                                    <a class="link break-all" href={fileUrl(d.file_id)} target="_blank" rel="noopener">
                                        {d.description || d.original_name}
                                    </a>
                                    <p class="muted truncate text-xs">{d.original_name} - {bytes(d.size)}</p>
                                </div>
                                {#if editable && can('parts:edit')}
                                    <div class="flex shrink-0">
                                        <button class="btn btn-ghost btn-sm" disabled={i === 0} onclick={() => moveItem('documents', part!.documents, i, -1)} aria-label="Move up">&uarr;</button>
                                        <button class="btn btn-ghost btn-sm" onclick={() => editDocument(d)} aria-label="Edit"><Icon name="edit" size={14} /></button>
                                        <button class="btn btn-ghost btn-sm" onclick={() => deleteDocument(d)} aria-label="Delete"><Icon name="trash" size={14} /></button>
                                    </div>
                                {/if}
                            </li>
                        {/each}
                    </ul>
                {:else}
                    <p class="muted text-sm">No documents.</p>
                {/if}
                {#if editable && can('parts:edit')}
                    <form class="mt-4 space-y-2 border-t border-zinc-100 pt-3 dark:border-zinc-800" onsubmit={uploadDocument}>
                        <input bind:this={docInput} type="file" required class="block w-full text-sm" aria-label="Document file" />
                        <div class="flex gap-2">
                            <input class="input" placeholder="Description" maxlength="200" bind:value={docDesc} aria-label="Document description" />
                            <button class="btn" type="submit" disabled={uploading > 0}>Upload</button>
                        </div>
                    </form>
                {/if}
            </section>
        </div>
    {:else if tab === 'history'}
        <section class="card p-4">
            {#key historyKey}
                <EventList endpoint="/parts/{part.id}/events" />
            {/key}
        </section>
    {/if}

    <StockDialog bind:open={stockOpen} {part} mode={stockMode} locationId={stockLoc} ondone={updated} />

    <Modal bind:open={entryOpen} title="Stock settings">
        {#if entry}
            <div class="space-y-3">
                <p class="text-sm">{entry.path}</p>
                <div>
                    <label class="label" for="e-min">Low stock below ({part.uom})</label>
                    <input id="e-min" class="input" inputmode="decimal" placeholder="No threshold" bind:value={entryMin} />
                </div>
                <div>
                    <label class="label" for="e-note">Note</label>
                    <input id="e-note" class="input" maxlength="200" placeholder="e.g. reel, cut tape" bind:value={entryNote} />
                </div>
            </div>
        {/if}
        {#snippet footer()}
            <button class="btn" onclick={() => (entryOpen = false)}>Cancel</button>
            <button class="btn btn-primary" onclick={saveEntry}>Save</button>
        {/snippet}
    </Modal>

    <Modal bind:open={addLocOpen} title="Add to a location">
        <label class="label" for="add-loc">Location</label>
        <LocationPicker id="add-loc" bind:value={addLocId} excludeStructural exclude={part.stock.map((s) => s.location_id)} />
        <p class="muted mt-2 text-xs">Creates an empty entry; use Add to put stock there.</p>
        {#snippet footer()}
            <button class="btn" onclick={() => (addLocOpen = false)}>Cancel</button>
            <button class="btn btn-primary" disabled={addLocId === null} onclick={addLocation}>Add location</button>
        {/snippet}
    </Modal>

    <Modal bind:open={lightboxOpen} title={lightbox?.caption || part.name} wide>
        {#if lightbox}
            {@const img = lightbox}
            {@const idx = part.images.findIndex((x) => x.id === img.id)}
            <a href={fileUrl(img.file_id)} target="_blank" rel="noopener">
                <img src={fileUrl(img.file_id, 'large')} alt={img.caption} class="mx-auto max-h-[55vh] rounded" />
            </a>
            <div class="mt-3 flex flex-wrap items-center gap-2">
                <button class="btn btn-sm" disabled={idx <= 0} onclick={() => (lightbox = part!.images[idx - 1])}>Previous</button>
                <button class="btn btn-sm" disabled={idx >= part.images.length - 1} onclick={() => (lightbox = part!.images[idx + 1])}>Next</button>
                {#if editable && can('parts:edit')}
                    <span class="flex-1"></span>
                    <button class="btn btn-sm" disabled={idx <= 0} onclick={() => moveItem('images', part!.images, idx, -1)}>Move earlier</button>
                    <button class="btn btn-sm" onclick={() => editCaption(img)}><Icon name="edit" size={14} /> Caption</button>
                    {#if img.file_id !== part.thumbnail_file_id}
                        <button class="btn btn-sm" onclick={() => setThumbnail(img)}><Icon name="star" size={14} /> Use as thumbnail</button>
                    {/if}
                    <button class="btn btn-sm" onclick={() => deleteImage(img)}><Icon name="trash" size={14} /></button>
                {/if}
            </div>
        {/if}
    </Modal>
{/if}
