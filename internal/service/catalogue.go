package service

import (
    "context"
    "database/sql"
    "net/url"
    "strings"

    "drawered/internal/db"
)

// CatalogueEntry is a manufacturer, supplier or tag with its usage count.
type CatalogueEntry struct {
    ID        int64  `json:"id"`
    Name      string `json:"name"`
    URL       string `json:"url,omitempty"`
    PartCount int    `json:"part_count"`
}

// NamedKind identifies the manufacturers or suppliers table.
type NamedKind string

const (
    Manufacturers NamedKind = "manufacturer"
    Suppliers     NamedKind = "supplier"
)

func (k NamedKind) table() string  { return string(k) + "s" }
func (k NamedKind) column() string { return string(k) + "_id" }

func validURL(u string) error {
    if u == "" {
        return nil
    }
    p, err := url.Parse(u)
    if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
        return Invalid("invalid_url", "url must be an http or https URL")
    }
    return checkLen("url", u, 0, 2000)
}

// ListNamed lists manufacturers or suppliers, filtered by substring.
func (s *Service) ListNamed(ctx context.Context, k NamedKind, q string, pg Paging) (*Page[CatalogueEntry], error) {
    pg = pg.Normalise()
    like := "%" + escapeLike(strings.TrimSpace(q)) + "%"
    out := &Page[CatalogueEntry]{Items: []CatalogueEntry{}}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+k.table()+` WHERE name LIKE ? ESCAPE '\'`, like).
        Scan(&out.Total); err != nil {
        return nil, err
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT n.id, n.name, n.url,
            (SELECT COUNT(*) FROM parts p WHERE p.`+k.column()+` = n.id AND p.deleted_at IS NULL)
        FROM `+k.table()+` n WHERE n.name LIKE ? ESCAPE '\'
        ORDER BY (n.name LIKE ? ESCAPE '\') DESC, n.name COLLATE NOCASE LIMIT ? OFFSET ?`,
        like, escapeLike(strings.TrimSpace(q))+"%", pg.Limit, pg.Offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    for rows.Next() {
        var e CatalogueEntry
        if err := rows.Scan(&e.ID, &e.Name, &e.URL, &e.PartCount); err != nil {
            return nil, err
        }
        out.Items = append(out.Items, e)
    }
    return out, rows.Err()
}

func escapeLike(s string) string {
    r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
    return r.Replace(s)
}

func loadNamed(ctx context.Context, q db.Querier, k NamedKind, id int64) (*CatalogueEntry, error) {
    e := &CatalogueEntry{ID: id}
    err := q.QueryRowContext(ctx, `SELECT name, url FROM `+k.table()+` WHERE id = ?`, id).Scan(&e.Name, &e.URL)
    if err == sql.ErrNoRows {
        return nil, NotFound(string(k))
    }
    return e, err
}

// CreateNamed creates a manufacturer or supplier explicitly.
func (s *Service) CreateNamed(ctx context.Context, k NamedKind, name, u string) (*CatalogueEntry, error) {
    name, u = strings.TrimSpace(name), strings.TrimSpace(u)
    if err := checkLen("name", name, 1, 100); err != nil {
        return nil, err
    }
    if err := validURL(u); err != nil {
        return nil, err
    }
    var id int64
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        res, err := tx.ExecContext(ctx, `INSERT INTO `+k.table()+` (name, url, created_at) VALUES (?, ?, ?)`, name, u, db.Now())
        if db.IsUniqueViolation(err) {
            return Conflict("duplicate_name", "%s %q already exists", k, name)
        }
        if err != nil {
            return err
        }
        id, _ = res.LastInsertId()
        return s.event(ctx, tx, string(k)+".created", Subject{string(k), id}, map[string]any{string(k): Ref{id, name}})
    })
    if err != nil {
        return nil, err
    }
    return loadNamed(ctx, s.DB.R, k, id)
}

// UpdateNamed renames a manufacturer or supplier or changes its URL.
func (s *Service) UpdateNamed(ctx context.Context, k NamedKind, id int64, p Patch) (*CatalogueEntry, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := loadNamed(ctx, tx, k, id)
        if err != nil {
            return err
        }
        next := *cur
        if _, err := p.Get("name", &next.Name); err != nil {
            return err
        }
        if _, err := p.Get("url", &next.URL); err != nil {
            return err
        }
        next.Name, next.URL = strings.TrimSpace(next.Name), strings.TrimSpace(next.URL)
        if err := checkLen("name", next.Name, 1, 100); err != nil {
            return err
        }
        if err := validURL(next.URL); err != nil {
            return err
        }
        ch := changes{}
        ch.add("name", cur.Name, next.Name)
        ch.add("url", cur.URL, next.URL)
        if len(ch) == 0 {
            return nil
        }
        _, err = tx.ExecContext(ctx, `UPDATE `+k.table()+` SET name = ?, url = ? WHERE id = ?`, next.Name, next.URL, id)
        if db.IsUniqueViolation(err) {
            return Conflict("duplicate_name", "%s %q already exists; merge instead", k, next.Name)
        }
        if err != nil {
            return err
        }
        if _, ok := ch["name"]; ok {
            parts, err := queryInt64s(ctx, tx, `SELECT id FROM parts WHERE `+k.column()+` = ?`, id)
            if err != nil {
                return err
            }
            if err := s.reindexParts(ctx, tx, parts); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, string(k)+".updated", Subject{string(k), id}, map[string]any{
            string(k): Ref{id, next.Name}, "changes": ch,
        })
    })
    if err != nil {
        return nil, err
    }
    return loadNamed(ctx, s.DB.R, k, id)
}

// MergeNamed re-points every part from id to into and deletes id.
func (s *Service) MergeNamed(ctx context.Context, k NamedKind, id, into int64) error {
    if id == into {
        return Invalid("same_entry", "cannot merge an entry into itself")
    }
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        from, err := loadNamed(ctx, tx, k, id)
        if err != nil {
            return err
        }
        to, err := loadNamed(ctx, tx, k, into)
        if err != nil {
            return err
        }
        parts, err := queryInt64s(ctx, tx, `SELECT id FROM parts WHERE `+k.column()+` = ?`, id)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `UPDATE parts SET `+k.column()+` = ? WHERE `+k.column()+` = ?`, into, id); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM `+k.table()+` WHERE id = ?`, id); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, parts); err != nil {
            return err
        }
        return s.event(ctx, tx, string(k)+".merged", Subject{string(k), into}, map[string]any{
            "from": Ref{id, from.Name}, "into": Ref{into, to.Name}, "parts": len(parts),
        }, Subject{string(k), id})
    })
}

// DeleteNamed deletes a manufacturer or supplier no part refers to.
func (s *Service) DeleteNamed(ctx context.Context, k NamedKind, id int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := loadNamed(ctx, tx, k, id)
        if err != nil {
            return err
        }
        var n int
        if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM parts WHERE `+k.column()+` = ?`, id).Scan(&n); err != nil {
            return err
        }
        if n > 0 {
            return Conflict("in_use", "%s is used by %d part(s)", k, n)
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM `+k.table()+` WHERE id = ?`, id); err != nil {
            return err
        }
        return s.event(ctx, tx, string(k)+".deleted", Subject{string(k), id}, map[string]any{string(k): Ref{id, cur.Name}})
    })
}

// ListTags lists tags filtered by substring, most used first when no
// filter is given.
func (s *Service) ListTags(ctx context.Context, q string, pg Paging) (*Page[CatalogueEntry], error) {
    pg = pg.Normalise()
    q = strings.ToLower(strings.TrimSpace(q))
    like := "%" + escapeLike(q) + "%"
    out := &Page[CatalogueEntry]{Items: []CatalogueEntry{}}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE name LIKE ? ESCAPE '\'`, like).Scan(&out.Total); err != nil {
        return nil, err
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT t.id, t.name,
            (SELECT COUNT(*) FROM part_tags pt JOIN parts p ON p.id = pt.part_id WHERE pt.tag_id = t.id AND p.deleted_at IS NULL) AS n
        FROM tags t WHERE t.name LIKE ? ESCAPE '\'
        ORDER BY (t.name LIKE ? ESCAPE '\') DESC, n DESC, t.name LIMIT ? OFFSET ?`,
        like, escapeLike(q)+"%", pg.Limit, pg.Offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    for rows.Next() {
        var e CatalogueEntry
        if err := rows.Scan(&e.ID, &e.Name, &e.PartCount); err != nil {
            return nil, err
        }
        out.Items = append(out.Items, e)
    }
    return out, rows.Err()
}

func loadTag(ctx context.Context, q db.Querier, id int64) (string, error) {
    var name string
    err := q.QueryRowContext(ctx, `SELECT name FROM tags WHERE id = ?`, id).Scan(&name)
    if err == sql.ErrNoRows {
        return "", NotFound("tag")
    }
    return name, err
}

// RenameTag renames a tag everywhere it is used.
func (s *Service) RenameTag(ctx context.Context, id int64, name string) error {
    tags, err := normaliseTags([]string{name})
    if err != nil {
        return err
    }
    if len(tags) == 0 {
        return Invalid("required", "name is required")
    }
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := loadTag(ctx, tx, id)
        if err != nil {
            return err
        }
        if cur == tags[0] {
            return nil
        }
        _, err = tx.ExecContext(ctx, `UPDATE tags SET name = ? WHERE id = ?`, tags[0], id)
        if db.IsUniqueViolation(err) {
            return Conflict("duplicate_name", "tag %q already exists; merge instead", tags[0])
        }
        if err != nil {
            return err
        }
        parts, err := queryInt64s(ctx, tx, `SELECT part_id FROM part_tags WHERE tag_id = ?`, id)
        if err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, parts); err != nil {
            return err
        }
        return s.event(ctx, tx, "tag.renamed", Subject{"tag", id}, map[string]any{
            "from": cur, "to": tags[0], "parts": len(parts),
        })
    })
}

// MergeTag moves every use of tag id onto tag into and deletes id.
func (s *Service) MergeTag(ctx context.Context, id, into int64) error {
    if id == into {
        return Invalid("same_entry", "cannot merge a tag into itself")
    }
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        from, err := loadTag(ctx, tx, id)
        if err != nil {
            return err
        }
        to, err := loadTag(ctx, tx, into)
        if err != nil {
            return err
        }
        parts, err := queryInt64s(ctx, tx, `SELECT part_id FROM part_tags WHERE tag_id = ?`, id)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO part_tags (part_id, tag_id) SELECT part_id, ? FROM part_tags WHERE tag_id = ?`,
            into, id); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, parts); err != nil {
            return err
        }
        return s.event(ctx, tx, "tag.merged", Subject{"tag", into}, map[string]any{
            "from": from, "into": to, "parts": len(parts),
        }, Subject{"tag", id})
    })
}

// DeleteTag removes a tag from every part.
func (s *Service) DeleteTag(ctx context.Context, id int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        name, err := loadTag(ctx, tx, id)
        if err != nil {
            return err
        }
        parts, err := queryInt64s(ctx, tx, `SELECT part_id FROM part_tags WHERE tag_id = ?`, id)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, parts); err != nil {
            return err
        }
        return s.event(ctx, tx, "tag.deleted", Subject{"tag", id}, map[string]any{"tag": name, "parts": len(parts)})
    })
}
