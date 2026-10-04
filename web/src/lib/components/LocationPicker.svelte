<script lang="ts">
    import { onMount } from 'svelte';
    import { loadLocations, locationCache } from '$lib/locations.svelte';
    import type { Location } from '$lib/types';
    import Swatch from './Swatch.svelte';
    import TreePicker from './TreePicker.svelte';

    let {
        value = $bindable(null),
        excludeStructural = false,
        exclude = [],
        allowNone = false,
        noneLabel = 'None',
        id,
        placeholder = 'Search locations, e.g. "grey sh2 a1"'
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
        loadLocations();
    });
</script>

{#snippet lead(l: Location)}<Swatch colour={l.effective_colour} />{/snippet}

<TreePicker items={locationCache.items} bind:value {excludeStructural} {exclude} {allowNone} {noneLabel} {id} {placeholder} {lead} />
