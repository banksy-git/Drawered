<script lang="ts">
    import { onMount } from 'svelte';
    import { api, errorMessage, qs } from '$lib/api';
    import { toast, toastError } from '$lib/toast.svelte';
    import type { Mapping, Page, Ref, Role, User } from '$lib/types';
    import Icon from '$lib/components/Icon.svelte';
    import Modal from '$lib/components/Modal.svelte';

    let mappings = $state<Mapping[]>([]);
    let roles = $state<Role[]>([]);
    let error = $state('');

    // Editor.
    let editOpen = $state(false);
    let editing = $state<Mapping | null>(null);
    let claim = $state('groups');
    let matchType = $state<Mapping['match_type']>('contains');
    let value = $state('');
    let roleId = $state<number | null>(null);
    let sortOrder = $state(0);
    let formError = $state('');

    // Tester.
    let testClaims = $state('{\n  "email": "someone@example.com",\n  "groups": ["inventory-stores"]\n}');
    let testUserQuery = $state('');
    let testUsers = $state<User[]>([]);
    let testResult = $state<{ roles: Ref[]; matched: Mapping[]; claims: Record<string, unknown> } | null>(null);
    let testError = $state('');

    async function load() {
        const [m, r] = await Promise.all([api.get<{ items: Mapping[] }>('/role-mappings'), api.get<{ items: Role[] }>('/roles')]);
        mappings = m.items;
        roles = r.items;
    }

    onMount(() => {
        load().catch((e) => (error = errorMessage(e)));
    });

    function edit(m: Mapping | null) {
        editing = m;
        claim = m?.claim ?? 'groups';
        matchType = m?.match_type ?? 'contains';
        value = m?.value ?? '';
        roleId = m?.role_id ?? roles[0]?.id ?? null;
        sortOrder = m?.sort_order ?? mappings.length;
        formError = '';
        editOpen = true;
    }

    async function save() {
        const body = { claim, match_type: matchType, value, role_id: roleId, sort_order: sortOrder };
        try {
            if (editing) await api.patch(`/role-mappings/${editing.id}`, body);
            else await api.post('/role-mappings', body);
            editOpen = false;
            toast('Mapping saved');
            await load();
        } catch (e) {
            formError = errorMessage(e);
        }
    }

    async function remove(m: Mapping) {
        if (!confirm('Delete this mapping? Users keep the role until their next login.')) return;
        try {
            await api.del(`/role-mappings/${m.id}`);
            await load();
        } catch (e) {
            toastError(e);
        }
    }

    async function runTest(body: unknown) {
        testError = '';
        try {
            testResult = await api.post('/role-mappings/test', body);
        } catch (e) {
            testError = errorMessage(e);
        }
    }

    function testClaimsJson() {
        try {
            runTest({ claims: JSON.parse(testClaims) });
        } catch {
            testError = 'Claims must be valid JSON.';
        }
    }

    async function findUsers() {
        const r = await api.get<Page<User>>('/users' + qs({ q: testUserQuery, limit: 8 }));
        testUsers = r.items;
    }

    const matchLabels: Record<Mapping['match_type'], string> = {
        equals: 'equals',
        contains: 'contains',
        ends_with: 'ends with',
        regex: 'matches regex'
    };
</script>

<svelte:head><title>Claim mappings - Drawered</title></svelte:head>

<div class="mb-3 flex flex-wrap items-center gap-2">
    <h1 class="mr-auto text-xl font-semibold">Claim mappings</h1>
    <button class="btn btn-primary" onclick={() => edit(null)}><Icon name="plus" size={16} /> Add mapping</button>
</div>
<p class="muted mb-4 text-sm">
    At each login, every rule is checked against the user's OIDC claims and matching roles are granted. Use a dotted
    claim name such as <code>realm_access.roles</code> for nested claims. Roles from mappings cannot be removed by hand.
</p>

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else}
    <div class="card overflow-x-auto">
        <table class="table">
            <thead><tr><th>#</th><th>Rule</th><th>Grants</th><th></th></tr></thead>
            <tbody>
                {#each mappings as m (m.id)}
                    <tr>
                        <td class="muted text-xs">{m.sort_order}</td>
                        <td><code>{m.claim}</code> {matchLabels[m.match_type]} <code>{m.value}</code></td>
                        <td><span class="badge">{m.role_name}</span></td>
                        <td class="text-right whitespace-nowrap">
                            <button class="btn btn-ghost btn-sm" onclick={() => edit(m)} aria-label="Edit"><Icon name="edit" size={14} /></button>
                            <button class="btn btn-ghost btn-sm" onclick={() => remove(m)} aria-label="Delete"><Icon name="trash" size={14} /></button>
                        </td>
                    </tr>
                {:else}
                    <tr><td colspan="4" class="muted">No mappings. Roles are assigned by hand on the Users page.</td></tr>
                {/each}
            </tbody>
        </table>
    </div>

    <section class="card mt-4 p-4">
        <h2 class="mb-1 font-semibold">Test the rules</h2>
        <p class="muted mb-3 text-sm">Paste a claim set, or check an existing user's most recent claims.</p>
        <div class="grid gap-4 md:grid-cols-2">
            <div>
                <label class="label" for="t-claims">Claims (JSON)</label>
                <textarea id="t-claims" class="input min-h-32 font-mono text-xs" bind:value={testClaims}></textarea>
                <button class="btn btn-sm mt-2" onclick={testClaimsJson}>Test claims</button>
            </div>
            <div>
                <label class="label" for="t-user">Existing user</label>
                <div class="flex gap-2">
                    <input id="t-user" class="input" placeholder="Name or email" bind:value={testUserQuery} />
                    <button class="btn" onclick={findUsers}>Find</button>
                </div>
                <ul class="mt-2 space-y-1">
                    {#each testUsers as u (u.id)}
                        <li><button class="link cursor-pointer text-sm" onclick={() => runTest({ user_id: u.id })}>{u.display_name} ({u.email})</button></li>
                    {/each}
                </ul>
            </div>
        </div>
        {#if testError}<p class="mt-3 text-sm text-red-600">{testError}</p>{/if}
        {#if testResult}
            <div class="mt-4 rounded-md bg-zinc-50 p-3 text-sm dark:bg-zinc-800/50">
                <p>
                    <strong>Roles granted:</strong>
                    {#each testResult.roles as r (r.id)}<span class="badge ml-1">{r.name}</span>{:else}<span class="muted">none</span>{/each}
                </p>
                {#if testResult.matched.length}
                    <p class="muted mt-1 text-xs">
                        Matched: {testResult.matched.map((m) => `${m.claim} ${matchLabels[m.match_type]} ${m.value}`).join('; ')}
                    </p>
                {/if}
            </div>
        {/if}
    </section>
{/if}

<Modal bind:open={editOpen} title={editing ? 'Edit mapping' : 'Add mapping'}>
    <div class="space-y-3">
        <div>
            <label class="label" for="m-claim">Claim</label>
            <input id="m-claim" class="input font-mono" bind:value={claim} />
        </div>
        <div>
            <label class="label" for="m-type">Match</label>
            <select id="m-type" class="input" bind:value={matchType}>
                <option value="contains">contains (any value of a list claim)</option>
                <option value="equals">equals</option>
                <option value="ends_with">ends with (case-insensitive)</option>
                <option value="regex">matches regular expression</option>
            </select>
        </div>
        <div>
            <label class="label" for="m-value">Value</label>
            <input id="m-value" class="input font-mono" bind:value placeholder={matchType === 'ends_with' ? '@example.com' : 'inventory-stores'} />
        </div>
        <div class="grid grid-cols-[1fr_6rem] gap-2">
            <div>
                <label class="label" for="m-role">Grant role</label>
                <select id="m-role" class="input" bind:value={roleId}>
                    {#each roles as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
                </select>
            </div>
            <div>
                <label class="label" for="m-order">Order</label>
                <input id="m-order" type="number" class="input" bind:value={sortOrder} />
            </div>
        </div>
        {#if formError}<p class="text-sm text-red-600">{formError}</p>{/if}
    </div>
    {#snippet footer()}
        <button class="btn" onclick={() => (editOpen = false)}>Cancel</button>
        <button class="btn btn-primary" onclick={save} disabled={!claim.trim() || !value.trim() || roleId === null}>Save</button>
    {/snippet}
</Modal>
