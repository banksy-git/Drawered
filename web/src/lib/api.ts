// Thin client for the Drawered JSON API.

export class ApiError extends Error {
    constructor(
        public status: number,
        public code: string,
        message: string,
        public details: Record<string, unknown> = {}
    ) {
        super(message);
    }
}

let csrfToken = '';

export function setCsrfToken(token: string) {
    csrfToken = token;
}

export function loginUrl(): string {
    const here = location.pathname + location.search;
    return '/auth/login?return_to=' + encodeURIComponent(here);
}

async function handle<T>(res: Response): Promise<T> {
    if (res.status === 401) {
        location.href = loginUrl();
        throw new ApiError(401, 'unauthenticated', 'Please log in');
    }
    if (res.status === 204) {
        return undefined as T;
    }
    const text = await res.text();
    let body: any = undefined;
    try {
        body = text ? JSON.parse(text) : undefined;
    } catch {
        // Not JSON; fall through to a generic error.
    }
    if (!res.ok) {
        const e = body?.error;
        throw new ApiError(res.status, e?.code ?? 'http_' + res.status, e?.message ?? res.statusText, e?.details);
    }
    return body as T;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (method !== 'GET') {
        headers['X-Drawered-CSRF'] = csrfToken;
    }
    let payload: BodyInit | undefined;
    if (body !== undefined) {
        headers['Content-Type'] = 'application/json';
        payload = JSON.stringify(body);
    }
    const res = await fetch('/api/v1' + path, { method, headers, body: payload, credentials: 'same-origin' });
    return handle<T>(res);
}

export const api = {
    get: <T>(path: string) => request<T>('GET', path),
    post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
    patch: <T>(path: string, body: unknown) => request<T>('PATCH', path, body),
    put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
    del: <T>(path: string) => request<T>('DELETE', path),
    async upload<T>(path: string, form: FormData): Promise<T> {
        const res = await fetch('/api/v1' + path, {
            method: 'POST',
            headers: { 'X-Drawered-CSRF': csrfToken, Accept: 'application/json' },
            body: form,
            credentials: 'same-origin'
        });
        return handle<T>(res);
    }
};

// qs builds a query string, skipping empty values.
export function qs(params: Record<string, string | number | boolean | null | undefined | string[]>): string {
    const p = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
        if (v === null || v === undefined || v === '' || v === false) continue;
        if (Array.isArray(v)) v.forEach((x) => p.append(k, x));
        else p.set(k, String(v));
    }
    const s = p.toString();
    return s ? '?' + s : '';
}

// logoutRequest ends the session and returns where to send the browser.
export async function logoutRequest(): Promise<string> {
    const res = await fetch('/auth/logout', {
        method: 'POST',
        headers: { 'X-Drawered-CSRF': csrfToken, Accept: 'application/json' },
        credentials: 'same-origin'
    });
    const body = await res.json().catch(() => ({}));
    return body.redirect ?? '/';
}

export function errorMessage(e: unknown): string {
    if (e instanceof ApiError) return e.message;
    if (e instanceof Error) return e.message;
    return String(e);
}
