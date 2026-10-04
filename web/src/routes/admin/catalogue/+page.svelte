<script lang="ts">
    import { api, ApiError, errorMessage, qs } from '$lib/api';
    import { toast, toastError } from '$lib/toast.svelte';
    import type { CatalogueEntry, Page } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import Modal from '$lib/components/Modal.svelte';
    import Pager from '$lib/components/Pager.svelte';

    type Kind = 'manufacturers' | 'suppliers' | 'tags';
    const limit = 50;

    let kind = $state<Kind>('manufacturers');
    let q = $state('');
    let offset = $state(0);
    let list = $state<Page<CatalogueEntry> | null>(null);
    let error = $state('');
    let reload = $state(0);

    $effect(() => {
        reload;
        api.get<Page<CatalogueEntry>>(`/${kind}` + qs({ q, limit, offset }))
            .then((r) => (list = r))
            .catch((e) => (error = errorMessage(e)));
    });

    function switchTo(k: Kind) {
        kind = k;
        q = '';
        offset = 0;
        list = null;
    }

    // Edit (rename / URL).
    let editOpen = $state(false);
    let editing = $state<CatalogueEntry | null>(null);
    let editName = $state('');
    let editUrl = $state('');
    let editError = $state('');

    function startEdit(e: CatalogueEntry | null) {
        editing = e;
        editName = e?.name ?? '';
        editUrl = e?.url ?? '';
        editError = '';
        editOpen = true;
    }

    async function saveEdit() {
        try {
            if (kind === 'tags') {
                await api.patch(`/tags/${editing!.id}`, { name: editName });
            } else if (editing) {
                await api.patch(`/${kind}/${editing.id}`, { name: editName, url: editUrl });
            } else {
                await api.post(`/${kind}`, { name: editName, url: editUrl });
            }
            editOpen = false;
            reload++;
            toast('Saved');
        } catch (e) {
            editError = e instanceof ApiError && e.code === 'duplicate_name' ? `${e.message}.` : errorMessage(e);
        }
    }

    // Merge.
    let mergeOpen = $state(false);
    let mergeFrom = $state<CatalogueEntry | null>(null);
    let mergeQuery = $state('');
    let mergeOptions = $state<CatalogueEntry[]>([]);
    let mergeInto = $state<CatalogueEntry | null>(null);

    function startMerge(e: CatalogueEntry) {
        mergeFrom = e;
        mergeQuery = '';
        mergeInto = null;
        mergeOptions = [];
        mergeOpen = true;
    }

    $effect(() => {
        if (!mergeOpen) return;
        const from = mergeFrom;
        api.get<Page<CatalogueEntry>>(`/${kind}` + qs({ q: mergeQuery, limit: 10 })).then(
            (r) => (mergeOptions = r.items.filter((x) => x.id !== from?.id))
        );
    });

    async function doMerge() {
        if (!mergeFrom || !mergeInto) return;
        try {
            await api.post(`/${kind}/${mergeFrom.id}/merge`, { into_id: mergeInto.id });
            mergeOpen = false;
            reload++;
            toast(`Merged ${mergeFrom.name} into ${mergeInto.name}`);
        } catch (e) {
            toastError(e);
        }
    }

    async function remove(e: CatalogueEntry) {
        const msg =
            kind === 'tags'
                ? `Delete tag ${e.name}? It will be removed from ${e.part_count} part(s).`
                : `Delete ${e.name}?`;
        if (!confirm(msg)) return;
        try {
            await api.del(`/${kind}/${e.id}`);
            reload++;
        } catch (err) {
            toastError(err);
        }
    }

    const labels: Record<Kind, string> = { manufacturers: 'Manufacturers', suppliers: 'Suppliers', tags: 'Tags' };
</script>

<svelte:head><title>Catalogue - Drawered</title></svelte:head>

<div class="mb-4 flex flex-wrap items-center gap-2">
    <div class="mr-auto flex gap-1 rounded-lg bg-zinc-100 p-1 dark:bg-zinc-800" role="tablist">
        {#each Object.keys(labels) as k (k)}
            <button
                role="tab"
                aria-selected={kind === k}
                class="cursor-pointer rounded-md px-3 py-1 text-sm font-medium {kind === k ? 'bg-white shadow-xs dark:bg-zinc-900' : 'muted'}"
                onclick={() => switchTo(k as Kind)}>{labels[k as Kind]}</button
            >
        {/each}
    </div>
    <input class="input w-full sm:w-56" placeholder="Filter" bind:value={q} oninput={() => (offset = 0)} aria-label="Filter" />
    {#if kind !== 'tags'}
        <button class="btn btn-primary" onclick={() => startEdit(null)}><Icon name="plus" size={16} /> Add</button>
    {/if}
</div>

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if list}
    <div class="card overflow-x-auto">
        <table class="table">
            <thead><tr><th>Name</th><th class="text-right">Parts</th><th></th></tr></thead>
            <tbody>
                {#each list.items as e (e.id)}
                    <tr>
                        <td>
                            {#if kind === 'tags'}
                                <a class="link" href="/?tag={encodeURIComponent(e.name)}">{e.name}</a>
                            {:else}
                                <a class="link" href="/?{kind === 'manufacturers' ? 'manufacturer' : 'supplier'}={e.id}">{e.name}</a>
                                {#if e.url}<a class="muted ml-2 text-xs hover:underline" href={e.url} target="_blank" rel="noopener noreferrer">{e.url}</a>{/if}
                            {/if}
                        </td>
                        <td class="text-right tabular-nums">{e.part_count}</td>
                        <td class="text-right whitespace-nowrap">
                            <button class="btn btn-ghost btn-sm" onclick={() => startEdit(e)}>Rename</button>
                            <button class="btn btn-ghost btn-sm" onclick={() => startMerge(e)}>Merge</button>
                            <button class="btn btn-ghost btn-sm" onclick={() => remove(e)} aria-label="Delete"><Icon name="trash" size={14} /></button>
                        </td>
                    </tr>
                {:else}
                    <tr><td colspan="3" class="muted">Nothing here yet. Entries are created as you type them on parts.</td></tr>
                {/each}
            </tbody>
        </table>
    </div>
    <Pager total={list.total} {limit} bind:offset />
{/if}

<Modal bind:open={editOpen} title={editing ? `Rename ${editing.name}` : `Add ${labels[kind].toLowerCase().replace(/s$/, '')}`}>
    <div class="space-y-3">
        <div>
            <label class="label" for="c-name">Name</label>
            <input id="c-name" class="input" bind:value={editName} maxlength={kind === 'tags' ? 50 : 100} />
        </div>
        {#if kind !== 'tags'}
            <div>
                <label class="label" for="c-url">Website</label>
                <input id="c-url" class="input" type="url" placeholder="https://" bind:value={editUrl} />
            </div>
        {/if}
        {#if editError}<p class="text-sm text-red-600">{editError}</p>{/if}
    </div>
    {#snippet footer()}
        <button class="btn" onclick={() => (editOpen = false)}>Cancel</button>
        <button class="btn btn-primary" onclick={saveEdit} disabled={!editName.trim()}>Save</button>
    {/snippet}
</Modal>

<Modal bind:open={mergeOpen} title="Merge {mergeFrom?.name}">
    <p class="mb-3 text-sm">
        Every part using <strong>{mergeFrom?.name}</strong> will be moved to the entry you choose, and
        <strong>{mergeFrom?.name}</strong> will be deleted.
    </p>
    <input class="input" placeholder="Search for the entry to keep" bind:value={mergeQuery} aria-label="Merge into" />
    <ul class="mt-2 max-h-56 space-y-1 overflow-auto">
        {#each mergeOptions as o (o.id)}
            <li>
                <label class="flex items-center gap-2 text-sm">
                    <input type="radio" name="merge-into" checked={mergeInto?.id === o.id} onchange={() => (mergeInto = o)} />
                    {o.name} <span class="muted text-xs">({o.part_count})</span>
                </label>
            </li>
        {/each}
    </ul>
    {#snippet footer()}
        <button class="btn" onclick={() => (mergeOpen = false)}>Cancel</button>
        <button class="btn btn-primary" disabled={!mergeInto} onclick={doMerge}>Merge</button>
    {/snippet}
</Modal>
