package service

import (
    "context"
    "database/sql"
    "strings"

    "drawered/internal/db"
)

// Import kinds recorded in import_links.
const (
    ImportLocation = "location"
    ImportCategory = "category"
    ImportPart     = "part"
)

// ImportedTarget returns the Drawered id previously created for a source
// entity, if the link exists and the target still exists. Soft-deleted
// parts count as existing: someone deleted them on purpose.
func (s *Service) ImportedTarget(ctx context.Context, source, kind string, sourceID int64) (int64, bool, error) {
    var target int64
    err := s.DB.R.QueryRowContext(ctx, `SELECT target_id FROM import_links WHERE source = ? AND kind = ? AND source_id = ?`,
        source, kind, sourceID).Scan(&target)
    if err == sql.ErrNoRows {
        return 0, false, nil
    }
    if err != nil {
        return 0, false, err
    }
    table := map[string]string{ImportLocation: "locations", ImportCategory: "categories", ImportPart: "parts"}[kind]
    if table == "" {
        return 0, false, Invalid("invalid_kind", "unknown import kind %q", kind)
    }
    var n int
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, target).Scan(&n); err != nil {
        return 0, false, err
    }
    return target, n > 0, nil
}

// RecordImport links a source entity to the Drawered entity created for it.
func (s *Service) RecordImport(ctx context.Context, source, kind string, sourceID, targetID int64) error {
    _, err := s.DB.W.ExecContext(ctx, `INSERT INTO import_links (source, kind, source_id, target_id, imported_at)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT (source, kind, source_id) DO UPDATE SET target_id = excluded.target_id, imported_at = excluded.imported_at`,
        source, kind, sourceID, targetID, db.Now())
    return err
}

// FindChildLocation returns the location with the given name under parent
// (nil for top level), compared case-insensitively.
func (s *Service) FindChildLocation(ctx context.Context, parent *int64, name string) (*Location, error) {
    ix, err := loadLocations(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    var key int64
    if parent != nil {
        key = *parent
    }
    for _, id := range ix.children[key] {
        if strings.EqualFold(ix.byID[id].Name, name) {
            l := ix.view(id, nil)
            return &l, nil
        }
    }
    return nil, nil
}

// FindChildCategory returns the category with the given name under parent
// (nil for top level), compared case-insensitively.
func (s *Service) FindChildCategory(ctx context.Context, parent *int64, name string) (*Category, error) {
    ix, err := loadCategories(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    var key int64
    if parent != nil {
        key = *parent
    }
    for _, id := range ix.children[key] {
        if strings.EqualFold(ix.byID[id].Name, name) {
            c := ix.view(id, nil)
            return &c, nil
        }
    }
    return nil, nil
}
