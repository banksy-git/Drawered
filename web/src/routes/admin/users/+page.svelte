<script lang="ts">
    import { api, errorMessage, qs } from '$lib/api';
    import { relative } from '$lib/format';
    import type { Page, User } from '$lib/types';
    import Pager from '$lib/components/Pager.svelte';

    const limit = 50;
    let q = $state('');
    let offset = $state(0);
    let users = $state<Page<User> | null>(null);
    let error = $state('');

    $effect(() => {
        const url = '/users' + qs({ q, limit, offset });
        api.get<Page<User>>(url)
            .then((r) => (users = r))
            .catch((e) => (error = errorMessage(e)));
    });
</script>

<svelte:head><title>Users - Drawered</title></svelte:head>

<div class="mb-3 flex flex-wrap items-center gap-2">
    <h1 class="mr-auto text-xl font-semibold">Users</h1>
    <input class="input w-full sm:w-64" placeholder="Search name or email" bind:value={q} oninput={() => (offset = 0)} aria-label="Search users" />
</div>
<p class="muted mb-3 text-sm">Accounts are created automatically on first login. Names and emails come from the identity provider.</p>

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if users}
    <div class="card overflow-x-auto">
        <table class="table">
            <thead><tr><th>Name</th><th class="hidden md:table-cell">Email</th><th>Roles</th><th class="hidden sm:table-cell">Last login</th></tr></thead>
            <tbody>
                {#each users.items as u (u.id)}
                    <tr class={u.disabled ? 'opacity-50' : ''}>
                        <td>
                            <a class="link font-medium" href="/admin/users/{u.id}">{u.display_name}</a>
                            {#if u.disabled}<span class="badge ml-1">disabled</span>{/if}
                        </td>
                        <td class="muted hidden md:table-cell">{u.email}</td>
                        <td>
                            <div class="flex flex-wrap gap-1">
                                {#each u.roles as r (r.role_id + r.source)}
                                    <span class="badge" title="Source: {r.source}">{r.name}{r.source !== 'manual' ? ' (IdP)' : ''}</span>
                                {:else}
                                    <span class="muted text-xs">No access</span>
                                {/each}
                            </div>
                        </td>
                        <td class="muted hidden text-xs sm:table-cell">{relative(u.last_login_at)}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </div>
    <Pager total={users.total} {limit} bind:offset />
{/if}
