<script lang="ts">
    import { qs } from '$lib/api';
    import { actionGroups } from '$lib/events';
    import EventList from '$lib/components/EventList.svelte';

    let action = $state('');
    let from = $state('');
    let to = $state('');

    // The "to" date is inclusive in the UI, so query up to the next day.
    function nextDay(d: string): string {
        if (!d) return '';
        const t = new Date(d + 'T00:00:00Z');
        t.setUTCDate(t.getUTCDate() + 1);
        return t.toISOString().slice(0, 10);
    }

    let endpoint = $derived('/events' + qs({ action, from, to: nextDay(to) }));
</script>

<svelte:head><title>Activity - Drawered</title></svelte:head>

<div class="mb-4 flex flex-wrap items-end gap-3">
    <h1 class="mr-auto text-xl font-semibold">Activity</h1>
    <div>
        <label class="label" for="a-action">Show</label>
        <select id="a-action" class="input" bind:value={action}>
            {#each actionGroups as g (g.value)}<option value={g.value}>{g.label}</option>{/each}
        </select>
    </div>
    <div>
        <label class="label" for="a-from">From</label>
        <input id="a-from" type="date" class="input" bind:value={from} />
    </div>
    <div>
        <label class="label" for="a-to">To</label>
        <input id="a-to" type="date" class="input" bind:value={to} />
    </div>
</div>

<section class="card p-4">
    {#key endpoint}
        <EventList {endpoint} linkSubjects />
    {/key}
</section>
