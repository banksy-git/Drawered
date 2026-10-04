<script lang="ts">
    import '../app.css';
    import '@tabler/icons-webfont/dist/tabler-icons.min.css';
    import { onMount, type Snippet } from 'svelte';
    import { goto } from '$app/navigation';
    import { page } from '$app/state';
    import { api, ApiError, logoutRequest, qs, setCsrfToken } from '$lib/api';
    import { can, session } from '$lib/session.svelte';
    import { applyTheme, getTheme, watchSystemTheme, type ThemePref } from '$lib/theme';
    import type { Me, Ref } from '$lib/types';
    import Icon, { type IconName } from '$lib/components/Icon.svelte';
    import Toasts from '$lib/components/Toasts.svelte';

    let { children }: { children: Snippet } = $props();

    let status = $state<'loading' | 'ready' | 'error'>('loading');
    let loadError = $state('');
    let navOpen = $state(false);
    let theme = $state<ThemePref>('system');
    let search = $state('');
    let searchEl: HTMLInputElement | undefined = $state();
    let timer: ReturnType<typeof setTimeout> | undefined;

    onMount(async () => {
        theme = getTheme();
        watchSystemTheme();
        try {
            const me = await api.get<Me>('/me');
            setCsrfToken(me.csrf_token);
            session.me = me;
            status = 'ready';
        } catch (e) {
            if (e instanceof ApiError && e.status === 401) return; // Redirecting to login.
            loadError = e instanceof Error ? e.message : String(e);
            status = 'error';
        }
    });

    // Keep the search box in step with the URL on the search page.
    $effect(() => {
        if (page.url.pathname === '/') search = page.url.searchParams.get('q') ?? '';
    });

    let waiting = $derived(status === 'ready' && (session.me?.permissions.length ?? 0) === 0);

    interface NavItem {
        href: string;
        label: string;
        icon: IconName;
        show: boolean;
    }
    let nav = $derived<NavItem[]>([
        { href: '/', label: 'Parts', icon: 'box', show: can('parts:read') },
        { href: '/categories', label: 'Categories', icon: 'folder', show: can('parts:read') },
        { href: '/locations', label: 'Locations', icon: 'map', show: can('locations:read') },
        { href: '/activity', label: 'Activity', icon: 'clock', show: can('events:read') },
        {
            href: '/admin',
            label: 'Admin',
            icon: 'settings',
            show: can('users:manage') || can('roles:manage') || can('catalogue:manage') || can('system:admin')
        }
    ]);

    function active(href: string): boolean {
        if (href === '/') return page.url.pathname === '/' || page.url.pathname.startsWith('/parts');
        return page.url.pathname.startsWith(href);
    }

    function searchUrl(q: string): string {
        const params = new URLSearchParams(page.url.pathname === '/' ? page.url.search : '');
        if (q) params.set('q', q);
        else params.delete('q');
        params.delete('offset');
        const s = params.toString();
        return '/' + (s ? '?' + s : '');
    }

    function oninput() {
        clearTimeout(timer);
        timer = setTimeout(() => {
            goto(searchUrl(search.trim()), { replaceState: page.url.pathname === '/', keepFocus: true, noScroll: true });
        }, 200);
    }

    // Enter checks for an exact barcode / MPN / SKU match first, which makes
    // keyboard-wedge barcode scanners jump straight to the part.
    async function onsubmit(e: Event) {
        e.preventDefault();
        clearTimeout(timer);
        const q = search.trim();
        if (q) {
            try {
                const r = await api.get<{ items: Ref[] }>('/parts/lookup' + qs({ code: q }));
                if (r.items.length === 1) {
                    search = '';
                    goto(`/parts/${r.items[0].id}`);
                    return;
                }
            } catch {
                // Fall back to text search.
            }
        }
        goto(searchUrl(q), { keepFocus: true });
    }

    function onkeydown(e: KeyboardEvent) {
        const t = e.target as HTMLElement;
        if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName) && !t.isContentEditable) {
            e.preventDefault();
            searchEl?.focus();
        }
    }

    function cycleTheme() {
        theme = theme === 'system' ? 'light' : theme === 'light' ? 'dark' : 'system';
        applyTheme(theme);
    }

    async function logout() {
        try {
            location.href = await logoutRequest();
        } catch {
            location.href = '/';
        }
    }
</script>

<svelte:window {onkeydown} />

{#if status === 'loading'}
    <div class="flex min-h-screen items-center justify-center">
        <p class="muted text-sm">Loading Drawered...</p>
    </div>
{:else if status === 'error'}
    <div class="flex min-h-screen flex-col items-center justify-center gap-3 p-4 text-center">
        <p class="font-medium">Drawered could not start.</p>
        <p class="muted text-sm">{loadError}</p>
        <button class="btn" onclick={() => location.reload()}>Try again</button>
    </div>
{:else if waiting}
    <div class="flex min-h-screen flex-col items-center justify-center gap-3 p-4 text-center">
        <Icon name="shield" size={40} class="text-accent-600" />
        <h1 class="text-xl font-semibold">Waiting for access</h1>
        <p class="muted max-w-sm text-sm">
            You are signed in as {session.me?.user.display_name}, but you have not been given a role yet. Ask an
            administrator to grant you access, then reload this page.
        </p>
        <div class="flex gap-2">
            <button class="btn" onclick={() => location.reload()}>Reload</button>
            <button class="btn" onclick={logout}>Log out</button>
        </div>
    </div>
{:else}
    <div class="min-h-screen md:flex">
        <!-- Sidebar (desktop) / drawer (mobile). -->
        <aside
            class="fixed inset-y-0 left-0 z-40 w-60 -translate-x-full border-r border-zinc-200 bg-white transition-transform md:sticky md:top-0 md:h-screen md:translate-x-0 dark:border-zinc-800 dark:bg-zinc-900 {navOpen
                ? 'translate-x-0'
                : ''}"
        >
            <div class="flex h-14 items-center gap-2 px-4">
                <img src="/favicon.svg" alt="" class="h-7 w-7" />
                <a href="/" class="text-lg font-semibold tracking-tight">Drawered</a>
            </div>
            <nav class="flex flex-col gap-0.5 px-2 py-2" aria-label="Main">
                {#each nav.filter((n) => n.show) as n (n.href)}
                    <a
                        href={n.href}
                        onclick={() => (navOpen = false)}
                        class="flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium {active(n.href)
                            ? 'bg-accent-50 text-accent-700 dark:bg-indigo-500/15 dark:text-indigo-300'
                            : 'text-zinc-700 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-zinc-800'}"
                        aria-current={active(n.href) ? 'page' : undefined}
                    >
                        <Icon name={n.icon} />
                        {n.label}
                    </a>
                {/each}
            </nav>
            <div class="absolute inset-x-0 bottom-0 border-t border-zinc-200 p-3 text-sm dark:border-zinc-800">
                <p class="truncate font-medium">{session.me?.user.display_name}</p>
                <p class="muted truncate text-xs">{session.me?.user.email}</p>
                <div class="mt-2 flex gap-1">
                    <button class="btn btn-ghost btn-sm" onclick={cycleTheme} title="Theme: {theme}" aria-label="Theme: {theme}">
                        <Icon name={theme === 'dark' ? 'moon' : theme === 'light' ? 'sun' : 'monitor'} size={16} />
                        <span class="capitalize">{theme}</span>
                    </button>
                    <button class="btn btn-ghost btn-sm ml-auto" onclick={logout}>
                        <Icon name="logout" size={16} /> Log out
                    </button>
                </div>
            </div>
        </aside>
        {#if navOpen}
            <button class="fixed inset-0 z-30 bg-zinc-950/40 md:hidden" aria-label="Close menu" onclick={() => (navOpen = false)}></button>
        {/if}

        <div class="min-w-0 flex-1">
            <header
                class="sticky top-0 z-20 flex h-14 items-center gap-2 border-b border-zinc-200 bg-white/90 px-4 backdrop-blur dark:border-zinc-800 dark:bg-zinc-900/90"
            >
                <button class="btn btn-ghost btn-sm md:hidden" onclick={() => (navOpen = true)} aria-label="Open menu">
                    <Icon name="menu" />
                </button>
                {#if can('parts:read')}
                    <form class="relative max-w-xl flex-1" role="search" {onsubmit}>
                        <Icon name="search" size={16} class="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-zinc-400" />
                        <input
                            bind:this={searchEl}
                            bind:value={search}
                            {oninput}
                            type="search"
                            class="input pl-9"
                            placeholder="Search parts or scan a barcode  ( / )"
                            aria-label="Search parts"
                            autocomplete="off"
                        />
                    </form>
                {/if}
            </header>
            <main class="mx-auto max-w-6xl px-4 py-5">
                {@render children()}
            </main>
        </div>
    </div>
{/if}

<Toasts />
