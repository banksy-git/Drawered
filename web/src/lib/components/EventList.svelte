<script lang="ts">
    import { api, errorMessage } from '$lib/api';
    import { describe, subjectLink } from '$lib/events';
    import { dateTime, relative } from '$lib/format';
    import type { AppEvent, Page } from '$lib/types';
    import Pager from './Pager.svelte';

    // endpoint is an API path returning Page<AppEvent>; it may carry a query.
    let { endpoint, linkSubjects = false }: { endpoint: string; linkSubjects?: boolean } = $props();

    const limit = 50;
    let offset = $state(0);
    let page = $state<Page<AppEvent> | null>(null);
    let error = $state('');
    let expanded = $state<Record<number, boolean>>({});

    $effect(() => {
        // Reset paging when the endpoint (filters) changes.
        endpoint;
        offset = 0;
    });

    $effect(() => {
        const sep = endpoint.includes('?') ? '&' : '?';
        const url = `${endpoint}${sep}limit=${limit}&offset=${offset}`;
        error = '';
        api.get<Page<AppEvent>>(url)
            .then((p) => (page = p))
            .catch((e) => (error = errorMessage(e)));
    });

    function fmt(v: unknown): string {
        if (v === null || v === undefined || v === '') return '(none)';
        return String(v);
    }
</script>

{#if error}
    <p class="text-sm text-red-600">{error}</p>
{:else if !page}
    <p class="muted text-sm">Loading history...</p>
{:else if page.items.length === 0}
    <p class="muted text-sm">Nothing has happened yet.</p>
{:else}
    <ol class="divide-y divide-zinc-100 dark:divide-zinc-800">
        {#each page.items as e (e.id)}
            {@const link = linkSubjects ? subjectLink(e) : null}
            {@const changes = e.data?.changes as Record<string, { from: unknown; to: unknown }> | undefined}
            <li class="py-2.5 text-sm">
                <div class="flex flex-wrap items-baseline justify-between gap-x-3">
                    <p>
                        <span class="font-medium">{e.actor?.name ?? 'System'}</span>
                        {#if link}
                            <a class="link" href={link}>{describe(e)}</a>
                        {:else}
                            {describe(e)}
                        {/if}
                    </p>
                    <time class="muted shrink-0 text-xs" datetime={e.occurred_at} title={dateTime(e.occurred_at)}>
                        {relative(e.occurred_at)}
                    </time>
                </div>
                {#if e.data?.reason}
                    <p class="muted mt-0.5 text-xs">Reason: {e.data.reason}</p>
                {/if}
                {#if changes && Object.keys(changes).length}
                    <button
                        class="link mt-0.5 cursor-pointer text-xs"
                        onclick={() => (expanded[e.id] = !expanded[e.id])}
                        aria-expanded={!!expanded[e.id]}
                    >
                        {expanded[e.id] ? 'Hide' : 'Show'}
                        {Object.keys(changes).length}
                        {Object.keys(changes).length === 1 ? 'change' : 'changes'}
                    </button>
                    {#if expanded[e.id]}
                        <dl class="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 rounded bg-zinc-50 p-2 text-xs dark:bg-zinc-800/50">
                            {#each Object.entries(changes) as [field, c] (field)}
                                <dt class="muted">{field.replace(/_/g, ' ')}</dt>
                                <dd class="break-words">
                                    <span class="line-through opacity-60">{fmt(c.from)}</span>
                                    &rarr; {fmt(c.to)}
                                </dd>
                            {/each}
                        </dl>
                    {/if}
                {/if}
            </li>
        {/each}
    </ol>
    <Pager total={page.total} {limit} bind:offset />
{/if}
