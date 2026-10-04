package importer

import (
    "bytes"
    "context"
    "encoding/json"
    "image"
    "image/color"
    "image/png"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
    "time"

    "drawered/internal/config"
    "drawered/internal/db"
    "drawered/internal/inventree"
    "drawered/internal/media"
    "drawered/internal/service"
)

const token = "secret-token"

// fakeInvenTree serves fixtures with limit/offset paging (capped at two
// records per page to exercise paging) and requires the token everywhere.
type fakeInvenTree struct {
    srv       *httptest.Server
    lists     map[string][]any
    oldLogin  bool
    imageHits int
}

func newFake(t *testing.T) *fakeInvenTree {
    f := &fakeInvenTree{lists: fixtures()}
    var buf bytes.Buffer
    img := image.NewRGBA(image.Rect(0, 0, 64, 48))
    for x := 0; x < 64; x++ {
        img.Set(x, 10, color.RGBA{200, 0, 0, 255})
    }
    png.Encode(&buf, img)
    pngBytes := buf.Bytes()

    f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/api/user/me/token/" || r.URL.Path == "/api/user/token/" {
            if f.oldLogin && r.URL.Path == "/api/user/me/token/" {
                http.NotFound(w, r)
                return
            }
            if u, p, ok := r.BasicAuth(); !ok || u != "alice" || p != "pw" {
                http.Error(w, `{"detail":"bad credentials"}`, http.StatusUnauthorized)
                return
            }
            json.NewEncoder(w).Encode(map[string]any{"token": token, "name": "x"})
            return
        }
        if r.Header.Get("Authorization") != "Token "+token {
            http.Error(w, `{"detail":"Authentication credentials were not provided."}`, http.StatusUnauthorized)
            return
        }
        if strings.HasPrefix(r.URL.Path, "/media/") {
            f.imageHits++
            w.Header().Set("Content-Type", "image/png")
            w.Write(pngBytes)
            return
        }
        items, ok := f.lists[r.URL.Path]
        if !ok {
            http.NotFound(w, r)
            return
        }
        if r.URL.Path == "/api/stock/" && r.URL.Query().Get("in_stock") != "true" {
            t.Errorf("stock fetched without in_stock filter")
        }
        limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
        offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
        if limit <= 0 || limit > 2 {
            limit = 2
        }
        end := min(offset+limit, len(items))
        if offset > len(items) {
            offset = end
        }
        var next any
        if end < len(items) {
            next = "http://" + r.Host + r.URL.Path + "?limit=2&offset=" + strconv.Itoa(end)
        }
        json.NewEncoder(w).Encode(map[string]any{"count": len(items), "next": next, "previous": nil, "results": items[offset:end]})
    }))
    t.Cleanup(f.srv.Close)
    return f
}

func fixtures() map[string][]any {
    m := func(kv ...any) map[string]any {
        out := map[string]any{}
        for i := 0; i < len(kv); i += 2 {
            out[kv[i].(string)] = kv[i+1]
        }
        return out
    }
    return map[string][]any{
        "/api/company/": {
            m("pk", 1, "name", "Mouser"), m("pk", 2, "name", "Digi-Key"), m("pk", 3, "name", "C&K"),
            m("pk", 4, "name", "Farnell"), m("pk", 5, "name", "Vishay"),
        },
        // Children listed before parents, to check ordering.
        "/api/stock/location/": {
            m("pk", 12, "name", "A1/A2", "description", "", "parent", 11, "structural", false, "icon", "ti:box:outline"),
            m("pk", 11, "name", "Shelf", "description", "Top shelf", "parent", 10, "structural", false),
            m("pk", 10, "name", "garage", "description", "", "parent", nil, "structural", true),
            m("pk", 13, "name", "Cabinet", "description", "", "parent", 10, "structural", true),
        },
        "/api/part/category/": {
            m("pk", 1, "name", "Electrical", "description", "", "parent", nil, "structural", true, "icon", "ti:bolt:outline", "level", 0, "pathstring", "Electrical"),
            m("pk", 2, "name", "Switches", "description", "", "parent", 1, "structural", false, "icon", "fas fa-toggle-on", "level", 1),
            m("pk", 3, "name", "Twoway", "description", "SPDT", "parent", 2, "structural", false, "icon", nil, "level", 2),
        },
        "/api/part/": {
            m("pk", 100, "name", "SPDT toggle", "description", "Panel mount", "IPN", "SW-001", "revision", "B",
                "keywords", "toggle spdt", "category", 3, "units", "", "minimum_stock", 5.0, "link", "https://example.com/spdt",
                "image", "/media/part_images/spdt.png", "active", true, "tags", []string{"switch", "panel"}),
            m("pk", 101, "name", "Hookup wire", "description", "", "IPN", nil, "category", nil, "units", "m",
                "minimum_stock", 0.0, "link", "", "image", nil, "active", true, "tags", []string{}),
            m("pk", 102, "name", "Old thing", "description", "", "category", 3, "units", "", "minimum_stock", 0,
                "image", nil, "active", false, "tags", []string{}),
            m("pk", 103, "name", "Resistor", "description", "", "category", 1, "units", "", "minimum_stock", 0,
                "image", nil, "active", true, "tags", []string{}, "default_supplier", 30),
        },
        "/api/company/part/": {
            m("pk", 10, "part", 100, "supplier", 1, "supplier_detail", m("pk", 1, "name", "Mouser"), "SKU", "M-1",
                "link", "", "active", true, "primary", false, "manufacturer_part", nil),
            m("pk", 11, "part", 100, "supplier", 2, "supplier_detail", m("pk", 2, "name", "Digi-Key"), "SKU", "DK-7101",
                "link", "https://digikey.example/7101", "active", true, "primary", true, "manufacturer_part", 20),
            // Older server: no "primary"; default_supplier on the part decides.
            m("pk", 30, "part", 103, "supplier", 4, "SKU", "FN-1", "link", nil, "active", false, "manufacturer_part", nil),
            m("pk", 31, "part", 103, "supplier", 1, "SKU", "M-2", "link", nil, "active", true, "manufacturer_part", nil),
        },
        "/api/company/part/manufacturer/": {
            m("pk", 20, "part", 100, "manufacturer", 3, "manufacturer_detail", m("pk", 3, "name", "C&K"), "MPN", "7101SYCQE",
                "link", "https://ck.example/7101"),
            m("pk", 21, "part", 103, "manufacturer", 5, "MPN", "CRCW060310K0", "link", ""),
        },
        "/api/company/price-break/": {
            m("pk", 1, "part", 11, "quantity", 10.0, "price", "1.00000", "price_currency", "GBP"),
            m("pk", 2, "part", 11, "quantity", 1.0, "price", "1.2345678", "price_currency", "GBP"),
            m("pk", 3, "part", 10, "quantity", 1.0, "price", "0.5", "price_currency", "USD"),
        },
        "/api/stock/": {
            m("pk", 1, "part", 100, "location", 12, "quantity", 10.0),
            m("pk", 2, "part", 100, "location", 12, "quantity", 5.0),
            m("pk", 3, "part", 101, "location", 11, "quantity", "12.5"),
            m("pk", 4, "part", 101, "location", nil, "quantity", 3.0),
            m("pk", 5, "part", 103, "location", 13, "quantity", 7.0),
            m("pk", 6, "part", 100, "location", 11, "quantity", 0.0001),
        },
    }
}

func newService(t *testing.T) *service.Service {
    t.Helper()
    dir := t.TempDir()
    cfg := &config.Config{DataDir: dir, DefaultCurrency: "GBP", MaxImageBytes: 5 << 20, MaxDocumentBytes: 5 << 20,
        SessionIdle: time.Hour, SessionMax: time.Hour, OIDCGroupsClaim: "groups"}
    d, err := db.Open(filepath.Join(dir, "drawered.db"))
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { d.Close() })
    if err := d.Migrate(context.Background()); err != nil {
        t.Fatal(err)
    }
    files, err := media.New(dir)
    if err != nil {
        t.Fatal(err)
    }
    svc := service.New(d, files, cfg)
    if err := svc.Seed(context.Background()); err != nil {
        t.Fatal(err)
    }
    return svc
}

func hasWarning(r *Report, sub string) bool {
    for _, w := range r.Warnings {
        if strings.Contains(w, sub) {
            return true
        }
    }
    return false
}

func TestImport(t *testing.T) {
    ctx := context.Background()
    f := newFake(t)
    svc := newService(t)

    // An existing top-level "Garage" is reused rather than duplicated.
    garage, err := svc.CreateLocation(ctx, service.LocationInput{Name: "Garage", Structural: true})
    if err != nil {
        t.Fatal(err)
    }

    client := inventree.New(f.srv.URL + "/")
    if err := client.Login(ctx, "alice", "pw"); err != nil {
        t.Fatal(err)
    }
    rep, err := Run(ctx, svc, client, Options{SkipInactive: true})
    if err != nil {
        t.Fatal(err)
    }
    if rep.Locations.Reused != 1 || rep.Locations.Created != 3 || rep.Categories.Created != 3 ||
        rep.Parts.Created != 3 || rep.Inactive != 1 || rep.Images != 1 {
        t.Fatalf("report %+v", rep)
    }

    locs, _ := svc.ListLocations(ctx)
    paths := map[string]service.Location{}
    for _, l := range locs {
        paths[l.Path] = l
    }
    if _, ok := paths["Garage / Shelf / A1-A2"]; !ok || len(locs) != 4 {
        t.Fatalf("locations %v", paths)
    }
    if paths["Garage"].ID != garage.ID || paths["Garage / Shelf"].Description != "Top shelf" {
        t.Fatalf("garage not reused or description lost")
    }

    cats, _ := svc.ListCategories(ctx)
    byPath := map[string]service.Category{}
    for _, c := range cats {
        byPath[c.Path] = c
    }
    elec, twoway := byPath["Electrical"], byPath["Electrical / Switches / Twoway"]
    if elec.Icon == nil || *elec.Icon != "bolt" || !elec.Structural {
        t.Fatalf("electrical %+v", elec)
    }
    if byPath["Electrical / Switches"].Icon != nil || !hasWarning(rep, "fas fa-toggle-on") {
        t.Fatal("non-Tabler icon should be dropped with a warning")
    }
    if twoway.EffectiveIcon == nil || *twoway.EffectiveIcon != "bolt" {
        t.Fatalf("twoway icon %+v", twoway.EffectiveIcon)
    }

    find := func(name string) *service.Part {
        t.Helper()
        page, err := svc.ListParts(ctx, service.PartFilter{Q: "\"" + name + "\""})
        if err != nil || page.Total != 1 {
            t.Fatalf("find %q: %v %+v", name, err, page)
        }
        p, err := svc.GetPart(ctx, page.Items[0].ID)
        if err != nil {
            t.Fatal(err)
        }
        return p
    }

    sw := find("SPDT toggle")
    if sw.Category == nil || sw.Category.Path != "Electrical / Switches / Twoway" {
        t.Errorf("category %+v", sw.Category)
    }
    if sw.Supplier == nil || sw.Supplier.Name != "Digi-Key" || sw.SupplierSKU != "DK-7101" {
        t.Errorf("primary supplier not chosen: %+v %q", sw.Supplier, sw.SupplierSKU)
    }
    if sw.Manufacturer == nil || sw.Manufacturer.Name != "C&K" || sw.MPN != "7101SYCQE" {
        t.Errorf("manufacturer %+v %q", sw.Manufacturer, sw.MPN)
    }
    if sw.Cost == nil || sw.Cost.String() != "1.234568" || *sw.Currency != "GBP" || !hasWarning(rep, "rounded to 1.234568") {
        t.Errorf("cost %v %v", sw.Cost, sw.Currency)
    }
    if !strings.Contains(sw.Description, "Panel mount") || !strings.Contains(sw.Description, "IPN: SW-001") ||
        !strings.Contains(sw.Description, "Revision: B") || !strings.Contains(sw.Description, "Keywords: toggle spdt") {
        t.Errorf("description %q", sw.Description)
    }
    if strings.Join(sw.Tags, ",") != "panel,switch" || sw.MinTotalQuantity == nil || sw.MinTotalQuantity.String() != "5" {
        t.Errorf("tags %v min %v", sw.Tags, sw.MinTotalQuantity)
    }
    // 10 + 5 summed at one location; 0.0001 rounds to zero and is dropped.
    if len(sw.Stock) != 1 || sw.Stock[0].Quantity.String() != "15" || sw.Stock[0].Path != "Garage / Shelf / A1-A2" || sw.AllowFractional {
        t.Errorf("stock %+v fractional %v", sw.Stock, sw.AllowFractional)
    }
    if len(sw.Images) != 1 || sw.ThumbnailFileID == nil {
        t.Errorf("images %+v", sw.Images)
    }
    if len(sw.Links) != 3 {
        t.Errorf("links %+v", sw.Links)
    }
    if p, err := svc.ListParts(ctx, service.PartFilter{Q: "sw-001"}); err != nil || p.Total != 1 {
        t.Errorf("IPN not searchable")
    }

    wire := find("Hookup wire")
    if wire.UOM != "m" || !wire.AllowFractional || wire.TotalQuantity.String() != "12.5" || wire.Category != nil {
        t.Errorf("wire %+v", wire)
    }
    if !hasWarning(rep, "stock item 4 (part 101): no location") {
        t.Errorf("missing no-location warning: %v", rep.Warnings)
    }

    res := find("Resistor")
    if res.Supplier == nil || res.Supplier.Name != "Farnell" || res.SupplierSKU != "FN-1" {
        t.Errorf("default_supplier not honoured: %+v", res.Supplier)
    }
    if res.Manufacturer == nil || res.Manufacturer.Name != "Vishay" || res.MPN != "CRCW060310K0" {
        t.Errorf("fallback manufacturer part: %+v %q", res.Manufacturer, res.MPN)
    }
    if res.Category != nil || !hasWarning(rep, "part 103 (Resistor): category is structural") {
        t.Errorf("structural category should leave the part uncategorised")
    }
    if len(res.Stock) != 0 || !hasWarning(rep, "location is structural") {
        t.Errorf("stock in a structural location should be skipped: %+v", res.Stock)
    }

    // Everything is in the event log, attributed to the system.
    ev, err := svc.SubjectEvents(ctx, "part", sw.ID, service.Paging{})
    if err != nil || ev.Total < 3 || ev.Items[0].Actor != nil {
        t.Errorf("events %+v %v", ev, err)
    }

    // A second run creates nothing new.
    hits := f.imageHits
    rep2, err := Run(ctx, svc, client, Options{})
    if err != nil {
        t.Fatal(err)
    }
    if rep2.Locations.Created+rep2.Categories.Created != 0 || rep2.Locations.Skipped != 4 || rep2.Categories.Skipped != 3 ||
        rep2.Parts.Skipped != 3 || rep2.Parts.Created != 1 || f.imageHits != hits {
        t.Fatalf("re-run %+v", rep2)
    }
    // The inactive part, skipped the first time, is picked up now.
    find("Old thing")
}

func TestDryRunWritesNothing(t *testing.T) {
    ctx := context.Background()
    f := newFake(t)
    svc := newService(t)
    client := inventree.New(f.srv.URL)
    client.Token = token
    rep, err := Run(ctx, svc, client, Options{DryRun: true})
    if err != nil {
        t.Fatal(err)
    }
    if rep.Locations.Created != 4 || rep.Parts.Created != 4 || rep.Images != 1 || f.imageHits != 0 {
        t.Fatalf("dry run report %+v hits %d", rep, f.imageHits)
    }
    locs, _ := svc.ListLocations(ctx)
    page, _ := svc.ListParts(ctx, service.PartFilter{})
    if len(locs) != 0 || page.Total != 0 {
        t.Fatalf("dry run wrote data: %d locations, %d parts", len(locs), page.Total)
    }
}

func TestLoginFallbackAndErrors(t *testing.T) {
    ctx := context.Background()
    f := newFake(t)
    f.oldLogin = true
    client := inventree.New(f.srv.URL)
    if err := client.Login(ctx, "alice", "pw"); err != nil || client.Token != token {
        t.Fatalf("old login endpoint: %v", err)
    }
    bad := inventree.New(f.srv.URL)
    if err := bad.Login(ctx, "alice", "wrong"); err == nil {
        t.Fatal("bad password accepted")
    }
    svc := newService(t)
    bad.Token = "nope"
    if _, err := Run(ctx, svc, bad, Options{}); err == nil || !strings.Contains(err.Error(), "401") {
        t.Fatalf("expected an authentication error, got %v", err)
    }
}

func TestMapIcon(t *testing.T) {
    for in, want := range map[string]string{
        "ti:bolt:outline": "bolt", "ti:toggle-left:filled": "toggle-left", "ti:cpu": "cpu",
        "ti-screw": "screw", "ti ti-plug": "plug", "": "",
    } {
        if got, ok := mapIcon(in); !ok || got != want {
            t.Errorf("%q: got %q %v", in, got, ok)
        }
    }
    for _, in := range []string{"fas fa-bolt", "mdi-chip", "ti:Bad Name"} {
        if _, ok := mapIcon(in); ok {
            t.Errorf("%q should not map", in)
        }
    }
}
