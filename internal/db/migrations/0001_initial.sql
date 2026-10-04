-- Initial schema.

CREATE TABLE users (
    id             INTEGER PRIMARY KEY,
    issuer         TEXT NOT NULL,
    subject        TEXT NOT NULL,
    display_name   TEXT NOT NULL DEFAULT '',
    email          TEXT NOT NULL DEFAULT '',
    username       TEXT NOT NULL DEFAULT '',
    groups_json    TEXT NOT NULL DEFAULT '[]',
    claims_json    TEXT NOT NULL DEFAULT '{}',
    disabled       INTEGER NOT NULL DEFAULT 0,
    first_login_at TEXT NOT NULL,
    last_login_at  TEXT NOT NULL,
    UNIQUE (issuer, subject)
);

CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY,
    token_hash   TEXT NOT NULL UNIQUE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE oidc_states (
    state      TEXT PRIMARY KEY,
    nonce      TEXT NOT NULL,
    verifier   TEXT NOT NULL,
    return_to  TEXT NOT NULL DEFAULT '/',
    expires_at TEXT NOT NULL
);

CREATE TABLE roles (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT NOT NULL DEFAULT '',
    builtin     INTEGER NOT NULL DEFAULT 0,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE role_permissions (
    role_id    INTEGER NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE user_roles (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id INTEGER NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    source  TEXT NOT NULL CHECK (source IN ('manual', 'mapping', 'bootstrap')),
    PRIMARY KEY (user_id, role_id, source)
);
CREATE INDEX user_roles_role ON user_roles (role_id);

CREATE TABLE role_mappings (
    id         INTEGER PRIMARY KEY,
    claim      TEXT NOT NULL,
    match_type TEXT NOT NULL CHECK (match_type IN ('equals', 'contains', 'ends_with', 'regex')),
    value      TEXT NOT NULL,
    role_id    INTEGER NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL
);

CREATE TABLE locations (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER REFERENCES locations (id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    colour      TEXT,
    structural  INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX locations_sibling_name ON locations (IFNULL(parent_id, 0), name COLLATE NOCASE);
CREATE INDEX locations_parent ON locations (parent_id);

CREATE TABLE manufacturers (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    url        TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE suppliers (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    url        TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE files (
    id            INTEGER PRIMARY KEY,
    sha256        TEXT NOT NULL,
    original_name TEXT NOT NULL,
    mime_type     TEXT NOT NULL,
    size          INTEGER NOT NULL,
    uploaded_by   INTEGER REFERENCES users (id),
    uploaded_at   TEXT NOT NULL
);
CREATE INDEX files_sha ON files (sha256);

CREATE TABLE parts (
    id                       INTEGER PRIMARY KEY,
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    uom                      TEXT NOT NULL DEFAULT 'pcs',
    allow_fractional         INTEGER NOT NULL DEFAULT 0,
    mpn                      TEXT NOT NULL DEFAULT '',
    manufacturer_id          INTEGER REFERENCES manufacturers (id),
    supplier_id              INTEGER REFERENCES suppliers (id),
    supplier_sku             TEXT NOT NULL DEFAULT '',
    barcode                  TEXT NOT NULL DEFAULT '',
    cost_micros              INTEGER,
    currency                 TEXT,
    min_total_quantity_milli INTEGER,
    thumbnail_image_id       INTEGER,
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    deleted_at               TEXT
);
CREATE INDEX parts_barcode ON parts (barcode COLLATE NOCASE);
CREATE INDEX parts_mpn ON parts (mpn COLLATE NOCASE);
CREATE INDEX parts_sku ON parts (supplier_sku COLLATE NOCASE);
CREATE INDEX parts_manufacturer ON parts (manufacturer_id);
CREATE INDEX parts_supplier ON parts (supplier_id);

CREATE TABLE part_tags (
    part_id INTEGER NOT NULL REFERENCES parts (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (part_id, tag_id)
);
CREATE INDEX part_tags_tag ON part_tags (tag_id);

CREATE TABLE part_links (
    id          INTEGER PRIMARY KEY,
    part_id     INTEGER NOT NULL REFERENCES parts (id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX part_links_part ON part_links (part_id);

CREATE TABLE part_images (
    id         INTEGER PRIMARY KEY,
    part_id    INTEGER NOT NULL REFERENCES parts (id) ON DELETE CASCADE,
    file_id    INTEGER NOT NULL REFERENCES files (id),
    caption    TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX part_images_part ON part_images (part_id);
CREATE INDEX part_images_file ON part_images (file_id);

CREATE TABLE part_documents (
    id          INTEGER PRIMARY KEY,
    part_id     INTEGER NOT NULL REFERENCES parts (id) ON DELETE CASCADE,
    file_id     INTEGER NOT NULL REFERENCES files (id),
    description TEXT NOT NULL DEFAULT '',
    sort_order  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX part_documents_part ON part_documents (part_id);
CREATE INDEX part_documents_file ON part_documents (file_id);

CREATE TABLE stock (
    part_id            INTEGER NOT NULL REFERENCES parts (id) ON DELETE CASCADE,
    location_id        INTEGER NOT NULL REFERENCES locations (id),
    quantity_milli     INTEGER NOT NULL DEFAULT 0 CHECK (quantity_milli >= 0),
    min_quantity_milli INTEGER CHECK (min_quantity_milli IS NULL OR min_quantity_milli >= 0),
    note               TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (part_id, location_id)
);
CREATE INDEX stock_location ON stock (location_id);

-- Structural locations never hold stock. The application checks this too;
-- the triggers keep the invariant even in the face of bugs.
CREATE TRIGGER stock_structural_insert BEFORE INSERT ON stock
WHEN (SELECT structural FROM locations WHERE id = NEW.location_id) = 1
BEGIN
    SELECT RAISE(ABORT, 'structural location cannot hold stock');
END;

CREATE TRIGGER stock_structural_update BEFORE UPDATE OF location_id ON stock
WHEN (SELECT structural FROM locations WHERE id = NEW.location_id) = 1
BEGIN
    SELECT RAISE(ABORT, 'structural location cannot hold stock');
END;

CREATE TRIGGER location_structural_guard BEFORE UPDATE OF structural ON locations
WHEN NEW.structural = 1 AND EXISTS (SELECT 1 FROM stock WHERE location_id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'location holds stock and cannot be structural');
END;

CREATE TABLE events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at   TEXT NOT NULL,
    actor_user_id INTEGER REFERENCES users (id),
    action        TEXT NOT NULL,
    subject_type  TEXT NOT NULL,
    subject_id    INTEGER NOT NULL,
    data_json     TEXT NOT NULL DEFAULT '{}',
    request_id    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX events_actor ON events (actor_user_id, id);
CREATE INDEX events_action ON events (action, id);
CREATE INDEX events_occurred ON events (occurred_at);

CREATE TABLE event_subjects (
    event_id     INTEGER NOT NULL REFERENCES events (id),
    subject_type TEXT NOT NULL,
    subject_id   INTEGER NOT NULL,
    PRIMARY KEY (subject_type, subject_id, event_id)
);

-- Events are append-only.
CREATE TRIGGER events_no_update BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;
CREATE TRIGGER events_no_delete BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE VIRTUAL TABLE parts_fts USING fts5 (
    name, mpn, tags, manufacturer, supplier, body, locations,
    tokenize = 'unicode61 remove_diacritics 2',
    prefix = '2 3'
);

CREATE VIRTUAL TABLE locations_fts USING fts5 (
    name, path, description,
    tokenize = 'unicode61 remove_diacritics 2',
    prefix = '2 3'
);
