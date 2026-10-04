import { api } from './api';
import type { Category } from './types';

// A shared cache of the category tree, which is small enough to hold whole.
export const categoryCache = $state<{ items: Category[]; loaded: boolean }>({ items: [], loaded: false });

let pending: Promise<Category[]> | null = null;

export function loadCategories(force = false): Promise<Category[]> {
    if (categoryCache.loaded && !force) return Promise.resolve(categoryCache.items);
    if (pending && !force) return pending;
    pending = api
        .get<{ items: Category[] }>('/categories')
        .then((r) => {
            categoryCache.items = r.items;
            categoryCache.loaded = true;
            return r.items;
        })
        .finally(() => (pending = null));
    return pending;
}

export function invalidateCategories() {
    categoryCache.loaded = false;
}
