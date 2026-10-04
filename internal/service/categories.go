package service

import (
    "context"
    "database/sql"
    "regexp"
    "strings"

    "drawered/internal/db"
)

var iconRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

type catNode struct {
    ID          int64
    ParentID    *int64
    Name        string
    Description string
    Icon        *string
    Structural  bool
    SortOrder   int
    Version     int64
    CreatedAt   string
    UpdatedAt   string
}

// catIndex is an in-memory snapshot of the category tree.
type catIndex struct {
    byID     map[int64]*catNode
    children map[int64][]int64 // key 0 holds roots
}

func loadCategories(ctx context.Context, q db.Querier) (*catIndex, error) {
    rows, err := q.QueryContext(ctx, `SELECT id, parent_id, name, description, icon, structural, sort_order,
            version, created_at, updated_at
        FROM categories ORDER BY sort_order, name COLLATE NOCASE`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    ix := &catIndex{byID: map[int64]*catNode{}, children: map[int64][]int64{}}
    for rows.Next() {
        n := &catNode{}
        var parent sql.NullInt64
        var icon sql.NullString
        if err := rows.Scan(&n.ID, &parent, &n.Name, &n.Description, &icon, &n.Structural, &n.SortOrder,
            &n.Version, &n.CreatedAt, &n.UpdatedAt); err != nil {
            return nil, err
        }
        n.ParentID, n.Icon = nullInt(parent), nullStr(icon)
        ix.byID[n.ID] = n
        var key int64
        if n.ParentID != nil {
            key = *n.ParentID
        }
        ix.children[key] = append(ix.children[key], n.ID)
    }
    return ix, rows.Err()
}

func (ix *catIndex) ancestors(id int64) []*catNode {
    var chain []*catNode
    seen := map[int64]bool{}
    for n, ok := ix.byID[id]; ok && !seen[n.ID]; {
        seen[n.ID] = true
        chain = append([]*catNode{n}, chain...)
        if n.ParentID == nil {
            break
        }
        n, ok = ix.byID[*n.ParentID]
    }
    return chain
}

func (ix *catIndex) path(id int64) string {
    var names []string
    for _, n := range ix.ancestors(id) {
        names = append(names, n.Name)
    }
    return strings.Join(names, PathSeparator)
}

func (ix *catIndex) effectiveIcon(id int64) *string {
    chain := ix.ancestors(id)
    for i := len(chain) - 1; i >= 0; i-- {
        if chain[i].Icon != nil {
            return chain[i].Icon
        }
    }
    return nil
}

func (ix *catIndex) subtree(id int64) []int64 {
    out := []int64{id}
    for i := 0; i < len(out); i++ {
        out = append(out, ix.children[out[i]]...)
    }
    return out
}

// CategoryRef is the compact category representation embedded in parts.
type CategoryRef struct {
    ID            int64   `json:"id"`
    Name          string  `json:"name"`
    Path          string  `json:"path"`
    Icon          *string `json:"icon"`
    EffectiveIcon *string `json:"effective_icon"`
}

func (ix *catIndex) ref(id *int64) *CategoryRef {
    if id == nil {
        return nil
    }
    n, ok := ix.byID[*id]
    if !ok {
        return nil
    }
    return &CategoryRef{ID: n.ID, Name: n.Name, Path: ix.path(n.ID), Icon: n.Icon, EffectiveIcon: ix.effectiveIcon(n.ID)}
}

// Category is the API representation of a part category.
type Category struct {
    ID             int64   `json:"id"`
    ParentID       *int64  `json:"parent_id"`
    Name           string  `json:"name"`
    Description    string  `json:"description"`
    Icon           *string `json:"icon"`
    EffectiveIcon  *string `json:"effective_icon"`
    Structural     bool    `json:"structural"`
    SortOrder      int     `json:"sort_order"`
    Path           string  `json:"path"`
    Ancestors      []Ref   `json:"ancestors"`
    Version        int64   `json:"version"`
    CreatedAt      string  `json:"created_at"`
    UpdatedAt      string  `json:"updated_at"`
    PartCount      int     `json:"part_count"`
    TotalPartCount int     `json:"total_part_count"`
    ChildCount     int     `json:"child_count"`
}

func (ix *catIndex) view(id int64, counts map[int64]int) Category {
    n := ix.byID[id]
    c := Category{
        ID: n.ID, ParentID: n.ParentID, Name: n.Name, Description: n.Description, Icon: n.Icon,
        EffectiveIcon: ix.effectiveIcon(id), Structural: n.Structural, SortOrder: n.SortOrder,
        Path: ix.path(id), Ancestors: []Ref{}, Version: n.Version, CreatedAt: n.CreatedAt,
        UpdatedAt: n.UpdatedAt, ChildCount: len(ix.children[id]),
    }
    chain := ix.ancestors(id)
    for _, a := range chain[:len(chain)-1] {
        c.Ancestors = append(c.Ancestors, Ref{a.ID, a.Name})
    }
    c.PartCount = counts[id]
    for _, d := range ix.subtree(id) {
        c.TotalPartCount += counts[d]
    }
    return c
}

func categoryCounts(ctx context.Context, q db.Querier) (map[int64]int, error) {
    rows, err := q.QueryContext(ctx, `SELECT category_id, COUNT(*) FROM parts
        WHERE category_id IS NOT NULL AND deleted_at IS NULL GROUP BY category_id`)
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

// ListCategories returns every category, ordered for tree display.
func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
    ix, err := loadCategories(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    counts, err := categoryCounts(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    out := []Category{}
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

// GetCategory returns one category.
func (s *Service) GetCategory(ctx context.Context, id int64) (*Category, error) {
    ix, err := loadCategories(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    if _, ok := ix.byID[id]; !ok {
        return nil, NotFound("category")
    }
    counts, err := categoryCounts(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    c := ix.view(id, counts)
    return &c, nil
}

// CategoryInput holds fields for creating a category.
type CategoryInput struct {
    ParentID    *int64  `json:"parent_id"`
    Name        string  `json:"name"`
    Description string  `json:"description"`
    Icon        *string `json:"icon"`
    Structural  bool    `json:"structural"`
    SortOrder   int     `json:"sort_order"`
}

func normaliseIcon(icon *string) (*string, error) {
    if icon == nil {
        return nil, nil
    }
    v := strings.TrimPrefix(strings.TrimSpace(*icon), "ti-")
    if v == "" {
        return nil, nil
    }
    if !iconRe.MatchString(v) {
        return nil, Invalid("invalid_icon", "icon must be an icon name such as \"bolt\"")
    }
    return &v, nil
}

func categoryConflict(err error) error {
    if db.IsUniqueViolation(err) {
        return Conflict("duplicate_name", "a category with that name already exists here")
    }
    return err
}

// CreateCategory creates a new category.
func (s *Service) CreateCategory(ctx context.Context, in CategoryInput) (*Category, error) {
    name, err := validateLocationName(in.Name)
    if err != nil {
        return nil, err
    }
    if err := checkLen("description", in.Description, 0, 10000); err != nil {
        return nil, err
    }
    icon, err := normaliseIcon(in.Icon)
    if err != nil {
        return nil, err
    }
    var id int64
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        ix, err := loadCategories(ctx, tx)
        if err != nil {
            return err
        }
        if in.ParentID != nil {
            if _, ok := ix.byID[*in.ParentID]; !ok {
                return Invalid("invalid_parent", "parent category does not exist")
            }
        }
        now := db.Now()
        res, err := tx.ExecContext(ctx, `INSERT INTO categories (parent_id, name, description, icon, structural, sort_order, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, in.ParentID, name, in.Description, icon, in.Structural, in.SortOrder, now, now)
        if err != nil {
            return categoryConflict(err)
        }
        id, _ = res.LastInsertId()
        if ix, err = loadCategories(ctx, tx); err != nil {
            return err
        }
        var related []Subject
        if in.ParentID != nil {
            related = append(related, Subject{"category", *in.ParentID})
        }
        return s.event(ctx, tx, "category.created", Subject{"category", id}, map[string]any{
            "category": Ref{id, name}, "path": ix.path(id), "icon": icon, "structural": in.Structural,
        }, related...)
    })
    if err != nil {
        return nil, err
    }
    return s.GetCategory(ctx, id)
}

// UpdateCategory applies a partial update, including moves via parent_id.
func (s *Service) UpdateCategory(ctx context.Context, id int64, p Patch) (*Category, error) {
    version, err := p.Version()
    if err != nil {
        return nil, err
    }
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        ix, err := loadCategories(ctx, tx)
        if err != nil {
            return err
        }
        cur, ok := ix.byID[id]
        if !ok {
            return NotFound("category")
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
        if p.Has("icon") {
            var icon *string
            if _, err := p.Get("icon", &icon); err != nil {
                return err
            }
            if next.Icon, err = normaliseIcon(icon); err != nil {
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
                    return Invalid("invalid_parent", "parent category does not exist")
                }
                for _, d := range ix.subtree(id) {
                    if d == *parent {
                        return Conflict("category_cycle", "a category cannot be moved inside itself")
                    }
                }
            }
            next.ParentID = parent
        }
        if next.Structural && !cur.Structural {
            var n int
            if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM parts WHERE category_id = ?`, id).Scan(&n); err != nil {
                return err
            }
            if n > 0 {
                return &Error{Status: 409, Code: "category_has_parts",
                    Message: "a category with parts (including deleted parts) cannot be made structural",
                    Details: map[string]any{"part_count": n}}
            }
        }

        ch := changes{}
        ch.add("name", cur.Name, next.Name)
        ch.add("description", cur.Description, next.Description)
        ch.add("icon", derefOr(cur.Icon), derefOr(next.Icon))
        ch.add("structural", cur.Structural, next.Structural)
        ch.add("sort_order", cur.SortOrder, next.SortOrder)
        moved := !sameParent(cur.ParentID, next.ParentID)
        if len(ch) == 0 && !moved {
            return nil
        }
        res, err := tx.ExecContext(ctx, `UPDATE categories SET parent_id = ?, name = ?, description = ?, icon = ?,
                structural = ?, sort_order = ?, version = version + 1, updated_at = ?
            WHERE id = ? AND version = ?`, next.ParentID, next.Name, next.Description, next.Icon,
            next.Structural, next.SortOrder, db.Now(), id, version)
        if err != nil {
            return categoryConflict(err)
        }
        if n, _ := res.RowsAffected(); n == 0 {
            return StaleVersion()
        }
        related := []Subject{}
        if cur.ParentID != nil {
            related = append(related, Subject{"category", *cur.ParentID})
        }
        if next.ParentID != nil {
            related = append(related, Subject{"category", *next.ParentID})
        }
        if ix, err = loadCategories(ctx, tx); err != nil {
            return err
        }
        newPath := ix.path(id)
        if oldPath != newPath {
            ch["path"] = Change{From: oldPath, To: newPath}
            sub := ix.subtree(id)
            parts, err := queryInt64s(ctx, tx, `SELECT id FROM parts WHERE category_id IN (`+placeholders(len(sub))+`)`,
                int64Args(sub)...)
            if err != nil {
                return err
            }
            if err := s.reindexParts(ctx, tx, parts); err != nil {
                return err
            }
        }
        action := "category.updated"
        if moved {
            action = "category.moved"
        }
        return s.event(ctx, tx, action, Subject{"category", id}, map[string]any{
            "category": Ref{id, next.Name}, "path": newPath, "changes": ch,
        }, related...)
    })
    if err != nil {
        return nil, err
    }
    return s.GetCategory(ctx, id)
}

// DeleteCategory deletes a category with no children and no live parts.
// Soft-deleted parts that reference it have their category cleared.
func (s *Service) DeleteCategory(ctx context.Context, id int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        ix, err := loadCategories(ctx, tx)
        if err != nil {
            return err
        }
        n, ok := ix.byID[id]
        if !ok {
            return NotFound("category")
        }
        if len(ix.children[id]) > 0 {
            return Conflict("category_not_empty", "category has child categories")
        }
        var live int
        if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM parts WHERE category_id = ? AND deleted_at IS NULL`, id).Scan(&live); err != nil {
            return err
        }
        if live > 0 {
            return &Error{Status: 409, Code: "category_not_empty", Message: "category still has parts",
                Details: map[string]any{"part_count": live}}
        }
        cleared, err := queryInt64s(ctx, tx, `SELECT id FROM parts WHERE category_id = ?`, id)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `UPDATE parts SET category_id = NULL WHERE category_id = ?`, id); err != nil {
            return err
        }
        path := ix.path(id)
        if _, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id); err != nil {
            return err
        }
        related := []Subject{}
        if n.ParentID != nil {
            related = append(related, Subject{"category", *n.ParentID})
        }
        for _, pid := range cleared {
            related = append(related, Subject{"part", pid})
        }
        return s.event(ctx, tx, "category.deleted", Subject{"category", id}, map[string]any{
            "category": Ref{id, n.Name}, "path": path, "cleared_deleted_parts": cleared,
        }, related...)
    })
}

// checkPartCategory validates a category for assignment to a part.
func checkPartCategory(ix *catIndex, id *int64) error {
    if id == nil {
        return nil
    }
    n, ok := ix.byID[*id]
    if !ok {
        return Invalid("invalid_category", "category does not exist")
    }
    if n.Structural {
        return Conflict("structural_category", "%s is structural and cannot hold parts", ix.path(*id))
    }
    return nil
}

// categoryPathSQL computes a part's category path for the search index.
// Segment order does not matter for full-text matching.
const categoryPathSQL = `WITH RECURSIVE chain (id, parent_id, name) AS (
        SELECT c.id, c.parent_id, c.name FROM categories c JOIN parts p ON p.category_id = c.id WHERE p.id = ?
        UNION ALL
        SELECT c.id, c.parent_id, c.name FROM categories c JOIN chain ON c.id = chain.parent_id
    )
    SELECT IFNULL(group_concat(name, ' / '), '') FROM chain`
