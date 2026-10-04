package service

import (
    "context"
    "database/sql"
    "strings"
    "unicode"

    "drawered/internal/db"
)

// ftsQuery converts user input into an FTS5 MATCH expression. Bare terms
// become prefix matches, quoted phrases match exactly, and all must match.
// It returns "" when the input has no searchable terms.
func ftsQuery(input string) string {
    var parts []string
    add := func(term string, phrase bool) {
        term = strings.ReplaceAll(term, `"`, "")
        if !strings.ContainsFunc(term, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
            return
        }
        if phrase {
            parts = append(parts, `"`+term+`"`)
        } else {
            parts = append(parts, `"`+term+`"*`)
        }
    }
    rest := input
    for {
        rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
        if rest == "" {
            break
        }
        if rest[0] == '"' {
            end := strings.IndexByte(rest[1:], '"')
            if end < 0 {
                add(rest[1:], false)
                break
            }
            add(rest[1:end+1], true)
            rest = rest[end+2:]
            continue
        }
        end := strings.IndexFunc(rest, unicode.IsSpace)
        if end < 0 {
            add(rest, false)
            break
        }
        add(rest[:end], false)
        rest = rest[end:]
    }
    return strings.Join(parts, " ")
}

// reindexParts rebuilds the search documents for the given parts.
func (s *Service) reindexParts(ctx context.Context, q db.Querier, ids []int64) error {
    if len(ids) == 0 {
        return nil
    }
    ix, err := loadLocations(ctx, q)
    if err != nil {
        return err
    }
    for _, id := range ids {
        if err := reindexPart(ctx, q, ix, id); err != nil {
            return err
        }
    }
    return nil
}

func reindexPart(ctx context.Context, q db.Querier, ix *locIndex, id int64) error {
    if _, err := q.ExecContext(ctx, `DELETE FROM parts_fts WHERE rowid = ?`, id); err != nil {
        return err
    }
    var name, desc, mpn, sku, barcode string
    var manufacturer, supplier sql.NullString
    var deleted sql.NullString
    err := q.QueryRowContext(ctx, `SELECT p.name, p.description, p.mpn, p.supplier_sku, p.barcode,
            m.name, su.name, p.deleted_at
        FROM parts p
        LEFT JOIN manufacturers m ON m.id = p.manufacturer_id
        LEFT JOIN suppliers su ON su.id = p.supplier_id
        WHERE p.id = ?`, id).Scan(&name, &desc, &mpn, &sku, &barcode, &manufacturer, &supplier, &deleted)
    if err == sql.ErrNoRows || deleted.Valid {
        return nil
    }
    if err != nil {
        return err
    }
    tags, err := queryStrings(ctx, q, `SELECT t.name FROM part_tags pt JOIN tags t ON t.id = pt.tag_id WHERE pt.part_id = ?`, id)
    if err != nil {
        return err
    }
    body := []string{desc, sku, barcode}
    more, err := queryStrings(ctx, q, `SELECT url || ' ' || description FROM part_links WHERE part_id = ?
        UNION ALL
        SELECT d.description || ' ' || f.original_name FROM part_documents d JOIN files f ON f.id = d.file_id WHERE d.part_id = ?
        UNION ALL
        SELECT caption FROM part_images WHERE part_id = ?`, id, id, id)
    if err != nil {
        return err
    }
    body = append(body, more...)
    locIDs, err := queryInt64s(ctx, q, `SELECT location_id FROM stock WHERE part_id = ?`, id)
    if err != nil {
        return err
    }
    var paths []string
    for _, l := range locIDs {
        paths = append(paths, ix.path(l))
    }
    var category string
    if err := q.QueryRowContext(ctx, categoryPathSQL, id).Scan(&category); err != nil {
        return err
    }
    _, err = q.ExecContext(ctx, `INSERT INTO parts_fts (rowid, name, mpn, tags, manufacturer, supplier, body, locations, category)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, name, mpn, strings.Join(tags, " "),
        manufacturer.String, supplier.String, strings.Join(body, "\n"), strings.Join(paths, "\n"), category)
    return err
}

// reindexLocations rebuilds the search documents for the given locations.
func reindexLocations(ctx context.Context, q db.Querier, ix *locIndex, ids []int64) error {
    for _, id := range ids {
        if _, err := q.ExecContext(ctx, `DELETE FROM locations_fts WHERE rowid = ?`, id); err != nil {
            return err
        }
        n, ok := ix.byID[id]
        if !ok {
            continue
        }
        if _, err := q.ExecContext(ctx, `INSERT INTO locations_fts (rowid, name, path, description) VALUES (?, ?, ?, ?)`,
            id, n.Name, ix.path(id), n.Description); err != nil {
            return err
        }
    }
    return nil
}

// Reindex rebuilds both search indexes from scratch.
func (s *Service) Reindex(ctx context.Context) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        if _, err := tx.ExecContext(ctx, `DELETE FROM parts_fts; DELETE FROM locations_fts;`); err != nil {
            return err
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        if err := reindexLocations(ctx, tx, ix, ix.allIDs()); err != nil {
            return err
        }
        ids, err := queryInt64s(ctx, tx, `SELECT id FROM parts WHERE deleted_at IS NULL`)
        if err != nil {
            return err
        }
        for _, id := range ids {
            if err := reindexPart(ctx, tx, ix, id); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "system.reindexed", Subject{"system", 1}, map[string]any{"parts": len(ids)})
    })
}

// EnsureSearchIndex rebuilds the search index if it does not hold exactly
// one document per live part, as after a migration that recreates it.
func (s *Service) EnsureSearchIndex(ctx context.Context) error {
    var docs, parts int
    if err := s.DB.R.QueryRowContext(ctx, `SELECT
            (SELECT COUNT(*) FROM parts_fts),
            (SELECT COUNT(*) FROM parts WHERE deleted_at IS NULL)`).Scan(&docs, &parts); err != nil {
        return err
    }
    if docs == parts {
        return nil
    }
    return s.Reindex(ctx)
}

func queryStrings(ctx context.Context, q db.Querier, query string, args ...any) ([]string, error) {
    rows, err := q.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var s sql.NullString
        if err := rows.Scan(&s); err != nil {
            return nil, err
        }
        out = append(out, s.String)
    }
    return out, rows.Err()
}

func queryInt64s(ctx context.Context, q db.Querier, query string, args ...any) ([]int64, error) {
    rows, err := q.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []int64
    for rows.Next() {
        var v int64
        if err := rows.Scan(&v); err != nil {
            return nil, err
        }
        out = append(out, v)
    }
    return out, rows.Err()
}
