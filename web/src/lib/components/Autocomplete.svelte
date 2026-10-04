<script lang="ts">
    let {
        value = $bindable(''),
        suggest,
        id,
        placeholder = '',
        maxlength
    }: {
        value: string;
        suggest: (q: string) => Promise<string[]>;
        id?: string;
        placeholder?: string;
        maxlength?: number;
    } = $props();

    let options = $state<string[]>([]);
    let open = $state(false);
    let active = $state(-1);
    let timer: ReturnType<typeof setTimeout> | undefined;
    const listId = `ac-${Math.random().toString(36).slice(2)}`;

    function refresh() {
        clearTimeout(timer);
        timer = setTimeout(async () => {
            try {
                const q = value;
                const res = await suggest(q);
                options = res.filter((o) => o !== q).slice(0, 8);
                active = -1;
            } catch {
                options = [];
            }
        }, 150);
    }

    function choose(o: string) {
        value = o;
        open = false;
    }

    function onkeydown(e: KeyboardEvent) {
        if (!open || options.length === 0) return;
        if (e.key === 'ArrowDown') {
            active = (active + 1) % options.length;
            e.preventDefault();
        } else if (e.key === 'ArrowUp') {
            active = (active - 1 + options.length) % options.length;
            e.preventDefault();
        } else if (e.key === 'Enter' && active >= 0) {
            choose(options[active]);
            e.preventDefault();
        } else if (e.key === 'Escape') {
            open = false;
        }
    }
</script>

<div class="relative">
    <input
        {id}
        class="input"
        {placeholder}
        {maxlength}
        autocomplete="off"
        role="combobox"
        aria-expanded={open && options.length > 0}
        aria-controls={listId}
        bind:value
        oninput={() => {
            open = true;
            refresh();
        }}
        onfocus={() => {
            open = true;
            refresh();
        }}
        onblur={() => setTimeout(() => (open = false), 150)}
        {onkeydown}
    />
    {#if open && options.length > 0}
        <ul
            id={listId}
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
                        onclick={() => choose(o)}>{o}</button
                    >
                </li>
            {/each}
        </ul>
    {/if}
</div>
