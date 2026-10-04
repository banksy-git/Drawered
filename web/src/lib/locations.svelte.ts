import { api } from './api';
import type { Location } from './types';

// A shared cache of the location tree, which is small enough to hold whole.
export const locationCache = $state<{ items: Location[]; loaded: boolean }>({ items: [], loaded: false });

let pending: Promise<Location[]> | null = null;

export function loadLocations(force = false): Promise<Location[]> {
    if (locationCache.loaded && !force) return Promise.resolve(locationCache.items);
    if (pending && !force) return pending;
    pending = api
        .get<{ items: Location[] }>('/locations')
        .then((r) => {
            locationCache.items = r.items;
            locationCache.loaded = true;
            return r.items;
        })
        .finally(() => (pending = null));
    return pending;
}

export function invalidateLocations() {
    locationCache.loaded = false;
}
