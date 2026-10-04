<script lang="ts" module>
    export type StockMode = 'add' | 'remove' | 'move' | 'set';

    const REASONS_KEY = 'drawered-reasons';

    function recentReasons(): string[] {
        try {
            return JSON.parse(localStorage.getItem(REASONS_KEY) ?? '[]');
        } catch {
            return [];
        }
    }

    function rememberReason(r: string) {
        if (!r) return;
        try {
            const list = [r, ...recentReasons().filter((x) => x !== r)].slice(0, 5);
            localStorage.setItem(REASONS_KEY, JSON.stringify(list));
        } catch {
            // Storage unavailable; nothing to remember.
        }
    }
</script>

<script lang="ts">
    import Modal from './Modal.svelte';
    import Icon from './Icon.svelte';
    import LocationPicker from './LocationPicker.svelte';
    import { api, errorMessage } from '$lib/api';
    import { qty } from '$lib/format';
    import { toast } from '$lib/toast.svelte';
    import { invalidateLocations } from '$lib/locations.svelte';
    import type { Part } from '$lib/types';

    let {
        open = $bindable(false),
        part,
        mode,
        locationId = null,
        ondone
    }: {
        open: boolean;
        part: Part;
        mode: StockMode;
        locationId?: number | null;
        ondone: (p: Part) => void;
    } = $props();

    let loc = $state<number | null>(null);
    let toLoc = $state<number | null>(null);
    let amount = $state('1');
    let reason = $state('');
    let busy = $state(false);
    let error = $state('');
    let reasons = $state<string[]>([]);

    // Reset the form each time the dialog opens.
    $effect(() => {
        if (open) {
            loc = locationId ?? (mode !== 'add' && part.stock.length === 1 ? part.stock[0].location_id : null);
            if (mode === 'add' && loc === null && part.stock.length === 1) loc = part.stock[0].location_id;
            toLoc = null;
            const current = part.stock.find((s) => s.location_id === loc)?.quantity ?? 0;
            amount = mode === 'set' ? String(current) : '1';
            reason = '';
            error = '';
            reasons = recentReasons();
        }
    });

    const titles: Record<StockMode, string> = {
        add: 'Add stock',
        remove: 'Remove stock',
        move: 'Move stock',
        set: 'Set quantity (stock-take)'
    };

    let current = $derived(part.stock.find((s) => s.location_id === loc)?.quantity ?? 0);
    let parsed = $derived(Number(amount));
    let validAmount = $derived(
        amount.trim() !== '' &&
            Number.isFinite(parsed) &&
            parsed >= 0 &&
            (mode === 'set' || parsed > 0) &&
            (part.allow_fractional ? Math.round(parsed * 1000) === parsed * 1000 : Number.isInteger(parsed))
    );
    let result = $derived.by(() => {
        if (!validAmount) return null;
        switch (mode) {
            case 'add':
                return current + parsed;
            case 'remove':
            case 'move':
                return current - parsed;
            case 'set':
                return parsed;
        }
    });
    let canSubmit = $derived(
        validAmount && loc !== null && (result ?? -1) >= 0 && (mode !== 'move' || (toLoc !== null && toLoc !== loc))
    );

    function step(d: number) {
        const v = Number.isFinite(parsed) ? parsed : 0;
        amount = String(Math.max(0, Math.round((v + d) * 1000) / 1000));
    }

    async function submit(e: Event) {
        e.preventDefault();
        if (!canSubmit) return;
        busy = true;
        error = '';
        try {
            const body: Record<string, unknown> = { part_id: part.id, quantity: amount.trim(), reason };
            if (mode === 'move') {
                body.from_location_id = loc;
                body.to_location_id = toLoc;
            } else {
                body.location_id = loc;
            }
            const p = await api.post<Part>(`/stock/${mode}`, body);
            rememberReason(reason.trim());
            invalidateLocations();
            toast(`${titles[mode].replace(/ \(.*/, '')}: ${qty(parsed, part.uom)}`);
            open = false;
            ondone(p);
        } catch (err) {
            error = errorMessage(err);
        } finally {
            busy = false;
        }
    }
</script>

<Modal bind:open title={titles[mode]}>
    <form id="stock-form" onsubmit={submit} class="space-y-4">
        <p class="text-sm font-medium">{part.name}</p>

        <div>
            <label class="label" for="stock-loc">{mode === 'move' ? 'From' : 'Location'}</label>
            {#if mode === 'add'}
                <LocationPicker id="stock-loc" bind:value={loc} excludeStructural />
            {:else}
                <select id="stock-loc" class="input" bind:value={loc}>
                    <option value={null} disabled>Choose a location</option>
                    {#each part.stock as s (s.location_id)}
                        <option value={s.location_id}>{s.path} ({qty(s.quantity, part.uom)})</option>
                    {/each}
                </select>
            {/if}
        </div>

        {#if mode === 'move'}
            <div>
                <label class="label" for="stock-to">To</label>
                <LocationPicker id="stock-to" bind:value={toLoc} excludeStructural exclude={loc ? [loc] : []} />
            </div>
        {/if}

        <div>
            <label class="label" for="stock-qty">{mode === 'set' ? 'New quantity' : 'Quantity'} ({part.uom})</label>
            <div class="flex items-stretch gap-2">
                <button type="button" class="btn h-12 w-12 text-lg" onclick={() => step(-1)} aria-label="Decrease">
                    <Icon name="minus" />
                </button>
                <input
                    id="stock-qty"
                    class="input h-12 text-center text-lg font-semibold"
                    inputmode={part.allow_fractional ? 'decimal' : 'numeric'}
                    bind:value={amount}
                    autocomplete="off"
                />
                <button type="button" class="btn h-12 w-12 text-lg" onclick={() => step(1)} aria-label="Increase">
                    <Icon name="plus" />
                </button>
            </div>
            {#if amount.trim() !== '' && !validAmount}
                <p class="mt-1 text-xs text-red-600">
                    {part.allow_fractional ? 'Enter a number with up to 3 decimal places.' : 'Enter a whole number.'}
                </p>
            {/if}
        </div>

        <div>
            <label class="label" for="stock-reason">Reason (optional)</label>
            <input id="stock-reason" class="input" list="stock-reasons" maxlength="500" bind:value={reason} />
            <datalist id="stock-reasons">
                {#each reasons as r (r)}<option value={r}></option>{/each}
            </datalist>
        </div>

        {#if loc !== null && result !== null}
            <div class="rounded-md bg-zinc-100 px-3 py-2 text-sm dark:bg-zinc-800">
                {#if mode === 'move'}
                    Source: {qty(current, part.uom)} &rarr;
                    <strong class={result < 0 ? 'text-red-600' : ''}>{qty(result, part.uom)}</strong>
                {:else}
                    {qty(current, part.uom)} &rarr;
                    <strong class={result < 0 ? 'text-red-600' : ''}>{qty(result, part.uom)}</strong>
                {/if}
                {#if result < 0}<span class="text-red-600"> - not enough stock</span>{/if}
            </div>
        {/if}

        {#if error}
            <p class="flex items-center gap-2 text-sm text-red-600"><Icon name="alert" size={16} />{error}</p>
        {/if}
    </form>
    {#snippet footer()}
        <button class="btn" onclick={() => (open = false)}>Cancel</button>
        <button class="btn btn-primary" form="stock-form" type="submit" disabled={!canSubmit || busy}>
            {titles[mode].split(' ')[0]}
        </button>
    {/snippet}
</Modal>
