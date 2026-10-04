// Package media stores uploaded files by content hash and produces image
// renditions.
package media

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "image"
    _ "image/gif"
    "image/jpeg"
    "image/png"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/disintegration/imaging"
    _ "golang.org/x/image/webp"
)

// ErrTooLarge is returned when an upload exceeds its size limit.
var ErrTooLarge = errors.New("file too large")

// ErrNotImage is returned when an image upload is not a supported format.
var ErrNotImage = errors.New("unsupported image type")

// MaxPixels bounds decoded image size to guard against decompression bombs.
const MaxPixels = 60_000_000

// Rendition describes one derived image size.
type Rendition struct {
    Name string
    Size int
}

// Renditions lists the derived sizes generated for every image.
var Renditions = []Rendition{
    {"thumb", 160},
    {"small", 480},
    {"large", 1600},
}

// ImageTypes are the accepted image MIME types.
var ImageTypes = map[string]bool{
    "image/jpeg": true,
    "image/png":  true,
    "image/gif":  true,
    "image/webp": true,
}

// Store is a content-addressed file store rooted at a directory.
type Store struct {
    Dir string
}

// New creates the store directories if needed.
func New(dir string) (*Store, error) {
    for _, d := range []string{"files", "renditions", "tmp"} {
        if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
            return nil, err
        }
    }
    return &Store{Dir: dir}, nil
}

// Saved describes a stored blob.
type Saved struct {
    SHA256 string
    Size   int64
    MIME   string
}

// Save streams r to the store, enforcing maxBytes, and returns its hash,
// size and sniffed MIME type.
func (s *Store) Save(r io.Reader, maxBytes int64) (*Saved, error) {
    tmp, err := os.CreateTemp(filepath.Join(s.Dir, "tmp"), "upload-*")
    if err != nil {
        return nil, err
    }
    defer os.Remove(tmp.Name())
    defer tmp.Close()

    h := sha256.New()
    head := make([]byte, 512)
    n, err := io.ReadFull(r, head)
    if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
        return nil, err
    }
    head = head[:n]
    mime := http.DetectContentType(head)
    if i := strings.Index(mime, ";"); i >= 0 {
        mime = mime[:i]
    }
    w := io.MultiWriter(tmp, h)
    if _, err := w.Write(head); err != nil {
        return nil, err
    }
    copied, err := io.Copy(w, io.LimitReader(r, maxBytes-int64(n)+1))
    if err != nil {
        return nil, err
    }
    size := int64(n) + copied
    if size > maxBytes {
        return nil, ErrTooLarge
    }
    if err := tmp.Close(); err != nil {
        return nil, err
    }
    sum := hex.EncodeToString(h.Sum(nil))
    dst := s.Path(sum)
    if _, err := os.Stat(dst); err == nil {
        // Refresh the timestamp so a concurrent sweep leaves the blob alone.
        now := time.Now()
        os.Chtimes(dst, now, now)
        return &Saved{SHA256: sum, Size: size, MIME: mime}, nil
    }
    if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
        return nil, err
    }
    if err := os.Rename(tmp.Name(), dst); err != nil {
        return nil, err
    }
    return &Saved{SHA256: sum, Size: size, MIME: mime}, nil
}

// Path returns the location of the original blob for a hash.
func (s *Store) Path(sha string) string {
    return filepath.Join(s.Dir, "files", sha[:2], sha[2:4], sha)
}

func (s *Store) renditionBase(sha, name string) string {
    return filepath.Join(s.Dir, "renditions", sha[:2], sha[2:4], sha+"_"+name)
}

// RenditionPath returns the path of an existing rendition and its MIME type.
func (s *Store) RenditionPath(sha, name string) (string, string, bool) {
    base := s.renditionBase(sha, name)
    for _, c := range []struct{ ext, mime string }{{".jpg", "image/jpeg"}, {".png", "image/png"}} {
        if _, err := os.Stat(base + c.ext); err == nil {
            return base + c.ext, c.mime, true
        }
    }
    return "", "", false
}

// ValidateImage checks that the blob is a decodable image of sane size.
func (s *Store) ValidateImage(sha string) error {
    f, err := os.Open(s.Path(sha))
    if err != nil {
        return err
    }
    defer f.Close()
    cfg, _, err := image.DecodeConfig(f)
    if err != nil {
        return ErrNotImage
    }
    if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
        return fmt.Errorf("%w: image dimensions out of range", ErrNotImage)
    }
    return nil
}

// MakeRenditions generates every rendition for an image blob. EXIF
// orientation is applied and metadata is not carried over.
func (s *Store) MakeRenditions(sha string) error {
    if err := s.ValidateImage(sha); err != nil {
        return err
    }
    img, err := imaging.Open(s.Path(sha), imaging.AutoOrientation(true))
    if err != nil {
        return ErrNotImage
    }
    alpha := !opaque(img)
    for _, r := range Renditions {
        if _, _, ok := s.RenditionPath(sha, r.Name); ok {
            continue
        }
        out := imaging.Fit(img, r.Size, r.Size, imaging.Lanczos)
        base := s.renditionBase(sha, r.Name)
        if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
            return err
        }
        if alpha {
            err = writeAtomic(base+".png", func(w io.Writer) error { return png.Encode(w, out) })
        } else {
            err = writeAtomic(base+".jpg", func(w io.Writer) error {
                return jpeg.Encode(w, out, &jpeg.Options{Quality: 85})
            })
        }
        if err != nil {
            return err
        }
    }
    return nil
}

func opaque(img image.Image) bool {
    if o, ok := img.(interface{ Opaque() bool }); ok {
        return o.Opaque()
    }
    return true
}

func writeAtomic(path string, fn func(io.Writer) error) error {
    tmp := path + ".tmp"
    f, err := os.Create(tmp)
    if err != nil {
        return err
    }
    if err := fn(f); err != nil {
        f.Close()
        os.Remove(tmp)
        return err
    }
    if err := f.Close(); err != nil {
        os.Remove(tmp)
        return err
    }
    return os.Rename(tmp, path)
}

// Remove deletes a blob and its renditions.
func (s *Store) Remove(sha string) error {
    err := os.Remove(s.Path(sha))
    if err != nil && !os.IsNotExist(err) {
        return err
    }
    for _, r := range Renditions {
        if p, _, ok := s.RenditionPath(sha, r.Name); ok {
            os.Remove(p)
        }
    }
    return nil
}

// Size returns the total size in bytes of the stored files and renditions.
func (s *Store) Size() int64 {
    var total int64
    for _, d := range []string{"files", "renditions"} {
        filepath.WalkDir(filepath.Join(s.Dir, d), func(_ string, e os.DirEntry, err error) error {
            if err == nil && !e.IsDir() {
                if fi, err := e.Info(); err == nil {
                    total += fi.Size()
                }
            }
            return nil
        })
    }
    return total
}
