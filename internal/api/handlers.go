package api

import (
    "fmt"
    "io"
    "net/http"
    "time"

    "drawered/internal/auth"
    "drawered/internal/perm"
    "drawered/internal/service"
)

// Me describes the current user and what the UI may offer them.
type Me struct {
    User             *service.User     `json:"user"`
    Permissions      []perm.Permission `json:"permissions"`
    CSRFToken        string            `json:"csrf_token"`
    DefaultCurrency  string            `json:"default_currency"`
    MaxImageBytes    int64             `json:"max_image_bytes"`
    MaxDocumentBytes int64             `json:"max_document_bytes"`
    Version          string            `json:"version"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
    u := userFrom(r.Context())
    return ok(w, Me{
        User: u, Permissions: u.Permissions, CSRFToken: sessionFrom(r.Context()).CSRFToken,
        DefaultCurrency: s.cfg.DefaultCurrency, MaxImageBytes: s.cfg.MaxImageBytes,
        MaxDocumentBytes: s.cfg.MaxDocumentBytes, Version: s.version,
    })
}

// Parts.

func (s *Server) listParts(w http.ResponseWriter, r *http.Request) error {
    q := r.URL.Query()
    f := service.PartFilter{Q: q.Get("q"), Tags: q["tag"], Stock: q.Get("stock"), Sort: q.Get("sort"), Paging: paging(r)}
    var err error
    if f.ManufacturerID, err = queryInt(r, "manufacturer"); err != nil {
        return err
    }
    if f.SupplierID, err = queryInt(r, "supplier"); err != nil {
        return err
    }
    if f.LocationID, err = queryInt(r, "location"); err != nil {
        return err
    }
    if q.Get("category") == "none" {
        f.Uncategorised = true
    } else if f.CategoryID, err = queryInt(r, "category"); err != nil {
        return err
    }
    if q.Get("deleted") == "only" {
        if !userFrom(r.Context()).Perms().Has(perm.PartsDelete) {
            return service.Forbidden("viewing deleted parts needs %s", perm.PartsDelete)
        }
        f.Deleted = true
    }
    page, err := s.svc.ListParts(r.Context(), f)
    if err != nil {
        return err
    }
    return ok(w, page)
}

func (s *Server) createPart(w http.ResponseWriter, r *http.Request) error {
    var in service.PartInput
    if err := decode(r, &in); err != nil {
        return err
    }
    p, err := s.svc.CreatePart(r.Context(), in)
    if err != nil {
        return err
    }
    return created(w, p)
}

func (s *Server) lookupPart(w http.ResponseWriter, r *http.Request) error {
    refs, err := s.svc.LookupCode(r.Context(), r.URL.Query().Get("code"))
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": refs})
}

func (s *Server) getPart(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := s.svc.GetPart(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, p)
}

func (s *Server) partStock(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := s.svc.GetPart(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": p.Stock, "total_quantity": p.TotalQuantity, "uom": p.UOM})
}

func (s *Server) updatePart(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    part, err := s.svc.UpdatePart(r.Context(), id, p)
    if err != nil {
        return err
    }
    return ok(w, part)
}

func (s *Server) deletePart(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.DeletePart(r.Context(), id, queryBool(r, "force")); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) restorePart(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := s.svc.RestorePart(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, p)
}

func (s *Server) purgePart(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.PurgePart(r.Context(), id); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) duplicatePart(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    var in struct {
        Images bool `json:"images"`
    }
    if r.ContentLength != 0 {
        if err := decode(r, &in); err != nil {
            return err
        }
    }
    p, err := s.svc.DuplicatePart(r.Context(), id, in.Images)
    if err != nil {
        return err
    }
    return created(w, p)
}

func (s *Server) subjectEvents(kind string) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        id, err := pathID(r, "id")
        if err != nil {
            return err
        }
        page, err := s.svc.SubjectEvents(r.Context(), kind, id, paging(r))
        if err != nil {
            return err
        }
        return ok(w, page)
    }
}

// Part resources.

func (s *Server) upload(w http.ResponseWriter, r *http.Request, max int64,
    fn func(id int64, file io.Reader, name, text string) (*service.Part, error), textField string) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    r.Body = http.MaxBytesReader(w, r.Body, max+1<<20)
    mr, err := r.MultipartReader()
    if err != nil {
        return service.Invalid("invalid_upload", "expected a multipart/form-data upload")
    }
    text := ""
    for {
        part, err := mr.NextPart()
        if err != nil {
            return service.Invalid("invalid_upload", "no file field in upload")
        }
        switch part.FormName() {
        case textField:
            b, err := io.ReadAll(io.LimitReader(part, 4096))
            if err != nil {
                return err
            }
            text = string(b)
        case "file":
            if part.FileName() == "" {
                return service.Invalid("invalid_upload", "file field has no filename")
            }
            p, err := fn(id, part, part.FileName(), text)
            if err != nil {
                return err
            }
            return created(w, p)
        }
    }
}

func (s *Server) addImage(w http.ResponseWriter, r *http.Request) error {
    return s.upload(w, r, s.cfg.MaxImageBytes, func(id int64, f io.Reader, name, caption string) (*service.Part, error) {
        return s.svc.AddImage(r.Context(), id, f, name, caption)
    }, "caption")
}

func (s *Server) addDocument(w http.ResponseWriter, r *http.Request) error {
    return s.upload(w, r, s.cfg.MaxDocumentBytes, func(id int64, f io.Reader, name, desc string) (*service.Part, error) {
        return s.svc.AddDocument(r.Context(), id, f, name, desc)
    }, "description")
}

func (s *Server) subResource(w http.ResponseWriter, r *http.Request,
    fn func(id, sub int64, p service.Patch) (*service.Part, error), withBody bool) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    sub, err := pathID(r, "sub")
    if err != nil {
        return err
    }
    var p service.Patch
    if withBody {
        if p, err = decodePatch(r); err != nil {
            return err
        }
    }
    part, err := fn(id, sub, p)
    if err != nil {
        return err
    }
    return ok(w, part)
}

func (s *Server) updateImage(w http.ResponseWriter, r *http.Request) error {
    return s.subResource(w, r, func(id, sub int64, p service.Patch) (*service.Part, error) {
        return s.svc.UpdateImage(r.Context(), id, sub, p)
    }, true)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) error {
    return s.subResource(w, r, func(id, sub int64, _ service.Patch) (*service.Part, error) {
        return s.svc.DeleteImage(r.Context(), id, sub)
    }, false)
}

func (s *Server) setThumbnail(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    var in struct {
        ImageID *int64 `json:"image_id"`
    }
    if err := decode(r, &in); err != nil {
        return err
    }
    p, err := s.svc.SetThumbnail(r.Context(), id, in.ImageID)
    if err != nil {
        return err
    }
    return ok(w, p)
}

func (s *Server) updateDocument(w http.ResponseWriter, r *http.Request) error {
    return s.subResource(w, r, func(id, sub int64, p service.Patch) (*service.Part, error) {
        return s.svc.UpdateDocument(r.Context(), id, sub, p)
    }, true)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) error {
    return s.subResource(w, r, func(id, sub int64, _ service.Patch) (*service.Part, error) {
        return s.svc.DeleteDocument(r.Context(), id, sub)
    }, false)
}

func (s *Server) addLink(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    var in service.LinkInput
    if err := decode(r, &in); err != nil {
        return err
    }
    p, err := s.svc.AddLink(r.Context(), id, in)
    if err != nil {
        return err
    }
    return created(w, p)
}

func (s *Server) updateLink(w http.ResponseWriter, r *http.Request) error {
    return s.subResource(w, r, func(id, sub int64, p service.Patch) (*service.Part, error) {
        return s.svc.UpdateLink(r.Context(), id, sub, p)
    }, true)
}

func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) error {
    return s.subResource(w, r, func(id, sub int64, _ service.Patch) (*service.Part, error) {
        return s.svc.DeleteLink(r.Context(), id, sub)
    }, false)
}

// Stock.

func (s *Server) stockAdjust(kind string) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        var op service.StockOp
        if err := decode(r, &op); err != nil {
            return err
        }
        p, err := s.svc.StockAdjust(r.Context(), kind, op)
        if err != nil {
            return err
        }
        return ok(w, p)
    }
}

func (s *Server) stockMove(w http.ResponseWriter, r *http.Request) error {
    var op service.StockOp
    if err := decode(r, &op); err != nil {
        return err
    }
    p, err := s.svc.StockMove(r.Context(), op)
    if err != nil {
        return err
    }
    return ok(w, p)
}

func (s *Server) addStockEntry(w http.ResponseWriter, r *http.Request) error {
    var in service.EntryInput
    if err := decode(r, &in); err != nil {
        return err
    }
    p, err := s.svc.AddStockLocation(r.Context(), in)
    if err != nil {
        return err
    }
    return created(w, p)
}

func stockKey(r *http.Request) (int64, int64, error) {
    part, err := pathID(r, "part")
    if err != nil {
        return 0, 0, err
    }
    loc, err := pathID(r, "loc")
    return part, loc, err
}

func (s *Server) updateStockEntry(w http.ResponseWriter, r *http.Request) error {
    part, loc, err := stockKey(r)
    if err != nil {
        return err
    }
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    res, err := s.svc.UpdateStockEntry(r.Context(), part, loc, p)
    if err != nil {
        return err
    }
    return ok(w, res)
}

func (s *Server) removeStockEntry(w http.ResponseWriter, r *http.Request) error {
    part, loc, err := stockKey(r)
    if err != nil {
        return err
    }
    res, err := s.svc.RemoveStockLocation(r.Context(), part, loc)
    if err != nil {
        return err
    }
    return ok(w, res)
}

// Locations.

func (s *Server) listLocations(w http.ResponseWriter, r *http.Request) error {
    ls, err := s.svc.ListLocations(r.Context())
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": ls})
}

func (s *Server) searchLocations(w http.ResponseWriter, r *http.Request) error {
    ls, err := s.svc.SearchLocations(r.Context(), r.URL.Query().Get("q"), paging(r).Limit)
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": ls})
}

func (s *Server) createLocation(w http.ResponseWriter, r *http.Request) error {
    var in service.LocationInput
    if err := decode(r, &in); err != nil {
        return err
    }
    l, err := s.svc.CreateLocation(r.Context(), in)
    if err != nil {
        return err
    }
    return created(w, l)
}

func (s *Server) getLocation(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    l, err := s.svc.GetLocation(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, l)
}

func (s *Server) updateLocation(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    l, err := s.svc.UpdateLocation(r.Context(), id, p)
    if err != nil {
        return err
    }
    return ok(w, l)
}

func (s *Server) deleteLocation(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.DeleteLocation(r.Context(), id); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) locationStock(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    page, err := s.svc.LocationStock(r.Context(), id, queryBool(r, "descendants"), paging(r))
    if err != nil {
        return err
    }
    return ok(w, page)
}

// Part categories.

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) error {
    cs, err := s.svc.ListCategories(r.Context())
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": cs})
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) error {
    var in service.CategoryInput
    if err := decode(r, &in); err != nil {
        return err
    }
    c, err := s.svc.CreateCategory(r.Context(), in)
    if err != nil {
        return err
    }
    return created(w, c)
}

func (s *Server) getCategory(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    c, err := s.svc.GetCategory(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, c)
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    c, err := s.svc.UpdateCategory(r.Context(), id, p)
    if err != nil {
        return err
    }
    return ok(w, c)
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.DeleteCategory(r.Context(), id); err != nil {
        return err
    }
    return noContent(w)
}

// Catalogue.

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) error {
    page, err := s.svc.ListTags(r.Context(), r.URL.Query().Get("q"), paging(r))
    if err != nil {
        return err
    }
    return ok(w, page)
}

func (s *Server) renameTag(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    var in struct {
        Name string `json:"name"`
    }
    if err := decode(r, &in); err != nil {
        return err
    }
    if err := s.svc.RenameTag(r.Context(), id, in.Name); err != nil {
        return err
    }
    return noContent(w)
}

type mergeInput struct {
    IntoID int64 `json:"into_id"`
}

func (s *Server) mergeTag(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    var in mergeInput
    if err := decode(r, &in); err != nil {
        return err
    }
    if err := s.svc.MergeTag(r.Context(), id, in.IntoID); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.DeleteTag(r.Context(), id); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) listUOMs(w http.ResponseWriter, r *http.Request) error {
    u, err := s.svc.UOMs(r.Context(), r.URL.Query().Get("q"))
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": u})
}

func (s *Server) listNamed(k service.NamedKind) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        page, err := s.svc.ListNamed(r.Context(), k, r.URL.Query().Get("q"), paging(r))
        if err != nil {
            return err
        }
        return ok(w, page)
    }
}

func (s *Server) createNamed(k service.NamedKind) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        var in struct {
            Name string `json:"name"`
            URL  string `json:"url"`
        }
        if err := decode(r, &in); err != nil {
            return err
        }
        e, err := s.svc.CreateNamed(r.Context(), k, in.Name, in.URL)
        if err != nil {
            return err
        }
        return created(w, e)
    }
}

func (s *Server) updateNamed(k service.NamedKind) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        id, err := pathID(r, "id")
        if err != nil {
            return err
        }
        p, err := decodePatch(r)
        if err != nil {
            return err
        }
        e, err := s.svc.UpdateNamed(r.Context(), k, id, p)
        if err != nil {
            return err
        }
        return ok(w, e)
    }
}

func (s *Server) mergeNamed(k service.NamedKind) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        id, err := pathID(r, "id")
        if err != nil {
            return err
        }
        var in mergeInput
        if err := decode(r, &in); err != nil {
            return err
        }
        if err := s.svc.MergeNamed(r.Context(), k, id, in.IntoID); err != nil {
            return err
        }
        return noContent(w)
    }
}

func (s *Server) deleteNamed(k service.NamedKind) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        id, err := pathID(r, "id")
        if err != nil {
            return err
        }
        if err := s.svc.DeleteNamed(r.Context(), k, id); err != nil {
            return err
        }
        return noContent(w)
    }
}

// Events.

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) error {
    q := r.URL.Query()
    f := service.EventFilter{Action: q.Get("action"), SubjectType: q.Get("subject_type"), Paging: paging(r)}
    var err error
    if f.ActorID, err = queryInt(r, "actor"); err != nil {
        return err
    }
    if f.SubjectID, err = queryInt(r, "subject_id"); err != nil {
        return err
    }
    for _, d := range []struct {
        key string
        dst *string
    }{{"from", &f.From}, {"to", &f.To}} {
        v := q.Get(d.key)
        if v == "" {
            continue
        }
        t, err := time.Parse(time.RFC3339, v)
        if err != nil {
            if t, err = time.Parse("2006-01-02", v); err != nil {
                return service.Invalid("invalid_query", "%s must be an RFC 3339 time or a date", d.key)
            }
        }
        *d.dst = t.UTC().Format("2006-01-02T15:04:05.000Z")
    }
    page, err := s.svc.ListEvents(r.Context(), f)
    if err != nil {
        return err
    }
    return ok(w, page)
}

// Administration.

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) error {
    rs, err := s.svc.ListRoles(r.Context())
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": rs})
}

func (s *Server) getRole(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    role, err := s.svc.GetRole(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, role)
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) error {
    var in service.RoleInput
    if err := decode(r, &in); err != nil {
        return err
    }
    role, err := s.svc.CreateRole(r.Context(), in)
    if err != nil {
        return err
    }
    return created(w, role)
}

func (s *Server) updateRole(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    role, err := s.svc.UpdateRole(r.Context(), id, p)
    if err != nil {
        return err
    }
    return ok(w, role)
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.DeleteRole(r.Context(), id, queryBool(r, "force")); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) listMappings(w http.ResponseWriter, r *http.Request) error {
    ms, err := s.svc.ListMappings(r.Context())
    if err != nil {
        return err
    }
    return ok(w, map[string]any{"items": ms})
}

func (s *Server) createMapping(w http.ResponseWriter, r *http.Request) error {
    var in service.MappingInput
    if err := decode(r, &in); err != nil {
        return err
    }
    m, err := s.svc.CreateMapping(r.Context(), in)
    if err != nil {
        return err
    }
    return created(w, m)
}

func (s *Server) updateMapping(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    m, err := s.svc.UpdateMapping(r.Context(), id, p)
    if err != nil {
        return err
    }
    return ok(w, m)
}

func (s *Server) deleteMapping(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    if err := s.svc.DeleteMapping(r.Context(), id); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) testMappings(w http.ResponseWriter, r *http.Request) error {
    var in struct {
        Claims map[string]any `json:"claims"`
        UserID *int64         `json:"user_id"`
    }
    if err := decode(r, &in); err != nil {
        return err
    }
    res, err := s.svc.TestMappings(r.Context(), in.Claims, in.UserID)
    if err != nil {
        return err
    }
    return ok(w, res)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) error {
    st, err := s.svc.GetSettings(r.Context())
    if err != nil {
        return err
    }
    return ok(w, st)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) error {
    p, err := decodePatch(r)
    if err != nil {
        return err
    }
    st, err := s.svc.UpdateSettings(r.Context(), p)
    if err != nil {
        return err
    }
    return ok(w, st)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
    page, err := s.svc.ListUsers(r.Context(), r.URL.Query().Get("q"), paging(r))
    if err != nil {
        return err
    }
    return ok(w, page)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    u, err := s.svc.GetUser(r.Context(), id)
    if err != nil {
        return err
    }
    return ok(w, u)
}

func (s *Server) setUserRoles(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    var in struct {
        RoleIDs []int64 `json:"role_ids"`
    }
    if err := decode(r, &in); err != nil {
        return err
    }
    u, err := s.svc.SetUserRoles(r.Context(), id, in.RoleIDs)
    if err != nil {
        return err
    }
    return ok(w, u)
}

func (s *Server) setUserDisabled(disabled bool) apiFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        id, err := pathID(r, "id")
        if err != nil {
            return err
        }
        u, err := s.svc.SetUserDisabled(r.Context(), id, disabled)
        if err != nil {
            return err
        }
        return ok(w, u)
    }
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    ss, err := s.svc.ListSessions(r.Context(), id)
    if err != nil {
        return err
    }
    cur := sessionFrom(r.Context())
    type item struct {
        service.Session
        Current bool `json:"current"`
    }
    out := []item{}
    for _, x := range ss {
        out = append(out, item{x, x.ID == cur.ID})
    }
    return ok(w, map[string]any{"items": out})
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r, "id")
    if err != nil {
        return err
    }
    sid, err := pathID(r, "sub")
    if err != nil {
        return err
    }
    if err := s.svc.RevokeSession(r.Context(), id, sid); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) error {
    si, err := s.svc.Info(r.Context(), s.version)
    if err != nil {
        return err
    }
    return ok(w, si)
}

func (s *Server) reindex(w http.ResponseWriter, r *http.Request) error {
    if err := s.svc.Reindex(r.Context()); err != nil {
        return err
    }
    return noContent(w)
}

func (s *Server) backup(w http.ResponseWriter, r *http.Request) error {
    name := fmt.Sprintf("drawered-backup-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
    w.Header().Set("Content-Type", "application/gzip")
    w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
    w.Header().Set("Cache-Control", "no-store")
    if err := s.svc.Backup(r.Context(), w); err != nil {
        // Headers are already sent; all we can do is log and truncate.
        s.log.Error("backup failed", "err", err)
    }
    return nil
}

// Auth.

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
    returnTo := r.URL.Query().Get("return_to")
    if s.oidc.DevMode() {
        u, err := s.oidc.DevLogin(r.Context())
        if err != nil {
            s.fail(w, r, err)
            return
        }
        s.startSession(w, r, u.ID, returnTo)
        return
    }
    target, err := s.oidc.LoginURL(r.Context(), returnTo)
    if err != nil {
        s.log.Error("login", "err", err)
        http.Error(w, "The identity provider is unavailable. Please try again shortly.", http.StatusBadGateway)
        return
    }
    http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
    u, returnTo, err := s.oidc.Callback(r.Context(), r.URL.Query())
    if err != nil {
        s.log.Warn("login callback failed", "err", err)
        msg := "Login failed. Please try again."
        if e, ok := service.AsError(err); ok && e.Code == "forbidden" {
            msg = "Your account has been disabled."
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.WriteHeader(http.StatusForbidden)
        fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Login failed</title>
<p>%s</p><p><a href="/auth/login">Try again</a></p>`, msg)
        return
    }
    s.startSession(w, r, u.ID, returnTo)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64, returnTo string) {
    token, err := s.svc.CreateSession(r.Context(), userID, r.UserAgent())
    if err != nil {
        s.fail(w, r, err)
        return
    }
    http.SetCookie(w, &http.Cookie{
        Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: !s.cfg.InsecureCookies,
        SameSite: http.SameSiteLaxMode, MaxAge: int(s.cfg.SessionMax.Seconds()),
    })
    http.Redirect(w, r, auth.SafeReturnTo(returnTo), http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
    if c, err := r.Cookie(SessionCookie); err == nil {
        if err := s.svc.DeleteSessionByToken(r.Context(), c.Value); err != nil {
            return err
        }
    }
    http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
        Secure: !s.cfg.InsecureCookies, SameSite: http.SameSiteLaxMode})
    redirect := "/"
    if !s.oidc.DevMode() {
        if u := s.oidc.LogoutURL(); u != "" {
            redirect = u
        }
    }
    return ok(w, map[string]string{"redirect": redirect})
}
