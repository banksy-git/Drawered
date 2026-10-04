<script lang="ts">
    import { onMount } from 'svelte';
    import { api, ApiError, errorMessage } from '$lib/api';
    import { can } from '$lib/session.svelte';
    import { toast, toastError } from '$lib/toast.svelte';
    import type { Permission, PermissionInfo, Role } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';

    let roles = $state<Role[]>([]);
    let perms = $state<PermissionInfo[]>([]);
    let selectedId = $state<number | 'new' | null>(null);
    let name = $state('');
    let description = $state('');
    let granted = $state<Permission[]>([]);
    let error = $state('');
    let busy = $state(false);

    let editable = $derived(can('roles:manage'));
    let selected = $derived(roles.find((r) => r.id === selectedId) ?? null);

    async function load() {
        const [r, p] = await Promise.all([api.get<{ items: Role[] }>('/roles'), api.get<PermissionInfo[]>('/permissions')]);
        roles = r.items;
        perms = p;
    }

    onMount(() => {
        load()
            .then(() => {
                if (roles.length) select(roles[0].id);
            })
            .catch((e) => (error = errorMessage(e)));
    });

    function select(id: number | 'new') {
        selectedId = id;
        const r = roles.find((x) => x.id === id);
        name = r?.name ?? '';
        description = r?.description ?? '';
        granted = r ? [...r.permissions] : ['parts:read', 'locations:read'];
        error = '';
    }

    let isAdmin = $derived(granted.includes('system:admin'));

    async function save() {
        busy = true;
        error = '';
        try {
            let r: Role;
            if (selectedId === 'new') {
                r = await api.post<Role>('/roles', { name, description, permissions: granted });
                toast(`Created ${r.name}`);
            } else {
                r = await api.patch<Role>(`/roles/${selectedId}`, { version: selected!.version, name, description, permissions: granted });
                toast('Role saved');
            }
            await load();
            select(r.id);
        } catch (e) {
            error = errorMessage(e);
        } finally {
            busy = false;
        }
    }

    async function remove() {
        if (!selected) return;
        if (!confirm(`Delete role ${selected.name}?`)) return;
        try {
            await api.del(`/roles/${selected.id}`);
        } catch (e) {
            if (e instanceof ApiError && e.code === 'role_in_use') {
                const d = e.details as { user_count: number; mapping_count: number };
                if (!confirm(`${selected.name} is held by ${d.user_count} user(s) and used by ${d.mapping_count} mapping(s). Remove it from all of them?`)) return;
                try {
                    await api.del(`/roles/${selected.id}?force=true`);
                } catch (e2) {
                    toastError(e2);
                    return;
                }
            } else {
                toastError(e);
                return;
            }
        }
        toast('Role deleted');
        await load();
        if (roles.length) select(roles[0].id);
    }
</script>

<svelte:head><title>Roles - Drawered</title></svelte:head>

<div class="grid gap-4 md:grid-cols-[14rem_1fr]">
    <aside class="card h-fit p-2">
        <ul>
            {#each roles as r (r.id)}
                <li>
                    <button
                        class="flex w-full cursor-pointer items-center gap-2 rounded px-3 py-2 text-left text-sm {selectedId === r.id
                            ? 'bg-zinc-100 font-medium dark:bg-zinc-800'
                            : 'hover:bg-zinc-50 dark:hover:bg-zinc-800/50'}"
                        onclick={() => select(r.id)}
                    >
                        {#if r.builtin}<Icon name="shield" size={14} />{/if}
                        <span class="flex-1 truncate">{r.name}</span>
                        <span class="muted text-xs">{r.user_count}</span>
                    </button>
                </li>
            {/each}
        </ul>
        {#if editable}
            <button class="btn btn-sm mt-2 w-full" onclick={() => select('new')}><Icon name="plus" size={14} /> New role</button>
        {/if}
    </aside>

    {#if selectedId !== null}
        <section class="card p-4">
            <div class="mb-4 grid gap-3 sm:grid-cols-2">
                <div>
                    <label class="label" for="r-name">Name</label>
                    <input id="r-name" class="input" maxlength="50" bind:value={name} disabled={!editable || selected?.builtin} />
                </div>
                <div>
                    <label class="label" for="r-desc">Description</label>
                    <input id="r-desc" class="input" maxlength="500" bind:value={description} disabled={!editable} />
                </div>
            </div>
            {#if selected}
                <p class="muted mb-3 text-sm">
                    Held by {selected.user_count} user{selected.user_count === 1 ? '' : 's'}, used by {selected.mapping_count} claim
                    mapping{selected.mapping_count === 1 ? '' : 's'}.
                    {#if selected.is_default}<strong>Given to new users by default.</strong>{/if}
                </p>
            {/if}
            <table class="table">
                <thead><tr><th class="w-10"></th><th>Permission</th><th class="hidden sm:table-cell">Grants</th></tr></thead>
                <tbody>
                    {#each perms as p (p.name)}
                        {@const implied = isAdmin && p.name !== 'system:admin'}
                        <tr>
                            <td>
                                <input
                                    type="checkbox"
                                    aria-label={p.name}
                                    value={p.name}
                                    bind:group={granted}
                                    disabled={!editable || (selected?.builtin && p.name === 'system:admin')}
                                />
                            </td>
                            <td class="font-mono text-xs {implied && !granted.includes(p.name) ? 'muted' : ''}">
                                {p.name}
                                {#if implied && !granted.includes(p.name)}<span class="muted font-sans">(implied)</span>{/if}
                            </td>
                            <td class="muted hidden text-sm sm:table-cell">{p.description}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
            {#if error}<p class="mt-3 text-sm text-red-600">{error}</p>{/if}
            {#if editable}
                <div class="mt-4 flex gap-2">
                    <button class="btn btn-primary" onclick={save} disabled={busy || !name.trim()}>
                        {selectedId === 'new' ? 'Create role' : 'Save'}
                    </button>
                    {#if selected && !selected.builtin}
                        <button class="btn ml-auto" onclick={remove}><Icon name="trash" size={16} /> Delete</button>
                    {/if}
                </div>
            {/if}
        </section>
    {/if}
</div>
