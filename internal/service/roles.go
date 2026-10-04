package service

import (
    "context"
    "database/sql"
    "regexp"
    "sort"
    "strings"

    "drawered/internal/db"
    "drawered/internal/perm"
)

// Role is the API representation of a role.
type Role struct {
    ID           int64             `json:"id"`
    Name         string            `json:"name"`
    Description  string            `json:"description"`
    Builtin      bool              `json:"builtin"`
    Permissions  []perm.Permission `json:"permissions"`
    UserCount    int               `json:"user_count"`
    MappingCount int               `json:"mapping_count"`
    IsDefault    bool              `json:"is_default"`
    Version      int64             `json:"version"`
}

func loadRoles(ctx context.Context, q db.Querier, id *int64) ([]Role, error) {
    where, args := "", []any{}
    if id != nil {
        where, args = "WHERE r.id = ?", append(args, *id)
    }
    rows, err := q.QueryContext(ctx, `SELECT r.id, r.name, r.description, r.builtin, r.version,
            (SELECT COUNT(DISTINCT user_id) FROM user_roles ur WHERE ur.role_id = r.id),
            (SELECT COUNT(*) FROM role_mappings m WHERE m.role_id = r.id)
        FROM roles r `+where+` ORDER BY r.builtin, r.name COLLATE NOCASE`, args...)
    if err != nil {
        return nil, err
    }
    out := []Role{}
    for rows.Next() {
        var r Role
        if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Builtin, &r.Version, &r.UserCount, &r.MappingCount); err != nil {
            rows.Close()
            return nil, err
        }
        out = append(out, r)
    }
    rows.Close()
    var def *int64
    if err := getSetting(ctx, q, "default_role_id", &def); err != nil {
        return nil, err
    }
    for i := range out {
        ps, err := queryStrings(ctx, q, `SELECT permission FROM role_permissions WHERE role_id = ?`, out[i].ID)
        if err != nil {
            return nil, err
        }
        out[i].Permissions = []perm.Permission{}
        for _, p := range ps {
            out[i].Permissions = append(out[i].Permissions, perm.Permission(p))
        }
        sortPerms(out[i].Permissions)
        out[i].IsDefault = def != nil && *def == out[i].ID
    }
    return out, nil
}

func sortPerms(ps []perm.Permission) {
    order := map[perm.Permission]int{}
    for i, p := range perm.All {
        order[p.Name] = i
    }
    sort.Slice(ps, func(i, j int) bool { return order[ps[i]] < order[ps[j]] })
}

// ListRoles returns every role.
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
    return loadRoles(ctx, s.DB.R, nil)
}

// GetRole returns one role.
func (s *Service) GetRole(ctx context.Context, id int64) (*Role, error) {
    return getRole(ctx, s.DB.R, id)
}

func getRole(ctx context.Context, q db.Querier, id int64) (*Role, error) {
    rs, err := loadRoles(ctx, q, &id)
    if err != nil {
        return nil, err
    }
    if len(rs) == 0 {
        return nil, NotFound("role")
    }
    return &rs[0], nil
}

func validatePerms(in []perm.Permission) ([]perm.Permission, error) {
    seen := map[perm.Permission]bool{}
    out := []perm.Permission{}
    for _, p := range in {
        if !perm.Valid(p) {
            return nil, Invalid("invalid_permission", "unknown permission %q", p)
        }
        if !seen[p] {
            seen[p] = true
            out = append(out, p)
        }
    }
    sortPerms(out)
    return out, nil
}

func permString(ps []perm.Permission) string {
    s := make([]string, len(ps))
    for i, p := range ps {
        s[i] = string(p)
    }
    return strings.Join(s, ", ")
}

// RoleInput holds fields for creating a role.
type RoleInput struct {
    Name        string            `json:"name"`
    Description string            `json:"description"`
    Permissions []perm.Permission `json:"permissions"`
}

// CreateRole creates a role.
func (s *Service) CreateRole(ctx context.Context, in RoleInput) (*Role, error) {
    name := strings.TrimSpace(in.Name)
    if err := checkLen("name", name, 1, 50); err != nil {
        return nil, err
    }
    if err := checkLen("description", in.Description, 0, 500); err != nil {
        return nil, err
    }
    perms, err := validatePerms(in.Permissions)
    if err != nil {
        return nil, err
    }
    var id int64
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        now := db.Now()
        res, err := tx.ExecContext(ctx, `INSERT INTO roles (name, description, created_at, updated_at) VALUES (?, ?, ?, ?)`,
            name, in.Description, now, now)
        if db.IsUniqueViolation(err) {
            return Conflict("duplicate_name", "a role called %q already exists", name)
        }
        if err != nil {
            return err
        }
        id, _ = res.LastInsertId()
        for _, p := range perms {
            if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)`, id, p); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "role.created", Subject{"role", id}, map[string]any{
            "role": Ref{id, name}, "permissions": perms,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetRole(ctx, id)
}

// UpdateRole changes a role's name, description or permissions.
func (s *Service) UpdateRole(ctx context.Context, id int64, p Patch) (*Role, error) {
    version, err := p.Version()
    if err != nil {
        return nil, err
    }
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := getRole(ctx, tx, id)
        if err != nil {
            return err
        }
        if cur.Version != version {
            return StaleVersion()
        }
        name, desc, perms := cur.Name, cur.Description, cur.Permissions
        if _, err := p.Get("name", &name); err != nil {
            return err
        }
        if _, err := p.Get("description", &desc); err != nil {
            return err
        }
        if _, err := p.Get("permissions", &perms); err != nil {
            return err
        }
        name = strings.TrimSpace(name)
        if err := checkLen("name", name, 1, 50); err != nil {
            return err
        }
        if err := checkLen("description", desc, 0, 500); err != nil {
            return err
        }
        if perms, err = validatePerms(perms); err != nil {
            return err
        }
        if cur.Builtin {
            if name != cur.Name {
                return Conflict("builtin_role", "the built-in Admin role cannot be renamed")
            }
            hasAdmin := false
            for _, p := range perms {
                hasAdmin = hasAdmin || p == perm.SystemAdmin
            }
            if !hasAdmin {
                return Conflict("builtin_role", "the built-in Admin role must keep system:admin")
            }
        }
        ch := changes{}
        ch.add("name", cur.Name, name)
        ch.add("description", cur.Description, desc)
        ch.add("permissions", permString(cur.Permissions), permString(perms))
        if len(ch) == 0 {
            return nil
        }
        admins, err := countAdmins(ctx, tx)
        if err != nil {
            return err
        }
        _, err = tx.ExecContext(ctx, `UPDATE roles SET name = ?, description = ?, version = version + 1, updated_at = ? WHERE id = ?`,
            name, desc, db.Now(), id)
        if db.IsUniqueViolation(err) {
            return Conflict("duplicate_name", "a role called %q already exists", name)
        }
        if err != nil {
            return err
        }
        if _, ok := ch["permissions"]; ok {
            if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, id); err != nil {
                return err
            }
            for _, p := range perms {
                if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)`, id, p); err != nil {
                    return err
                }
            }
            if err := guardAdmins(ctx, tx, admins); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "role.updated", Subject{"role", id}, map[string]any{
            "role": Ref{id, name}, "changes": ch,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetRole(ctx, id)
}

// DeleteRole deletes a role. A role that is held or mapped needs force,
// which also removes those assignments and mappings.
func (s *Service) DeleteRole(ctx context.Context, id int64, force bool) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := getRole(ctx, tx, id)
        if err != nil {
            return err
        }
        if cur.Builtin {
            return Conflict("builtin_role", "the built-in Admin role cannot be deleted")
        }
        if (cur.UserCount > 0 || cur.MappingCount > 0) && !force {
            return &Error{Status: 409, Code: "role_in_use", Message: "role is assigned or mapped; delete with force to proceed",
                Details: map[string]any{"user_count": cur.UserCount, "mapping_count": cur.MappingCount}}
        }
        admins, err := countAdmins(ctx, tx)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, id); err != nil {
            return err
        }
        if err := guardAdmins(ctx, tx, admins); err != nil {
            return err
        }
        if cur.IsDefault {
            if err := setSetting(ctx, tx, "default_role_id", nil); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "role.deleted", Subject{"role", id}, map[string]any{
            "role": Ref{id, cur.Name}, "user_count": cur.UserCount, "mapping_count": cur.MappingCount,
            "was_default": cur.IsDefault,
        })
    })
}

// MappingInput holds fields for a claim mapping.
type MappingInput struct {
    Claim     string `json:"claim"`
    MatchType string `json:"match_type"`
    Value     string `json:"value"`
    RoleID    int64  `json:"role_id"`
    SortOrder int    `json:"sort_order"`
}

func (m *MappingInput) validate() error {
    m.Claim, m.Value = strings.TrimSpace(m.Claim), strings.TrimSpace(m.Value)
    if err := checkLen("claim", m.Claim, 1, 100); err != nil {
        return err
    }
    if err := checkLen("value", m.Value, 1, 500); err != nil {
        return err
    }
    switch m.MatchType {
    case "equals", "contains", "ends_with":
    case "regex":
        if _, err := regexp.Compile(m.Value); err != nil {
            return Invalid("invalid_regex", "invalid regular expression: %v", err)
        }
    default:
        return Invalid("invalid_match_type", "match_type must be equals, contains, ends_with or regex")
    }
    return nil
}

// ListMappings returns the claim mapping rules in evaluation order.
func (s *Service) ListMappings(ctx context.Context) ([]Mapping, error) {
    return loadMappings(ctx, s.DB.R)
}

func getMapping(ctx context.Context, q db.Querier, id int64) (*Mapping, error) {
    ms, err := loadMappings(ctx, q)
    if err != nil {
        return nil, err
    }
    for _, m := range ms {
        if m.ID == id {
            return &m, nil
        }
    }
    return nil, NotFound("mapping")
}

func mappingData(id int64, m MappingInput, role string) map[string]any {
    return map[string]any{"mapping_id": id, "claim": m.Claim, "match_type": m.MatchType, "value": m.Value, "role": Ref{m.RoleID, role}}
}

// CreateMapping adds a claim mapping rule.
func (s *Service) CreateMapping(ctx context.Context, in MappingInput) (*Mapping, error) {
    if err := in.validate(); err != nil {
        return nil, err
    }
    var id int64
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        role, err := roleName(ctx, tx, in.RoleID)
        if err != nil {
            return err
        }
        res, err := tx.ExecContext(ctx, `INSERT INTO role_mappings (claim, match_type, value, role_id, sort_order) VALUES (?, ?, ?, ?, ?)`,
            in.Claim, in.MatchType, in.Value, in.RoleID, in.SortOrder)
        if err != nil {
            return err
        }
        id, _ = res.LastInsertId()
        return s.event(ctx, tx, "mapping.created", Subject{"mapping", id}, mappingData(id, in, role), Subject{"role", in.RoleID})
    })
    if err != nil {
        return nil, err
    }
    return getMapping(ctx, s.DB.R, id)
}

// UpdateMapping changes a claim mapping rule.
func (s *Service) UpdateMapping(ctx context.Context, id int64, p Patch) (*Mapping, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := getMapping(ctx, tx, id)
        if err != nil {
            return err
        }
        in := MappingInput{Claim: cur.Claim, MatchType: cur.MatchType, Value: cur.Value, RoleID: cur.RoleID, SortOrder: cur.SortOrder}
        for _, f := range []struct {
            k string
            d any
        }{{"claim", &in.Claim}, {"match_type", &in.MatchType}, {"value", &in.Value}, {"role_id", &in.RoleID}, {"sort_order", &in.SortOrder}} {
            if _, err := p.Get(f.k, f.d); err != nil {
                return err
            }
        }
        if err := in.validate(); err != nil {
            return err
        }
        role, err := roleName(ctx, tx, in.RoleID)
        if err != nil {
            return err
        }
        ch := changes{}
        ch.add("claim", cur.Claim, in.Claim)
        ch.add("match_type", cur.MatchType, in.MatchType)
        ch.add("value", cur.Value, in.Value)
        ch.add("role", cur.RoleName, role)
        ch.add("sort_order", cur.SortOrder, in.SortOrder)
        if len(ch) == 0 {
            return nil
        }
        if _, err := tx.ExecContext(ctx, `UPDATE role_mappings SET claim = ?, match_type = ?, value = ?, role_id = ?, sort_order = ? WHERE id = ?`,
            in.Claim, in.MatchType, in.Value, in.RoleID, in.SortOrder, id); err != nil {
            return err
        }
        data := mappingData(id, in, role)
        data["changes"] = ch
        return s.event(ctx, tx, "mapping.updated", Subject{"mapping", id}, data, Subject{"role", in.RoleID}, Subject{"role", cur.RoleID})
    })
    if err != nil {
        return nil, err
    }
    return getMapping(ctx, s.DB.R, id)
}

// DeleteMapping removes a claim mapping rule. Roles already granted by it
// are removed at each user's next login.
func (s *Service) DeleteMapping(ctx context.Context, id int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := getMapping(ctx, tx, id)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM role_mappings WHERE id = ?`, id); err != nil {
            return err
        }
        in := MappingInput{Claim: cur.Claim, MatchType: cur.MatchType, Value: cur.Value, RoleID: cur.RoleID}
        return s.event(ctx, tx, "mapping.deleted", Subject{"mapping", id}, mappingData(id, in, cur.RoleName), Subject{"role", cur.RoleID})
    })
}

// MappingTestResult reports which rules match a claim set.
type MappingTestResult struct {
    Claims  map[string]any `json:"claims"`
    Matched []Mapping      `json:"matched"`
    Roles   []Ref          `json:"roles"`
}

// TestMappings evaluates the mapping rules against a claim set, or against
// a user's most recently seen claims.
func (s *Service) TestMappings(ctx context.Context, claims map[string]any, userID *int64) (*MappingTestResult, error) {
    if userID != nil {
        u, err := loadUser(ctx, s.DB.R, *userID)
        if err != nil {
            return nil, err
        }
        claims = u.claims
    }
    if claims == nil {
        claims = map[string]any{}
    }
    ms, err := loadMappings(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    out := &MappingTestResult{Claims: claims, Matched: []Mapping{}, Roles: []Ref{}}
    seen := map[int64]bool{}
    for _, m := range ms {
        if m.matches(claims) {
            out.Matched = append(out.Matched, m)
            if !seen[m.RoleID] {
                seen[m.RoleID] = true
                out.Roles = append(out.Roles, Ref{m.RoleID, m.RoleName})
            }
        }
    }
    return out, nil
}
