<script lang="ts">
    let {
        total,
        limit,
        offset = $bindable(0)
    }: { total: number; limit: number; offset: number } = $props();

    let from = $derived(total === 0 ? 0 : offset + 1);
    let to = $derived(Math.min(offset + limit, total));
</script>

{#if total > limit}
    <nav class="flex items-center justify-between gap-2 py-3 text-sm" aria-label="Pagination">
        <span class="muted">{from}-{to} of {total}</span>
        <div class="flex gap-2">
            <button class="btn btn-sm" disabled={offset === 0} onclick={() => (offset = Math.max(0, offset - limit))}>
                Previous
            </button>
            <button class="btn btn-sm" disabled={to >= total} onclick={() => (offset = offset + limit)}>Next</button>
        </div>
    </nav>
{:else if total > 0}
    <p class="muted py-3 text-sm">{total} {total === 1 ? 'result' : 'results'}</p>
{/if}
