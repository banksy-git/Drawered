<script lang="ts">
    import { untrack } from 'svelte';
    import { api, errorMessage } from '$lib/api';
    import { invalidateLocations, loadLocations, locationCache } from '$lib/locations.svelte';
    import type { Location } from '$lib/types';
    import Modal from './Modal.svelte';
    import LocationPicker from './LocationPicker.svelte';

    let {
        open = $bindable(false),
        location = null,
        parentId = null,
        onsaved
    }: {
        open: boolean;
        location?: Location | null;
        parentId?: number | null;
        onsaved: (l: Location) => void;
    } = $props();

    let name = $state('');
    let description = $state('');
    let colour = $state<string | null>(null);
    let structural = $state(false);
    let parent = $state<number | null>(null);
    let busy = $state(false);
    let error = $state('');

    $effect(() => {
        if (!open) return;
        untrack(() => {
            name = location?.name ?? '';
            description = location?.description ?? '';
            colour = location?.colour ?? null;
            structural = location?.structural ?? false;
            parent = location ? location.parent_id : parentId;
            error = '';
            loadLocations();
        });
    });

    // When editing, a location cannot move inside its own subtree.
    let excluded = $derived.by(() => {
        if (!location) return [];
        const out = [location.id];
        for (let i = 0; i < out.length; i++) {
            for (const l of locationCache.items) if (l.parent_id === out[i]) out.push(l.id);
        }
        return out;
    });

    async function submit(e: Event) {
        e.preventDefault();
        busy = true;
        error = '';
        const body = { name, description, colour, structural, parent_id: parent };
        try {
            const l = location
                ? await api.patch<Location>(`/locations/${location.id}`, { ...body, version: location.version })
                : await api.post<Location>('/locations', body);
            invalidateLocations();
            open = false;
            onsaved(l);
        } catch (err) {
            error = errorMessage(err);
        } finally {
            busy = false;
        }
    }
</script>

<Modal bind:open title={location ? `Edit ${location.name}` : 'New location'}>
    <form id="loc-form" class="space-y-4" onsubmit={submit}>
        <div>
            <label class="label" for="l-name">Name</label>
            <input id="l-name" class="input" required maxlength="100" bind:value={name} placeholder="e.g. Shelf 2" />
        </div>
        <div>
            <label class="label" for="l-parent">Inside</label>
            <LocationPicker id="l-parent" bind:value={parent} allowNone noneLabel="Top level" exclude={excluded} />
        </div>
        <div>
            <label class="label" for="l-desc">Description</label>
            <textarea id="l-desc" class="input min-h-20" maxlength="10000" bind:value={description}></textarea>
        </div>
        <div class="flex items-center gap-3">
            <div>
                <label class="label" for="l-colour">Colour</label>
                <div class="flex items-center gap-2">
                    <input
                        id="l-colour"
                        type="color"
                        class="h-9 w-14 cursor-pointer rounded border border-zinc-300 bg-white dark:border-zinc-700 dark:bg-zinc-900"
                        value={colour ?? '#6366f1'}
                        oninput={(e) => (colour = e.currentTarget.value)}
                    />
                    {#if colour}
                        <code class="text-xs">{colour}</code>
                        <button type="button" class="btn btn-ghost btn-sm" onclick={() => (colour = null)}>Clear</button>
                    {:else}
                        <span class="muted text-xs">None (inherits from parent)</span>
                    {/if}
                </div>
            </div>
        </div>
        <label class="flex items-start gap-2 text-sm">
            <input type="checkbox" class="mt-0.5" bind:checked={structural} />
            <span>
                Structural
                <span class="muted block text-xs">Holds only other locations, never parts (e.g. a room or cabinet).</span>
            </span>
        </label>
        {#if error}<p class="text-sm text-red-600">{error}</p>{/if}
    </form>
    {#snippet footer()}
        <button class="btn" onclick={() => (open = false)}>Cancel</button>
        <button class="btn btn-primary" type="submit" form="loc-form" disabled={busy || !name.trim()}>
            {location ? 'Save' : 'Create'}
        </button>
    {/snippet}
</Modal>
