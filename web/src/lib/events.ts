// Human-readable rendering of audit events.

import type { AppEvent } from './types';
import { qty } from './format';

function name(r: any): string {
    return r?.name ?? '?';
}

// describe returns a sentence fragment that follows the actor's name.
export function describe(e: AppEvent): string {
    const d = e.data ?? {};
    const part = name(d.part);
    const n = (v: unknown) => qty(v as number, d.uom);
    switch (e.action) {
        case 'part.created':
            if (d.duplicated_from) return `created ${part} as a copy of ${name(d.duplicated_from)}`;
            if (d.stock?.length) {
                const where = d.stock.map((s: any) => `${n(s.quantity)} in ${s.path}`).join(', ');
                return `created ${part} with ${where}`;
            }
            return `created ${part}`;
        case 'part.updated':
            return `edited ${part}`;
        case 'part.deleted':
            return `deleted ${part}`;
        case 'part.restored':
            return `restored ${part}`;
        case 'part.purged':
            return `permanently purged ${part}`;
        case 'part.image_added':
            return `added an image to ${part}`;
        case 'part.image_updated':
            return `edited an image on ${part}`;
        case 'part.image_removed':
            return `removed an image from ${part}`;
        case 'part.thumbnail_changed':
            return `changed the thumbnail of ${part}`;
        case 'part.document_added':
            return `attached ${d.filename} to ${part}`;
        case 'part.document_updated':
            return `edited a document on ${part}`;
        case 'part.document_removed':
            return `removed ${d.filename} from ${part}`;
        case 'part.link_added':
            return `added a link to ${part}`;
        case 'part.link_updated':
            return `edited a link on ${part}`;
        case 'part.link_removed':
            return `removed a link from ${part}`;
        case 'stock.added':
            return `added ${n(d.quantity)} of ${part} to ${d.path} (now ${n(d.after)})`;
        case 'stock.removed':
            return `removed ${n(d.quantity)} of ${part} from ${d.path} (now ${n(d.after)})`;
        case 'stock.moved':
            return `moved ${n(d.quantity)} of ${part} from ${d.from_path} to ${d.to_path}`;
        case 'stock.set':
            return `set ${part} at ${d.path} to ${n(d.after)} (was ${n(d.before)})`;
        case 'stock.location_added':
            return `added ${d.path} as a location for ${part}`;
        case 'stock.location_removed':
            return `removed ${d.path} as a location for ${part}`;
        case 'stock.entry_updated':
            return `changed stock settings for ${part} at ${d.path}`;
        case 'location.created':
            return `created location ${d.path}`;
        case 'location.updated':
            return `edited location ${d.path}`;
        case 'location.moved':
            return `moved location to ${d.path}`;
        case 'location.deleted':
            return `deleted location ${d.path}`;
        case 'category.created':
            return `created category ${d.path}`;
        case 'category.updated':
            return `edited category ${d.path}`;
        case 'category.moved':
            return `moved category to ${d.path}`;
        case 'category.deleted':
            return `deleted category ${d.path}`;
        case 'manufacturer.created':
        case 'supplier.created':
            return `created ${e.subject_type} ${name(d[e.subject_type])}`;
        case 'manufacturer.updated':
        case 'supplier.updated':
            return `edited ${e.subject_type} ${name(d[e.subject_type])}`;
        case 'manufacturer.merged':
        case 'supplier.merged':
            return `merged ${e.subject_type} ${name(d.from)} into ${name(d.into)}`;
        case 'manufacturer.deleted':
        case 'supplier.deleted':
            return `deleted ${e.subject_type} ${name(d[e.subject_type])}`;
        case 'tag.renamed':
            return `renamed tag ${d.from} to ${d.to}`;
        case 'tag.merged':
            return `merged tag ${d.from} into ${d.into}`;
        case 'tag.deleted':
            return `deleted tag ${d.tag}`;
        case 'user.created':
            return `account created for ${name(d.user)}`;
        case 'user.logged_in':
            return `logged in`;
        case 'user.roles_changed':
            return `changed roles for ${name(d.user)}`;
        case 'user.disabled':
            return `disabled ${name(d.user)}`;
        case 'user.enabled':
            return `enabled ${name(d.user)}`;
        case 'user.session_revoked':
            return `revoked a session for ${name(d.user)}`;
        case 'role.created':
            return `created role ${name(d.role)}`;
        case 'role.updated':
            return `edited role ${name(d.role)}`;
        case 'role.deleted':
            return `deleted role ${name(d.role)}`;
        case 'mapping.created':
            return `added a claim mapping to ${name(d.role)}`;
        case 'mapping.updated':
            return `edited a claim mapping for ${name(d.role)}`;
        case 'mapping.deleted':
            return `removed a claim mapping for ${name(d.role)}`;
        case 'settings.updated':
            return `changed settings`;
        case 'system.reindexed':
            return `rebuilt the search index`;
        case 'system.backup_created':
            return `downloaded a backup`;
        default:
            return e.action;
    }
}

// subjectLink returns the UI route for an event's subject, if any.
export function subjectLink(e: AppEvent): string | null {
    switch (e.subject_type) {
        case 'part':
            return e.action === 'part.purged' ? null : `/parts/${e.subject_id}`;
        case 'location':
            return e.action === 'location.deleted' ? null : `/locations/${e.subject_id}`;
        case 'category':
            return e.action === 'category.deleted' ? null : `/categories/${e.subject_id}`;
        case 'user':
            return `/admin/users/${e.subject_id}`;
        case 'role':
            return `/admin/roles`;
        case 'mapping':
            return `/admin/mappings`;
        default:
            return null;
    }
}

export const actionGroups: { value: string; label: string }[] = [
    { value: '', label: 'All actions' },
    { value: 'stock.', label: 'Stock' },
    { value: 'part.', label: 'Parts' },
    { value: 'location.', label: 'Locations' },
    { value: 'category.', label: 'Categories' },
    { value: 'manufacturer.', label: 'Manufacturers' },
    { value: 'supplier.', label: 'Suppliers' },
    { value: 'tag.', label: 'Tags' },
    { value: 'user.', label: 'Users' },
    { value: 'role.', label: 'Roles' },
    { value: 'mapping.', label: 'Claim mappings' },
    { value: 'settings.', label: 'Settings' },
    { value: 'system.', label: 'System' }
];
