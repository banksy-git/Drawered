// Command drawered runs the Drawered inventory server.
package main

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

    "drawered/internal/api"
    "drawered/internal/app"
    "drawered/internal/auth"
    "drawered/internal/service"
    "drawered/internal/webui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `usage: drawered [command]

commands:
  serve              run the web server (default)
  migrate            apply database migrations and exit
  backup <out>       write a backup archive (.tar.gz) to <out>
  reindex            rebuild the search index
  version            print the version

Configuration is read from DRAWERED_* environment variables; see SPEC.md.
`

func main() {
    cmd := "serve"
    if len(os.Args) > 1 {
        cmd = os.Args[1]
    }
    var err error
    switch cmd {
    case "serve":
        err = serve()
    case "migrate":
        err = withService(func(ctx context.Context, svc *service.Service) error { return nil })
    case "backup":
        if len(os.Args) < 3 {
            fmt.Fprint(os.Stderr, usage)
            os.Exit(2)
        }
        err = withService(func(ctx context.Context, svc *service.Service) error {
            return backupTo(ctx, svc, os.Args[2])
        })
    case "reindex":
        err = withService(func(ctx context.Context, svc *service.Service) error { return svc.Reindex(ctx) })
    case "version":
        fmt.Println(version)
    case "help", "-h", "--help":
        fmt.Print(usage)
    default:
        fmt.Fprint(os.Stderr, usage)
        os.Exit(2)
    }
    if err != nil {
        fmt.Fprintln(os.Stderr, "drawered:", err)
        os.Exit(1)
    }
}

func newLogger(level string) *slog.Logger {
    var l slog.Level
    if err := l.UnmarshalText([]byte(level)); err != nil {
        l = slog.LevelInfo
    }
    return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func withService(fn func(ctx context.Context, svc *service.Service) error) error {
    ctx := context.Background()
    _, svc, closeFn, err := app.Open(ctx)
    if err != nil {
        return err
    }
    defer closeFn()
    return fn(ctx, svc)
}

func backupTo(ctx context.Context, svc *service.Service, out string) error {
    tmp := out + ".partial"
    f, err := os.Create(tmp)
    if err != nil {
        return err
    }
    if err := svc.Backup(ctx, f); err != nil {
        f.Close()
        os.Remove(tmp)
        return err
    }
    if err := f.Close(); err != nil {
        return err
    }
    return os.Rename(tmp, out)
}

func serve() error {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    cfg, svc, closeFn, err := app.Open(ctx)
    if err != nil {
        return err
    }
    defer closeFn()
    if err := cfg.ValidateServe(auth.DevAuthAllowed); err != nil {
        return err
    }
    log := newLogger(cfg.LogLevel)
    o := auth.New(cfg, svc)
    if o.DevMode() {
        log.Warn("development login enabled; OIDC is bypassed", "user", cfg.DevAuth)
    }
    if strings.HasPrefix(cfg.BaseURL, "http://") && !cfg.InsecureCookies {
        log.Warn("base URL is plain HTTP but cookies are Secure; set DRAWERED_INSECURE_COOKIES=true for local use")
    }

    srv := &http.Server{
        Addr:              cfg.Listen,
        Handler:           api.New(svc, cfg, o, log, version, webui.FS()).Handler(),
        ReadHeaderTimeout: 10 * time.Second,
        IdleTimeout:       120 * time.Second,
    }

    go func() {
        t := time.NewTicker(time.Hour)
        defer t.Stop()
        for {
            if err := svc.Sweep(ctx, 24*time.Hour); err != nil && ctx.Err() == nil {
                log.Error("sweep", "err", err)
            }
            select {
            case <-ctx.Done():
                return
            case <-t.C:
            }
        }
    }()

    errc := make(chan error, 1)
    go func() {
        log.Info("listening", "addr", cfg.Listen, "base_url", cfg.BaseURL, "version", version)
        errc <- srv.ListenAndServe()
    }()
    select {
    case err := <-errc:
        if !errors.Is(err, http.ErrServerClosed) {
            return err
        }
    case <-ctx.Done():
        log.Info("shutting down")
        sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        return srv.Shutdown(sctx)
    }
    return nil
}
