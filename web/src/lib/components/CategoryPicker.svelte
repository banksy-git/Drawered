<script lang="ts">
    import { onMount } from 'svelte';
    import { categoryCache, loadCategories } from '$lib/categories.svelte';
    import type { Category } from '$lib/types';
    import CategoryIcon from './CategoryIcon.svelte';
    import TreePicker from './TreePicker.svelte';

    let {
        value = $bindable(null),
        excludeStructural = false,
        exclude = [],
        allowNone = false,
        noneLabel = 'None',
        id,
        placeholder = 'Search categories, e.g. "elec sw two"'
    }: {
        value: number | null;
        excludeStructural?: boolean;
        exclude?: number[];
        allowNone?: boolean;
        noneLabel?: string;
        id?: string;
        placeholder?: string;
    } = $props();

    onMount(() => {
        loadCategories();
    });
</script>

{#snippet lead(c: Category)}<CategoryIcon icon={c.effective_icon} />{/snippet}

<TreePicker items={categoryCache.items} bind:value {excludeStructural} {exclude} {allowNone} {noneLabel} {id} {placeholder} {lead} />
