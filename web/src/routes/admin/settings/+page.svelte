<script lang="ts">
    import { onMount } from 'svelte';
    import { api, errorMessage } from '$lib/api';
    import { toast, toastError } from '$lib/toast.svelte';
    import type { Role } from '$lib/types';

    let roles = $state<Role[]>([]);
    let defaultRole = $state<number | null>(null);
    let saved = $state<number | null>(null);
    let error = $state('');

    onMount(async () => {
        try {
            const [r, s] = await Promise.all([
                api.get<{ items: Role[] }>('/roles'),
                api.get<{ default_role_id: number | null }>('/settings')
            ]);
            roles = r.items;
            defaultRole = saved = s.default_role_id;
        } catch (e) {
            error = errorMessage(e);
        }
    });

    async function save() {
        try {
            const s = await api.patch<{ default_role_id: number | null }>('/settings', { default_role_id: defaultRole });
            defaultRole = saved = s.default_role_id;
            toast('Settings saved');
        } catch (e) {
            toastError(e);
        }
    }
</script>

<svelte:head><title>Settings - Drawered</title></svelte:head>

<h1 class="mb-4 text-xl font-semibold">Settings</h1>
{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else}
    <section class="card max-w-xl p-4">
        <label class="label" for="s-default">Default role for new users</label>
        <select id="s-default" class="input" bind:value={defaultRole}>
            <option value={null}>None - new users wait for an administrator</option>
            {#each roles as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
        </select>
        <p class="muted mt-2 text-sm">
            Given to each user the first time they log in. Changing it does not affect existing users. Claim mappings
            are applied as well.
        </p>
        <button class="btn btn-primary mt-4" disabled={defaultRole === saved} onclick={save}>Save</button>
    </section>
{/if}
