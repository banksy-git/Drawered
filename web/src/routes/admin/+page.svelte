<script lang="ts">
    import { onMount } from 'svelte';
    import { goto } from '$app/navigation';
    import { can } from '$lib/session.svelte';
    import { adminSections } from './sections';

    onMount(() => {
        const first = adminSections.find((s) => s.perms.some((p) => can(p)));
        goto(first ? first.href : '/', { replaceState: true });
    });
</script>
