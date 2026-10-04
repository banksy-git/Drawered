import type { Permission } from '$lib/types';

export const adminSections: { href: string; label: string; perms: Permission[] }[] = [
    { href: '/admin/users', label: 'Users', perms: ['users:manage'] },
    { href: '/admin/roles', label: 'Roles', perms: ['users:manage', 'roles:manage'] },
    { href: '/admin/mappings', label: 'Claim mappings', perms: ['roles:manage'] },
    { href: '/admin/settings', label: 'Settings', perms: ['roles:manage'] },
    { href: '/admin/catalogue', label: 'Catalogue', perms: ['catalogue:manage'] },
    { href: '/admin/system', label: 'System', perms: ['system:admin'] }
];
