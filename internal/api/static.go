package api

import (
    "bytes"
    "crypto/sha256"
    "encoding/base64"
    "io/fs"
    "mime"
    "net/http"
    "os"
    "path"
    "regexp"
    "strings"
    "time"

    "drawered/internal/media"
    "drawered/internal/perm"
    "drawered/internal/service"
)

// serveFile streams an original upload or an image rendition.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
    u := userFrom(r.Context())
    if u == nil {
        http.Error(w, "unauthenticated", http.StatusUnauthorized)
        return
    }
    if !u.Perms().Has(perm.PartsRead) {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }
    id, err := pathID(r, "id")
    if err != nil {
        http.NotFound(w, r)
        return
    }
    f, err := s.svc.GetFile(r.Context(), id)
    if err != nil {
        if e, ok := service.AsError(err); ok && e.Status == http.StatusNotFound {
            http.NotFound(w, r)
        } else {
            s.fail(w, r, err)
        }
        return
    }
    filePath, mimeType, name := s.svc.Files.Path(f.SHA256), f.MIME, f.OriginalName
    if rendition := r.PathValue("rendition"); rendition != "" {
        valid := false
        for _, rd := range media.Renditions {
            valid = valid || rd.Name == rendition
        }
        p, m, ok := s.svc.Files.RenditionPath(f.SHA256, rendition)
        if !valid || !ok {
            http.NotFound(w, r)
            return
        }
        filePath, mimeType = p, m
        ext := ".jpg"
        if m == "image/png" {
            ext = ".png"
        }
        name = strings.TrimSuffix(name, path.Ext(name)) + "-" + rendition + ext
    }
    fh, err := os.Open(filePath)
    if err != nil {
        s.log.Error("open stored file", "err", err, "file_id", id)
        http.NotFound(w, r)
        return
    }
    defer fh.Close()

    inline := (strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml") || mimeType == "application/pdf"
    disposition := "attachment"
    if inline {
        disposition = "inline"
    }
    h := w.Header()
    h.Set("Content-Type", mimeType)
    h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
    h.Set("Cache-Control", "private, max-age=31536000, immutable")
    if mimeType == "application/pdf" {
        h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
    } else {
        h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox")
    }
    http.ServeContent(w, r, "", f.UploadedAt, fh)
}

// spa serves the built frontend, falling back to index.html for client
// routes.
type spa struct {
    fs    fs.FS
    index []byte
    csp   string
}

var inlineScript = regexp.MustCompile(`(?s)<script(\s[^>]*)?>(.*?)</script>`)

func newSPA(static fs.FS) *spa {
    s := &spa{fs: static}
    if static != nil {
        s.index, _ = fs.ReadFile(static, "index.html")
    }
    // Hash inline bootstrap scripts so the CSP can stay free of
    // 'unsafe-inline' for scripts.
    var hashes []string
    for _, m := range inlineScript.FindAllSubmatch(s.index, -1) {
        if bytes.Contains(m[1], []byte("src=")) {
            continue
        }
        sum := sha256.Sum256(m[2])
        hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
    }
    s.csp = strings.Join([]string{
        "default-src 'self'",
        "script-src 'self' " + strings.Join(hashes, " "),
        "style-src 'self' 'unsafe-inline'",
        "img-src 'self' data: blob:",
        "connect-src 'self'",
        "frame-src 'self'",
        "object-src 'none'",
        "base-uri 'self'",
        "form-action 'self'",
        "frame-ancestors 'none'",
    }, "; ")
    return s
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if s.index == nil {
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte("The web interface has not been built. Run `make web` and rebuild.\n"))
        return
    }
    p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
    if p != "" && p != "index.html" {
        if st, err := fs.Stat(s.fs, p); err == nil && !st.IsDir() {
            if strings.HasPrefix(p, "_app/immutable/") {
                w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
            } else {
                w.Header().Set("Cache-Control", "no-cache")
            }
            http.ServeFileFS(w, r, s.fs, p)
            return
        }
        if path.Ext(p) != "" && !strings.Contains(path.Base(p), ".html") {
            // A missing asset, not a client route.
            http.NotFound(w, r)
            return
        }
    }
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Content-Security-Policy", s.csp)
    w.Header().Set("X-Frame-Options", "DENY")
    http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(s.index))
}
