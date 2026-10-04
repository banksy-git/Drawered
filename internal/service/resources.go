package service

import (
    "context"
    "database/sql"
    "errors"
    "io"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strings"
    "time"

    "drawered/internal/db"
    "drawered/internal/media"
)

func editablePart(ctx context.Context, q db.Querier, id int64) (string, error) {
    var name string
    var deleted sql.NullString
    err := q.QueryRowContext(ctx, `SELECT name, deleted_at FROM parts WHERE id = ?`, id).Scan(&name, &deleted)
    if err == sql.ErrNoRows {
        return "", NotFound("part")
    }
    if err != nil {
        return "", err
    }
    if deleted.Valid {
        return "", Conflict("part_deleted", "part is deleted")
    }
    return name, nil
}

func nextSort(ctx context.Context, tx *sql.Tx, table string, partID int64) (int, error) {
    var n int
    err := tx.QueryRowContext(ctx, `SELECT IFNULL(MAX(sort_order), -1) + 1 FROM `+table+` WHERE part_id = ?`, partID).Scan(&n)
    return n, err
}

func cleanFilename(name string) string {
    name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
    name = strings.Map(func(r rune) rune {
        if r < 32 || r == 127 {
            return -1
        }
        return r
    }, name)
    if name == "" || name == "." || name == "/" {
        name = "file"
    }
    return truncateRunes(name, 200)
}

func (s *Service) storeUpload(r io.Reader, max int64) (*media.Saved, error) {
    saved, err := s.Files.Save(r, max)
    if errors.Is(err, media.ErrTooLarge) {
        return nil, &Error{Status: http.StatusRequestEntityTooLarge, Code: "file_too_large",
            Message: "file exceeds the maximum upload size", Details: map[string]any{"max_bytes": max}}
    }
    return saved, err
}

func insertFile(ctx context.Context, tx *sql.Tx, saved *media.Saved, name string) (int64, error) {
    var uploader any
    if a := ActorFrom(ctx); a.UserID != 0 {
        uploader = a.UserID
    }
    res, err := tx.ExecContext(ctx, `INSERT INTO files (sha256, original_name, mime_type, size, uploaded_by, uploaded_at)
        VALUES (?, ?, ?, ?, ?, ?)`, saved.SHA256, name, saved.MIME, saved.Size, uploader, db.Now())
    if err != nil {
        return 0, err
    }
    return res.LastInsertId()
}

// AddImage stores an uploaded image, generates its renditions and attaches
// it to a part.
func (s *Service) AddImage(ctx context.Context, partID int64, r io.Reader, filename, caption string) (*Part, error) {
    if _, err := editablePart(ctx, s.DB.R, partID); err != nil {
        return nil, err
    }
    caption = strings.TrimSpace(caption)
    if err := checkLen("caption", caption, 0, 200); err != nil {
        return nil, err
    }
    saved, err := s.storeUpload(r, s.Cfg.MaxImageBytes)
    if err != nil {
        return nil, err
    }
    if !media.ImageTypes[saved.MIME] {
        return nil, &Error{Status: http.StatusUnsupportedMediaType, Code: "unsupported_image",
            Message: "images must be JPEG, PNG, GIF or WebP"}
    }
    if err := s.Files.MakeRenditions(saved.SHA256); err != nil {
        if errors.Is(err, media.ErrNotImage) {
            return nil, &Error{Status: http.StatusUnsupportedMediaType, Code: "unsupported_image", Message: err.Error()}
        }
        return nil, err
    }
    name := cleanFilename(filename)
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        fileID, err := insertFile(ctx, tx, saved, name)
        if err != nil {
            return err
        }
        order, err := nextSort(ctx, tx, "part_images", partID)
        if err != nil {
            return err
        }
        res, err := tx.ExecContext(ctx, `INSERT INTO part_images (part_id, file_id, caption, sort_order) VALUES (?, ?, ?, ?)`,
            partID, fileID, caption, order)
        if err != nil {
            return err
        }
        imgID, _ := res.LastInsertId()
        if caption != "" {
            if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "part.image_added", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "image_id": imgID, "file_id": fileID, "filename": name,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// UpdateImage changes an image's caption or sort order.
func (s *Service) UpdateImage(ctx context.Context, partID, imageID int64, p Patch) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var caption string
        var order int
        err = tx.QueryRowContext(ctx, `SELECT caption, sort_order FROM part_images WHERE id = ? AND part_id = ?`, imageID, partID).
            Scan(&caption, &order)
        if err == sql.ErrNoRows {
            return NotFound("image")
        }
        if err != nil {
            return err
        }
        nc, no := caption, order
        if _, err := p.Get("caption", &nc); err != nil {
            return err
        }
        if _, err := p.Get("sort_order", &no); err != nil {
            return err
        }
        nc = strings.TrimSpace(nc)
        if err := checkLen("caption", nc, 0, 200); err != nil {
            return err
        }
        ch := changes{}
        ch.add("caption", caption, nc)
        ch.add("sort_order", order, no)
        if len(ch) == 0 {
            return nil
        }
        if _, err := tx.ExecContext(ctx, `UPDATE part_images SET caption = ?, sort_order = ? WHERE id = ?`, nc, no, imageID); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.image_updated", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "image_id": imageID, "changes": ch,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// DeleteImage detaches an image from a part.
func (s *Service) DeleteImage(ctx context.Context, partID, imageID int64) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var fileID int64
        err = tx.QueryRowContext(ctx, `SELECT file_id FROM part_images WHERE id = ? AND part_id = ?`, imageID, partID).Scan(&fileID)
        if err == sql.ErrNoRows {
            return NotFound("image")
        }
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM part_images WHERE id = ?`, imageID); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `UPDATE parts SET thumbnail_image_id = NULL WHERE id = ? AND thumbnail_image_id = ?`,
            partID, imageID); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.image_removed", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "image_id": imageID, "file_id": fileID,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// SetThumbnail chooses which image represents a part; nil reverts to the
// first image.
func (s *Service) SetThumbnail(ctx context.Context, partID int64, imageID *int64) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        if imageID != nil {
            var n int
            if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM part_images WHERE id = ? AND part_id = ?`, *imageID, partID).Scan(&n); err != nil {
                return err
            }
            if n == 0 {
                return NotFound("image")
            }
        }
        if _, err := tx.ExecContext(ctx, `UPDATE parts SET thumbnail_image_id = ?, updated_at = ? WHERE id = ?`,
            imageID, db.Now(), partID); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.thumbnail_changed", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "image_id": imageID,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// AddDocument stores an uploaded document and attaches it to a part.
func (s *Service) AddDocument(ctx context.Context, partID int64, r io.Reader, filename, description string) (*Part, error) {
    if _, err := editablePart(ctx, s.DB.R, partID); err != nil {
        return nil, err
    }
    description = strings.TrimSpace(description)
    if err := checkLen("description", description, 0, 200); err != nil {
        return nil, err
    }
    saved, err := s.storeUpload(r, s.Cfg.MaxDocumentBytes)
    if err != nil {
        return nil, err
    }
    name := cleanFilename(filename)
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        fileID, err := insertFile(ctx, tx, saved, name)
        if err != nil {
            return err
        }
        order, err := nextSort(ctx, tx, "part_documents", partID)
        if err != nil {
            return err
        }
        res, err := tx.ExecContext(ctx, `INSERT INTO part_documents (part_id, file_id, description, sort_order) VALUES (?, ?, ?, ?)`,
            partID, fileID, description, order)
        if err != nil {
            return err
        }
        docID, _ := res.LastInsertId()
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.document_added", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "document_id": docID, "file_id": fileID, "filename": name,
            "description": description,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// UpdateDocument changes a document's description or sort order.
func (s *Service) UpdateDocument(ctx context.Context, partID, docID int64, p Patch) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var desc string
        var order int
        err = tx.QueryRowContext(ctx, `SELECT description, sort_order FROM part_documents WHERE id = ? AND part_id = ?`, docID, partID).
            Scan(&desc, &order)
        if err == sql.ErrNoRows {
            return NotFound("document")
        }
        if err != nil {
            return err
        }
        nd, no := desc, order
        if _, err := p.Get("description", &nd); err != nil {
            return err
        }
        if _, err := p.Get("sort_order", &no); err != nil {
            return err
        }
        nd = strings.TrimSpace(nd)
        if err := checkLen("description", nd, 0, 200); err != nil {
            return err
        }
        ch := changes{}
        ch.add("description", desc, nd)
        ch.add("sort_order", order, no)
        if len(ch) == 0 {
            return nil
        }
        if _, err := tx.ExecContext(ctx, `UPDATE part_documents SET description = ?, sort_order = ? WHERE id = ?`, nd, no, docID); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.document_updated", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "document_id": docID, "changes": ch,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// DeleteDocument detaches a document from a part.
func (s *Service) DeleteDocument(ctx context.Context, partID, docID int64) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var desc, fname string
        err = tx.QueryRowContext(ctx, `SELECT d.description, f.original_name FROM part_documents d JOIN files f ON f.id = d.file_id
            WHERE d.id = ? AND d.part_id = ?`, docID, partID).Scan(&desc, &fname)
        if err == sql.ErrNoRows {
            return NotFound("document")
        }
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM part_documents WHERE id = ?`, docID); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.document_removed", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "document_id": docID, "filename": fname, "description": desc,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// LinkInput holds fields for an external link.
type LinkInput struct {
    URL         string `json:"url"`
    Description string `json:"description"`
}

func validateLink(rawURL, desc string) (string, string, error) {
    rawURL = strings.TrimSpace(rawURL)
    desc = strings.TrimSpace(desc)
    u, err := url.Parse(rawURL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
        return "", "", Invalid("invalid_url", "links must be http or https URLs")
    }
    if err := checkLen("url", rawURL, 1, 2000); err != nil {
        return "", "", err
    }
    if err := checkLen("description", desc, 0, 200); err != nil {
        return "", "", err
    }
    return rawURL, desc, nil
}

// AddLink attaches an external link to a part.
func (s *Service) AddLink(ctx context.Context, partID int64, in LinkInput) (*Part, error) {
    u, desc, err := validateLink(in.URL, in.Description)
    if err != nil {
        return nil, err
    }
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        order, err := nextSort(ctx, tx, "part_links", partID)
        if err != nil {
            return err
        }
        res, err := tx.ExecContext(ctx, `INSERT INTO part_links (part_id, url, description, sort_order) VALUES (?, ?, ?, ?)`,
            partID, u, desc, order)
        if err != nil {
            return err
        }
        linkID, _ := res.LastInsertId()
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.link_added", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "link_id": linkID, "url": u, "description": desc,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// UpdateLink changes a link's URL, description or sort order.
func (s *Service) UpdateLink(ctx context.Context, partID, linkID int64, p Patch) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var cu, cd string
        var co int
        err = tx.QueryRowContext(ctx, `SELECT url, description, sort_order FROM part_links WHERE id = ? AND part_id = ?`, linkID, partID).
            Scan(&cu, &cd, &co)
        if err == sql.ErrNoRows {
            return NotFound("link")
        }
        if err != nil {
            return err
        }
        nu, nd, no := cu, cd, co
        for _, f := range []struct {
            k string
            d any
        }{{"url", &nu}, {"description", &nd}, {"sort_order", &no}} {
            if _, err := p.Get(f.k, f.d); err != nil {
                return err
            }
        }
        if nu, nd, err = validateLink(nu, nd); err != nil {
            return err
        }
        ch := changes{}
        ch.add("url", cu, nu)
        ch.add("description", cd, nd)
        ch.add("sort_order", co, no)
        if len(ch) == 0 {
            return nil
        }
        if _, err := tx.ExecContext(ctx, `UPDATE part_links SET url = ?, description = ?, sort_order = ? WHERE id = ?`,
            nu, nd, no, linkID); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.link_updated", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "link_id": linkID, "changes": ch,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// DeleteLink removes a link from a part.
func (s *Service) DeleteLink(ctx context.Context, partID, linkID int64) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        partName, err := editablePart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var u, d string
        err = tx.QueryRowContext(ctx, `SELECT url, description FROM part_links WHERE id = ? AND part_id = ?`, linkID, partID).Scan(&u, &d)
        if err == sql.ErrNoRows {
            return NotFound("link")
        }
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM part_links WHERE id = ?`, linkID); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{partID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.link_removed", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, partName}, "link_id": linkID, "url": u, "description": d,
        })
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// FileInfo describes a stored file for serving.
type FileInfo struct {
    ID           int64
    SHA256       string
    OriginalName string
    MIME         string
    Size         int64
    UploadedAt   time.Time
}

// GetFile returns metadata for a stored file.
func (s *Service) GetFile(ctx context.Context, id int64) (*FileInfo, error) {
    f := &FileInfo{ID: id}
    var at string
    err := s.DB.R.QueryRowContext(ctx, `SELECT sha256, original_name, mime_type, size, uploaded_at FROM files WHERE id = ?`, id).
        Scan(&f.SHA256, &f.OriginalName, &f.MIME, &f.Size, &at)
    if err == sql.ErrNoRows {
        return nil, NotFound("file")
    }
    if err != nil {
        return nil, err
    }
    f.UploadedAt, _ = time.Parse(db.TimeFormat, at)
    return f, nil
}

// Sweep removes expired sessions and login states, and files that have
// been unreferenced for longer than grace.
func (s *Service) Sweep(ctx context.Context, grace time.Duration) error {
    now := db.Now()
    if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now); err != nil {
        return err
    }
    if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM oidc_states WHERE expires_at < ?`, now); err != nil {
        return err
    }
    cutoff := db.FormatTime(time.Now().Add(-grace))
    var orphans []string
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        shas, err := queryStrings(ctx, tx, `SELECT DISTINCT sha256 FROM files f WHERE uploaded_at < ?
                AND NOT EXISTS (SELECT 1 FROM part_images i WHERE i.file_id = f.id)
                AND NOT EXISTS (SELECT 1 FROM part_documents d WHERE d.file_id = f.id)`, cutoff)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE uploaded_at < ?
                AND NOT EXISTS (SELECT 1 FROM part_images i WHERE i.file_id = files.id)
                AND NOT EXISTS (SELECT 1 FROM part_documents d WHERE d.file_id = files.id)`, cutoff); err != nil {
            return err
        }
        for _, sha := range shas {
            var n int
            if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE sha256 = ?`, sha).Scan(&n); err != nil {
                return err
            }
            if n == 0 {
                orphans = append(orphans, sha)
            }
        }
        return nil
    })
    if err != nil {
        return err
    }
    for _, sha := range orphans {
        if fi, err := os.Stat(s.Files.Path(sha)); err == nil && fi.ModTime().After(time.Now().Add(-grace)) {
            // Re-uploaded recently; its new row may not be committed yet.
            continue
        }
        if err := s.Files.Remove(sha); err != nil {
            return err
        }
    }
    return nil
}
