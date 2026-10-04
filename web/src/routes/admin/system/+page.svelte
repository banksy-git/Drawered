<script lang="ts">
    import { onMount } from 'svelte';
    import { api, errorMessage } from '$lib/api';
    import { bytes } from '$lib/format';
    import { toast, toastError } from '$lib/toast.svelte';
    import type { SystemInfo } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';

    let info = $state<SystemInfo | null>(null);
    let error = $state('');
    let reindexing = $state(false);

    onMount(() => {
        api.get<SystemInfo>('/system')
            .then((i) => (info = i))
            .catch((e) => (error = errorMessage(e)));
    });

    async function reindex() {
        reindexing = true;
        try {
            await api.post('/system/reindex');
            toast('Search index rebuilt');
        } catch (e) {
            toastError(e);
        } finally {
            reindexing = false;
        }
    }
</script>

<svelte:head><title>System - Drawered</title></svelte:head>

<h1 class="mb-4 text-xl font-semibold">System</h1>
{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if info}
    <div class="grid gap-4 md:grid-cols-2">
        <section class="card p-4">
            <h2 class="mb-3 font-semibold">Overview</h2>
            <dl class="grid grid-cols-[10rem_1fr] gap-y-1.5 text-sm">
                <dt class="muted">Version</dt><dd>{info.version}</dd>
                <dt class="muted">Parts</dt><dd>{info.parts}</dd>
                <dt class="muted">Deleted parts</dt>
                <dd>{info.deleted_parts}{#if info.deleted_parts}<a class="link ml-2" href="/?deleted=only">view</a>{/if}</dd>
                <dt class="muted">Locations</dt><dd>{info.locations}</dd>
                <dt class="muted">Users</dt><dd>{info.users}</dd>
                <dt class="muted">Events</dt><dd>{info.events}</dd>
                <dt class="muted">Database size</dt><dd>{bytes(info.database_bytes)}</dd>
                <dt class="muted">File store size</dt><dd>{bytes(info.file_bytes)}</dd>
            </dl>
        </section>
        <section class="card space-y-4 p-4">
            <div>
                <h2 class="font-semibold">Backup</h2>
                <p class="muted mb-2 text-sm">
                    A consistent snapshot of the database and every uploaded file. Restore by extracting it into an
                    empty data directory.
                </p>
                <a class="btn" href="/api/v1/system/backup" download><Icon name="download" size={16} /> Download backup</a>
            </div>
            <div>
                <h2 class="font-semibold">Search index</h2>
                <p class="muted mb-2 text-sm">The index is kept current automatically. Rebuild it if search results look wrong.</p>
                <button class="btn" onclick={reindex} disabled={reindexing}>{reindexing ? 'Rebuilding...' : 'Rebuild index'}</button>
            </div>
        </section>
    </div>
{:else}
    <p class="muted text-sm">Loading...</p>
{/if}
