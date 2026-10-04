package service

import (
    "context"
    "database/sql"
    "regexp"
    "sort"
    "strings"

    "drawered/internal/db"
    "drawered/internal/decimal"
)

// PathSeparator joins location names into a path.
const PathSeparator = " / "

var colourRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type locNode struct {
    ID          int64
    ParentID    *int64
    Name        string
    Description string
    Colour      *string
    Structural  bool
    SortOrder   int
    Version     int64
    CreatedAt   string
    UpdatedAt   string
}

// locIndex is an in-memory snapshot of the location tree used for path
// computation and subtree queries.
type locIndex struct {
    byID     map[int64]*locNode
    children map[int64][]int64 // key 0 holds roots
}

func loadLocations(ctx context.Context, q db.Querier) (*locIndex, error) {
    rows, err := q.QueryContext(ctx, `SELECT id, parent_id, name, description, colour, structural, sort_order,
            version, created_at, updated_at
        FROM locations ORDER BY sort_order, name COLLATE NOCASE`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    ix := &locIndex{byID: map[int64]*locNode{}, children: map[int64][]int64{}}
    for rows.Next() {
        n := &locNode{}
        var parent sql.NullInt64
        var colour sql.NullString
        if err := rows.Scan(&n.ID, &parent, &n.Name, &n.Description, &colour, &n.Structural, &n.SortOrder,
            &n.Version, &n.CreatedAt, &n.UpdatedAt); err != nil {
            return nil, err
        }
        n.ParentID = nullInt(parent)
        n.Colour = nullStr(colour)
        ix.byID[n.ID] = n
        var key int64
        if n.ParentID != nil {
            key = *n.ParentID
        }
        ix.children[key] = append(ix.children[key], n.ID)
    }
    return ix, rows.Err()
}

func (ix *locIndex) ancestors(id int64) []*locNode {
    var chain []*locNode
    seen := map[int64]bool{}
    for n, ok := ix.byID[id]; ok && !seen[n.ID]; {
        seen[n.ID] = true
        chain = append([]*locNode{n}, chain...)
        if n.ParentID == nil {
            break
        }
        n, ok = ix.byID[*n.ParentID]
    }
    return chain
}

func (ix *locIndex) path(id int64) string {
    var names []string
    for _, n := range ix.ancestors(id) {
        names = append(names, n.Name)
    }
    return strings.Join(names, PathSeparator)
}

func (ix *locIndex) effectiveColour(id int64) *string {
    chain := ix.ancestors(id)
    for i := len(chain) - 1; i >= 0; i-- {
        if chain[i].Colour != nil {
            return chain[i].Colour
        }
    }
    return nil
}

// subtree returns id and all of its descendants.
func (ix *locIndex) subtree(id int64) []int64 {
    out := []int64{id}
    for i := 0; i < len(out); i++ {
        out = append(out, ix.children[out[i]]...)
    }
    return out
}

func (ix *locIndex) allIDs() []int64 {
    out := make([]int64, 0, len(ix.byID))
    for id := range ix.byID {
        out = append(out, id)
    }
    sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
    return out
}

// Location is the API representation of a location.
type Location struct {
    ID              int64   `json:"id"`
    ParentID        *int64  `json:"parent_id"`
    Name            string  `json:"name"`
    Description     string  `json:"description"`
    Colour          *string `json:"colour"`
    EffectiveColour *string `json:"effective_colour"`
    Structural      bool    `json:"structural"`
    SortOrder       int     `json:"sort_order"`
    Path            string  `json:"path"`
    Ancestors       []Ref   `json:"ancestors"`
    Version         int64   `json:"version"`
    CreatedAt       string  `json:"created_at"`
    UpdatedAt       string  `json:"updated_at"`
    PartCount       int     `json:"part_count"`
    TotalPartCount  int     `json:"total_part_count"`
    ChildCount      int     `json:"child_count"`
}

func (ix *locIndex) view(id int64, counts map[int64]int) Location {
    n := ix.byID[id]
    l := Location{
        ID: n.ID, ParentID: n.ParentID, Name: n.Name, Description: n.Description, Colour: n.Colour,
        EffectiveColour: ix.effectiveColour(id), Structural: n.Structural, SortOrder: n.SortOrder,
        Path: ix.path(id), Ancestors: []Ref{}, Version: n.Version, CreatedAt: n.CreatedAt,
        UpdatedAt: n.UpdatedAt, ChildCount: len(ix.children[id]),
    }
    chain := ix.ancestors(id)
    for _, a := range chain[:len(chain)-1] {
        l.Ancestors = append(l.Ancestors, Ref{a.ID, a.Name})
    }
    if counts != nil {
        l.PartCount = counts[id]
        for _, d := range ix.subtree(id) {
            l.TotalPartCount += counts[d]
        }
    }
    return l
}

func stockCounts(ctx context.Context, q db.Querier) (map[int64]int, error) {
    rows, err := q.QueryContext(ctx, `SELECT s.location_id, COUNT(*) FROM stock s
        JOIN parts p ON p.id = s.part_id AND p.deleted_at IS NULL
        GROUP BY s.location_id`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    counts := map[int64]int{}
    for rows.Next() {
        var id int64
        var n int
        if err := rows.Scan(&id, &n); err != nil {
            return nil, err
        }
        counts[id] = n
    }
    return counts, rows.Err()
}

// ListLocations returns every location, ordered for tree display.
func (s *Service) ListLocations(ctx context.Context) ([]Location, error) {
    ix, err := loadLocations(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    counts, err := stockCounts(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    out := []Location{}
    var walk func(parent int64)
    walk = func(parent int64) {
        for _, id := range ix.children[parent] {
            out = append(out, ix.view(id, counts))
            walk(id)
        }
    }
    walk(0)
    return out, nil
}

// GetLocation returns one location.
func (s *Service) GetLocation(ctx context.Context, id int64) (*Location, error) {
    ix, err := loadLocations(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    if _, ok := ix.byID[id]; !ok {
        return nil, NotFound("location")
    }
    counts, err := stockCounts(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    l := ix.view(id, counts)
    return &l, nil
}

// SearchLocations performs a full-text search over location names, paths
// and descriptions.
func (s *Service) SearchLocations(ctx context.Context, q string, limit int) ([]Location, error) {
    match := ftsQuery(q)
    out := []Location{}
    if match == "" {
        return out, nil
    }
    if limit <= 0 || limit > 200 {
        limit = 50
    }
    ids, err := queryInt64s(ctx, s.DB.R, `SELECT rowid FROM locations_fts WHERE locations_fts MATCH ?
        ORDER BY bm25(locations_fts, 10, 5, 1) LIMIT ?`, match, limit)
    if err != nil {
        return nil, err
    }
    ix, err := loadLocations(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    for _, id := range ids {
        if _, ok := ix.byID[id]; ok {
            out = append(out, ix.view(id, nil))
        }
    }
    return out, nil
}

// LocationInput holds fields for creating a location.
type LocationInput struct {
    ParentID    *int64  `json:"parent_id"`
    Name        string  `json:"name"`
    Description string  `json:"description"`
    Colour      *string `json:"colour"`
    Structural  bool    `json:"structural"`
    SortOrder   int     `json:"sort_order"`
}

func validateLocationName(name string) (string, error) {
    name = strings.TrimSpace(name)
    if err := checkLen("name", name, 1, 100); err != nil {
        return "", err
    }
    if strings.Contains(name, "/") {
        return "", Invalid("invalid_name", "name must not contain '/'")
    }
    return name, nil
}

func normaliseColour(c *string) (*string, error) {
    if c == nil || *c == "" {
        return nil, nil
    }
    if !colourRe.MatchString(*c) {
        return nil, Invalid("invalid_colour", "colour must be #RRGGBB")
    }
    v := strings.ToLower(*c)
    return &v, nil
}

func siblingConflict(err error) error {
    if db.IsUniqueViolation(err) {
        return Conflict("duplicate_name", "a location with that name already exists here")
    }
    return err
}

// CreateLocation creates a new location.
func (s *Service) CreateLocation(ctx context.Context, in LocationInput) (*Location, error) {
    name, err := validateLocationName(in.Name)
    if err != nil {
        return nil, err
    }
    if err := checkLen("description", in.Description, 0, 10000); err != nil {
        return nil, err
    }
    colour, err := normaliseColour(in.Colour)
    if err != nil {
        return nil, err
    }
    var id int64
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        if in.ParentID != nil {
            if _, ok := ix.byID[*in.ParentID]; !ok {
                return Invalid("invalid_parent", "parent location does not exist")
            }
        }
        now := db.Now()
        res, err := tx.ExecContext(ctx, `INSERT INTO locations (parent_id, name, description, colour, structural, sort_order, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, in.ParentID, name, in.Description, colour, in.Structural, in.SortOrder, now, now)
        if err != nil {
            return siblingConflict(err)
        }
        id, _ = res.LastInsertId()
        if ix, err = loadLocations(ctx, tx); err != nil {
            return err
        }
        if err := reindexLocations(ctx, tx, ix, []int64{id}); err != nil {
            return err
        }
        var related []Subject
        if in.ParentID != nil {
            related = append(related, Subject{"location", *in.ParentID})
        }
        return s.event(ctx, tx, "location.created", Subject{"location", id}, map[string]any{
            "location": Ref{id, name}, "path": ix.path(id), "structural": in.Structural,
        }, related...)
    })
    if err != nil {
        return nil, err
    }
    return s.GetLocation(ctx, id)
}

// UpdateLocation applies a partial update, including moves via parent_id.
func (s *Service) UpdateLocation(ctx context.Context, id int64, p Patch) (*Location, error) {
    version, err := p.Version()
    if err != nil {
        return nil, err
    }
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        cur, ok := ix.byID[id]
        if !ok {
            return NotFound("location")
        }
        if cur.Version != version {
            return StaleVersion()
        }
        next := *cur
        oldPath := ix.path(id)
        if ok, err := p.Get("name", &next.Name); err != nil {
            return err
        } else if ok {
            if next.Name, err = validateLocationName(next.Name); err != nil {
                return err
            }
        }
        if _, err := p.Get("description", &next.Description); err != nil {
            return err
        }
        if err := checkLen("description", next.Description, 0, 10000); err != nil {
            return err
        }
        if p.Has("colour") {
            var c *string
            if _, err := p.Get("colour", &c); err != nil {
                return err
            }
            if next.Colour, err = normaliseColour(c); err != nil {
                return err
            }
        }
        if _, err := p.Get("structural", &next.Structural); err != nil {
            return err
        }
        if _, err := p.Get("sort_order", &next.SortOrder); err != nil {
            return err
        }
        if p.Has("parent_id") {
            var parent *int64
            if _, err := p.Get("parent_id", &parent); err != nil {
                return err
            }
            if parent != nil {
                if _, ok := ix.byID[*parent]; !ok {
                    return Invalid("invalid_parent", "parent location does not exist")
                }
                for _, d := range ix.subtree(id) {
                    if d == *parent {
                        return Conflict("location_cycle", "a location cannot be moved inside itself")
                    }
                }
            }
            next.ParentID = parent
        }
        if next.Structural && !cur.Structural {
            var n int
            if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock WHERE location_id = ?`, id).Scan(&n); err != nil {
                return err
            }
            if n > 0 {
                return Conflict("location_has_stock", "a location holding stock cannot be made structural")
            }
        }

        ch := changes{}
        ch.add("name", cur.Name, next.Name)
        ch.add("description", cur.Description, next.Description)
        ch.add("colour", derefOr(cur.Colour), derefOr(next.Colour))
        ch.add("structural", cur.Structural, next.Structural)
        ch.add("sort_order", cur.SortOrder, next.SortOrder)
        moved := !sameParent(cur.ParentID, next.ParentID)
        if len(ch) == 0 && !moved {
            return nil
        }
        res, err := tx.ExecContext(ctx, `UPDATE locations SET parent_id = ?, name = ?, description = ?, colour = ?,
                structural = ?, sort_order = ?, version = version + 1, updated_at = ?
            WHERE id = ? AND version = ?`, next.ParentID, next.Name, next.Description, next.Colour,
            next.Structural, next.SortOrder, db.Now(), id, version)
        if err != nil {
            return siblingConflict(err)
        }
        if n, _ := res.RowsAffected(); n == 0 {
            return StaleVersion()
        }

        related := []Subject{}
        if cur.ParentID != nil {
            related = append(related, Subject{"location", *cur.ParentID})
        }
        if next.ParentID != nil {
            related = append(related, Subject{"location", *next.ParentID})
        }
        if ix, err = loadLocations(ctx, tx); err != nil {
            return err
        }
        newPath := ix.path(id)
        action := "location.updated"
        if moved {
            action = "location.moved"
        }
        if oldPath != newPath {
            ch["path"] = Change{From: oldPath, To: newPath}
            sub := ix.subtree(id)
            if err := reindexLocations(ctx, tx, ix, sub); err != nil {
                return err
            }
            parts, err := queryInt64s(ctx, tx, `SELECT DISTINCT part_id FROM stock WHERE location_id IN (`+
                placeholders(len(sub))+`)`, int64Args(sub)...)
            if err != nil {
                return err
            }
            for _, pid := range parts {
                if err := reindexPart(ctx, tx, ix, pid); err != nil {
                    return err
                }
            }
        } else if err := reindexLocations(ctx, tx, ix, []int64{id}); err != nil {
            return err
        }
        return s.event(ctx, tx, action, Subject{"location", id}, map[string]any{
            "location": Ref{id, next.Name}, "path": newPath, "changes": ch,
        }, related...)
    })
    if err != nil {
        return nil, err
    }
    return s.GetLocation(ctx, id)
}

func derefOr(s *string) string {
    if s == nil {
        return ""
    }
    return *s
}

func sameParent(a, b *int64) bool {
    if a == nil || b == nil {
        return a == nil && b == nil
    }
    return *a == *b
}

// DeleteLocation deletes a location with no children and no stock.
func (s *Service) DeleteLocation(ctx context.Context, id int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        n, ok := ix.byID[id]
        if !ok {
            return NotFound("location")
        }
        if len(ix.children[id]) > 0 {
            return Conflict("location_not_empty", "location has child locations")
        }
        var held int
        if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock WHERE location_id = ? AND quantity_milli > 0`, id).Scan(&held); err != nil {
            return err
        }
        if held > 0 {
            return Conflict("location_not_empty", "location holds stock")
        }
        parts, err := queryInt64s(ctx, tx, `SELECT part_id FROM stock WHERE location_id = ?`, id)
        if err != nil {
            return err
        }
        path := ix.path(id)
        if _, err := tx.ExecContext(ctx, `DELETE FROM stock WHERE location_id = ?`, id); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id = ?`, id); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM locations_fts WHERE rowid = ?`, id); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, parts); err != nil {
            return err
        }
        related := []Subject{}
        if n.ParentID != nil {
            related = append(related, Subject{"location", *n.ParentID})
        }
        return s.event(ctx, tx, "location.deleted", Subject{"location", id}, map[string]any{
            "location": Ref{id, n.Name}, "path": path,
        }, related...)
    })
}

// LocationStockItem is a part held at a location.
type LocationStockItem struct {
    Part            Ref            `json:"part"`
    MPN             string         `json:"mpn"`
    UOM             string         `json:"uom"`
    ThumbnailFileID *int64         `json:"thumbnail_file_id"`
    LocationID      int64          `json:"location_id"`
    Path            string         `json:"path"`
    Quantity        decimal.Milli  `json:"quantity"`
    MinQuantity     *decimal.Milli `json:"min_quantity"`
    Note            string         `json:"note"`
    Low             bool           `json:"low"`
}

// LocationStock lists stock held at a location, optionally including its
// descendants.
func (s *Service) LocationStock(ctx context.Context, id int64, descendants bool, pg Paging) (*Page[LocationStockItem], error) {
    pg = pg.Normalise()
    ix, err := loadLocations(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    if _, ok := ix.byID[id]; !ok {
        return nil, NotFound("location")
    }
    ids := []int64{id}
    if descendants {
        ids = ix.subtree(id)
    }
    where := `s.location_id IN (` + placeholders(len(ids)) + `) AND p.deleted_at IS NULL`
    args := int64Args(ids)
    out := &Page[LocationStockItem]{Items: []LocationStockItem{}}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock s JOIN parts p ON p.id = s.part_id WHERE `+where,
        args...).Scan(&out.Total); err != nil {
        return nil, err
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT p.id, p.name, p.mpn, p.uom, `+thumbnailExpr+`,
            s.location_id, s.quantity_milli, s.min_quantity_milli, s.note
        FROM stock s JOIN parts p ON p.id = s.part_id
        WHERE `+where+` ORDER BY p.name COLLATE NOCASE, s.location_id LIMIT ? OFFSET ?`,
        append(args, pg.Limit, pg.Offset)...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    for rows.Next() {
        var it LocationStockItem
        var thumb, min sql.NullInt64
        if err := rows.Scan(&it.Part.ID, &it.Part.Name, &it.MPN, &it.UOM, &thumb, &it.LocationID,
            &it.Quantity, &min, &it.Note); err != nil {
            return nil, err
        }
        it.ThumbnailFileID = nullInt(thumb)
        if min.Valid {
            m := decimal.Milli(min.Int64)
            it.MinQuantity = &m
            it.Low = it.Quantity < m
        }
        it.Path = ix.path(it.LocationID)
        out.Items = append(out.Items, it)
    }
    return out, rows.Err()
}

// thumbnailExpr selects a part's thumbnail file id, falling back to its
// first image. It expects the parts table aliased as p.
const thumbnailExpr = `COALESCE(
        (SELECT i.file_id FROM part_images i WHERE i.id = p.thumbnail_image_id),
        (SELECT i.file_id FROM part_images i WHERE i.part_id = p.id ORDER BY i.sort_order, i.id LIMIT 1))`
