export type ThemePref = 'system' | 'light' | 'dark';

const KEY = 'drawered-theme';

export function getTheme(): ThemePref {
    try {
        const v = localStorage.getItem(KEY);
        if (v === 'light' || v === 'dark') return v;
    } catch {
        // Storage unavailable.
    }
    return 'system';
}

export function applyTheme(pref: ThemePref) {
    try {
        if (pref === 'system') localStorage.removeItem(KEY);
        else localStorage.setItem(KEY, pref);
    } catch {
        // Storage unavailable; the choice lasts for this page only.
    }
    const dark = pref === 'dark' || (pref === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
}

export function watchSystemTheme() {
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (getTheme() === 'system') applyTheme('system');
    });
}
