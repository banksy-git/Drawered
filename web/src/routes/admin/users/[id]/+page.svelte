<script lang="ts">
    import { page } from '$app/state';
    import { api, errorMessage } from '$lib/api';
    import { can, session } from '$lib/session.svelte';
    import { toast, toastError } from '$lib/toast.svelte';
    import { dateTime, relative } from '$lib/format';
    import type { Role, Session, User } from '$lib/types';
    import EventList from '$lib/components/EventList.svelte';

    let user = $state<User | null>(null);
    let roles = $state<Role[]>([]);
    let sessions = $state<Session[]>([]);
    let manual = $state<number[]>([]);
    let error = $state('');
    let historyKey = $state(0);

    let id = $derived(Number(page.params.id));

    function setUser(u: User) {
        user = u;
        manual = u.roles.filter((r) => r.source === 'manual').map((r) => r.role_id);
        historyKey++;
    }

    $effect(() => {
        api.get<User>(`/users/${id}`)
            .then(setUser)
            .catch((e) => (error = errorMessage(e)));
        api.get<{ items: Role[] }>('/roles').then((r) => (roles = r.items));
        api.get<{ items: Session[] }>(`/users/${id}/sessions`).then((r) => (sessions = r.items));
    });

    let idpRoles = $derived(user?.roles.filter((r) => r.source !== 'manual') ?? []);
    let dirty = $derived(
        !!user &&
            JSON.stringify([...manual].sort()) !==
                JSON.stringify(
                    user.roles
                        .filter((r) => r.source === 'manual')
                        .map((r) => r.role_id)
                        .sort()
                )
    );

    async function saveRoles() {
        try {
            setUser(await api.put<User>(`/users/${id}/roles`, { role_ids: manual }));
            toast('Roles updated');
        } catch (e) {
            toastError(e);
        }
    }

    async function setDisabled(disabled: boolean) {
        if (!user) return;
        if (disabled && !confirm(`Disable ${user.display_name}? They will be logged out everywhere.`)) return;
        try {
            setUser(await api.post<User>(`/users/${id}/${disabled ? 'disable' : 'enable'}`));
            if (disabled) sessions = [];
        } catch (e) {
            toastError(e);
        }
    }

    async function revoke(s: Session) {
        try {
            await api.del(`/users/${id}/sessions/${s.id}`);
            sessions = sessions.filter((x) => x.id !== s.id);
            historyKey++;
        } catch (e) {
            toastError(e);
        }
    }
</script>

<svelte:head><title>{user?.display_name ?? 'User'} - Drawered</title></svelte:head>

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if user}
    <div class="mb-4 flex flex-wrap items-start gap-3">
        <div class="mr-auto">
            <h1 class="text-xl font-semibold">
                {user.display_name}
                {#if user.disabled}<span class="badge ml-1">disabled</span>{/if}
            </h1>
            <p class="muted text-sm">{user.email}</p>
        </div>
        {#if user.id !== session.me?.user.id}
            {#if user.disabled}
                <button class="btn" onclick={() => setDisabled(false)}>Enable</button>
            {:else}
                <button class="btn btn-danger" onclick={() => setDisabled(true)}>Disable</button>
            {/if}
        {/if}
    </div>

    <div class="grid gap-4 lg:grid-cols-2">
        <section class="card p-4">
            <h2 class="mb-3 font-semibold">Identity (from the identity provider)</h2>
            <dl class="grid grid-cols-[8rem_1fr] gap-y-1.5 text-sm">
                <dt class="muted">Username</dt><dd>{user.username || '-'}</dd>
                <dt class="muted">Issuer</dt><dd class="break-all">{user.issuer}</dd>
                <dt class="muted">Subject</dt><dd class="font-mono text-xs break-all">{user.subject}</dd>
                <dt class="muted">Groups</dt><dd>{user.groups.length ? user.groups.join(', ') : '-'}</dd>
                <dt class="muted">First login</dt><dd>{dateTime(user.first_login_at)}</dd>
                <dt class="muted">Last login</dt><dd>{dateTime(user.last_login_at)}</dd>
            </dl>
        </section>

        <section class="card p-4">
            <h2 class="mb-3 font-semibold">Roles</h2>
            <fieldset class="space-y-1.5">
                <legend class="label">Assigned here</legend>
                {#each roles as r (r.id)}
                    <label class="flex items-center gap-2 text-sm">
                        <input type="checkbox" value={r.id} bind:group={manual} />
                        {r.name}
                        <span class="muted text-xs">{r.description}</span>
                    </label>
                {/each}
            </fieldset>
            <div class="mt-3"><button class="btn btn-primary btn-sm" disabled={!dirty} onclick={saveRoles}>Save roles</button></div>
            {#if idpRoles.length}
                <p class="label mt-4">From the identity provider (recomputed at each login)</p>
                <div class="flex flex-wrap gap-1">
                    {#each idpRoles as r (r.role_id + r.source)}
                        <span class="badge">{r.name} ({r.source === 'bootstrap' ? 'bootstrap admin' : 'claim mapping'})</span>
                    {/each}
                </div>
            {/if}
            <p class="label mt-4">Effective permissions</p>
            <div class="flex flex-wrap gap-1">
                {#each user.permissions as p (p)}<code class="badge font-mono">{p}</code>{:else}<span class="muted text-sm">None</span>{/each}
            </div>
        </section>

        <section class="card p-4 lg:col-span-2">
            <h2 class="mb-3 font-semibold">Sessions</h2>
            {#if sessions.length}
                <ul class="divide-y divide-zinc-100 text-sm dark:divide-zinc-800">
                    {#each sessions as s (s.id)}
                        <li class="flex flex-wrap items-center gap-2 py-2">
                            <span class="min-w-0 flex-1 truncate" title={s.user_agent}>{s.user_agent || 'Unknown browser'}</span>
                            <span class="muted text-xs">active {relative(s.last_seen_at)}</span>
                            {#if s.current}
                                <span class="badge">this session</span>
                            {:else}
                                <button class="btn btn-sm" onclick={() => revoke(s)}>Revoke</button>
                            {/if}
                        </li>
                    {/each}
                </ul>
            {:else}
                <p class="muted text-sm">No active sessions.</p>
            {/if}
        </section>
    </div>

    {#if can('events:read')}
        <section class="card mt-4 p-4">
            <h2 class="mb-2 font-semibold">History</h2>
            {#key historyKey}<EventList endpoint="/users/{user.id}/events" />{/key}
            <p class="mt-3 text-sm"><a class="link" href="/activity">Everything this user did is in Activity.</a></p>
        </section>
    {/if}
{:else}
    <p class="muted text-sm">Loading...</p>
{/if}
