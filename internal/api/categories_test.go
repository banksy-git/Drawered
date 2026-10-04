package api

import (
    "context"
    "testing"
)

type catResp struct {
    ID            int64   `json:"id"`
    Version       int64   `json:"version"`
    Path          string  `json:"path"`
    Icon          *string `json:"icon"`
    EffectiveIcon *string `json:"effective_icon"`
    PartCount     int     `json:"part_count"`
    TotalCount    int     `json:"total_part_count"`
}

type partCatResp struct {
    ID       int64 `json:"id"`
    Version  int64 `json:"version"`
    Category *struct {
        ID            int64   `json:"id"`
        Path          string  `json:"path"`
        EffectiveIcon *string `json:"effective_icon"`
    } `json:"category"`
}

func (c *client) createCategory(parent *int64, name string, structural bool, icon *string) catResp {
    c.t.Helper()
    var r catResp
    c.must("POST", "/api/v1/categories", map[string]any{"parent_id": parent, "name": name, "structural": structural, "icon": icon}, 201, &r)
    return r
}

func TestCategories(t *testing.T) {
    h := newHarness(t)
    stores := h.login("stores@example.com", "Stores")
    eng := h.login("eng@example.com", "Engineer")
    reader := h.login("reader@example.com", "Read only")

    // Only categories:manage may change categories; parts:read may view them.
    eng.errCode("POST", "/api/v1/categories", map[string]any{"name": "X"}, 403)
    elec := stores.createCategory(nil, "Electrical", true, ptr("ti-bolt"))
    if elec.Icon == nil || *elec.Icon != "bolt" {
        t.Fatalf("icon not normalised: %v", elec.Icon)
    }
    switches := stores.createCategory(&elec.ID, "Switches", true, nil)
    twoway := stores.createCategory(&switches.ID, "Twoway", false, ptr("toggle-left"))
    rocker := stores.createCategory(&switches.ID, "Rocker", false, nil)
    reader.must("GET", "/api/v1/categories", nil, 200, nil)
    reader.errCode("PATCH", pf("/api/v1/categories/%d", twoway.ID), map[string]any{"version": 1, "name": "x"}, 403)

    if twoway.Path != "Electrical / Switches / Twoway" {
        t.Fatalf("path %q", twoway.Path)
    }
    if rocker.EffectiveIcon == nil || *rocker.EffectiveIcon != "bolt" {
        t.Fatalf("inherited icon %v", rocker.EffectiveIcon)
    }
    stores.errCode("POST", "/api/v1/categories", map[string]any{"name": "Bad", "icon": "Bolt!"}, 400)
    stores.errCode("POST", "/api/v1/categories", map[string]any{"parent_id": switches.ID, "name": "twoway"}, 409)

    // Parts may not go in structural categories.
    if code := eng.errCode("POST", "/api/v1/parts", map[string]any{"name": "SPDT", "category_id": switches.ID}, 409); code != "structural_category" {
        t.Fatalf("code %q", code)
    }
    var p partCatResp
    eng.must("POST", "/api/v1/parts", map[string]any{"name": "SPDT toggle", "category_id": twoway.ID}, 201, &p)
    if p.Category == nil || p.Category.Path != "Electrical / Switches / Twoway" || *p.Category.EffectiveIcon != "toggle-left" {
        t.Fatalf("part category %+v", p.Category)
    }
    var other partCatResp
    eng.must("POST", "/api/v1/parts", map[string]any{"name": "Loose screw"}, 201, &other)

    // Engineers assign categories through parts:edit.
    eng.must("PATCH", pf("/api/v1/parts/%d", other.ID), map[string]any{"version": other.Version, "category_id": rocker.ID}, 200, &other)
    eng.must("PATCH", pf("/api/v1/parts/%d", other.ID), map[string]any{"version": other.Version, "category_id": nil}, 200, &other)
    if other.Category != nil {
        t.Fatal("category not cleared")
    }

    // Counts include descendants.
    var c catResp
    reader.must("GET", pf("/api/v1/categories/%d", elec.ID), nil, 200, &c)
    if c.PartCount != 0 || c.TotalCount != 1 {
        t.Fatalf("counts %+v", c)
    }

    // A category with parts cannot become structural.
    stores.must("GET", pf("/api/v1/categories/%d", twoway.ID), nil, 200, &c)
    if code := stores.errCode("PATCH", pf("/api/v1/categories/%d", twoway.ID), map[string]any{"version": c.Version, "structural": true}, 409); code != "category_has_parts" {
        t.Fatalf("code %q", code)
    }
    // No cycles.
    stores.must("GET", pf("/api/v1/categories/%d", elec.ID), nil, 200, &c)
    stores.errCode("PATCH", pf("/api/v1/categories/%d", elec.ID), map[string]any{"version": c.Version, "parent_id": twoway.ID}, 409)

    // Search: by category name, and filters including descendants and uncategorised.
    type page struct {
        Total int `json:"total"`
        Items []struct {
            ID       int64 `json:"id"`
            Category *struct {
                Name string `json:"name"`
            } `json:"category"`
        } `json:"items"`
    }
    search := func(q string) page {
        var pg page
        reader.must("GET", "/api/v1/parts?"+q, nil, 200, &pg)
        return pg
    }
    if pg := search("q=twoway"); pg.Total != 1 || pg.Items[0].ID != p.ID || pg.Items[0].Category.Name != "Twoway" {
        t.Errorf("search by category %+v", pg)
    }
    if pg := search(pf("category=%d", elec.ID)); pg.Total != 1 {
        t.Errorf("category filter %+v", pg)
    }
    if pg := search("category=none"); pg.Total != 1 || pg.Items[0].ID != other.ID {
        t.Errorf("uncategorised filter %+v", pg)
    }

    // Renaming an ancestor updates the index.
    stores.must("GET", pf("/api/v1/categories/%d", switches.ID), nil, 200, &c)
    stores.must("PATCH", pf("/api/v1/categories/%d", switches.ID), map[string]any{"version": c.Version, "name": "Toggles"}, 200, &c)
    if c.Path != "Electrical / Toggles" {
        t.Fatalf("renamed path %q", c.Path)
    }
    if pg := search("q=toggles"); pg.Total != 1 {
        t.Errorf("search after rename %d", pg.Total)
    }
    if pg := search("q=switches"); pg.Total != 0 {
        t.Errorf("old name still indexed")
    }

    // Deletion: refused with children or live parts; clears soft-deleted parts.
    stores.errCode("DELETE", pf("/api/v1/categories/%d", elec.ID), nil, 409)
    stores.errCode("DELETE", pf("/api/v1/categories/%d", twoway.ID), nil, 409)
    stores.must("DELETE", pf("/api/v1/parts/%d", p.ID), nil, 204, nil)
    stores.must("DELETE", pf("/api/v1/categories/%d", twoway.ID), nil, 204, nil)
    stores.must("POST", pf("/api/v1/parts/%d/restore", p.ID), nil, 200, &p)
    if p.Category != nil {
        t.Fatal("restored part still points at deleted category")
    }

    // History records the category on both sides.
    var ev struct {
        Items []struct {
            Action string `json:"action"`
        } `json:"items"`
    }
    reader.must("GET", pf("/api/v1/categories/%d/events", switches.ID), nil, 200, &ev)
    actions := map[string]bool{}
    for _, e := range ev.Items {
        actions[e.Action] = true
    }
    if !actions["category.created"] || !actions["category.updated"] {
        t.Errorf("category history %v", actions)
    }
    reader.must("GET", pf("/api/v1/parts/%d/events", other.ID), nil, 200, &ev)
    if ev.Items[0].Action != "part.updated" {
        t.Errorf("part history %+v", ev.Items)
    }

    // Stores holds the new permission.
    var role struct {
        Permissions []string `json:"permissions"`
    }
    admin := h.login("admin@example.com", "Admin")
    admin.must("GET", pf("/api/v1/roles/%d", h.roleID("Stores")), nil, 200, &role)
    found := false
    for _, p := range role.Permissions {
        found = found || p == "categories:manage"
    }
    if !found {
        t.Errorf("Stores lacks categories:manage: %v", role.Permissions)
    }
}

func TestEnsureSearchIndex(t *testing.T) {
    h := newHarness(t)
    c := h.login("stores@example.com", "Stores")
    c.must("POST", "/api/v1/parts", map[string]any{"name": "Capacitor"}, 201, nil)
    if _, err := h.svc.DB.W.Exec(`DELETE FROM parts_fts`); err != nil {
        t.Fatal(err)
    }
    if err := h.svc.EnsureSearchIndex(context.Background()); err != nil {
        t.Fatal(err)
    }
    var pg struct {
        Total int `json:"total"`
    }
    c.must("GET", "/api/v1/parts?q=capac", nil, 200, &pg)
    if pg.Total != 1 {
        t.Fatalf("index not rebuilt: %d", pg.Total)
    }
}
