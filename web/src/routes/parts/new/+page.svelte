<script lang="ts">
    import { goto } from '$app/navigation';
    import { toast } from '$lib/toast.svelte';
    import PartForm from '$lib/components/PartForm.svelte';
</script>

<svelte:head><title>New part - Drawered</title></svelte:head>

<h1 class="mb-4 text-xl font-semibold">New part</h1>
<PartForm
    onsaved={(p) => {
        toast(`Created ${p.name}`);
        if (p.warnings?.includes('duplicate_barcode')) toast('Another part already has this barcode', 'info', 6000);
        goto(`/parts/${p.id}`);
    }}
    oncancel={() => history.back()}
/>
