import { errorMessage } from './api';

export interface Toast {
    id: number;
    kind: 'info' | 'success' | 'error';
    text: string;
}

export const toasts = $state<Toast[]>([]);

let next = 1;

export function toast(text: string, kind: Toast['kind'] = 'success', ms = 4000) {
    const id = next++;
    toasts.push({ id, kind, text });
    setTimeout(() => dismiss(id), ms);
}

export function toastError(e: unknown) {
    toast(errorMessage(e), 'error', 7000);
}

export function dismiss(id: number) {
    const i = toasts.findIndex((t) => t.id === id);
    if (i >= 0) toasts.splice(i, 1);
}
