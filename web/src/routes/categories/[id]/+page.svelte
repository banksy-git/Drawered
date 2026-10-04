<script lang="ts">
    import { goto } from '$app/navigation';
    import { page } from '$app/state';
    import { api, errorMessage, qs } from '$lib/api';
    import { can } from '$lib/session.svelte';
    import { toast, toastError } from '$lib/toast.svelte';
    import { qty } from '$lib/format';
    import { categoryCache, invalidateCategories, loadCategories } from '$lib/categories.svelte';
    import type { Category, Page, PartSummary } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import CategoryIcon from '$lib/components/CategoryIcon.svelte';
    import Thumb from '$lib/components/Thumb.svelte';
    import EventList from '$lib/components/EventList.svelte';
    import CategoryForm from '$lib/components/CategoryForm.svelte';

    const limit = 20;

    let cat = $state<Category | null>(null);
    let error = $state('');
    let parts = $state<Page<PartSummary> | null>(null);
    let editOpen = $state(false);
    let childOpen = $state(false);
    let historyKey = $state(0);

    let id = $derived(Number(page.params.id));

    $effect(() => {
        error = '';
        api.get<Category>(`/categories/${id}`)
            .then((c) => (cat = c))
            .catch((e) => (error = errorMessage(e)));
        loadCategories();
        api.get<Page<PartSummary>>('/parts' + qs({ category: id, limit }))
            .then((p) => (parts = p))
            .catch(toastError);
    });

    let childCategories = $derived(categoryCache.items.filter((c) => c.parent_id === id));

    async function remove() {
        if (!cat || !confirm(`Delete category ${cat.path}?`)) return;
        try {
            await api.del(`/categories/${cat.id}`);
            invalidateCategories();
            toast(`Deleted ${cat.name}`);
            goto(cat.parent_id ? `/categories/${cat.parent_id}` : '/categories');
        } catch (e) {
            toastError(e);
        }
    }
</script>

<svelte:head><title>{cat?.name ?? 'Category'} - Drawered</title></svelte:head>

{#if error}
    <div class="card p-6 text-center">
        <p class="font-medium">{error}</p>
        <a class="link text-sm" href="/categories">Back to categories</a>
    </div>
{:else if cat}
    <nav class="muted mb-2 flex flex-wrap items-center gap-1 text-sm" aria-label="Breadcrumb">
        <a class="hover:underline" href="/categories">Categories</a>
        {#each cat.ancestors as a (a.id)}
            <Icon name="chevronRight" size={14} />
            <a class="hover:underline" href="/categories/{a.id}">{a.name}</a>
        {/each}
    </nav>

    <header class="mb-4 flex flex-wrap items-start gap-3">
        <div class="min-w-0 flex-1">
            <h1 class="flex items-center gap-2 text-xl font-semibold">
                <CategoryIcon icon={cat.effective_icon} size={24} class="text-accent-600 dark:text-indigo-400" />
                {cat.name}
                {#if cat.structural}<span class="badge">structural</span>{/if}
            </h1>
            {#if cat.description}<p class="muted mt-1 text-sm whitespace-pre-line">{cat.description}</p>{/if}
        </div>
        {#if can('categories:manage')}
            <div class="flex flex-wrap gap-2">
                <button class="btn" onclick={() => (childOpen = true)}><Icon name="plus" size={16} /> Add inside</button>
                <button class="btn" onclick={() => (editOpen = true)}><Icon name="edit" size={16} /> Edit</button>
                <button class="btn" onclick={remove} aria-label="Delete category"><Icon name="trash" size={16} /></button>
            </div>
        {/if}
    </header>

    <div class="grid gap-4 lg:grid-cols-[1fr_18rem]">
        <section class="card">
            <div class="flex items-center gap-3 border-b border-zinc-100 px-4 py-3 dark:border-zinc-800">
                <h2 class="mr-auto font-semibold">Parts {cat.child_count ? '(including inside)' : ''}</h2>
                {#if parts && parts.total > 0}
                    <a class="link text-sm" href="/?category={cat.id}">Search within</a>
                {/if}
            </div>
            {#if parts && parts.items.length}
                <ul class="divide-y divide-zinc-100 dark:divide-zinc-800">
                    {#each parts.items as p (p.id)}
                        <li>
                            <a href="/parts/{p.id}" class="flex items-center gap-3 px-4 py-2 hover:bg-zinc-50 dark:hover:bg-zinc-800/50">
                                <Thumb fileId={p.thumbnail_file_id} size={40} />
                                <div class="min-w-0 flex-1">
                                    <p class="truncate text-sm font-medium">{p.name}</p>
                                    <p class="muted truncate text-xs">
                                        {[p.mpn, p.category && p.category.id !== cat.id ? p.category.path : ''].filter(Boolean).join(' | ')}
                                    </p>
                                </div>
                                <span class="text-sm font-semibold tabular-nums">{qty(p.total_quantity, p.uom)}</span>
                            </a>
                        </li>
                    {/each}
                </ul>
                {#if parts.total > parts.items.length}
                    <p class="px-4 py-3 text-sm">
                        <a class="link" href="/?category={cat.id}">See all {parts.total} parts</a>
                    </p>
                {/if}
            {:else if parts}
                <p class="muted p-4 text-sm">
                    {cat.structural ? 'Structural categories group other categories; parts go in the categories inside.' : 'No parts in this category.'}
                </p>
            {/if}
        </section>

        <section class="card p-4">
            <h2 class="mb-2 font-semibold">Inside</h2>
            {#if childCategories.length}
                <ul class="space-y-1">
                    {#each childCategories as c (c.id)}
                        <li>
                            <a class="flex items-center gap-2 text-sm hover:underline" href="/categories/{c.id}">
                                <CategoryIcon icon={c.effective_icon} />{c.name}
                                <span class="muted ml-auto text-xs">{c.total_part_count}</span>
                            </a>
                        </li>
                    {/each}
                </ul>
            {:else}
                <p class="muted text-sm">No categories inside.</p>
            {/if}
        </section>
    </div>

    {#if can('events:read')}
        <section class="card mt-4 p-4">
            <h2 class="mb-2 font-semibold">History</h2>
            {#key historyKey}
                <EventList endpoint="/categories/{cat.id}/events" linkSubjects />
            {/key}
        </section>
    {/if}

    <CategoryForm
        bind:open={editOpen}
        category={cat}
        onsaved={(c) => {
            cat = c;
            historyKey++;
            toast('Saved');
        }}
    />
    <CategoryForm bind:open={childOpen} parentId={cat.id} onsaved={(c) => goto(`/categories/${c.id}`)} />
{:else}
    <p class="muted text-sm">Loading...</p>
{/if}
