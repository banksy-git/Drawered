<script lang="ts">
    import { api } from '$lib/api';
    import type { CatalogueEntry, Page } from '$lib/types';
    import Icon from './Icon.svelte';

    let { value = $bindable([]), id }: { value: string[]; id?: string } = $props();

    let text = $state('');
    let options = $state<string[]>([]);
    let open = $state(false);
    let active = $state(-1);
    let timer: ReturnType<typeof setTimeout> | undefined;

    function add(t: string) {
        t = t.trim().toLowerCase().replace(/,/g, '');
        if (t && !value.includes(t)) value = [...value, t];
        text = '';
        options = [];
        active = -1;
    }

    function remove(t: string) {
        value = value.filter((v) => v !== t);
    }

    function refresh() {
        clearTimeout(timer);
        timer = setTimeout(async () => {
            try {
                const r = await api.get<Page<CatalogueEntry>>('/tags?limit=8&q=' + encodeURIComponent(text));
                options = r.items.map((t) => t.name).filter((t) => !value.includes(t));
            } catch {
                options = [];
            }
        }, 150);
    }

    function onkeydown(e: KeyboardEvent) {
        if (e.key === 'Enter' || e.key === ',' || e.key === 'Tab') {
            if (active >= 0 && options[active]) {
                add(options[active]);
                e.preventDefault();
            } else if (text.trim()) {
                add(text);
                e.preventDefault();
            }
        } else if (e.key === 'Backspace' && text === '' && value.length) {
            value = value.slice(0, -1);
        } else if (e.key === 'ArrowDown' && options.length) {
            active = (active + 1) % options.length;
            e.preventDefault();
        } else if (e.key === 'ArrowUp' && options.length) {
            active = (active - 1 + options.length) % options.length;
            e.preventDefault();
        }
    }
</script>

<div class="relative">
    <div
        class="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-zinc-300 bg-white px-2 py-1 shadow-xs focus-within:border-accent-500 dark:border-zinc-700 dark:bg-zinc-900"
    >
        {#each value as t (t)}
            <span class="badge">
                {t}
                <button type="button" class="cursor-pointer opacity-60 hover:opacity-100" onclick={() => remove(t)} aria-label="Remove {t}">
                    <Icon name="x" size={12} />
                </button>
            </span>
        {/each}
        <input
            {id}
            class="min-w-24 flex-1 border-0 bg-transparent py-0.5 text-sm outline-none"
            placeholder={value.length ? '' : 'Add tags'}
            autocomplete="off"
            bind:value={text}
            oninput={() => {
                open = true;
                refresh();
            }}
            onfocus={() => {
                open = true;
                refresh();
            }}
            onblur={() =>
                setTimeout(() => {
                    open = false;
                    if (text.trim()) add(text);
                }, 150)}
            {onkeydown}
        />
    </div>
    {#if open && options.length > 0}
        <ul
            role="listbox"
            class="absolute z-20 mt-1 max-h-60 w-full overflow-auto rounded-md border border-zinc-200 bg-white py-1 text-sm shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
        >
            {#each options as o, i (o)}
                <li role="option" aria-selected={i === active}>
                    <button
                        type="button"
                        class="block w-full px-3 py-1.5 text-left hover:bg-zinc-100 dark:hover:bg-zinc-800 {i === active
                            ? 'bg-zinc-100 dark:bg-zinc-800'
                            : ''}"
                        onmousedown={(e) => e.preventDefault()}
                        onclick={() => add(o)}>{o}</button
                    >
                </li>
            {/each}
        </ul>
    {/if}
</div>
