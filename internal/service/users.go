package service

import (
    "context"
    "crypto/rand"
    "crypto/sha256"
    "database/sql"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "regexp"
    "sort"
    "strings"
    "time"

    "drawered/internal/db"
    "drawered/internal/perm"
)

// Seed creates the default roles and settings on an empty database.
func (s *Service) Seed(ctx context.Context) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        var n int
        if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles`).Scan(&n); err != nil {
            return err
        }
        if n > 0 {
            return nil
        }
        now := db.Now()
        seeds := []struct {
            name, desc string
            builtin    bool
            perms      []perm.Permission
        }{
            {"Read only", "View inventory, locations and history.", false, perm.ReadOnlyPerms},
            {"Engineer", "Read only, plus add, remove and move stock and create and edit parts.", false, perm.EngineerPerms},
            {"Stores", "Engineer, plus stock-take, delete parts, manage locations and the catalogue.", false, perm.StoresPerms},
            {"Admin", "Everything.", true, perm.AdminPerms},
        }
        var readOnlyID int64
        for _, r := range seeds {
            res, err := tx.ExecContext(ctx, `INSERT INTO roles (name, description, builtin, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
                r.name, r.desc, r.builtin, now, now)
            if err != nil {
                return err
            }
            id, _ := res.LastInsertId()
            if r.name == "Read only" {
                readOnlyID = id
            }
            for _, p := range r.perms {
                if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)`, id, p); err != nil {
                    return err
                }
            }
        }
        return setSetting(ctx, tx, "default_role_id", readOnlyID)
    })
}

func setSetting(ctx context.Context, tx *sql.Tx, key string, v any) error {
    b, err := json.Marshal(v)
    if err != nil {
        return err
    }
    _, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?)
        ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json`, key, string(b))
    return err
}

func getSetting(ctx context.Context, q db.Querier, key string, dst any) error {
    var v string
    err := q.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&v)
    if err == sql.ErrNoRows {
        return nil
    }
    if err != nil {
        return err
    }
    return json.Unmarshal([]byte(v), dst)
}

// Settings are instance-wide settings editable in the admin UI.
type Settings struct {
    DefaultRoleID *int64 `json:"default_role_id"`
}

// GetSettings returns the instance settings.
func (s *Service) GetSettings(ctx context.Context) (*Settings, error) {
    st := &Settings{}
    return st, getSetting(ctx, s.DB.R, "default_role_id", &st.DefaultRoleID)
}

// UpdateSettings changes instance settings.
func (s *Service) UpdateSettings(ctx context.Context, p Patch) (*Settings, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur := &Settings{}
        if err := getSetting(ctx, tx, "default_role_id", &cur.DefaultRoleID); err != nil {
            return err
        }
        if !p.Has("default_role_id") {
            return nil
        }
        var next *int64
        if _, err := p.Get("default_role_id", &next); err != nil {
            return err
        }
        from, to := "", ""
        if cur.DefaultRoleID != nil {
            from, _ = roleName(ctx, tx, *cur.DefaultRoleID)
        }
        if next != nil {
            var err error
            if to, err = roleName(ctx, tx, *next); err != nil {
                return err
            }
        }
        if from == to {
            return nil
        }
        if err := setSetting(ctx, tx, "default_role_id", next); err != nil {
            return err
        }
        return s.event(ctx, tx, "settings.updated", Subject{"settings", 1}, map[string]any{
            "changes": changes{"default_role": {From: from, To: to}},
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetSettings(ctx)
}

func roleName(ctx context.Context, q db.Querier, id int64) (string, error) {
    var name string
    err := q.QueryRowContext(ctx, `SELECT name FROM roles WHERE id = ?`, id).Scan(&name)
    if err == sql.ErrNoRows {
        return "", Invalid("invalid_role", "role does not exist")
    }
    return name, err
}

// UserRole is one role held by a user and where it came from.
type UserRole struct {
    RoleID int64  `json:"role_id"`
    Name   string `json:"name"`
    Source string `json:"source"`
}

// User is the API representation of a user.
type User struct {
    ID           int64             `json:"id"`
    Issuer       string            `json:"issuer"`
    Subject      string            `json:"subject"`
    DisplayName  string            `json:"display_name"`
    Email        string            `json:"email"`
    Username     string            `json:"username"`
    Groups       []string          `json:"groups"`
    Disabled     bool              `json:"disabled"`
    FirstLoginAt string            `json:"first_login_at"`
    LastLoginAt  string            `json:"last_login_at"`
    Roles        []UserRole        `json:"roles"`
    Permissions  []perm.Permission `json:"permissions"`
    claims       map[string]any
}

// Perms returns the user's effective permission set.
func (u *User) Perms() perm.Set {
    s := perm.Set{}
    for _, p := range u.Permissions {
        s[p] = true
    }
    return s
}

const userCols = `id, issuer, subject, display_name, email, username, groups_json, claims_json, disabled, first_login_at, last_login_at`

func scanUser(sc interface{ Scan(...any) error }) (*User, error) {
    u := &User{}
    var groups, claims string
    if err := sc.Scan(&u.ID, &u.Issuer, &u.Subject, &u.DisplayName, &u.Email, &u.Username, &groups, &claims,
        &u.Disabled, &u.FirstLoginAt, &u.LastLoginAt); err != nil {
        return nil, err
    }
    json.Unmarshal([]byte(groups), &u.Groups)
    json.Unmarshal([]byte(claims), &u.claims)
    if u.Groups == nil {
        u.Groups = []string{}
    }
    return u, nil
}

func loadUser(ctx context.Context, q db.Querier, id int64) (*User, error) {
    u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
    if err == sql.ErrNoRows {
        return nil, NotFound("user")
    }
    if err != nil {
        return nil, err
    }
    return u, fillRoles(ctx, q, u)
}

func fillRoles(ctx context.Context, q db.Querier, u *User) error {
    rows, err := q.QueryContext(ctx, `SELECT r.id, r.name, ur.source FROM user_roles ur JOIN roles r ON r.id = ur.role_id
        WHERE ur.user_id = ? ORDER BY r.name, ur.source`, u.ID)
    if err != nil {
        return err
    }
    u.Roles = []UserRole{}
    for rows.Next() {
        var r UserRole
        if err := rows.Scan(&r.RoleID, &r.Name, &r.Source); err != nil {
            rows.Close()
            return err
        }
        u.Roles = append(u.Roles, r)
    }
    rows.Close()
    perms, err := queryStrings(ctx, q, `SELECT DISTINCT rp.permission FROM user_roles ur
        JOIN role_permissions rp ON rp.role_id = ur.role_id WHERE ur.user_id = ?`, u.ID)
    if err != nil {
        return err
    }
    set := perm.Set{}
    for _, p := range perms {
        set[perm.Permission(p)] = true
    }
    u.Permissions = set.List()
    return nil
}

// GetUser returns one user with roles and effective permissions.
func (s *Service) GetUser(ctx context.Context, id int64) (*User, error) {
    return loadUser(ctx, s.DB.R, id)
}

// ListUsers lists users, filtered by name, email or username.
func (s *Service) ListUsers(ctx context.Context, q string, pg Paging) (*Page[User], error) {
    pg = pg.Normalise()
    like := "%" + escapeLike(strings.TrimSpace(q)) + "%"
    where := `display_name LIKE ?1 ESCAPE '\' OR email LIKE ?1 ESCAPE '\' OR username LIKE ?1 ESCAPE '\'`
    out := &Page[User]{Items: []User{}}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE `+where, like).Scan(&out.Total); err != nil {
        return nil, err
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE `+where+`
        ORDER BY display_name COLLATE NOCASE LIMIT ?2 OFFSET ?3`, like, pg.Limit, pg.Offset)
    if err != nil {
        return nil, err
    }
    var users []*User
    for rows.Next() {
        u, err := scanUser(rows)
        if err != nil {
            rows.Close()
            return nil, err
        }
        users = append(users, u)
    }
    rows.Close()
    for _, u := range users {
        if err := fillRoles(ctx, s.DB.R, u); err != nil {
            return nil, err
        }
        out.Items = append(out.Items, *u)
    }
    return out, nil
}

func countAdmins(ctx context.Context, q db.Querier) (int, error) {
    var n int
    err := q.QueryRowContext(ctx, `SELECT COUNT(DISTINCT u.id) FROM users u
        JOIN user_roles ur ON ur.user_id = u.id
        JOIN role_permissions rp ON rp.role_id = ur.role_id
        WHERE u.disabled = 0 AND rp.permission = ?`, perm.SystemAdmin).Scan(&n)
    return n, err
}

// guardAdmins rejects a change that leaves no enabled admin, provided there
// was at least one before.
func guardAdmins(ctx context.Context, tx *sql.Tx, before int) error {
    if before == 0 {
        return nil
    }
    after, err := countAdmins(ctx, tx)
    if err != nil {
        return err
    }
    if after == 0 {
        return Conflict("last_admin", "this change would leave no enabled administrator")
    }
    return nil
}

// claimValues resolves a claim, which may be a dotted path into nested
// objects, to a list of strings.
func claimValues(claims map[string]any, name string) []string {
    var cur any = claims
    for _, part := range strings.Split(name, ".") {
        m, ok := cur.(map[string]any)
        if !ok {
            return nil
        }
        if cur, ok = m[part]; !ok {
            return nil
        }
    }
    switch v := cur.(type) {
    case nil:
        return nil
    case []any:
        out := make([]string, 0, len(v))
        for _, e := range v {
            out = append(out, fmt.Sprint(e))
        }
        return out
    case []string:
        return v
    default:
        return []string{fmt.Sprint(v)}
    }
}

// Mapping is a claim-to-role rule evaluated at login.
type Mapping struct {
    ID        int64  `json:"id"`
    Claim     string `json:"claim"`
    MatchType string `json:"match_type"`
    Value     string `json:"value"`
    RoleID    int64  `json:"role_id"`
    RoleName  string `json:"role_name"`
    SortOrder int    `json:"sort_order"`
}

func (m *Mapping) matches(claims map[string]any) bool {
    vals := claimValues(claims, m.Claim)
    var re *regexp.Regexp
    if m.MatchType == "regex" {
        var err error
        if re, err = regexp.Compile(m.Value); err != nil {
            return false
        }
    }
    for _, v := range vals {
        switch m.MatchType {
        case "equals", "contains":
            if v == m.Value {
                return true
            }
        case "ends_with":
            if strings.HasSuffix(strings.ToLower(v), strings.ToLower(m.Value)) {
                return true
            }
        case "regex":
            if re.MatchString(v) {
                return true
            }
        }
    }
    return false
}

func loadMappings(ctx context.Context, q db.Querier) ([]Mapping, error) {
    rows, err := q.QueryContext(ctx, `SELECT m.id, m.claim, m.match_type, m.value, m.role_id, r.name, m.sort_order
        FROM role_mappings m JOIN roles r ON r.id = m.role_id ORDER BY m.sort_order, m.id`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    out := []Mapping{}
    for rows.Next() {
        var m Mapping
        if err := rows.Scan(&m.ID, &m.Claim, &m.MatchType, &m.Value, &m.RoleID, &m.RoleName, &m.SortOrder); err != nil {
            return nil, err
        }
        out = append(out, m)
    }
    return out, rows.Err()
}

func (s *Service) isBootstrapAdmin(email, subject string) bool {
    for _, b := range s.Cfg.BootstrapAdmins {
        if (email != "" && strings.EqualFold(b, email)) || b == subject {
            return true
        }
    }
    return false
}

func claimString(claims map[string]any, keys ...string) string {
    for _, k := range keys {
        if v, ok := claims[k].(string); ok && v != "" {
            return v
        }
    }
    return ""
}

// LoginUser creates or refreshes a user from OIDC claims and recomputes
// their mapped roles. Disabled users are refused.
func (s *Service) LoginUser(ctx context.Context, issuer, subject string, claims map[string]any) (*User, error) {
    email := claimString(claims, "email")
    username := claimString(claims, "preferred_username", "nickname")
    display := claimString(claims, "name")
    if display == "" {
        display = strings.TrimSpace(claimString(claims, "given_name") + " " + claimString(claims, "family_name"))
    }
    if display == "" {
        display = username
    }
    if display == "" {
        display = email
    }
    if display == "" {
        display = subject
    }
    groups := claimValues(claims, s.Cfg.OIDCGroupsClaim)
    if groups == nil {
        groups = []string{}
    }
    groupsJSON, _ := json.Marshal(groups)
    claimsJSON, _ := json.Marshal(claims)

    var userID int64
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        now := db.Now()
        var disabled bool
        err := tx.QueryRowContext(ctx, `SELECT id, disabled FROM users WHERE issuer = ? AND subject = ?`, issuer, subject).
            Scan(&userID, &disabled)
        created := false
        switch {
        case err == sql.ErrNoRows:
            res, err := tx.ExecContext(ctx, `INSERT INTO users (issuer, subject, display_name, email, username, groups_json,
                    claims_json, first_login_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
                issuer, subject, display, email, username, string(groupsJSON), string(claimsJSON), now, now)
            if err != nil {
                return err
            }
            userID, _ = res.LastInsertId()
            created = true
        case err != nil:
            return err
        case disabled:
            return Forbidden("your account has been disabled")
        default:
            if _, err := tx.ExecContext(ctx, `UPDATE users SET display_name = ?, email = ?, username = ?, groups_json = ?,
                    claims_json = ?, last_login_at = ? WHERE id = ?`,
                display, email, username, string(groupsJSON), string(claimsJSON), now, userID); err != nil {
                return err
            }
        }
        actx := WithActor(ctx, Actor{UserID: userID, Name: display, RequestID: ActorFrom(ctx).RequestID})
        if created {
            var defaultRole *int64
            if err := getSetting(ctx, tx, "default_role_id", &defaultRole); err != nil {
                return err
            }
            if defaultRole != nil {
                if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_roles (user_id, role_id, source)
                        SELECT ?, id, 'manual' FROM roles WHERE id = ?`, userID, *defaultRole); err != nil {
                    return err
                }
            }
            if err := s.event(actx, tx, "user.created", Subject{"user", userID}, map[string]any{
                "user": Ref{userID, display}, "email": email, "issuer": issuer,
            }); err != nil {
                return err
            }
        }

        before, err := queryStrings(ctx, tx, `SELECT r.name || ' (' || ur.source || ')' FROM user_roles ur
            JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = ? AND ur.source != 'manual' ORDER BY 1`, userID)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ? AND source IN ('mapping', 'bootstrap')`, userID); err != nil {
            return err
        }
        mappings, err := loadMappings(ctx, tx)
        if err != nil {
            return err
        }
        for _, m := range mappings {
            if m.matches(claims) {
                if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_roles (user_id, role_id, source) VALUES (?, ?, 'mapping')`,
                    userID, m.RoleID); err != nil {
                    return err
                }
            }
        }
        if s.isBootstrapAdmin(email, subject) {
            if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_roles (user_id, role_id, source)
                    SELECT ?, id, 'bootstrap' FROM roles WHERE builtin = 1`, userID); err != nil {
                return err
            }
        }
        after, err := queryStrings(ctx, tx, `SELECT r.name || ' (' || ur.source || ')' FROM user_roles ur
            JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = ? AND ur.source != 'manual' ORDER BY 1`, userID)
        if err != nil {
            return err
        }
        data := map[string]any{"user": Ref{userID, display}}
        if strings.Join(before, ",") != strings.Join(after, ",") {
            data["changes"] = changes{"idp_roles": {From: strings.Join(before, ", "), To: strings.Join(after, ", ")}}
        }
        return s.event(actx, tx, "user.logged_in", Subject{"user", userID}, data)
    })
    if err != nil {
        return nil, err
    }
    return s.GetUser(ctx, userID)
}

// SetUserRoles replaces a user's manually assigned roles.
func (s *Service) SetUserRoles(ctx context.Context, userID int64, roleIDs []int64) (*User, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        u, err := loadUser(ctx, tx, userID)
        if err != nil {
            return err
        }
        admins, err := countAdmins(ctx, tx)
        if err != nil {
            return err
        }
        var before []string
        for _, r := range u.Roles {
            if r.Source == "manual" {
                before = append(before, r.Name)
            }
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ? AND source = 'manual'`, userID); err != nil {
            return err
        }
        var after []string
        seen := map[int64]bool{}
        for _, id := range roleIDs {
            if seen[id] {
                continue
            }
            seen[id] = true
            name, err := roleName(ctx, tx, id)
            if err != nil {
                return err
            }
            if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles (user_id, role_id, source) VALUES (?, ?, 'manual')`, userID, id); err != nil {
                return err
            }
            after = append(after, name)
        }
        sort.Strings(before)
        sort.Strings(after)
        if strings.Join(before, ",") == strings.Join(after, ",") {
            return nil
        }
        if err := guardAdmins(ctx, tx, admins); err != nil {
            return err
        }
        return s.event(ctx, tx, "user.roles_changed", Subject{"user", userID}, map[string]any{
            "user": Ref{userID, u.DisplayName}, "changes": changes{"roles": {From: strings.Join(before, ", "), To: strings.Join(after, ", ")}},
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetUser(ctx, userID)
}

// SetUserDisabled disables or enables a user. Disabling revokes sessions.
func (s *Service) SetUserDisabled(ctx context.Context, userID int64, disabled bool) (*User, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        u, err := loadUser(ctx, tx, userID)
        if err != nil {
            return err
        }
        if u.Disabled == disabled {
            return nil
        }
        admins, err := countAdmins(ctx, tx)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled = ? WHERE id = ?`, disabled, userID); err != nil {
            return err
        }
        action := "user.enabled"
        if disabled {
            action = "user.disabled"
            if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
                return err
            }
            if err := guardAdmins(ctx, tx, admins); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, action, Subject{"user", userID}, map[string]any{"user": Ref{userID, u.DisplayName}})
    })
    if err != nil {
        return nil, err
    }
    return s.GetUser(ctx, userID)
}

// Session is a logged-in browser session.
type Session struct {
    ID         int64  `json:"id"`
    UserID     int64  `json:"user_id"`
    CSRFToken  string `json:"-"`
    CreatedAt  string `json:"created_at"`
    LastSeenAt string `json:"last_seen_at"`
    ExpiresAt  string `json:"expires_at"`
    UserAgent  string `json:"user_agent"`
}

// RandomToken returns n random bytes encoded as URL-safe base64.
func RandomToken(n int) string {
    b := make([]byte, n)
    if _, err := rand.Read(b); err != nil {
        panic(err)
    }
    return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
    h := sha256.Sum256([]byte(t))
    return hex.EncodeToString(h[:])
}

// CreateSession starts a session and returns its secret token.
func (s *Service) CreateSession(ctx context.Context, userID int64, userAgent string) (string, error) {
    token := RandomToken(32)
    now := time.Now()
    _, err := s.DB.W.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, hashToken(token), userID, RandomToken(24), db.FormatTime(now), db.FormatTime(now),
        db.FormatTime(now.Add(s.Cfg.SessionMax)), truncateRunes(userAgent, 300))
    return token, err
}

// SessionByToken returns a live session and its user, or nil if the token
// is unknown, expired, idle too long or belongs to a disabled user.
func (s *Service) SessionByToken(ctx context.Context, token string) (*Session, *User, error) {
    if token == "" {
        return nil, nil, nil
    }
    sess := &Session{}
    err := s.DB.R.QueryRowContext(ctx, `SELECT id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent
        FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&sess.ID, &sess.UserID, &sess.CSRFToken,
        &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &sess.UserAgent)
    if err == sql.ErrNoRows {
        return nil, nil, nil
    }
    if err != nil {
        return nil, nil, err
    }
    now := time.Now()
    expires, _ := time.Parse(db.TimeFormat, sess.ExpiresAt)
    lastSeen, _ := time.Parse(db.TimeFormat, sess.LastSeenAt)
    if now.After(expires) || now.Sub(lastSeen) > s.Cfg.SessionIdle {
        s.DB.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sess.ID)
        return nil, nil, nil
    }
    u, err := loadUser(ctx, s.DB.R, sess.UserID)
    if err != nil {
        return nil, nil, err
    }
    if u.Disabled {
        return nil, nil, nil
    }
    if now.Sub(lastSeen) > time.Minute {
        s.DB.W.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, db.FormatTime(now), sess.ID)
    }
    return sess, u, nil
}

// DeleteSessionByToken ends a session (logout).
func (s *Service) DeleteSessionByToken(ctx context.Context, token string) error {
    _, err := s.DB.W.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
    return err
}

// ListSessions lists a user's live sessions.
func (s *Service) ListSessions(ctx context.Context, userID int64) ([]Session, error) {
    rows, err := s.DB.R.QueryContext(ctx, `SELECT id, user_id, created_at, last_seen_at, expires_at, user_agent
        FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC`, userID, db.Now())
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    out := []Session{}
    for rows.Next() {
        var sess Session
        if err := rows.Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &sess.UserAgent); err != nil {
            return nil, err
        }
        out = append(out, sess)
    }
    return out, rows.Err()
}

// RevokeSession ends one of a user's sessions.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        u, err := loadUser(ctx, tx, userID)
        if err != nil {
            return err
        }
        res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?`, sessionID, userID)
        if err != nil {
            return err
        }
        if n, _ := res.RowsAffected(); n == 0 {
            return NotFound("session")
        }
        return s.event(ctx, tx, "user.session_revoked", Subject{"user", userID}, map[string]any{
            "user": Ref{userID, u.DisplayName}, "session_id": sessionID,
        })
    })
}

// LoginState is a pending OIDC authorisation request.
type LoginState struct {
    Nonce    string
    Verifier string
    ReturnTo string
}

// SaveLoginState stores a pending OIDC request for ten minutes.
func (s *Service) SaveLoginState(ctx context.Context, state string, ls LoginState) error {
    _, err := s.DB.W.ExecContext(ctx, `INSERT INTO oidc_states (state, nonce, verifier, return_to, expires_at) VALUES (?, ?, ?, ?, ?)`,
        state, ls.Nonce, ls.Verifier, ls.ReturnTo, db.FormatTime(time.Now().Add(10*time.Minute)))
    return err
}

// TakeLoginState retrieves and deletes a pending OIDC request.
func (s *Service) TakeLoginState(ctx context.Context, state string) (*LoginState, error) {
    ls := &LoginState{}
    var expires string
    err := s.DB.W.QueryRowContext(ctx, `DELETE FROM oidc_states WHERE state = ? RETURNING nonce, verifier, return_to, expires_at`, state).
        Scan(&ls.Nonce, &ls.Verifier, &ls.ReturnTo, &expires)
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    if exp, _ := time.Parse(db.TimeFormat, expires); time.Now().After(exp) {
        return nil, nil
    }
    return ls, nil
}
