// Package importer copies an InvenTree inventory into Drawered through the
// Drawered service layer. See SPEC.md section 18.
package importer

import (
    "context"
    "errors"
    "fmt"
    "net/url"
    "regexp"
    "sort"
    "strconv"
    "strings"

    "drawered/internal/decimal"
    "drawered/internal/inventree"
    "drawered/internal/service"
)

// Options control an import run.
type Options struct {
    DryRun          bool
    SkipInactive    bool
    NoStock         bool
    NoImages        bool
    DefaultCurrency string
    // Log receives progress messages; nil discards them.
    Log func(format string, args ...any)
}

// Counts tallies what happened to one kind of entity.
type Counts struct {
    Created int `json:"created"`
    Reused  int `json:"reused"`
    Skipped int `json:"skipped"`
    Failed  int `json:"failed"`
}

// Report summarises an import run.
type Report struct {
    DryRun       bool     `json:"dry_run"`
    Locations    Counts   `json:"locations"`
    Categories   Counts   `json:"categories"`
    Parts        Counts   `json:"parts"`
    Inactive     int      `json:"inactive_parts_skipped"`
    StockEntries int      `json:"stock_entries"`
    Images       int      `json:"images"`
    Links        int      `json:"links"`
    Warnings     []string `json:"warnings"`
}

func (r *Report) warn(format string, args ...any) {
    r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Source normalises an InvenTree base URL into the key stored in
// import_links, so differently written URLs for one server match.
func Source(baseURL string) string {
    u, err := url.Parse(strings.TrimSpace(baseURL))
    if err != nil || u.Host == "" {
        return "inventree:" + strings.TrimRight(baseURL, "/")
    }
    return "inventree:" + strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/")
}

type importer struct {
    svc    *service.Service
    client *inventree.Client
    opt    Options
    source string
    rep    *Report
    data   *inventree.Data

    // InvenTree pk to Drawered id. Dry runs use negative placeholders.
    locs map[int64]int64
    cats map[int64]int64
    // Whether the Drawered target is structural, by Drawered id.
    locStructural map[int64]bool
    catStructural map[int64]bool
    nextFake      int64
}

// Run fetches everything from InvenTree and imports it.
func Run(ctx context.Context, svc *service.Service, client *inventree.Client, opt Options) (*Report, error) {
    if opt.Log == nil {
        opt.Log = func(string, ...any) {}
    }
    if opt.DefaultCurrency == "" {
        opt.DefaultCurrency = svc.Cfg.DefaultCurrency
    }
    im := &importer{
        svc: svc, client: client, opt: opt, source: Source(client.BaseURL),
        rep:  &Report{DryRun: opt.DryRun, Warnings: []string{}},
        locs: map[int64]int64{}, cats: map[int64]int64{},
        locStructural: map[int64]bool{}, catStructural: map[int64]bool{},
    }
    ctx = service.WithActor(ctx, service.Actor{RequestID: "inventree-import"})

    opt.Log("Fetching from %s", client.BaseURL)
    data, err := client.FetchAll(ctx, !opt.NoStock, func(what string, n int) { opt.Log("  %d %s", n, what) })
    if err != nil {
        return nil, err
    }
    im.data = data

    opt.Log("Importing locations")
    if err := im.importLocations(ctx); err != nil {
        return im.rep, err
    }
    opt.Log("Importing categories")
    if err := im.importCategories(ctx); err != nil {
        return im.rep, err
    }
    opt.Log("Importing parts")
    if err := im.importParts(ctx); err != nil {
        return im.rep, err
    }
    return im.rep, nil
}

func (im *importer) fakeID() int64 {
    im.nextFake--
    return im.nextFake
}

// cleanName makes an InvenTree name acceptable as a Drawered tree name.
func cleanName(s string, max int) string {
    s = strings.TrimSpace(strings.ReplaceAll(s, "/", "-"))
    return truncate(s, max)
}

func truncate(s string, max int) string {
    r := []rune(s)
    if len(r) > max {
        return strings.TrimSpace(string(r[:max]))
    }
    return s
}

// treeOrder returns nodes ordered so parents come before children. Nodes
// whose parent is unknown are treated as roots.
func treeOrder[T any](nodes []T, pk func(T) int64, parent func(T) *int64) []T {
    byParent := map[int64][]T{}
    known := map[int64]bool{}
    for _, n := range nodes {
        known[pk(n)] = true
    }
    var roots []T
    for _, n := range nodes {
        if p := parent(n); p != nil && known[*p] {
            byParent[*p] = append(byParent[*p], n)
        } else {
            roots = append(roots, n)
        }
    }
    out := make([]T, 0, len(nodes))
    var walk func([]T)
    walk = func(level []T) {
        sort.SliceStable(level, func(i, j int) bool { return pk(level[i]) < pk(level[j]) })
        for _, n := range level {
            out = append(out, n)
            walk(byParent[pk(n)])
        }
    }
    walk(roots)
    return out
}

func (im *importer) parentTarget(m map[int64]int64, parent *int64) *int64 {
    if parent == nil {
        return nil
    }
    if t, ok := m[*parent]; ok {
        return &t
    }
    return nil
}

func (im *importer) importLocations(ctx context.Context) error {
    ordered := treeOrder(im.data.Locations,
        func(l inventree.Location) int64 { return l.PK },
        func(l inventree.Location) *int64 { return l.Parent })
    for _, l := range ordered {
        if target, ok, err := im.svc.ImportedTarget(ctx, im.source, service.ImportLocation, l.PK); err != nil {
            return err
        } else if ok {
            loc, err := im.svc.GetLocation(ctx, target)
            if err != nil {
                return err
            }
            im.locs[l.PK], im.locStructural[target] = target, loc.Structural
            im.rep.Locations.Skipped++
            continue
        }
        name := cleanName(string(l.Name), 100)
        if name == "" {
            name = fmt.Sprintf("Location %d", l.PK)
        }
        parent := im.parentTarget(im.locs, l.Parent)
        if parent == nil || *parent > 0 {
            existing, err := im.svc.FindChildLocation(ctx, parent, name)
            if err != nil {
                return err
            }
            if existing != nil {
                im.locs[l.PK], im.locStructural[existing.ID] = existing.ID, existing.Structural
                im.rep.Locations.Reused++
                if !im.opt.DryRun {
                    if err := im.svc.RecordImport(ctx, im.source, service.ImportLocation, l.PK, existing.ID); err != nil {
                        return err
                    }
                }
                continue
            }
        }
        if im.opt.DryRun {
            id := im.fakeID()
            im.locs[l.PK], im.locStructural[id] = id, l.Structural
            im.rep.Locations.Created++
            continue
        }
        created, err := im.svc.CreateLocation(ctx, service.LocationInput{
            ParentID: parent, Name: name, Description: truncate(string(l.Description), 10000), Structural: l.Structural,
        })
        if err != nil {
            im.rep.Locations.Failed++
            im.rep.warn("location %d (%s): %v", l.PK, name, err)
            continue
        }
        im.locs[l.PK], im.locStructural[created.ID] = created.ID, created.Structural
        im.rep.Locations.Created++
        if err := im.svc.RecordImport(ctx, im.source, service.ImportLocation, l.PK, created.ID); err != nil {
            return err
        }
    }
    return nil
}

var tablerIcon = regexp.MustCompile(`^(?:ti:([a-z0-9-]+)(?::[a-z]+)?|(?:ti\s+)?ti-([a-z0-9-]+))$`)

// mapIcon converts an InvenTree icon reference to a Tabler icon name.
func mapIcon(icon string) (string, bool) {
    icon = strings.TrimSpace(icon)
    if icon == "" {
        return "", true
    }
    m := tablerIcon.FindStringSubmatch(icon)
    if m == nil {
        return "", false
    }
    if m[1] != "" {
        return m[1], true
    }
    return m[2], true
}

func (im *importer) importCategories(ctx context.Context) error {
    ordered := treeOrder(im.data.Categories,
        func(c inventree.Category) int64 { return c.PK },
        func(c inventree.Category) *int64 { return c.Parent })
    for _, c := range ordered {
        if target, ok, err := im.svc.ImportedTarget(ctx, im.source, service.ImportCategory, c.PK); err != nil {
            return err
        } else if ok {
            cat, err := im.svc.GetCategory(ctx, target)
            if err != nil {
                return err
            }
            im.cats[c.PK], im.catStructural[target] = target, cat.Structural
            im.rep.Categories.Skipped++
            continue
        }
        name := cleanName(string(c.Name), 100)
        if name == "" {
            name = fmt.Sprintf("Category %d", c.PK)
        }
        parent := im.parentTarget(im.cats, c.Parent)
        if parent == nil || *parent > 0 {
            existing, err := im.svc.FindChildCategory(ctx, parent, name)
            if err != nil {
                return err
            }
            if existing != nil {
                im.cats[c.PK], im.catStructural[existing.ID] = existing.ID, existing.Structural
                im.rep.Categories.Reused++
                if !im.opt.DryRun {
                    if err := im.svc.RecordImport(ctx, im.source, service.ImportCategory, c.PK, existing.ID); err != nil {
                        return err
                    }
                }
                continue
            }
        }
        var icon *string
        if name, ok := mapIcon(string(c.Icon)); !ok {
            im.rep.warn("category %d (%s): icon %q is not a Tabler icon; dropped", c.PK, c.Name, c.Icon)
        } else if name != "" {
            icon = &name
        }
        if im.opt.DryRun {
            id := im.fakeID()
            im.cats[c.PK], im.catStructural[id] = id, c.Structural
            im.rep.Categories.Created++
            continue
        }
        created, err := im.svc.CreateCategory(ctx, service.CategoryInput{
            ParentID: parent, Name: name, Description: truncate(string(c.Description), 10000),
            Icon: icon, Structural: c.Structural,
        })
        if err != nil {
            im.rep.Categories.Failed++
            im.rep.warn("category %d (%s): %v", c.PK, name, err)
            continue
        }
        im.cats[c.PK], im.catStructural[created.ID] = created.ID, created.Structural
        im.rep.Categories.Created++
        if err := im.svc.RecordImport(ctx, im.source, service.ImportCategory, c.PK, created.ID); err != nil {
            return err
        }
    }
    return nil
}

// quantity converts an InvenTree quantity to Drawered's fixed point,
// rounding to 3 decimal places. It reports whether rounding happened.
func quantity(n inventree.Number) (decimal.Milli, bool, error) {
    if n == "" {
        return 0, false, nil
    }
    q, err := decimal.ParseMilli(string(n))
    if err == nil {
        return q, false, nil
    }
    if !errors.Is(err, decimal.ErrPrecision) {
        return 0, false, err
    }
    q, err = decimal.ParseMilli(strconv.FormatFloat(n.Float(), 'f', 3, 64))
    return q, true, err
}

// money converts an InvenTree price to Drawered's fixed point, rounding to
// 6 decimal places.
func money(n inventree.Number) (decimal.Micro, bool, error) {
    m, err := decimal.ParseMicro(string(n))
    if err == nil {
        return m, false, nil
    }
    if !errors.Is(err, decimal.ErrPrecision) {
        return 0, false, err
    }
    m, err = decimal.ParseMicro(strconv.FormatFloat(n.Float(), 'f', 6, 64))
    return m, true, err
}

// countUnits are InvenTree units that mean "a number of items".
var countUnits = map[string]bool{"": true, "pcs": true, "pc": true, "piece": true, "pieces": true, "each": true, "ea": true, "item": true, "items": true}

// chooseSupplier picks one supplier part as SPEC.md section 18.3 describes.
func chooseSupplier(p inventree.Part, sps []inventree.SupplierPart) *inventree.SupplierPart {
    if len(sps) == 0 {
        return nil
    }
    sort.Slice(sps, func(i, j int) bool { return sps[i].PK < sps[j].PK })
    for i := range sps {
        if sps[i].Primary {
            return &sps[i]
        }
    }
    if p.DefaultSupplier != nil {
        for i := range sps {
            if sps[i].PK == *p.DefaultSupplier {
                return &sps[i]
            }
        }
    }
    for i := range sps {
        if sps[i].IsActive() {
            return &sps[i]
        }
    }
    return &sps[0]
}

type partLink struct {
    url, description string
}

func (im *importer) importParts(ctx context.Context) error {
    companies := map[int64]string{}
    for _, c := range im.data.Companies {
        companies[c.PK] = strings.TrimSpace(string(c.Name))
    }
    companyName := func(pk int64, detail *inventree.CompanyBrief) string {
        if detail != nil && strings.TrimSpace(string(detail.Name)) != "" {
            return strings.TrimSpace(string(detail.Name))
        }
        return companies[pk]
    }
    suppliers := map[int64][]inventree.SupplierPart{}
    for _, sp := range im.data.SupplierParts {
        suppliers[sp.Part] = append(suppliers[sp.Part], sp)
    }
    mfrParts := map[int64]inventree.ManufacturerPart{}
    mfrByPart := map[int64][]inventree.ManufacturerPart{}
    for _, mp := range im.data.ManufacturerParts {
        mfrParts[mp.PK] = mp
        mfrByPart[mp.Part] = append(mfrByPart[mp.Part], mp)
    }
    breaks := map[int64][]inventree.PriceBreak{}
    for _, pb := range im.data.PriceBreaks {
        breaks[pb.Part] = append(breaks[pb.Part], pb)
    }

    // Sum stock per part and location.
    type key struct{ part, loc int64 }
    stock := map[key]decimal.Milli{}
    for _, si := range im.data.Stock {
        if si.Location == nil {
            im.rep.warn("stock item %d (part %d): no location; skipped", si.PK, si.Part)
            continue
        }
        q, rounded, err := quantity(si.Quantity)
        if err != nil {
            im.rep.warn("stock item %d: quantity %q: %v; skipped", si.PK, si.Quantity, err)
            continue
        }
        if rounded {
            im.rep.warn("stock item %d: quantity %s rounded to %s", si.PK, si.Quantity, q)
        }
        if q > 0 {
            stock[key{si.Part, *si.Location}] += q
        }
    }
    stockByPart := map[int64][]service.InitialStock{}
    for k, q := range stock {
        target, ok := im.locs[k.loc]
        if !ok {
            im.rep.warn("stock of part %d at location %d: location was not imported; skipped", k.part, k.loc)
            continue
        }
        if im.locStructural[target] {
            im.rep.warn("stock of part %d at location %d: location is structural in Drawered; skipped", k.part, k.loc)
            continue
        }
        stockByPart[k.part] = append(stockByPart[k.part], service.InitialStock{LocationID: target, Quantity: q})
    }

    parts := append([]inventree.Part{}, im.data.Parts...)
    sort.Slice(parts, func(i, j int) bool { return parts[i].PK < parts[j].PK })
    for i, p := range parts {
        if i > 0 && i%100 == 0 {
            im.opt.Log("  %d of %d parts", i, len(parts))
        }
        if im.opt.SkipInactive && !p.IsActive() {
            im.rep.Inactive++
            continue
        }
        if _, ok, err := im.svc.ImportedTarget(ctx, im.source, service.ImportPart, p.PK); err != nil {
            return err
        } else if ok {
            im.rep.Parts.Skipped++
            continue
        }
        if err := im.importPart(ctx, p, companyName, suppliers[p.PK], mfrParts, mfrByPart[p.PK], breaks, stockByPart[p.PK]); err != nil {
            return err
        }
    }
    return nil
}

func (im *importer) importPart(ctx context.Context, p inventree.Part,
    companyName func(int64, *inventree.CompanyBrief) string,
    sps []inventree.SupplierPart, mfrParts map[int64]inventree.ManufacturerPart,
    ownMfrParts []inventree.ManufacturerPart, breaks map[int64][]inventree.PriceBreak,
    stock []service.InitialStock) error {

    name := truncate(strings.TrimSpace(string(p.Name)), 200)
    if name == "" {
        name = fmt.Sprintf("Part %d", p.PK)
    }
    in := service.PartInput{Name: name, Tags: []string{}}

    // Description, with InvenTree-only fields kept searchable.
    var desc []string
    if d := strings.TrimSpace(string(p.Description)); d != "" {
        desc = append(desc, d, "")
    }
    for _, f := range []struct{ label, v string }{
        {"IPN", string(p.IPN)}, {"Revision", string(p.Revision)}, {"Keywords", string(p.Keywords)},
    } {
        if v := strings.TrimSpace(f.v); v != "" {
            desc = append(desc, f.label+": "+v)
        }
    }
    in.Description = truncate(strings.TrimSpace(strings.Join(desc, "\n")), 50000)

    for _, t := range p.Tags {
        t = truncate(strings.TrimSpace(strings.ReplaceAll(t, ",", " ")), 50)
        if t != "" {
            in.Tags = append(in.Tags, t)
        }
    }

    if p.Category != nil {
        if target, ok := im.cats[*p.Category]; !ok {
            im.rep.warn("part %d (%s): category %d was not imported; left uncategorised", p.PK, name, *p.Category)
        } else if im.catStructural[target] {
            im.rep.warn("part %d (%s): category is structural in Drawered; left uncategorised", p.PK, name)
        } else {
            in.CategoryID = &target
        }
    }

    units := strings.TrimSpace(string(p.Units))
    in.UOM = truncate(units, 20)
    if in.UOM == "" {
        in.UOM = "pcs"
    }
    fractional := !countUnits[strings.ToLower(units)]

    if min, rounded, err := quantity(p.MinimumStock); err != nil {
        im.rep.warn("part %d: minimum stock %q: %v; ignored", p.PK, p.MinimumStock, err)
    } else if min > 0 {
        if rounded {
            im.rep.warn("part %d: minimum stock %s rounded to %s", p.PK, p.MinimumStock, min)
        }
        in.MinTotalQuantity = &min
        fractional = fractional || !min.IsWhole()
    }
    for _, s := range stock {
        fractional = fractional || !s.Quantity.IsWhole()
    }
    in.AllowFractional = fractional
    sort.Slice(stock, func(i, j int) bool { return stock[i].LocationID < stock[j].LocationID })
    in.Stock = stock

    var links []partLink
    if l := strings.TrimSpace(string(p.Link)); l != "" {
        links = append(links, partLink{l, "Product page"})
    }

    // Supplier, manufacturer and cost.
    var mp *inventree.ManufacturerPart
    if sp := chooseSupplier(p, sps); sp != nil {
        if sup := companyName(sp.Supplier, sp.SupplierDetail); sup != "" {
            s := truncate(sup, 100)
            in.Supplier = &s
            if l := strings.TrimSpace(string(sp.Link)); l != "" {
                links = append(links, partLink{l, s + " product page"})
            }
        }
        in.SupplierSKU = truncate(strings.TrimSpace(string(sp.SKU)), 100)
        if sp.ManufacturerPart != nil {
            if m, ok := mfrParts[*sp.ManufacturerPart]; ok {
                mp = &m
            }
        }
        if mp == nil && sp.ManufacturerPartDetail != nil {
            mp = sp.ManufacturerPartDetail
        }
        im.applyCost(&in, p, sp, breaks[sp.PK])
    }
    if mp == nil && len(ownMfrParts) > 0 {
        sort.Slice(ownMfrParts, func(i, j int) bool { return ownMfrParts[i].PK < ownMfrParts[j].PK })
        mp = &ownMfrParts[0]
    }
    if mp != nil {
        in.MPN = truncate(strings.TrimSpace(string(mp.MPN)), 100)
        if mfr := companyName(mp.Manufacturer, mp.ManufacturerDetail); mfr != "" {
            m := truncate(mfr, 100)
            in.Manufacturer = &m
            if l := strings.TrimSpace(string(mp.Link)); l != "" {
                links = append(links, partLink{l, m + " product page"})
            }
        }
    }

    if im.opt.DryRun {
        im.rep.Parts.Created++
        im.rep.StockEntries += len(in.Stock)
        im.rep.Links += len(links)
        if strings.TrimSpace(string(p.Image)) != "" && !im.opt.NoImages {
            im.rep.Images++
        }
        return nil
    }

    created, err := im.svc.CreatePart(ctx, in)
    if err != nil {
        if _, isDomain := service.AsError(err); !isDomain {
            return err
        }
        im.rep.Parts.Failed++
        im.rep.warn("part %d (%s): %v", p.PK, name, err)
        return nil
    }
    im.rep.Parts.Created++
    im.rep.StockEntries += len(in.Stock)
    if err := im.svc.RecordImport(ctx, im.source, service.ImportPart, p.PK, created.ID); err != nil {
        return err
    }

    seen := map[string]bool{}
    for _, l := range links {
        if seen[l.url] {
            continue
        }
        seen[l.url] = true
        if _, err := im.svc.AddLink(ctx, created.ID, service.LinkInput{URL: l.url, Description: l.description}); err != nil {
            im.rep.warn("part %d (%s): link %q: %v", p.PK, name, l.url, err)
            continue
        }
        im.rep.Links++
    }

    if img := strings.TrimSpace(string(p.Image)); img != "" && !im.opt.NoImages {
        if err := im.importImage(ctx, p.PK, created.ID, img); err != nil {
            im.rep.warn("part %d (%s): image: %v", p.PK, name, err)
        } else {
            im.rep.Images++
        }
    }
    return nil
}

func (im *importer) applyCost(in *service.PartInput, p inventree.Part, sp *inventree.SupplierPart, pbs []inventree.PriceBreak) {
    var best *inventree.PriceBreak
    for i := range pbs {
        if pbs[i].Price == "" {
            continue
        }
        if best == nil || pbs[i].Quantity.Float() < best.Quantity.Float() {
            best = &pbs[i]
        }
    }
    if best == nil {
        return
    }
    cost, rounded, err := money(best.Price)
    if err != nil || cost < 0 {
        im.rep.warn("part %d: price %q on supplier part %d: invalid; ignored", p.PK, best.Price, sp.PK)
        return
    }
    if rounded {
        im.rep.warn("part %d: price %s rounded to %s", p.PK, best.Price, cost)
    }
    currency := strings.ToUpper(strings.TrimSpace(string(best.Currency)))
    if currency == "" {
        currency = im.opt.DefaultCurrency
    }
    in.Cost, in.Currency = &cost, &currency
}

func (im *importer) importImage(ctx context.Context, sourcePK, partID int64, imageURL string) error {
    body, name, err := im.client.Download(ctx, imageURL)
    if err != nil {
        return err
    }
    defer body.Close()
    if name == "" || name == "." || name == "/" {
        name = fmt.Sprintf("inventree-part-%d", sourcePK)
    }
    _, err = im.svc.AddImage(ctx, partID, body, name, "")
    return err
}
