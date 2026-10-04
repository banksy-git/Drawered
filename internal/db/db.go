// Package db opens the SQLite database and applies migrations.
package db

import (
    "context"
    "database/sql"
    "embed"
    "fmt"
    "io/fs"
    "net/url"
    "sort"
    "strings"
    "time"

    _ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB wraps a single-connection writer pool and a multi-connection reader
// pool on the same SQLite file.
type DB struct {
    W    *sql.DB
    R    *sql.DB
    Path string
}

func dsn(path string, immediate bool) string {
    q := url.Values{}
    for _, p := range []string{
        "busy_timeout(5000)",
        "foreign_keys(1)",
        "journal_mode(WAL)",
        "synchronous(NORMAL)",
    } {
        q.Add("_pragma", p)
    }
    if immediate {
        q.Set("_txlock", "immediate")
    }
    return "file:" + path + "?" + q.Encode()
}

// Open opens (creating if necessary) the database at path.
func Open(path string) (*DB, error) {
    w, err := sql.Open("sqlite", dsn(path, true))
    if err != nil {
        return nil, err
    }
    w.SetMaxOpenConns(1)
    w.SetConnMaxIdleTime(0)
    if err := w.Ping(); err != nil {
        w.Close()
        return nil, fmt.Errorf("open database: %w", err)
    }
    r, err := sql.Open("sqlite", dsn(path, false))
    if err != nil {
        w.Close()
        return nil, err
    }
    r.SetMaxOpenConns(8)
    return &DB{W: w, R: r, Path: path}, nil
}

// Close closes both pools.
func (d *DB) Close() error {
    err1 := d.R.Close()
    err2 := d.W.Close()
    if err1 != nil {
        return err1
    }
    return err2
}

// Tx runs fn in a write transaction, committing if fn returns nil.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
    tx, err := d.W.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    if err := fn(tx); err != nil {
        tx.Rollback()
        return err
    }
    return tx.Commit()
}

// Migrate applies any migrations not yet recorded in schema_migrations.
func (d *DB) Migrate(ctx context.Context) error {
    if _, err := d.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version TEXT PRIMARY KEY,
        applied_at TEXT NOT NULL
    )`); err != nil {
        return err
    }
    names, err := fs.Glob(migrationFS, "migrations/*.sql")
    if err != nil {
        return err
    }
    sort.Strings(names)
    for _, name := range names {
        version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
        var n int
        if err := d.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
            return err
        }
        if n > 0 {
            continue
        }
        body, err := migrationFS.ReadFile(name)
        if err != nil {
            return err
        }
        err = d.Tx(ctx, func(tx *sql.Tx) error {
            if _, err := tx.ExecContext(ctx, string(body)); err != nil {
                return fmt.Errorf("migration %s: %w", version, err)
            }
            _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
                version, Now())
            return err
        })
        if err != nil {
            return err
        }
    }
    return nil
}

// TimeFormat is the stored timestamp format: RFC 3339, UTC, milliseconds.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// Now returns the current time in TimeFormat.
func Now() string { return FormatTime(time.Now()) }

// FormatTime renders t in TimeFormat.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// IsUniqueViolation reports whether err is a SQLite UNIQUE constraint error.
func IsUniqueViolation(err error) bool {
    return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
