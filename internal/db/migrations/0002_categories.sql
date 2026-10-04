-- Part categories: a hierarchy like locations, with an optional icon.

CREATE TABLE categories (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER REFERENCES categories (id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    icon        TEXT,
    structural  INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX categories_sibling_name ON categories (IFNULL(parent_id, 0), name COLLATE NOCASE);
CREATE INDEX categories_parent ON categories (parent_id);

ALTER TABLE parts ADD COLUMN category_id INTEGER REFERENCES categories (id);
CREATE INDEX parts_category ON parts (category_id);

-- Structural categories never hold parts. The application checks this too.
CREATE TRIGGER parts_category_structural_insert BEFORE INSERT ON parts
WHEN NEW.category_id IS NOT NULL AND (SELECT structural FROM categories WHERE id = NEW.category_id) = 1
BEGIN
    SELECT RAISE(ABORT, 'structural category cannot hold parts');
END;

CREATE TRIGGER parts_category_structural_update BEFORE UPDATE OF category_id ON parts
WHEN NEW.category_id IS NOT NULL AND (SELECT structural FROM categories WHERE id = NEW.category_id) = 1
BEGIN
    SELECT RAISE(ABORT, 'structural category cannot hold parts');
END;

CREATE TRIGGER category_structural_guard BEFORE UPDATE OF structural ON categories
WHEN NEW.structural = 1 AND EXISTS (SELECT 1 FROM parts WHERE category_id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'category has parts and cannot be structural');
END;

-- The search index gains a category column. It is rebuilt at start-up
-- because its document count no longer matches the parts table.
DROP TABLE parts_fts;
CREATE VIRTUAL TABLE parts_fts USING fts5 (
    name, mpn, tags, manufacturer, supplier, body, locations, category,
    tokenize = 'unicode61 remove_diacritics 2',
    prefix = '2 3'
);

-- Existing installations: Stores manages categories.
INSERT OR IGNORE INTO role_permissions (role_id, permission)
    SELECT id, 'categories:manage' FROM roles WHERE name = 'Stores';
