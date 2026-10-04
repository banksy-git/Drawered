// Display helpers.

export function qty(q: number | null | undefined, uom?: string): string {
    if (q === null || q === undefined) return '';
    const s = Number(q).toLocaleString('en-GB', { maximumFractionDigits: 3 });
    return uom ? `${s} ${uom}` : s;
}

export function money(v: number | null | undefined, currency: string | null | undefined): string {
    if (v === null || v === undefined) return '';
    try {
        return new Intl.NumberFormat('en-GB', {
            style: 'currency',
            currency: currency || 'GBP',
            minimumFractionDigits: 2,
            maximumFractionDigits: 6
        }).format(v);
    } catch {
        return `${v} ${currency ?? ''}`;
    }
}

export function bytes(n: number): string {
    if (n < 1024) return `${n} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let v = n;
    let i = -1;
    do {
        v /= 1024;
        i++;
    } while (v >= 1024 && i < units.length - 1);
    return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}

export function dateTime(s: string | null | undefined): string {
    if (!s) return '';
    return new Date(s).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'short' });
}

export function relative(s: string): string {
    const d = new Date(s).getTime();
    const diff = (Date.now() - d) / 1000;
    if (diff < 45) return 'just now';
    if (diff < 60 * 60) return plural(Math.max(1, Math.round(diff / 60)), 'minute');
    if (diff < 86400) return plural(Math.round(diff / 3600), 'hour');
    if (diff < 86400 * 14) return plural(Math.round(diff / 86400), 'day');
    return dateTime(s);
}

function plural(n: number, unit: string): string {
    return `${n} ${unit}${n === 1 ? '' : 's'} ago`;
}

export function fileUrl(fileId: number, rendition?: 'thumb' | 'small' | 'large'): string {
    return rendition ? `/files/${fileId}/${rendition}` : `/files/${fileId}`;
}

// Fuzzy path matching for the location picker: every whitespace-separated
// term must be a subsequence of one path segment, e.g. "grey sh2 a1"
// matches "Garage / Grey Cabinet / Shelf2 / Box1 / A1".
export function pathMatches(path: string, query: string): boolean {
    const segments = path
        .toLowerCase()
        .split(' / ')
        .map((s) => s.replace(/\s+/g, ''));
    const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
    let from = 0;
    for (const term of terms) {
        let found = -1;
        for (let i = from; i < segments.length; i++) {
            if (subsequence(term, segments[i])) {
                found = i;
                break;
            }
        }
        if (found < 0) return false;
        from = found;
    }
    return true;
}

function subsequence(needle: string, hay: string): boolean {
    let j = 0;
    for (let i = 0; i < hay.length && j < needle.length; i++) {
        if (hay[i] === needle[j]) j++;
    }
    return j === needle.length;
}
