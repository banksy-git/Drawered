-- Records entities brought in by import utilities so re-runs skip them.

CREATE TABLE import_links (
    source      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    source_id   INTEGER NOT NULL,
    target_id   INTEGER NOT NULL,
    imported_at TEXT NOT NULL,
    PRIMARY KEY (source, kind, source_id)
);
