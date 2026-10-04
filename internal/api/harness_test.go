package api

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "mime/multipart"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"
    "time"

    "drawered/internal/auth"
    "drawered/internal/config"
    "drawered/internal/db"
    "drawered/internal/media"
    "drawered/internal/service"
)

type harness struct {
    t   *testing.T
    svc *service.Service
    srv *httptest.Server
    cfg *config.Config
}

func newHarness(t *testing.T) *harness {
    t.Helper()
    dir := t.TempDir()
    cfg := &config.Config{
        BaseURL: "http://test", DataDir: dir, DefaultCurrency: "GBP", OIDCGroupsClaim: "groups",
        MaxImageBytes: 5 << 20, MaxDocumentBytes: 5 << 20, SessionIdle: time.Hour, SessionMax: 24 * time.Hour,
        InsecureCookies: true,
    }
    d, err := db.Open(filepath.Join(dir, "drawered.db"))
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { d.Close() })
    ctx := context.Background()
    if err := d.Migrate(ctx); err != nil {
        t.Fatal(err)
    }
    files, err := media.New(dir)
    if err != nil {
        t.Fatal(err)
    }
    svc := service.New(d, files, cfg)
    if err := svc.Seed(ctx); err != nil {
        t.Fatal(err)
    }
    log := slog.New(slog.NewTextHandler(io.Discard, nil))
    srv := httptest.NewServer(New(svc, cfg, auth.New(cfg, svc), log, "test", nil).Handler())
    t.Cleanup(srv.Close)
    return &harness{t: t, svc: svc, srv: srv, cfg: cfg}
}

type client struct {
    h      *harness
    t      *testing.T
    cookie string
    csrf   string
    userID int64
}

func (h *harness) roleID(name string) int64 {
    h.t.Helper()
    roles, err := h.svc.ListRoles(context.Background())
    if err != nil {
        h.t.Fatal(err)
    }
    for _, r := range roles {
        if r.Name == name {
            return r.ID
        }
    }
    h.t.Fatalf("no role %q", name)
    return 0
}

// login creates (or reuses) a user holding exactly the named roles and
// returns an authenticated client.
func (h *harness) login(email string, roles ...string) *client {
    h.t.Helper()
    ctx := context.Background()
    u, err := h.svc.LoginUser(ctx, "test", email, map[string]any{"email": email, "name": email})
    if err != nil {
        h.t.Fatal(err)
    }
    ids := []int64{}
    for _, r := range roles {
        ids = append(ids, h.roleID(r))
    }
    if _, err := h.svc.SetUserRoles(ctx, u.ID, ids); err != nil {
        h.t.Fatal(err)
    }
    token, err := h.svc.CreateSession(ctx, u.ID, "test")
    if err != nil {
        h.t.Fatal(err)
    }
    c := &client{h: h, t: h.t, cookie: token, userID: u.ID}
    var me struct {
        CSRFToken string `json:"csrf_token"`
    }
    c.must("GET", "/api/v1/me", nil, 200, &me)
    c.csrf = me.CSRFToken
    return c
}

func (c *client) request(method, path string, body io.Reader, contentType string) *http.Response {
    c.t.Helper()
    req, err := http.NewRequest(method, c.h.srv.URL+path, body)
    if err != nil {
        c.t.Fatal(err)
    }
    if c.cookie != "" {
        req.AddCookie(&http.Cookie{Name: SessionCookie, Value: c.cookie})
    }
    if c.csrf != "" {
        req.Header.Set(CSRFHeader, c.csrf)
    }
    if contentType != "" {
        req.Header.Set("Content-Type", contentType)
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        c.t.Fatal(err)
    }
    return resp
}

// do performs a JSON request and returns the status and raw body.
func (c *client) do(method, path string, body any) (int, []byte) {
    c.t.Helper()
    var r io.Reader
    if body != nil {
        b, err := json.Marshal(body)
        if err != nil {
            c.t.Fatal(err)
        }
        r = bytes.NewReader(b)
    }
    resp := c.request(method, path, r, "application/json")
    defer resp.Body.Close()
    b, _ := io.ReadAll(resp.Body)
    return resp.StatusCode, b
}

// must performs a request, asserting the status and decoding into out.
func (c *client) must(method, path string, body any, status int, out any) {
    c.t.Helper()
    got, b := c.do(method, path, body)
    if got != status {
        c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, got, status, b)
    }
    if out != nil {
        if err := json.Unmarshal(b, out); err != nil {
            c.t.Fatalf("%s %s: decode: %v: %s", method, path, err, b)
        }
    }
}

// errCode performs a request expecting failure and returns the error code.
func (c *client) errCode(method, path string, body any, status int) string {
    c.t.Helper()
    got, b := c.do(method, path, body)
    if got != status {
        c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, got, status, b)
    }
    var e struct {
        Error struct {
            Code string `json:"code"`
        } `json:"error"`
    }
    json.Unmarshal(b, &e)
    return e.Error.Code
}

func (c *client) upload(path, field, filename string, content []byte, extra map[string]string) (int, []byte) {
    c.t.Helper()
    var buf bytes.Buffer
    mw := multipart.NewWriter(&buf)
    for k, v := range extra {
        mw.WriteField(k, v)
    }
    fw, err := mw.CreateFormFile(field, filename)
    if err != nil {
        c.t.Fatal(err)
    }
    fw.Write(content)
    mw.Close()
    resp := c.request("POST", path, &buf, mw.FormDataContentType())
    defer resp.Body.Close()
    b, _ := io.ReadAll(resp.Body)
    return resp.StatusCode, b
}

type idResp struct {
    ID      int64 `json:"id"`
    Version int64 `json:"version"`
}

func (c *client) createLocation(parent *int64, name string, structural bool) idResp {
    c.t.Helper()
    var l idResp
    c.must("POST", "/api/v1/locations", map[string]any{"parent_id": parent, "name": name, "structural": structural}, 201, &l)
    return l
}

func ptr[T any](v T) *T { return &v }

func pf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func (h *harness) eventCount() int {
    h.t.Helper()
    var n int
    if err := h.svc.DB.R.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
        h.t.Fatal(err)
    }
    return n
}
