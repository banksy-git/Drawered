// Package service implements Drawered's domain rules. Every state change
// runs in one write transaction that also records its event and keeps the
// search index current.
package service

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "strings"

    "drawered/internal/config"
    "drawered/internal/db"
    "drawered/internal/media"
)

// Service is the entry point for all domain operations.
type Service struct {
    DB    *db.DB
    Files *media.Store
    Cfg   *config.Config
}

// New creates a Service.
func New(d *db.DB, files *media.Store, cfg *config.Config) *Service {
    return &Service{DB: d, Files: files, Cfg: cfg}
}

// Error is a domain error carrying an HTTP status and stable code.
type Error struct {
    Status  int            `json:"-"`
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newErr(status int, code, format string, args ...any) *Error {
    return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Invalid reports a validation failure (400).
func Invalid(code, format string, args ...any) *Error {
    return newErr(http.StatusBadRequest, code, format, args...)
}

// Conflict reports a rule violation or stale version (409).
func Conflict(code, format string, args ...any) *Error {
    return newErr(http.StatusConflict, code, format, args...)
}

// NotFound reports a missing entity (404).
func NotFound(what string) *Error {
    return newErr(http.StatusNotFound, "not_found", "%s not found", what)
}

// Forbidden reports a missing permission (403).
func Forbidden(format string, args ...any) *Error {
    return newErr(http.StatusForbidden, "forbidden", format, args...)
}

// StaleVersion is returned when an optimistic concurrency check fails.
func StaleVersion() *Error {
    return Conflict("stale_version", "this item was changed by someone else; reload and try again")
}

// AsError extracts a *Error from err.
func AsError(err error) (*Error, bool) {
    var e *Error
    ok := errors.As(err, &e)
    return e, ok
}

// Actor identifies who is performing an action.
type Actor struct {
    UserID    int64
    Name      string
    RequestID string
}

type actorKey struct{}

// WithActor attaches an actor to ctx.
func WithActor(ctx context.Context, a Actor) context.Context {
    return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor attached to ctx, or the zero (system) actor.
func ActorFrom(ctx context.Context) Actor {
    a, _ := ctx.Value(actorKey{}).(Actor)
    return a
}

// Subject identifies an entity an event relates to.
type Subject struct {
    Type string
    ID   int64
}

// Ref is a compact id/name pair stored in event payloads so history stays
// readable after renames and deletions.
type Ref struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

// event records one audit event within tx.
func (s *Service) event(ctx context.Context, tx *sql.Tx, action string, subject Subject, data any, related ...Subject) error {
    a := ActorFrom(ctx)
    var actor any
    if a.UserID != 0 {
        actor = a.UserID
    }
    if data == nil {
        data = map[string]any{}
    }
    b, err := json.Marshal(data)
    if err != nil {
        return err
    }
    res, err := tx.ExecContext(ctx, `INSERT INTO events (occurred_at, actor_user_id, action, subject_type, subject_id, data_json, request_id)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, db.Now(), actor, action, subject.Type, subject.ID, string(b), a.RequestID)
    if err != nil {
        return err
    }
    id, _ := res.LastInsertId()
    seen := map[Subject]bool{}
    for _, sub := range append([]Subject{subject}, related...) {
        if seen[sub] || sub.ID == 0 {
            continue
        }
        seen[sub] = true
        if _, err := tx.ExecContext(ctx, `INSERT INTO event_subjects (event_id, subject_type, subject_id) VALUES (?, ?, ?)`,
            id, sub.Type, sub.ID); err != nil {
            return err
        }
    }
    return nil
}

// Change is one field change recorded in an update event.
type Change struct {
    From any `json:"from"`
    To   any `json:"to"`
}

// changes accumulates field differences for an update event.
type changes map[string]Change

func (c changes) add(field string, from, to any) {
    if fmt.Sprint(from) != fmt.Sprint(to) {
        c[field] = Change{From: from, To: to}
    }
}

// Page is a generic paged result.
type Page[T any] struct {
    Items []T `json:"items"`
    Total int `json:"total"`
}

// Paging holds list limits.
type Paging struct {
    Limit  int
    Offset int
}

// Normalise clamps paging to sane bounds.
func (p Paging) Normalise() Paging {
    if p.Limit <= 0 {
        p.Limit = 50
    }
    if p.Limit > 200 {
        p.Limit = 200
    }
    if p.Offset < 0 {
        p.Offset = 0
    }
    return p
}

func nullStr(ns sql.NullString) *string {
    if !ns.Valid {
        return nil
    }
    return &ns.String
}

func nullInt(ni sql.NullInt64) *int64 {
    if !ni.Valid {
        return nil
    }
    return &ni.Int64
}

func placeholders(n int) string {
    if n <= 0 {
        return "NULL"
    }
    return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func int64Args(ids []int64) []any {
    out := make([]any, len(ids))
    for i, id := range ids {
        out[i] = id
    }
    return out
}

// checkLen validates a string's length in characters.
func checkLen(field, v string, min, max int) error {
    n := len([]rune(v))
    if n < min {
        if min == 1 {
            return Invalid("required", "%s is required", field)
        }
        return Invalid("too_short", "%s must be at least %d characters", field, min)
    }
    if n > max {
        return Invalid("too_long", "%s must be at most %d characters", field, max)
    }
    return nil
}

// Patch is a decoded JSON object used for partial updates, distinguishing
// absent keys from explicit nulls.
type Patch map[string]json.RawMessage

// Has reports whether key is present.
func (p Patch) Has(key string) bool {
    _, ok := p[key]
    return ok
}

// IsNull reports whether key is present and null.
func (p Patch) IsNull(key string) bool {
    v, ok := p[key]
    return ok && string(v) == "null"
}

// Get decodes key into dst if present, returning whether it was present.
func (p Patch) Get(key string, dst any) (bool, error) {
    v, ok := p[key]
    if !ok {
        return false, nil
    }
    if err := json.Unmarshal(v, dst); err != nil {
        return true, Invalid("invalid_field", "%s: %v", key, err)
    }
    return true, nil
}

// Version returns the required version field.
func (p Patch) Version() (int64, error) {
    var v int64
    ok, err := p.Get("version", &v)
    if err != nil {
        return 0, err
    }
    if !ok {
        return 0, Invalid("version_required", "version is required")
    }
    return v, nil
}
