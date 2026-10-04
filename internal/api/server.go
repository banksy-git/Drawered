// Package api exposes the JSON API, the OIDC endpoints, file downloads and
// the embedded single-page application.
package api

import (
    "context"
    "crypto/subtle"
    "encoding/json"
    "errors"
    "io/fs"
    "log/slog"
    "net"
    "net/http"
    "strconv"
    "strings"
    "time"

    "drawered/internal/auth"
    "drawered/internal/config"
    "drawered/internal/perm"
    "drawered/internal/service"
)

// SessionCookie is the name of the session cookie.
const SessionCookie = "drawered_session"

// CSRFHeader must accompany every state-changing API request.
const CSRFHeader = "X-Drawered-CSRF"

// Server holds the HTTP handlers' dependencies.
type Server struct {
    svc     *service.Service
    cfg     *config.Config
    oidc    *auth.OIDC
    log     *slog.Logger
    version string
    spa     *spa
}

// New creates a Server. static is the built frontend (may be empty).
func New(svc *service.Service, cfg *config.Config, o *auth.OIDC, log *slog.Logger, version string, static fs.FS) *Server {
    return &Server{svc: svc, cfg: cfg, oidc: o, log: log, version: version, spa: newSPA(static)}
}

type ctxKey int

const (
    userKey ctxKey = iota
    sessionKey
)

func userFrom(ctx context.Context) *service.User {
    u, _ := ctx.Value(userKey).(*service.User)
    return u
}

func sessionFrom(ctx context.Context) *service.Session {
    s, _ := ctx.Value(sessionKey).(*service.Session)
    return s
}

// apiFunc is a handler that reports failures by returning an error.
type apiFunc func(w http.ResponseWriter, r *http.Request) error

// Handler builds the complete HTTP handler.
func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()
    s.routes(mux)
    return s.withRequest(mux)
}

type statusWriter struct {
    http.ResponseWriter
    status int
}

func (w *statusWriter) WriteHeader(code int) {
    if w.status == 0 {
        w.status = code
    }
    w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
    if w.status == 0 {
        w.status = http.StatusOK
    }
    return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// withRequest assigns a request id, loads the session, and logs.
func (s *Server) withRequest(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        reqID := service.RandomToken(9)
        w.Header().Set("X-Request-Id", reqID)
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Referrer-Policy", "same-origin")

        ctx := r.Context()
        actor := service.Actor{RequestID: reqID}
        if c, err := r.Cookie(SessionCookie); err == nil {
            sess, u, err := s.svc.SessionByToken(ctx, c.Value)
            if err != nil {
                s.log.Error("session lookup", "err", err, "request_id", reqID)
            } else if sess != nil {
                ctx = context.WithValue(ctx, userKey, u)
                ctx = context.WithValue(ctx, sessionKey, sess)
                actor.UserID, actor.Name = u.ID, u.DisplayName
            }
        }
        ctx = service.WithActor(ctx, actor)
        sw := &statusWriter{ResponseWriter: w}
        next.ServeHTTP(sw, r.WithContext(ctx))
        if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
            return
        }
        s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
            "duration_ms", time.Since(start).Milliseconds(), "user_id", actor.UserID,
            "ip", s.clientIP(r), "request_id", reqID)
    })
}

// clientIP honours X-Forwarded-For only from trusted proxies.
func (s *Server) clientIP(r *http.Request) string {
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        host = r.RemoteAddr
    }
    ip := net.ParseIP(host)
    trusted := false
    for _, n := range s.cfg.TrustedProxies {
        if ip != nil && n.Contains(ip) {
            trusted = true
        }
    }
    if trusted {
        if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
            parts := strings.Split(xff, ",")
            return strings.TrimSpace(parts[len(parts)-1])
        }
    }
    return host
}

// handle registers an authenticated API route requiring permission p
// (empty means any authenticated user).
func (s *Server) handle(mux *http.ServeMux, pattern string, p perm.Permission, h apiFunc) {
    mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r.Context())
        if u == nil {
            writeError(w, &service.Error{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "please log in"})
            return
        }
        if r.Method != http.MethodGet && r.Method != http.MethodHead {
            sess := sessionFrom(r.Context())
            got := r.Header.Get(CSRFHeader)
            if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
                writeError(w, &service.Error{Status: http.StatusForbidden, Code: "csrf", Message: "missing or invalid CSRF token"})
                return
            }
        }
        if p != "" && !u.Perms().Has(p) {
            writeError(w, service.Forbidden("you do not have the %s permission", p))
            return
        }
        if err := h(w, r); err != nil {
            s.fail(w, r, err)
        }
    })
}

// requireAny wraps h so that any one of perms suffices.
func (s *Server) requireAny(h apiFunc, perms ...perm.Permission) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        have := userFrom(r.Context()).Perms()
        for _, p := range perms {
            if have.Has(p) {
                return h(w, r)
            }
        }
        return service.Forbidden("you do not have permission to view roles")
    }
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
    if e, ok := service.AsError(err); ok {
        writeError(w, e)
        return
    }
    var mbe *http.MaxBytesError
    if errors.As(err, &mbe) {
        writeError(w, &service.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "request body too large"})
        return
    }
    s.log.Error("request failed", "err", err, "path", r.URL.Path, "request_id", w.Header().Get("X-Request-Id"))
    writeError(w, &service.Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal server error"})
}

func writeError(w http.ResponseWriter, e *service.Error) {
    writeJSON(w, e.Status, map[string]any{"error": e})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.Header().Set("Cache-Control", "no-store")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) error {
    writeJSON(w, http.StatusOK, v)
    return nil
}

func created(w http.ResponseWriter, v any) error {
    writeJSON(w, http.StatusCreated, v)
    return nil
}

func noContent(w http.ResponseWriter) error {
    w.WriteHeader(http.StatusNoContent)
    return nil
}

const maxJSONBody = 1 << 20

func decode(r *http.Request, dst any) error {
    dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody))
    if err := dec.Decode(dst); err != nil {
        var mbe *http.MaxBytesError
        if errors.As(err, &mbe) {
            return err
        }
        return service.Invalid("invalid_json", "invalid request body: %v", err)
    }
    return nil
}

func decodePatch(r *http.Request) (service.Patch, error) {
    p := service.Patch{}
    if err := decode(r, &p); err != nil {
        return nil, err
    }
    return p, nil
}

func pathID(r *http.Request, name string) (int64, error) {
    v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
    if err != nil || v <= 0 {
        return 0, service.NotFound(name)
    }
    return v, nil
}

func queryInt(r *http.Request, name string) (*int64, error) {
    s := r.URL.Query().Get(name)
    if s == "" {
        return nil, nil
    }
    v, err := strconv.ParseInt(s, 10, 64)
    if err != nil {
        return nil, service.Invalid("invalid_query", "%s must be an integer", name)
    }
    return &v, nil
}

func paging(r *http.Request) service.Paging {
    q := r.URL.Query()
    limit, _ := strconv.Atoi(q.Get("limit"))
    offset, _ := strconv.Atoi(q.Get("offset"))
    return service.Paging{Limit: limit, Offset: offset}.Normalise()
}

func queryBool(r *http.Request, name string) bool {
    v, _ := strconv.ParseBool(r.URL.Query().Get(name))
    return v
}
