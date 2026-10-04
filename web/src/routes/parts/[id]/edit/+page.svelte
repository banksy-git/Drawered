<script lang="ts">
    import { goto } from '$app/navigation';
    import { page } from '$app/state';
    import { api, errorMessage } from '$lib/api';
    import { toast } from '$lib/toast.svelte';
    import type { Part } from '$lib/types';
    import PartForm from '$lib/components/PartForm.svelte';

    let part = $state<Part | null>(null);
    let error = $state('');

    $effect(() => {
        api.get<Part>(`/parts/${page.params.id}`)
            .then((p) => (part = p))
            .catch((e) => (error = errorMessage(e)));
    });
</script>

<svelte:head><title>Edit {part?.name ?? 'part'} - Drawered</title></svelte:head>

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if part}
    <h1 class="mb-4 text-xl font-semibold">Edit {part.name}</h1>
    {#key part.id}
        <PartForm
            {part}
            onsaved={(p) => {
                toast('Saved');
                if (p.warnings?.includes('duplicate_barcode')) toast('Another part already has this barcode', 'info', 6000);
                goto(`/parts/${p.id}`);
            }}
            oncancel={() => goto(`/parts/${part?.id}`)}
        />
    {/key}
{:else}
    <p class="muted text-sm">Loading...</p>
{/if}
