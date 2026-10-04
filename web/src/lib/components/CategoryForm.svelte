<script lang="ts">
    import { untrack } from 'svelte';
    import { api, errorMessage } from '$lib/api';
    import { categoryCache, invalidateCategories, loadCategories } from '$lib/categories.svelte';
    import type { Category } from '$lib/types';
    import Modal from './Modal.svelte';
    import CategoryPicker from './CategoryPicker.svelte';
    import IconPicker from './IconPicker.svelte';

    let {
        open = $bindable(false),
        category = null,
        parentId = null,
        onsaved
    }: {
        open: boolean;
        category?: Category | null;
        parentId?: number | null;
        onsaved: (c: Category) => void;
    } = $props();

    let name = $state('');
    let description = $state('');
    let icon = $state<string | null>(null);
    let structural = $state(false);
    let parent = $state<number | null>(null);
    let busy = $state(false);
    let error = $state('');

    $effect(() => {
        if (!open) return;
        untrack(() => {
            name = category?.name ?? '';
            description = category?.description ?? '';
            icon = category?.icon ?? null;
            structural = category?.structural ?? false;
            parent = category ? category.parent_id : parentId;
            error = '';
            loadCategories();
        });
    });

    // A category cannot move inside its own subtree.
    let excluded = $derived.by(() => {
        if (!category) return [];
        const out = [category.id];
        for (let i = 0; i < out.length; i++) {
            for (const c of categoryCache.items) if (c.parent_id === out[i]) out.push(c.id);
        }
        return out;
    });

    let inherited = $derived(parent !== null ? (categoryCache.items.find((c) => c.id === parent)?.effective_icon ?? null) : null);

    async function submit(e: Event) {
        e.preventDefault();
        busy = true;
        error = '';
        const body = { name, description, icon, structural, parent_id: parent };
        try {
            const c = category
                ? await api.patch<Category>(`/categories/${category.id}`, { ...body, version: category.version })
                : await api.post<Category>('/categories', body);
            invalidateCategories();
            open = false;
            onsaved(c);
        } catch (err) {
            error = errorMessage(err);
        } finally {
            busy = false;
        }
    }
</script>

<Modal bind:open title={category ? `Edit ${category.name}` : 'New category'} wide>
    <form id="cat-form" class="grid gap-4 sm:grid-cols-2" onsubmit={submit}>
        <div class="space-y-4">
            <div>
                <label class="label" for="c-name">Name</label>
                <input id="c-name" class="input" required maxlength="100" bind:value={name} placeholder="e.g. Switches" />
            </div>
            <div>
                <label class="label" for="c-parent">Inside</label>
                <CategoryPicker id="c-parent" bind:value={parent} allowNone noneLabel="Top level" exclude={excluded} />
            </div>
            <div>
                <label class="label" for="c-desc">Description</label>
                <textarea id="c-desc" class="input min-h-20" maxlength="10000" bind:value={description}></textarea>
            </div>
            <label class="flex items-start gap-2 text-sm">
                <input type="checkbox" class="mt-0.5" bind:checked={structural} />
                <span>
                    Structural
                    <span class="muted block text-xs">Groups other categories only; parts cannot be assigned to it.</span>
                </span>
            </label>
        </div>
        <div>
            <span class="label">Icon</span>
            <IconPicker bind:value={icon} {inherited} />
        </div>
        {#if error}<p class="text-sm text-red-600 sm:col-span-2">{error}</p>{/if}
    </form>
    {#snippet footer()}
        <button class="btn" onclick={() => (open = false)}>Cancel</button>
        <button class="btn btn-primary" type="submit" form="cat-form" disabled={busy || !name.trim()}>
            {category ? 'Save' : 'Create'}
        </button>
    {/snippet}
</Modal>
