// Package app opens a Drawered data directory: configuration, database,
// migrations, file store and service. Both the server and the import
// utility start here.
package app

import (
    "context"
    "os"
    "path/filepath"

    "drawered/internal/config"
    "drawered/internal/db"
    "drawered/internal/media"
    "drawered/internal/service"
)

// Open loads configuration, opens and migrates the database, seeds it and
// makes sure the search index is current. The returned function closes it.
func Open(ctx context.Context) (*config.Config, *service.Service, func(), error) {
    cfg, err := config.Load()
    if err != nil {
        return nil, nil, nil, err
    }
    if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
        return nil, nil, nil, err
    }
    d, err := db.Open(filepath.Join(cfg.DataDir, "drawered.db"))
    if err != nil {
        return nil, nil, nil, err
    }
    fail := func(err error) (*config.Config, *service.Service, func(), error) {
        d.Close()
        return nil, nil, nil, err
    }
    if err := d.Migrate(ctx); err != nil {
        return fail(err)
    }
    files, err := media.New(cfg.DataDir)
    if err != nil {
        return fail(err)
    }
    svc := service.New(d, files, cfg)
    if err := svc.Seed(ctx); err != nil {
        return fail(err)
    }
    if err := svc.EnsureSearchIndex(ctx); err != nil {
        return fail(err)
    }
    return cfg, svc, func() { d.Close() }, nil
}
