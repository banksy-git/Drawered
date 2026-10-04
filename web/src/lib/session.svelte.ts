import type { Me, Permission } from './types';

// The signed-in user's session, loaded once by the root layout.
export const session = $state<{ me: Me | null }>({ me: null });

export function can(p: Permission): boolean {
    const perms = session.me?.permissions ?? [];
    return perms.includes(p) || perms.includes('system:admin');
}
