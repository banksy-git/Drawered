package service

import (
    "archive/tar"
    "compress/gzip"
    "context"
    "database/sql"
    "io"
    "io/fs"
    "os"
    "path/filepath"
    "time"
)

// SystemInfo summarises the instance for the admin system page.
type SystemInfo struct {
    Version       string `json:"version"`
    DatabaseBytes int64  `json:"database_bytes"`
    FileBytes     int64  `json:"file_bytes"`
    Parts         int    `json:"parts"`
    DeletedParts  int    `json:"deleted_parts"`
    Locations     int    `json:"locations"`
    Users         int    `json:"users"`
    Events        int    `json:"events"`
}

// Info gathers system statistics.
func (s *Service) Info(ctx context.Context, version string) (*SystemInfo, error) {
    si := &SystemInfo{Version: version, FileBytes: s.Files.Size()}
    for _, suffix := range []string{"", "-wal"} {
        if fi, err := os.Stat(s.DB.Path + suffix); err == nil {
            si.DatabaseBytes += fi.Size()
        }
    }
    err := s.DB.R.QueryRowContext(ctx, `SELECT
            (SELECT COUNT(*) FROM parts WHERE deleted_at IS NULL),
            (SELECT COUNT(*) FROM parts WHERE deleted_at IS NOT NULL),
            (SELECT COUNT(*) FROM locations),
            (SELECT COUNT(*) FROM users),
            (SELECT COUNT(*) FROM events)`).Scan(&si.Parts, &si.DeletedParts, &si.Locations, &si.Users, &si.Events)
    return si, err
}

// Backup writes a gzipped tar of a consistent database snapshot and the
// file store to w. Restore by extracting it into an empty data directory.
func (s *Service) Backup(ctx context.Context, w io.Writer) error {
    snap := filepath.Join(s.Files.Dir, "tmp", "backup-"+RandomToken(6)+".db")
    defer os.Remove(snap)
    if _, err := s.DB.R.ExecContext(ctx, `VACUUM INTO ?`, snap); err != nil {
        return err
    }
    gz := gzip.NewWriter(w)
    tw := tar.NewWriter(gz)
    if err := addFile(tw, snap, "drawered.db"); err != nil {
        return err
    }
    for _, dir := range []string{"files", "renditions"} {
        root := filepath.Join(s.Files.Dir, dir)
        err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
            if err != nil {
                return err
            }
            if d.IsDir() || filepath.Ext(path) == ".tmp" {
                return nil
            }
            rel, err := filepath.Rel(s.Files.Dir, path)
            if err != nil {
                return err
            }
            return addFile(tw, path, filepath.ToSlash(rel))
        })
        if err != nil {
            return err
        }
    }
    if err := tw.Close(); err != nil {
        return err
    }
    if err := gz.Close(); err != nil {
        return err
    }
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        return s.event(ctx, tx, "system.backup_created", Subject{"system", 1}, map[string]any{
            "at": time.Now().UTC().Format(time.RFC3339),
        })
    })
}

func addFile(tw *tar.Writer, path, name string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()
    fi, err := f.Stat()
    if err != nil {
        return err
    }
    hdr := &tar.Header{Name: name, Mode: 0o640, Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg}
    if err := tw.WriteHeader(hdr); err != nil {
        return err
    }
    _, err = io.Copy(tw, f)
    return err
}
