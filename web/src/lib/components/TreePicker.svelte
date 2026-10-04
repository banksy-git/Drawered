<script lang="ts" generics="T extends { id: number; path: string; structural: boolean }">
    import type { Snippet } from 'svelte';
    import { pathMatches } from '$lib/format';
    import Icon from './Icon.svelte';

    // A searchable picker over a flattened tree (locations or categories).
    let {
        items,
        value = $bindable(null),
        excludeStructural = false,
        exclude = [],
        allowNone = false,
        noneLabel = 'None',
        id,
        placeholder,
        lead
    }: {
        items: T[];
        value: number | null;
        excludeStructural?: boolean;
        exclude?: number[];
        allowNone?: boolean;
        noneLabel?: string;
        id?: string;
        placeholder: string;
        lead: Snippet<[T]>;
    } = $props();

    let query = $state('');
    let open = $state(false);
    let active = $state(0);

    let selected = $derived(items.find((l) => l.id === value) ?? null);
    let matches = $derived(
        items
            .filter((l) => !(excludeStructural && l.structural) && !exclude.includes(l.id))
            .filter((l) => !query.trim() || pathMatches(l.path, query))
            .slice(0, 50)
    );

    function choose(id: number | null) {
        value = id;
        query = '';
        open = false;
    }

    function onkeydown(e: KeyboardEvent) {
        if (e.key === 'ArrowDown') {
            open = true;
            active = Math.min(active + 1, matches.length - 1);
            e.preventDefault();
        } else if (e.key === 'ArrowUp') {
            active = Math.max(active - 1, 0);
            e.preventDefault();
        } else if (e.key === 'Enter' && open && matches[active]) {
            choose(matches[active].id);
            e.preventDefault();
        } else if (e.key === 'Escape') {
            open = false;
        }
    }
</script>

<div class="relative">
    {#if selected && !open}
        <button
            type="button"
            {id}
            class="input flex cursor-pointer items-center gap-2 text-left"
            onclick={() => {
                open = true;
                active = 0;
            }}
        >
            {@render lead(selected)}
            <span class="truncate">{selected.path}</span>
            <Icon name="chevronDown" size={14} class="ml-auto opacity-50" />
        </button>
    {:else}
        <!-- svelte-ignore a11y_autofocus -->
        <input
            {id}
            class="input"
            {placeholder}
            autocomplete="off"
            autofocus={open && !!selected}
            bind:value={query}
            oninput={() => {
                open = true;
                active = 0;
            }}
            onfocus={() => (open = true)}
            onblur={() => setTimeout(() => (open = false), 150)}
            {onkeydown}
        />
    {/if}
    {#if open}
        <ul
            role="listbox"
            class="absolute z-30 mt-1 max-h-72 w-full overflow-auto rounded-md border border-zinc-200 bg-white py-1 text-sm shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
        >
            {#if allowNone}
                <li role="option" aria-selected={value === null}>
                    <button
                        type="button"
                        class="block w-full px-3 py-1.5 text-left italic hover:bg-zinc-100 dark:hover:bg-zinc-800"
                        onmousedown={(e) => e.preventDefault()}
                        onclick={() => choose(null)}>{noneLabel}</button
                    >
                </li>
            {/if}
            {#each matches as l, i (l.id)}
                <li role="option" aria-selected={l.id === value}>
                    <button
                        type="button"
                        class="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-zinc-100 dark:hover:bg-zinc-800 {i ===
                        active
                            ? 'bg-zinc-100 dark:bg-zinc-800'
                            : ''}"
                        onmousedown={(e) => e.preventDefault()}
                        onclick={() => choose(l.id)}
                    >
                        {@render lead(l)}
                        <span class="truncate">{l.path}</span>
                        {#if l.structural}<span class="badge ml-auto">structural</span>{/if}
                    </button>
                </li>
            {:else}
                <li class="muted px-3 py-2">No matches</li>
            {/each}
        </ul>
    {/if}
</div>
