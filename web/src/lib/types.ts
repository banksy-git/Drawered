// API types mirroring internal/service.

export type Permission =
    | 'parts:read'
    | 'locations:read'
    | 'events:read'
    | 'stock:move'
    | 'stock:adjust'
    | 'parts:create'
    | 'parts:edit'
    | 'parts:delete'
    | 'locations:create'
    | 'locations:edit'
    | 'locations:delete'
    | 'catalogue:manage'
    | 'categories:manage'
    | 'users:manage'
    | 'roles:manage'
    | 'system:admin';

export interface Ref {
    id: number;
    name: string;
}

export interface Page<T> {
    items: T[];
    total: number;
}

export interface UserRole {
    role_id: number;
    name: string;
    source: 'manual' | 'mapping' | 'bootstrap';
}

export interface User {
    id: number;
    issuer: string;
    subject: string;
    display_name: string;
    email: string;
    username: string;
    groups: string[];
    disabled: boolean;
    first_login_at: string;
    last_login_at: string;
    roles: UserRole[];
    permissions: Permission[];
}

export interface Me {
    user: User;
    permissions: Permission[];
    csrf_token: string;
    default_currency: string;
    max_image_bytes: number;
    max_document_bytes: number;
    version: string;
}

export interface Location {
    id: number;
    parent_id: number | null;
    name: string;
    description: string;
    colour: string | null;
    effective_colour: string | null;
    structural: boolean;
    sort_order: number;
    path: string;
    ancestors: Ref[];
    version: number;
    created_at: string;
    updated_at: string;
    part_count: number;
    total_part_count: number;
    child_count: number;
}

export interface CategoryRef {
    id: number;
    name: string;
    path: string;
    icon: string | null;
    effective_icon: string | null;
}

export interface Category {
    id: number;
    parent_id: number | null;
    name: string;
    description: string;
    icon: string | null;
    effective_icon: string | null;
    structural: boolean;
    sort_order: number;
    path: string;
    ancestors: Ref[];
    version: number;
    created_at: string;
    updated_at: string;
    part_count: number;
    total_part_count: number;
    child_count: number;
}

export interface StockEntry {
    location_id: number;
    path: string;
    effective_colour: string | null;
    quantity: number;
    min_quantity: number | null;
    note: string;
    low: boolean;
}

export interface Image {
    id: number;
    file_id: number;
    caption: string;
    sort_order: number;
}

export interface Link {
    id: number;
    url: string;
    description: string;
    sort_order: number;
}

export interface Document {
    id: number;
    file_id: number;
    description: string;
    sort_order: number;
    original_name: string;
    mime_type: string;
    size: number;
}

export interface Part {
    id: number;
    name: string;
    description: string;
    tags: string[];
    category: CategoryRef | null;
    uom: string;
    allow_fractional: boolean;
    mpn: string;
    manufacturer: Ref | null;
    supplier: Ref | null;
    supplier_sku: string;
    barcode: string;
    cost: number | null;
    currency: string | null;
    min_total_quantity: number | null;
    thumbnail_image_id: number | null;
    thumbnail_file_id: number | null;
    images: Image[];
    links: Link[];
    documents: Document[];
    stock: StockEntry[];
    total_quantity: number;
    total_value: number | null;
    low: boolean;
    version: number;
    created_at: string;
    updated_at: string;
    deleted_at: string | null;
    warnings?: string[];
}

export interface PartSummary {
    id: number;
    name: string;
    mpn: string;
    manufacturer: string | null;
    tags: string[];
    category: CategoryRef | null;
    uom: string;
    total_quantity: number;
    thumbnail_file_id: number | null;
    locations: string[];
    low: boolean;
    updated_at: string;
    deleted_at: string | null;
}

export interface LocationStockItem {
    part: Ref;
    mpn: string;
    uom: string;
    thumbnail_file_id: number | null;
    location_id: number;
    path: string;
    quantity: number;
    min_quantity: number | null;
    note: string;
    low: boolean;
}

export interface Change {
    from: unknown;
    to: unknown;
}

export interface AppEvent {
    id: number;
    occurred_at: string;
    actor: Ref | null;
    action: string;
    subject_type: string;
    subject_id: number;
    data: Record<string, any>;
    request_id: string;
}

export interface CatalogueEntry {
    id: number;
    name: string;
    url?: string;
    part_count: number;
}

export interface Role {
    id: number;
    name: string;
    description: string;
    builtin: boolean;
    permissions: Permission[];
    user_count: number;
    mapping_count: number;
    is_default: boolean;
    version: number;
}

export interface PermissionInfo {
    name: Permission;
    description: string;
}

export interface Mapping {
    id: number;
    claim: string;
    match_type: 'equals' | 'contains' | 'ends_with' | 'regex';
    value: string;
    role_id: number;
    role_name: string;
    sort_order: number;
}

export interface Session {
    id: number;
    user_id: number;
    created_at: string;
    last_seen_at: string;
    expires_at: string;
    user_agent: string;
    current: boolean;
}

export interface SystemInfo {
    version: string;
    database_bytes: number;
    file_bytes: number;
    parts: number;
    deleted_parts: number;
    locations: number;
    users: number;
    events: number;
}
