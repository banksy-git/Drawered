package api

import (
    "bytes"
    "context"
    "encoding/json"
    "image"
    "image/color"
    "image/png"
    "io"
    "net/http"
    "strings"
    "testing"
)

type partResp struct {
    ID            int64  `json:"id"`
    Name          string `json:"name"`
    Version       int64  `json:"version"`
    UOM           string `json:"uom"`
    TotalQuantity json.Number
    Low           bool     `json:"low"`
    Tags          []string `json:"tags"`
    Manufacturer  *struct {
        Name string `json:"name"`
    } `json:"manufacturer"`
    Stock []struct {
        LocationID int64       `json:"location_id"`
        Path       string      `json:"path"`
        Quantity   json.Number `json:"quantity"`
    } `json:"stock"`
    Images []struct {
        ID     int64 `json:"id"`
        FileID int64 `json:"file_id"`
    } `json:"images"`
    Documents []struct {
        FileID int64 `json:"file_id"`
    } `json:"documents"`
    ThumbnailFileID *int64   `json:"thumbnail_file_id"`
    Warnings        []string `json:"warnings"`
}

func (p *partResp) UnmarshalJSON(b []byte) error {
    type alias partResp
    var a struct {
        alias
        Total json.Number `json:"total_quantity"`
    }
    dec := json.NewDecoder(bytes.NewReader(b))
    dec.UseNumber()
    if err := dec.Decode(&a); err != nil {
        return err
    }
    *p = partResp(a.alias)
    p.TotalQuantity = a.Total
    return nil
}

func TestUnauthenticatedAndCSRF(t *testing.T) {
    h := newHarness(t)
    anon := &client{h: h, t: t}
    if code := anon.errCode("GET", "/api/v1/parts", nil, 401); code != "unauthenticated" {
        t.Fatalf("code %q", code)
    }
    c := h.login("admin@example.com", "Admin")
    c.csrf = ""
    if code := c.errCode("POST", "/api/v1/locations", map[string]any{"name": "X"}, 403); code != "csrf" {
        t.Fatalf("code %q", code)
    }
    c.csrf = "wrong"
    c.errCode("POST", "/api/v1/locations", map[string]any{"name": "X"}, 403)
}

func TestPermissions(t *testing.T) {
    h := newHarness(t)
    admin := h.login("admin@example.com", "Admin")
    reader := h.login("reader@example.com", "Read only")
    eng := h.login("eng@example.com", "Engineer")
    stores := h.login("stores@example.com", "Stores")
    none := h.login("none@example.com")

    loc := stores.createLocation(nil, "Bench", false)
    var p partResp
    eng.must("POST", "/api/v1/parts", map[string]any{"name": "Resistor"}, 201, &p)

    cases := []struct {
        c      *client
        method string
        path   string
        body   any
        status int
    }{
        {none, "GET", "/api/v1/parts", nil, 403},
        {none, "GET", "/api/v1/me", nil, 200},
        {reader, "GET", "/api/v1/parts", nil, 200},
        {reader, "GET", "/api/v1/locations", nil, 200},
        {reader, "GET", "/api/v1/events", nil, 200},
        {reader, "POST", "/api/v1/parts", map[string]any{"name": "x"}, 403},
        {reader, "POST", "/api/v1/stock/add", map[string]any{"part_id": p.ID, "location_id": loc.ID, "quantity": 1}, 403},
        {eng, "POST", "/api/v1/stock/add", map[string]any{"part_id": p.ID, "location_id": loc.ID, "quantity": 1}, 200},
        {eng, "POST", "/api/v1/stock/set", map[string]any{"part_id": p.ID, "location_id": loc.ID, "quantity": 5}, 403},
        {eng, "POST", "/api/v1/locations", map[string]any{"name": "Shelf"}, 403},
        {eng, "PATCH", pf("/api/v1/locations/%d", loc.ID), map[string]any{"version": loc.Version, "colour": "#ff0000"}, 403},
        {eng, "DELETE", pf("/api/v1/parts/%d", p.ID), nil, 403},
        {eng, "GET", "/api/v1/users", nil, 403},
        {stores, "POST", "/api/v1/stock/set", map[string]any{"part_id": p.ID, "location_id": loc.ID, "quantity": 5}, 200},
        {stores, "GET", "/api/v1/users", nil, 403},
        {stores, "GET", "/api/v1/system", nil, 403},
        {admin, "GET", "/api/v1/users", nil, 200},
        {admin, "GET", "/api/v1/system", nil, 200},
        {admin, "GET", "/api/v1/settings", nil, 200},
    }
    for _, tc := range cases {
        if got, b := tc.c.do(tc.method, tc.path, tc.body); got != tc.status {
            t.Errorf("%s %s: status %d, want %d: %s", tc.method, tc.path, got, tc.status, b)
        }
    }
}

func TestLocations(t *testing.T) {
    h := newHarness(t)
    c := h.login("stores@example.com", "Stores")
    garage := c.createLocation(nil, "Garage", true)
    cab := c.createLocation(&garage.ID, "Grey Cabinet", true)
    shelf := c.createLocation(&cab.ID, "Shelf2", false)
    box := c.createLocation(&shelf.ID, "Box1", false)

    var l struct {
        Path      string `json:"path"`
        Ancestors []struct {
            Name string `json:"name"`
        } `json:"ancestors"`
        EffectiveColour *string `json:"effective_colour"`
        Version         int64   `json:"version"`
    }
    c.must("GET", pf("/api/v1/locations/%d", box.ID), nil, 200, &l)
    if l.Path != "Garage / Grey Cabinet / Shelf2 / Box1" || len(l.Ancestors) != 3 {
        t.Fatalf("path %q ancestors %d", l.Path, len(l.Ancestors))
    }

    // Duplicate sibling names (case-insensitive) and slashes are rejected.
    c.errCode("POST", "/api/v1/locations", map[string]any{"parent_id": shelf.ID, "name": "box1"}, 409)
    c.errCode("POST", "/api/v1/locations", map[string]any{"name": "a/b"}, 400)

    // Colour inherits.
    c.must("PATCH", pf("/api/v1/locations/%d", cab.ID), map[string]any{"version": cab.Version, "colour": "#AA0000"}, 200, nil)
    c.must("GET", pf("/api/v1/locations/%d", box.ID), nil, 200, &l)
    if l.EffectiveColour == nil || *l.EffectiveColour != "#aa0000" {
        t.Fatalf("effective colour %v", l.EffectiveColour)
    }

    // Cannot move into own subtree.
    c.must("GET", pf("/api/v1/locations/%d", garage.ID), nil, 200, &l)
    if code := c.errCode("PATCH", pf("/api/v1/locations/%d", garage.ID),
        map[string]any{"version": l.Version, "parent_id": box.ID}, 409); code != "location_cycle" {
        t.Fatalf("code %q", code)
    }

    // Stale version.
    if code := c.errCode("PATCH", pf("/api/v1/locations/%d", garage.ID),
        map[string]any{"version": l.Version + 5, "name": "G"}, 409); code != "stale_version" {
        t.Fatalf("code %q", code)
    }

    // Structural locations refuse stock, and stocked ones can't become structural.
    var p partResp
    c.must("POST", "/api/v1/parts", map[string]any{"name": "Widget"}, 201, &p)
    if code := c.errCode("POST", "/api/v1/stock/add", map[string]any{"part_id": p.ID, "location_id": garage.ID, "quantity": 1}, 409); code != "structural_location" {
        t.Fatalf("code %q", code)
    }
    c.must("POST", "/api/v1/stock/add", map[string]any{"part_id": p.ID, "location_id": box.ID, "quantity": 3}, 200, nil)
    c.must("GET", pf("/api/v1/locations/%d", box.ID), nil, 200, &l)
    if code := c.errCode("PATCH", pf("/api/v1/locations/%d", box.ID), map[string]any{"version": l.Version, "structural": true}, 409); code != "location_has_stock" {
        t.Fatalf("code %q", code)
    }

    // Moving a location updates part paths and search.
    other := c.createLocation(nil, "Bench", false)
    c.must("GET", pf("/api/v1/locations/%d", box.ID), nil, 200, &l)
    c.must("PATCH", pf("/api/v1/locations/%d", box.ID), map[string]any{"version": l.Version, "parent_id": other.ID}, 200, nil)
    c.must("GET", pf("/api/v1/parts/%d", p.ID), nil, 200, &p)
    if p.Stock[0].Path != "Bench / Box1" {
        t.Fatalf("path after move %q", p.Stock[0].Path)
    }
    var page struct {
        Total int `json:"total"`
    }
    c.must("GET", "/api/v1/parts?q=bench", nil, 200, &page)
    if page.Total != 1 {
        t.Fatalf("search by new path: %d", page.Total)
    }

    // Delete is refused while stock or children remain.
    c.errCode("DELETE", pf("/api/v1/locations/%d", box.ID), nil, 409)
    c.errCode("DELETE", pf("/api/v1/locations/%d", other.ID), nil, 409)
    c.must("POST", "/api/v1/stock/remove", map[string]any{"part_id": p.ID, "location_id": box.ID, "quantity": 3}, 200, nil)
    c.must("DELETE", pf("/api/v1/locations/%d", box.ID), nil, 204, nil)

    // Location history records the move.
    var ev struct {
        Items []struct {
            Action string `json:"action"`
        } `json:"items"`
    }
    c.must("GET", pf("/api/v1/locations/%d/events", other.ID), nil, 200, &ev)
    found := false
    for _, e := range ev.Items {
        found = found || e.Action == "location.moved"
    }
    if !found {
        t.Fatalf("location history missing move: %+v", ev.Items)
    }

    var tree struct {
        Items []struct {
            Name           string `json:"name"`
            TotalPartCount int    `json:"total_part_count"`
        } `json:"items"`
    }
    c.must("GET", "/api/v1/locations", nil, 200, &tree)
    if len(tree.Items) != 4 || tree.Items[0].Name != "Bench" {
        t.Fatalf("tree %+v", tree.Items)
    }
}

func TestPartsAndStock(t *testing.T) {
    h := newHarness(t)
    c := h.login("stores@example.com", "Stores")
    a := c.createLocation(nil, "A1", false)
    b := c.createLocation(nil, "B1", false)

    var p partResp
    c.must("POST", "/api/v1/parts", map[string]any{
        "name": "10k resistor 0603", "tags": []string{"Resistor", " SMD ", "resistor"}, "mpn": "RC0603FR-0710KL",
        "manufacturer": "Yageo", "cost": "0.0023", "barcode": "12345",
        "stock": []map[string]any{{"location_id": a.ID, "quantity": 100}},
    }, 201, &p)
    if p.TotalQuantity.String() != "100" || p.UOM != "pcs" || len(p.Tags) != 2 || p.Manufacturer.Name != "Yageo" {
        t.Fatalf("created %+v", p)
    }

    // Whole numbers only by default.
    if code := c.errCode("POST", "/api/v1/stock/add", map[string]any{"part_id": p.ID, "location_id": a.ID, "quantity": 1.5}, 400); code != "fractional_not_allowed" {
        t.Fatalf("code %q", code)
    }
    c.must("POST", "/api/v1/stock/remove", map[string]any{"part_id": p.ID, "location_id": a.ID, "quantity": 30, "reason": "project"}, 200, &p)
    if p.TotalQuantity.String() != "70" {
        t.Fatalf("after remove %s", p.TotalQuantity)
    }
    if code := c.errCode("POST", "/api/v1/stock/remove", map[string]any{"part_id": p.ID, "location_id": a.ID, "quantity": 71}, 409); code != "insufficient_stock" {
        t.Fatalf("code %q", code)
    }
    c.must("POST", "/api/v1/stock/move", map[string]any{"part_id": p.ID, "from_location_id": a.ID, "to_location_id": b.ID, "quantity": 20}, 200, &p)
    if len(p.Stock) != 2 || p.Stock[0].Quantity.String() != "50" || p.Stock[1].Quantity.String() != "20" {
        t.Fatalf("after move %+v", p.Stock)
    }
    c.must("POST", "/api/v1/stock/set", map[string]any{"part_id": p.ID, "location_id": b.ID, "quantity": 25}, 200, &p)
    if p.TotalQuantity.String() != "75" {
        t.Fatalf("after set %s", p.TotalQuantity)
    }

    // Low stock threshold on an entry.
    c.must("PATCH", pf("/api/v1/stock/%d/%d", p.ID, b.ID), map[string]any{"min_quantity": 30}, 200, &p)
    if !p.Low {
        t.Fatal("expected low stock")
    }
    var page struct {
        Total int `json:"total"`
    }
    c.must("GET", "/api/v1/parts?stock=low", nil, 200, &page)
    if page.Total != 1 {
        t.Fatalf("low filter %d", page.Total)
    }

    // Fractional parts with a unit of measure.
    var wire partResp
    c.must("POST", "/api/v1/parts", map[string]any{"name": "Hookup wire", "uom": "m", "allow_fractional": true}, 201, &wire)
    c.must("POST", "/api/v1/stock/add", map[string]any{"part_id": wire.ID, "location_id": a.ID, "quantity": "2.5"}, 200, &wire)
    c.must("POST", "/api/v1/stock/remove", map[string]any{"part_id": wire.ID, "location_id": a.ID, "quantity": 0.25}, 200, &wire)
    if wire.TotalQuantity.String() != "2.25" || wire.UOM != "m" {
        t.Fatalf("wire %s %s", wire.TotalQuantity, wire.UOM)
    }
    c.errCode("POST", "/api/v1/stock/add", map[string]any{"part_id": wire.ID, "location_id": a.ID, "quantity": 0.0001}, 400)
    // Cannot turn off fractional while fractional stock exists.
    if code := c.errCode("PATCH", pf("/api/v1/parts/%d", wire.ID), map[string]any{"version": wire.Version, "allow_fractional": false}, 409); code != "fractional_stock" {
        t.Fatalf("code %q", code)
    }

    // Edit with stale version fails; correct version succeeds.
    c.errCode("PATCH", pf("/api/v1/parts/%d", p.ID), map[string]any{"version": p.Version + 1, "name": "x"}, 409)
    c.must("PATCH", pf("/api/v1/parts/%d", p.ID), map[string]any{"version": p.Version, "manufacturer": nil, "tags": []string{"passive"}}, 200, &p)
    if p.Manufacturer != nil || len(p.Tags) != 1 || p.Tags[0] != "passive" {
        t.Fatalf("after patch %+v", p)
    }

    // Duplicate barcode warning.
    var dup partResp
    c.must("POST", "/api/v1/parts", map[string]any{"name": "Other", "barcode": "12345"}, 201, &dup)
    if len(dup.Warnings) != 1 || dup.Warnings[0] != "duplicate_barcode" {
        t.Fatalf("warnings %v", dup.Warnings)
    }

    // Part history includes the stock operations.
    var ev struct {
        Total int `json:"total"`
        Items []struct {
            Action string `json:"action"`
            Actor  *struct {
                Name string `json:"name"`
            } `json:"actor"`
        } `json:"items"`
    }
    c.must("GET", pf("/api/v1/parts/%d/events", p.ID), nil, 200, &ev)
    actions := map[string]bool{}
    for _, e := range ev.Items {
        actions[e.Action] = true
        if e.Actor == nil || e.Actor.Name != "stores@example.com" {
            t.Fatalf("event actor %+v", e.Actor)
        }
    }
    for _, want := range []string{"part.created", "stock.removed", "stock.moved", "stock.set", "stock.entry_updated", "part.updated"} {
        if !actions[want] {
            t.Errorf("history missing %s: %v", want, actions)
        }
    }

    // Delete needs force while stock remains; restore and purge.
    if code := c.errCode("DELETE", pf("/api/v1/parts/%d", p.ID), nil, 409); code != "part_has_stock" {
        t.Fatalf("code %q", code)
    }
    c.must("DELETE", pf("/api/v1/parts/%d?force=true", p.ID), nil, 204, nil)
    c.must("GET", "/api/v1/parts?q=resistor", nil, 200, &page)
    if page.Total != 0 {
        t.Fatal("deleted part still searchable")
    }
    c.must("GET", "/api/v1/parts?deleted=only", nil, 200, &page)
    if page.Total != 1 {
        t.Fatalf("deleted listing %d", page.Total)
    }
    c.must("POST", pf("/api/v1/parts/%d/restore", p.ID), nil, 200, nil)
    c.must("GET", "/api/v1/parts?q=10k", nil, 200, &page)
    if page.Total != 1 {
        t.Fatal("restored part not searchable")
    }
    admin := h.login("admin@example.com", "Admin")
    c.must("DELETE", pf("/api/v1/parts/%d?force=true", p.ID), nil, 204, nil)
    c.errCode("POST", pf("/api/v1/parts/%d/purge", p.ID), nil, 403)
    admin.must("POST", pf("/api/v1/parts/%d/purge", p.ID), nil, 204, nil)
    c.errCode("GET", pf("/api/v1/parts/%d", p.ID), nil, 404)
}

func TestSearch(t *testing.T) {
    h := newHarness(t)
    c := h.login("stores@example.com", "Stores")
    garage := c.createLocation(nil, "Garage", true)
    drawer := c.createLocation(&garage.ID, "Drawer 7", false)
    var p1, p2 partResp
    c.must("POST", "/api/v1/parts", map[string]any{
        "name": "LM317T voltage regulator", "mpn": "LM317T", "manufacturer": "Texas Instruments",
        "tags": []string{"regulator", "to-220"}, "description": "Adjustable linear regulator",
        "stock": []map[string]any{{"location_id": drawer.ID, "quantity": 4}},
    }, 201, &p1)
    c.must("POST", "/api/v1/parts", map[string]any{
        "name": "NE555 timer", "mpn": "NE555P", "supplier": "Farnell", "supplier_sku": "9589899", "tags": []string{"timer"},
    }, 201, &p2)
    c.must("POST", pf("/api/v1/parts/%d/links", p2.ID), map[string]any{"url": "https://example.com/ne555.pdf", "description": "Datasheet monostable"}, 201, nil)

    type page struct {
        Total int `json:"total"`
        Items []struct {
            ID        int64    `json:"id"`
            Locations []string `json:"locations"`
            Tags      []string `json:"tags"`
        } `json:"items"`
    }
    search := func(q string) page {
        var pg page
        c.must("GET", "/api/v1/parts?"+q, nil, 200, &pg)
        return pg
    }
    for q, want := range map[string]int64{
        "q=lm31":                     p1.ID, // prefix
        "q=texas":                    p1.ID, // manufacturer
        "q=adjustable+linear":        p1.ID, // description, AND
        "q=drawer+7":                 p1.ID, // location path
        "q=monostable":               p2.ID, // link description
        "q=farnell":                  p2.ID,
        "q=%22linear+regulator%22":   p1.ID, // phrase
        "tag=timer":                  p2.ID,
        "q=regulator&tag=to-220":     p1.ID,
        "stock=none":                 p2.ID,
        pf("location=%d", garage.ID): p1.ID, // includes descendants
    } {
        pg := search(q)
        if pg.Total != 1 || pg.Items[0].ID != want {
            t.Errorf("search %s: %+v", q, pg)
        }
    }
    if pg := search("q=lm317+timer"); pg.Total != 0 {
        t.Errorf("AND semantics: %d", pg.Total)
    }
    if pg := search("q=%22%2A%22"); pg.Total != 2 {
        t.Errorf("punctuation-only query should list all, got %d", pg.Total)
    }
    if pg := search("q=lm317"); len(pg.Items[0].Locations) != 1 || pg.Items[0].Locations[0] != "Garage / Drawer 7" {
        t.Errorf("summary locations %+v", pg.Items)
    }
    var lk struct {
        Items []struct {
            ID int64 `json:"id"`
        } `json:"items"`
    }
    c.must("GET", "/api/v1/parts/lookup?code=9589899", nil, 200, &lk)
    if len(lk.Items) != 1 || lk.Items[0].ID != p2.ID {
        t.Errorf("lookup %+v", lk)
    }
    var locs struct {
        Items []struct {
            ID int64 `json:"id"`
        } `json:"items"`
    }
    c.must("GET", "/api/v1/locations/search?q=gar+dra", nil, 200, &locs)
    if len(locs.Items) != 1 || locs.Items[0].ID != drawer.ID {
        t.Errorf("location search %+v", locs)
    }

    // Reindex leaves results unchanged.
    admin := h.login("admin@example.com", "Admin")
    admin.must("POST", "/api/v1/system/reindex", nil, 204, nil)
    if pg := search("q=texas"); pg.Total != 1 {
        t.Errorf("after reindex %d", pg.Total)
    }

    // Renaming a manufacturer updates search.
    var mfrs struct {
        Items []struct {
            ID int64 `json:"id"`
        } `json:"items"`
    }
    c.must("GET", "/api/v1/manufacturers?q=tex", nil, 200, &mfrs)
    c.must("PATCH", pf("/api/v1/manufacturers/%d", mfrs.Items[0].ID), map[string]any{"name": "TI"}, 200, nil)
    if pg := search("q=texas"); pg.Total != 0 {
        t.Errorf("old manufacturer name still matches")
    }
}

func pngBytes(t *testing.T, w, hgt int, alpha bool) []byte {
    img := image.NewNRGBA(image.Rect(0, 0, w, hgt))
    for y := 0; y < hgt; y++ {
        for x := 0; x < w; x++ {
            a := uint8(255)
            if alpha && x < w/2 {
                a = 0
            }
            img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 100, a})
        }
    }
    var buf bytes.Buffer
    if err := png.Encode(&buf, img); err != nil {
        t.Fatal(err)
    }
    return buf.Bytes()
}

func TestFiles(t *testing.T) {
    h := newHarness(t)
    c := h.login("eng@example.com", "Engineer")
    var p partResp
    c.must("POST", "/api/v1/parts", map[string]any{"name": "Board"}, 201, &p)

    status, body := c.upload(pf("/api/v1/parts/%d/images", p.ID), "file", "photo.png", pngBytes(t, 800, 400, false), map[string]string{"caption": "top side"})
    if status != 201 {
        t.Fatalf("upload: %d %s", status, body)
    }
    json.Unmarshal(body, &p)
    if len(p.Images) != 1 || p.ThumbnailFileID == nil || *p.ThumbnailFileID != p.Images[0].FileID {
        t.Fatalf("images %+v thumb %v", p.Images, p.ThumbnailFileID)
    }
    resp := c.request("GET", pf("/files/%d/thumb", p.Images[0].FileID), nil, "")
    img, format, err := image.Decode(resp.Body)
    resp.Body.Close()
    if err != nil || format != "jpeg" || img.Bounds().Dx() != 160 || img.Bounds().Dy() != 80 {
        t.Fatalf("thumb %v %s %v", err, format, img)
    }

    // Transparent images keep alpha in PNG renditions.
    status, body = c.upload(pf("/api/v1/parts/%d/images", p.ID), "file", "alpha.png", pngBytes(t, 50, 50, true), nil)
    json.Unmarshal(body, &p)
    resp = c.request("GET", pf("/files/%d/small", p.Images[1].FileID), nil, "")
    if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
        t.Fatalf("alpha rendition type %q", ct)
    }
    resp.Body.Close()

    // Choose the second image as thumbnail.
    c.must("PUT", pf("/api/v1/parts/%d/thumbnail", p.ID), map[string]any{"image_id": p.Images[1].ID}, 200, &p)
    if *p.ThumbnailFileID != p.Images[1].FileID {
        t.Fatal("thumbnail not changed")
    }

    // Non-images are rejected as images.
    status, _ = c.upload(pf("/api/v1/parts/%d/images", p.ID), "file", "x.png", []byte("not an image"), nil)
    if status != 415 {
        t.Fatalf("bad image status %d", status)
    }

    // Documents: SVG/HTML served as attachments with a sandbox CSP.
    status, body = c.upload(pf("/api/v1/parts/%d/documents", p.ID), "file", "evil.html",
        []byte("<html><script>alert(1)</script></html>"), map[string]string{"description": "Schematic"})
    if status != 201 {
        t.Fatalf("doc upload %d %s", status, body)
    }
    json.Unmarshal(body, &p)
    resp = c.request("GET", pf("/files/%d", p.Documents[0].FileID), nil, "")
    io.Copy(io.Discard, resp.Body)
    resp.Body.Close()
    if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
        t.Fatalf("disposition %q", cd)
    }
    if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
        t.Fatalf("csp %q", csp)
    }

    // Documents are searchable by description.
    var pg struct {
        Total int `json:"total"`
    }
    c.must("GET", "/api/v1/parts?q=schematic", nil, 200, &pg)
    if pg.Total != 1 {
        t.Fatal("document description not indexed")
    }

    // Files need parts:read.
    none := h.login("none@example.com")
    resp = none.request("GET", pf("/files/%d", p.Documents[0].FileID), nil, "")
    resp.Body.Close()
    if resp.StatusCode != http.StatusForbidden {
        t.Fatalf("file access without permission: %d", resp.StatusCode)
    }

    // Removing images clears the thumbnail and the sweep later reclaims files.
    c.must("DELETE", pf("/api/v1/parts/%d/images/%d", p.ID, p.Images[1].ID), nil, 200, &p)
    if *p.ThumbnailFileID != p.Images[0].FileID {
        t.Fatal("thumbnail should fall back to first image")
    }
    if err := h.svc.Sweep(context.Background(), 0); err != nil {
        t.Fatal(err)
    }
}

func TestRolesAndUsers(t *testing.T) {
    h := newHarness(t)
    admin := h.login("admin@example.com", "Admin")

    // New users get the default role (Read only).
    u, err := h.svc.LoginUser(context.Background(), "test", "new1", map[string]any{"email": "new1@example.com"})
    if err != nil {
        t.Fatal(err)
    }
    if len(u.Roles) != 1 || u.Roles[0].Name != "Read only" {
        t.Fatalf("default role %+v", u.Roles)
    }
    // Default None.
    admin.must("PATCH", "/api/v1/settings", map[string]any{"default_role_id": nil}, 200, nil)
    u, _ = h.svc.LoginUser(context.Background(), "test", "new2", map[string]any{"email": "new2@example.com"})
    if len(u.Roles) != 0 || len(u.Permissions) != 0 {
        t.Fatalf("expected no roles %+v", u.Roles)
    }

    // Claim mappings grant roles at login and are recomputed.
    var m struct {
        ID int64 `json:"id"`
    }
    admin.must("POST", "/api/v1/role-mappings", map[string]any{"claim": "groups", "match_type": "contains",
        "value": "inventory-stores", "role_id": h.roleID("Stores")}, 201, &m)
    admin.errCode("POST", "/api/v1/role-mappings", map[string]any{"claim": "groups", "match_type": "regex",
        "value": "(", "role_id": h.roleID("Stores")}, 400)
    u, _ = h.svc.LoginUser(context.Background(), "test", "new2", map[string]any{"groups": []any{"x", "inventory-stores"}})
    if len(u.Roles) != 1 || u.Roles[0].Source != "mapping" {
        t.Fatalf("mapped roles %+v", u.Roles)
    }
    u, _ = h.svc.LoginUser(context.Background(), "test", "new2", map[string]any{"groups": []any{"x"}})
    if len(u.Roles) != 0 {
        t.Fatalf("mapping not revoked %+v", u.Roles)
    }
    var res struct {
        Roles []struct {
            Name string `json:"name"`
        } `json:"roles"`
    }
    admin.must("POST", "/api/v1/role-mappings/test", map[string]any{"claims": map[string]any{"groups": "inventory-stores"}}, 200, &res)
    if len(res.Roles) != 1 || res.Roles[0].Name != "Stores" {
        t.Fatalf("mapping test %+v", res)
    }

    // The last admin cannot be removed or disabled.
    admin.errCode("PUT", pf("/api/v1/users/%d/roles", admin.userID), map[string]any{"role_ids": []int64{}}, 409)
    admin.errCode("POST", pf("/api/v1/users/%d/disable", admin.userID), nil, 409)

    // The Admin role keeps system:admin and cannot be deleted.
    adminRole := h.roleID("Admin")
    var role struct {
        Version int64 `json:"version"`
    }
    admin.must("GET", pf("/api/v1/roles/%d", adminRole), nil, 200, &role)
    admin.errCode("PATCH", pf("/api/v1/roles/%d", adminRole), map[string]any{"version": role.Version, "permissions": []string{"parts:read"}}, 409)
    admin.errCode("DELETE", pf("/api/v1/roles/%d", adminRole), nil, 409)

    // Custom role lifecycle; deleting a held role needs force.
    var custom struct {
        ID      int64 `json:"id"`
        Version int64 `json:"version"`
    }
    admin.must("POST", "/api/v1/roles", map[string]any{"name": "Auditor", "permissions": []string{"events:read", "parts:read"}}, 201, &custom)
    admin.errCode("POST", "/api/v1/roles", map[string]any{"name": "x", "permissions": []string{"bogus"}}, 400)
    other := h.login("other@example.com", "Auditor")
    other.must("GET", "/api/v1/events", nil, 200, nil)
    other.errCode("GET", "/api/v1/locations", nil, 403)
    admin.errCode("DELETE", pf("/api/v1/roles/%d", custom.ID), nil, 409)
    admin.must("DELETE", pf("/api/v1/roles/%d?force=true", custom.ID), nil, 204, nil)
    other.errCode("GET", "/api/v1/events", nil, 403)

    // Disabling a user ends their sessions.
    victim := h.login("victim@example.com", "Read only")
    admin.must("POST", pf("/api/v1/users/%d/disable", victim.userID), nil, 200, nil)
    victim.errCode("GET", "/api/v1/me", nil, 401)
    if _, err := h.svc.LoginUser(context.Background(), "test", "victim@example.com", map[string]any{}); err == nil {
        t.Fatal("disabled user logged in")
    }
}

func TestEventsAreRecorded(t *testing.T) {
    h := newHarness(t)
    c := h.login("admin@example.com", "Admin")
    before := h.eventCount()
    loc := c.createLocation(nil, "Shelf", false)
    var p partResp
    c.must("POST", "/api/v1/parts", map[string]any{"name": "Nut"}, 201, &p)
    c.must("POST", "/api/v1/stock/add", map[string]any{"part_id": p.ID, "location_id": loc.ID, "quantity": 1}, 200, nil)
    c.must("POST", pf("/api/v1/parts/%d/links", p.ID), map[string]any{"url": "https://example.com"}, 201, nil)
    if got := h.eventCount() - before; got != 4 {
        t.Fatalf("expected 4 events, got %d", got)
    }
    // Events are append-only at the database level.
    if _, err := h.svc.DB.W.Exec(`DELETE FROM events`); err == nil {
        t.Fatal("events deleted")
    }
    var feed struct {
        Total int `json:"total"`
    }
    c.must("GET", "/api/v1/events?action=stock.", nil, 200, &feed)
    if feed.Total != 1 {
        t.Fatalf("action prefix filter %d", feed.Total)
    }
}

func TestBackup(t *testing.T) {
    h := newHarness(t)
    c := h.login("admin@example.com", "Admin")
    c.createLocation(nil, "Shelf", false)
    resp := c.request("GET", "/api/v1/system/backup", nil, "")
    defer resp.Body.Close()
    b, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != 200 || len(b) < 100 || b[0] != 0x1f || b[1] != 0x8b {
        t.Fatalf("backup status %d len %d", resp.StatusCode, len(b))
    }
}
