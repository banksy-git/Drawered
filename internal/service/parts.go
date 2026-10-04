package service

import (
    "context"
    "database/sql"
    "fmt"
    "regexp"
    "sort"
    "strings"

    "drawered/internal/db"
    "drawered/internal/decimal"
)

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// StockEntry is one location's holding of a part.
type StockEntry struct {
    LocationID      int64          `json:"location_id"`
    Path            string         `json:"path"`
    EffectiveColour *string        `json:"effective_colour"`
    Quantity        decimal.Milli  `json:"quantity"`
    MinQuantity     *decimal.Milli `json:"min_quantity"`
    Note            string         `json:"note"`
    Low             bool           `json:"low"`
}

// Image is a part image.
type Image struct {
    ID        int64  `json:"id"`
    FileID    int64  `json:"file_id"`
    Caption   string `json:"caption"`
    SortOrder int    `json:"sort_order"`
}

// Link is an external link on a part.
type Link struct {
    ID          int64  `json:"id"`
    URL         string `json:"url"`
    Description string `json:"description"`
    SortOrder   int    `json:"sort_order"`
}

// Document is an attached file on a part.
type Document struct {
    ID           int64  `json:"id"`
    FileID       int64  `json:"file_id"`
    Description  string `json:"description"`
    SortOrder    int    `json:"sort_order"`
    OriginalName string `json:"original_name"`
    MIME         string `json:"mime_type"`
    Size         int64  `json:"size"`
}

// Part is the full API representation of a part.
type Part struct {
    ID               int64          `json:"id"`
    Name             string         `json:"name"`
    Description      string         `json:"description"`
    Tags             []string       `json:"tags"`
    Category         *CategoryRef   `json:"category"`
    UOM              string         `json:"uom"`
    AllowFractional  bool           `json:"allow_fractional"`
    MPN              string         `json:"mpn"`
    Manufacturer     *Ref           `json:"manufacturer"`
    Supplier         *Ref           `json:"supplier"`
    SupplierSKU      string         `json:"supplier_sku"`
    Barcode          string         `json:"barcode"`
    Cost             *decimal.Micro `json:"cost"`
    Currency         *string        `json:"currency"`
    MinTotalQuantity *decimal.Milli `json:"min_total_quantity"`
    ThumbnailImageID *int64         `json:"thumbnail_image_id"`
    ThumbnailFileID  *int64         `json:"thumbnail_file_id"`
    Images           []Image        `json:"images"`
    Links            []Link         `json:"links"`
    Documents        []Document     `json:"documents"`
    Stock            []StockEntry   `json:"stock"`
    TotalQuantity    decimal.Milli  `json:"total_quantity"`
    TotalValue       *decimal.Micro `json:"total_value"`
    Low              bool           `json:"low"`
    Version          int64          `json:"version"`
    CreatedAt        string         `json:"created_at"`
    UpdatedAt        string         `json:"updated_at"`
    DeletedAt        *string        `json:"deleted_at"`
    Warnings         []string       `json:"warnings,omitempty"`
}

// PartSummary is a compact part representation for lists.
type PartSummary struct {
    ID              int64         `json:"id"`
    Name            string        `json:"name"`
    MPN             string        `json:"mpn"`
    Manufacturer    *string       `json:"manufacturer"`
    Tags            []string      `json:"tags"`
    Category        *CategoryRef  `json:"category"`
    UOM             string        `json:"uom"`
    TotalQuantity   decimal.Milli `json:"total_quantity"`
    ThumbnailFileID *int64        `json:"thumbnail_file_id"`
    Locations       []string      `json:"locations"`
    Low             bool          `json:"low"`
    UpdatedAt       string        `json:"updated_at"`
    DeletedAt       *string       `json:"deleted_at"`
}

// partFields are the directly editable columns of a part.
type partFields struct {
    Name             string
    Description      string
    Tags             []string
    CategoryID       *int64
    UOM              string
    AllowFractional  bool
    MPN              string
    Manufacturer     *string
    Supplier         *string
    SupplierSKU      string
    Barcode          string
    Cost             *decimal.Micro
    Currency         *string
    MinTotalQuantity *decimal.Milli
}

func normaliseTags(in []string) ([]string, error) {
    seen := map[string]bool{}
    out := []string{}
    for _, t := range in {
        t = strings.ToLower(strings.TrimSpace(t))
        if t == "" {
            continue
        }
        if err := checkLen("tag", t, 1, 50); err != nil {
            return nil, err
        }
        if strings.Contains(t, ",") {
            return nil, Invalid("invalid_tag", "tags must not contain commas")
        }
        if !seen[t] {
            seen[t] = true
            out = append(out, t)
        }
    }
    sort.Strings(out)
    return out, nil
}

func trimPtr(s *string) *string {
    if s == nil {
        return nil
    }
    v := strings.TrimSpace(*s)
    if v == "" {
        return nil
    }
    return &v
}

func (f *partFields) validate(defaultCurrency string) error {
    f.Name = strings.TrimSpace(f.Name)
    if err := checkLen("name", f.Name, 1, 200); err != nil {
        return err
    }
    if err := checkLen("description", f.Description, 0, 50000); err != nil {
        return err
    }
    var err error
    if f.Tags, err = normaliseTags(f.Tags); err != nil {
        return err
    }
    f.UOM = strings.TrimSpace(f.UOM)
    if f.UOM == "" {
        f.UOM = "pcs"
    }
    if err := checkLen("uom", f.UOM, 1, 20); err != nil {
        return err
    }
    f.MPN = strings.TrimSpace(f.MPN)
    f.SupplierSKU = strings.TrimSpace(f.SupplierSKU)
    f.Barcode = strings.TrimSpace(f.Barcode)
    for _, c := range []struct {
        name string
        v    string
    }{{"mpn", f.MPN}, {"supplier_sku", f.SupplierSKU}, {"barcode", f.Barcode}} {
        if err := checkLen(c.name, c.v, 0, 100); err != nil {
            return err
        }
    }
    f.Manufacturer = trimPtr(f.Manufacturer)
    f.Supplier = trimPtr(f.Supplier)
    for _, c := range []*string{f.Manufacturer, f.Supplier} {
        if c != nil {
            if err := checkLen("name", *c, 1, 100); err != nil {
                return err
            }
        }
    }
    if f.Cost != nil {
        if *f.Cost < 0 {
            return Invalid("invalid_cost", "cost must not be negative")
        }
        if f.Currency == nil || *f.Currency == "" {
            c := defaultCurrency
            f.Currency = &c
        }
    }
    if f.Currency != nil {
        c := strings.ToUpper(strings.TrimSpace(*f.Currency))
        if c == "" {
            f.Currency = nil
        } else if !currencyRe.MatchString(c) {
            return Invalid("invalid_currency", "currency must be an ISO 4217 code")
        } else {
            f.Currency = &c
        }
    }
    if f.Cost == nil {
        f.Currency = nil
    }
    if f.MinTotalQuantity != nil {
        if err := checkQuantity(*f.MinTotalQuantity, f.AllowFractional, true); err != nil {
            return err
        }
    }
    return nil
}

// checkQuantity validates a quantity against a part's fractional setting.
func checkQuantity(q decimal.Milli, allowFractional, allowZero bool) error {
    if q < 0 || (q == 0 && !allowZero) {
        return Invalid("invalid_quantity", "quantity must be greater than zero")
    }
    if !allowFractional && !q.IsWhole() {
        return Invalid("fractional_not_allowed", "this part only allows whole quantities")
    }
    return nil
}

// resolveNamed finds or creates a manufacturer or supplier by name.
func (s *Service) resolveNamed(ctx context.Context, tx *sql.Tx, table string, name *string) (*int64, error) {
    if name == nil {
        return nil, nil
    }
    var id int64
    err := tx.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE name = ? COLLATE NOCASE`, *name).Scan(&id)
    if err == nil {
        return &id, nil
    }
    if err != sql.ErrNoRows {
        return nil, err
    }
    res, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (name, created_at) VALUES (?, ?)`, *name, db.Now())
    if err != nil {
        return nil, err
    }
    id, _ = res.LastInsertId()
    kind := strings.TrimSuffix(table, "s")
    if err := s.event(ctx, tx, kind+".created", Subject{kind, id}, map[string]any{kind: Ref{id, *name}}); err != nil {
        return nil, err
    }
    return &id, nil
}

func setTags(ctx context.Context, tx *sql.Tx, partID int64, tags []string) error {
    if _, err := tx.ExecContext(ctx, `DELETE FROM part_tags WHERE part_id = ?`, partID); err != nil {
        return err
    }
    for _, t := range tags {
        if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags (name) VALUES (?)`, t); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `INSERT INTO part_tags (part_id, tag_id) SELECT ?, id FROM tags WHERE name = ?`, partID, t); err != nil {
            return err
        }
    }
    return pruneTags(ctx, tx)
}

func pruneTags(ctx context.Context, tx *sql.Tx) error {
    _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM part_tags)`)
    return err
}

func (s *Service) barcodeWarnings(ctx context.Context, q db.Querier, id int64, barcode string) ([]string, error) {
    if barcode == "" {
        return nil, nil
    }
    var n int
    if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM parts WHERE barcode = ? COLLATE NOCASE AND id != ? AND deleted_at IS NULL`,
        barcode, id).Scan(&n); err != nil {
        return nil, err
    }
    if n > 0 {
        return []string{"duplicate_barcode"}, nil
    }
    return nil, nil
}

// InitialStock is a stock entry supplied when creating a part.
type InitialStock struct {
    LocationID int64         `json:"location_id"`
    Quantity   decimal.Milli `json:"quantity"`
}

// PartInput holds fields for creating a part.
type PartInput struct {
    Name             string         `json:"name"`
    Description      string         `json:"description"`
    Tags             []string       `json:"tags"`
    CategoryID       *int64         `json:"category_id"`
    UOM              string         `json:"uom"`
    AllowFractional  bool           `json:"allow_fractional"`
    MPN              string         `json:"mpn"`
    Manufacturer     *string        `json:"manufacturer"`
    Supplier         *string        `json:"supplier"`
    SupplierSKU      string         `json:"supplier_sku"`
    Barcode          string         `json:"barcode"`
    Cost             *decimal.Micro `json:"cost"`
    Currency         *string        `json:"currency"`
    MinTotalQuantity *decimal.Milli `json:"min_total_quantity"`
    Stock            []InitialStock `json:"stock"`
}

func (s *Service) insertPart(ctx context.Context, tx *sql.Tx, f *partFields) (int64, error) {
    mfr, err := s.resolveNamed(ctx, tx, "manufacturers", f.Manufacturer)
    if err != nil {
        return 0, err
    }
    sup, err := s.resolveNamed(ctx, tx, "suppliers", f.Supplier)
    if err != nil {
        return 0, err
    }
    now := db.Now()
    res, err := tx.ExecContext(ctx, `INSERT INTO parts (name, description, category_id, uom, allow_fractional, mpn,
            manufacturer_id, supplier_id, supplier_sku, barcode, cost_micros, currency, min_total_quantity_milli,
            created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        f.Name, f.Description, f.CategoryID, f.UOM, f.AllowFractional, f.MPN, mfr, sup, f.SupplierSKU, f.Barcode,
        f.Cost, f.Currency, f.MinTotalQuantity, now, now)
    if err != nil {
        return 0, err
    }
    id, _ := res.LastInsertId()
    return id, setTags(ctx, tx, id, f.Tags)
}

// CreatePart creates a part, optionally with initial stock.
func (s *Service) CreatePart(ctx context.Context, in PartInput) (*Part, error) {
    f := partFields{
        Name: in.Name, Description: in.Description, Tags: in.Tags, CategoryID: in.CategoryID, UOM: in.UOM,
        AllowFractional: in.AllowFractional,
        MPN:             in.MPN, Manufacturer: in.Manufacturer, Supplier: in.Supplier, SupplierSKU: in.SupplierSKU,
        Barcode: in.Barcode, Cost: in.Cost, Currency: in.Currency, MinTotalQuantity: in.MinTotalQuantity,
    }
    if err := f.validate(s.Cfg.DefaultCurrency); err != nil {
        return nil, err
    }
    seenLoc := map[int64]bool{}
    for _, st := range in.Stock {
        if err := checkQuantity(st.Quantity, f.AllowFractional, true); err != nil {
            return nil, err
        }
        if seenLoc[st.LocationID] {
            return nil, Invalid("duplicate_location", "each location may appear only once")
        }
        seenLoc[st.LocationID] = true
    }
    var id int64
    var warnings []string
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cix, err := loadCategories(ctx, tx)
        if err != nil {
            return err
        }
        if err := checkPartCategory(cix, f.CategoryID); err != nil {
            return err
        }
        if id, err = s.insertPart(ctx, tx, &f); err != nil {
            return err
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        related := []Subject{}
        stockData := []map[string]any{}
        for _, st := range in.Stock {
            if err := checkStockLocation(ix, st.LocationID); err != nil {
                return err
            }
            if _, err := tx.ExecContext(ctx, `INSERT INTO stock (part_id, location_id, quantity_milli) VALUES (?, ?, ?)`,
                id, st.LocationID, st.Quantity); err != nil {
                return err
            }
            related = append(related, Subject{"location", st.LocationID})
            stockData = append(stockData, map[string]any{
                "location": Ref{st.LocationID, ix.byID[st.LocationID].Name},
                "path":     ix.path(st.LocationID), "quantity": st.Quantity,
            })
        }
        if err := reindexPart(ctx, tx, ix, id); err != nil {
            return err
        }
        if warnings, err = s.barcodeWarnings(ctx, tx, id, f.Barcode); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.created", Subject{"part", id}, map[string]any{
            "part": Ref{id, f.Name}, "uom": f.UOM, "stock": stockData, "category": categoryPath(cix, f.CategoryID),
        }, related...)
    })
    if err != nil {
        return nil, err
    }
    p, err := s.GetPart(ctx, id)
    if p != nil {
        p.Warnings = warnings
    }
    return p, err
}

func checkStockLocation(ix *locIndex, id int64) error {
    n, ok := ix.byID[id]
    if !ok {
        return Invalid("invalid_location", "location does not exist")
    }
    if n.Structural {
        return Conflict("structural_location", "%s is structural and cannot hold stock", ix.path(id))
    }
    return nil
}

type partRow struct {
    partFields
    ID               int64
    ManufacturerID   *int64
    SupplierID       *int64
    ThumbnailImageID *int64
    Version          int64
    CreatedAt        string
    UpdatedAt        string
    DeletedAt        *string
    manufacturerName *string
    supplierName     *string
}

func loadPartRow(ctx context.Context, q db.Querier, id int64) (*partRow, error) {
    r := &partRow{ID: id}
    var mfrID, supID, thumb, cost, minTotal, category sql.NullInt64
    var mfr, sup, currency, deleted sql.NullString
    err := q.QueryRowContext(ctx, `SELECT p.name, p.description, p.uom, p.allow_fractional, p.mpn,
            p.manufacturer_id, m.name, p.supplier_id, su.name, p.supplier_sku, p.barcode, p.cost_micros, p.currency,
            p.min_total_quantity_milli, p.thumbnail_image_id, p.version, p.created_at, p.updated_at, p.deleted_at,
            p.category_id
        FROM parts p
        LEFT JOIN manufacturers m ON m.id = p.manufacturer_id
        LEFT JOIN suppliers su ON su.id = p.supplier_id
        WHERE p.id = ?`, id).Scan(&r.Name, &r.Description, &r.UOM, &r.AllowFractional, &r.MPN,
        &mfrID, &mfr, &supID, &sup, &r.SupplierSKU, &r.Barcode, &cost, &currency,
        &minTotal, &thumb, &r.Version, &r.CreatedAt, &r.UpdatedAt, &deleted, &category)
    if err == sql.ErrNoRows {
        return nil, NotFound("part")
    }
    if err != nil {
        return nil, err
    }
    r.ManufacturerID, r.SupplierID, r.ThumbnailImageID = nullInt(mfrID), nullInt(supID), nullInt(thumb)
    r.CategoryID = nullInt(category)
    r.manufacturerName, r.supplierName = nullStr(mfr), nullStr(sup)
    r.Manufacturer, r.Supplier = r.manufacturerName, r.supplierName
    r.Currency, r.DeletedAt = nullStr(currency), nullStr(deleted)
    if cost.Valid {
        c := decimal.Micro(cost.Int64)
        r.Cost = &c
    }
    if minTotal.Valid {
        m := decimal.Milli(minTotal.Int64)
        r.MinTotalQuantity = &m
    }
    r.Tags, err = queryStrings(ctx, q, `SELECT t.name FROM part_tags pt JOIN tags t ON t.id = pt.tag_id
        WHERE pt.part_id = ? ORDER BY t.name`, id)
    if r.Tags == nil {
        r.Tags = []string{}
    }
    return r, err
}

// GetPart returns the full representation of a part, including deleted parts.
func (s *Service) GetPart(ctx context.Context, id int64) (*Part, error) {
    return s.getPart(ctx, s.DB.R, id)
}

func (s *Service) getPart(ctx context.Context, q db.Querier, id int64) (*Part, error) {
    r, err := loadPartRow(ctx, q, id)
    if err != nil {
        return nil, err
    }
    p := &Part{
        ID: id, Name: r.Name, Description: r.Description, Tags: r.Tags, UOM: r.UOM,
        AllowFractional: r.AllowFractional, MPN: r.MPN, SupplierSKU: r.SupplierSKU, Barcode: r.Barcode,
        Cost: r.Cost, Currency: r.Currency, MinTotalQuantity: r.MinTotalQuantity,
        ThumbnailImageID: r.ThumbnailImageID, Version: r.Version, CreatedAt: r.CreatedAt,
        UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
        Images: []Image{}, Links: []Link{}, Documents: []Document{}, Stock: []StockEntry{},
    }
    if r.ManufacturerID != nil {
        p.Manufacturer = &Ref{*r.ManufacturerID, *r.manufacturerName}
    }
    cix, err := loadCategories(ctx, q)
    if err != nil {
        return nil, err
    }
    p.Category = cix.ref(r.CategoryID)
    if r.SupplierID != nil {
        p.Supplier = &Ref{*r.SupplierID, *r.supplierName}
    }

    rows, err := q.QueryContext(ctx, `SELECT id, file_id, caption, sort_order FROM part_images
        WHERE part_id = ? ORDER BY sort_order, id`, id)
    if err != nil {
        return nil, err
    }
    for rows.Next() {
        var i Image
        if err := rows.Scan(&i.ID, &i.FileID, &i.Caption, &i.SortOrder); err != nil {
            rows.Close()
            return nil, err
        }
        p.Images = append(p.Images, i)
    }
    rows.Close()
    for _, i := range p.Images {
        if p.ThumbnailImageID != nil && i.ID == *p.ThumbnailImageID {
            fid := i.FileID
            p.ThumbnailFileID = &fid
        }
    }
    if p.ThumbnailFileID == nil && len(p.Images) > 0 {
        fid := p.Images[0].FileID
        p.ThumbnailFileID = &fid
    }

    rows, err = q.QueryContext(ctx, `SELECT id, url, description, sort_order FROM part_links
        WHERE part_id = ? ORDER BY sort_order, id`, id)
    if err != nil {
        return nil, err
    }
    for rows.Next() {
        var l Link
        if err := rows.Scan(&l.ID, &l.URL, &l.Description, &l.SortOrder); err != nil {
            rows.Close()
            return nil, err
        }
        p.Links = append(p.Links, l)
    }
    rows.Close()

    rows, err = q.QueryContext(ctx, `SELECT d.id, d.file_id, d.description, d.sort_order, f.original_name, f.mime_type, f.size
        FROM part_documents d JOIN files f ON f.id = d.file_id
        WHERE d.part_id = ? ORDER BY d.sort_order, d.id`, id)
    if err != nil {
        return nil, err
    }
    for rows.Next() {
        var d Document
        if err := rows.Scan(&d.ID, &d.FileID, &d.Description, &d.SortOrder, &d.OriginalName, &d.MIME, &d.Size); err != nil {
            rows.Close()
            return nil, err
        }
        p.Documents = append(p.Documents, d)
    }
    rows.Close()

    ix, err := loadLocations(ctx, q)
    if err != nil {
        return nil, err
    }
    rows, err = q.QueryContext(ctx, `SELECT location_id, quantity_milli, min_quantity_milli, note FROM stock WHERE part_id = ?`, id)
    if err != nil {
        return nil, err
    }
    for rows.Next() {
        var e StockEntry
        var min sql.NullInt64
        if err := rows.Scan(&e.LocationID, &e.Quantity, &min, &e.Note); err != nil {
            rows.Close()
            return nil, err
        }
        if min.Valid {
            m := decimal.Milli(min.Int64)
            e.MinQuantity = &m
            e.Low = e.Quantity < m
        }
        e.Path = ix.path(e.LocationID)
        e.EffectiveColour = ix.effectiveColour(e.LocationID)
        p.Stock = append(p.Stock, e)
        p.TotalQuantity += e.Quantity
        p.Low = p.Low || e.Low
    }
    rows.Close()
    sort.Slice(p.Stock, func(i, j int) bool { return strings.ToLower(p.Stock[i].Path) < strings.ToLower(p.Stock[j].Path) })
    if p.MinTotalQuantity != nil && p.TotalQuantity < *p.MinTotalQuantity {
        p.Low = true
    }
    if p.Cost != nil {
        v := decimal.MulMilli(*p.Cost, p.TotalQuantity)
        p.TotalValue = &v
    }
    return p, nil
}

// UpdatePart applies a partial update to a part's details and specification.
func (s *Service) UpdatePart(ctx context.Context, id int64, p Patch) (*Part, error) {
    version, err := p.Version()
    if err != nil {
        return nil, err
    }
    var warnings []string
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        cur, err := loadPartRow(ctx, tx, id)
        if err != nil {
            return err
        }
        if cur.DeletedAt != nil {
            return Conflict("part_deleted", "part is deleted")
        }
        if cur.Version != version {
            return StaleVersion()
        }
        next := cur.partFields
        next.Tags = append([]string{}, cur.Tags...)
        fields := []struct {
            key string
            dst any
        }{
            {"name", &next.Name}, {"description", &next.Description}, {"tags", &next.Tags},
            {"uom", &next.UOM}, {"allow_fractional", &next.AllowFractional}, {"mpn", &next.MPN},
            {"manufacturer", &next.Manufacturer}, {"supplier", &next.Supplier},
            {"supplier_sku", &next.SupplierSKU}, {"barcode", &next.Barcode}, {"cost", &next.Cost},
            {"currency", &next.Currency}, {"min_total_quantity", &next.MinTotalQuantity},
            {"category_id", &next.CategoryID},
        }
        for _, f := range fields {
            if p.IsNull(f.key) {
                switch d := f.dst.(type) {
                case **string:
                    *d = nil
                case **decimal.Micro:
                    *d = nil
                case **decimal.Milli:
                    *d = nil
                case **int64:
                    *d = nil
                default:
                    return Invalid("invalid_field", "%s must not be null", f.key)
                }
                continue
            }
            if _, err := p.Get(f.key, f.dst); err != nil {
                return err
            }
        }
        if err := next.validate(s.Cfg.DefaultCurrency); err != nil {
            return err
        }
        if cur.AllowFractional && !next.AllowFractional {
            var n int
            if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock WHERE part_id = ?
                    AND (quantity_milli % 1000 != 0 OR IFNULL(min_quantity_milli, 0) % 1000 != 0)`, id).Scan(&n); err != nil {
                return err
            }
            if n > 0 {
                return Conflict("fractional_stock", "some stock quantities are fractional; make them whole first")
            }
        }

        ch := changes{}
        ch.add("name", cur.Name, next.Name)
        ch.add("description", cur.Description, next.Description)
        ch.add("tags", strings.Join(cur.Tags, ", "), strings.Join(next.Tags, ", "))
        cix, err := loadCategories(ctx, tx)
        if err != nil {
            return err
        }
        if !sameParent(cur.CategoryID, next.CategoryID) {
            if err := checkPartCategory(cix, next.CategoryID); err != nil {
                return err
            }
            ch["category"] = Change{From: categoryPath(cix, cur.CategoryID), To: categoryPath(cix, next.CategoryID)}
        }
        ch.add("uom", cur.UOM, next.UOM)
        ch.add("allow_fractional", cur.AllowFractional, next.AllowFractional)
        ch.add("mpn", cur.MPN, next.MPN)
        ch.add("manufacturer", derefOr(cur.Manufacturer), derefOr(next.Manufacturer))
        ch.add("supplier", derefOr(cur.Supplier), derefOr(next.Supplier))
        ch.add("supplier_sku", cur.SupplierSKU, next.SupplierSKU)
        ch.add("barcode", cur.Barcode, next.Barcode)
        ch.add("cost", fmtMicro(cur.Cost), fmtMicro(next.Cost))
        ch.add("currency", derefOr(cur.Currency), derefOr(next.Currency))
        ch.add("min_total_quantity", fmtMilli(cur.MinTotalQuantity), fmtMilli(next.MinTotalQuantity))
        if _, ok := ch["description"]; ok {
            // Long text is noisy in history; record that it changed, not the text.
            ch["description"] = Change{From: "(previous)", To: "(updated)"}
        }
        if len(ch) == 0 {
            return nil
        }

        mfr, err := s.resolveNamed(ctx, tx, "manufacturers", next.Manufacturer)
        if err != nil {
            return err
        }
        sup, err := s.resolveNamed(ctx, tx, "suppliers", next.Supplier)
        if err != nil {
            return err
        }
        res, err := tx.ExecContext(ctx, `UPDATE parts SET name = ?, description = ?, uom = ?, allow_fractional = ?, mpn = ?,
                manufacturer_id = ?, supplier_id = ?, supplier_sku = ?, barcode = ?, cost_micros = ?, currency = ?,
                min_total_quantity_milli = ?, category_id = ?, version = version + 1, updated_at = ?
            WHERE id = ? AND version = ?`,
            next.Name, next.Description, next.UOM, next.AllowFractional, next.MPN, mfr, sup, next.SupplierSKU,
            next.Barcode, next.Cost, next.Currency, next.MinTotalQuantity, next.CategoryID, db.Now(), id, version)
        if err != nil {
            return err
        }
        if n, _ := res.RowsAffected(); n == 0 {
            return StaleVersion()
        }
        if _, ok := ch["tags"]; ok {
            if err := setTags(ctx, tx, id, next.Tags); err != nil {
                return err
            }
        }
        if err := s.reindexParts(ctx, tx, []int64{id}); err != nil {
            return err
        }
        if _, ok := ch["barcode"]; ok {
            if warnings, err = s.barcodeWarnings(ctx, tx, id, next.Barcode); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "part.updated", Subject{"part", id}, map[string]any{
            "part": Ref{id, next.Name}, "changes": ch,
        })
    })
    if err != nil {
        return nil, err
    }
    part, err := s.GetPart(ctx, id)
    if part != nil {
        part.Warnings = warnings
    }
    return part, err
}

func categoryPath(ix *catIndex, id *int64) string {
    if id == nil {
        return ""
    }
    return ix.path(*id)
}

func fmtMicro(v *decimal.Micro) string {
    if v == nil {
        return ""
    }
    return v.String()
}

func fmtMilli(v *decimal.Milli) string {
    if v == nil {
        return ""
    }
    return v.String()
}

// DeletePart soft-deletes a part. Parts holding stock need force.
func (s *Service) DeletePart(ctx context.Context, id int64, force bool) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        p, err := s.getPart(ctx, tx, id)
        if err != nil {
            return err
        }
        if p.DeletedAt != nil {
            return Conflict("part_deleted", "part is already deleted")
        }
        if p.TotalQuantity > 0 && !force {
            return &Error{Status: 409, Code: "part_has_stock", Message: "part still has stock; delete with force to proceed",
                Details: map[string]any{"total_quantity": p.TotalQuantity}}
        }
        if _, err := tx.ExecContext(ctx, `UPDATE parts SET deleted_at = ?, version = version + 1 WHERE id = ?`, db.Now(), id); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM parts_fts WHERE rowid = ?`, id); err != nil {
            return err
        }
        stock := []map[string]any{}
        for _, e := range p.Stock {
            stock = append(stock, map[string]any{"location_id": e.LocationID, "path": e.Path, "quantity": e.Quantity})
        }
        return s.event(ctx, tx, "part.deleted", Subject{"part", id}, map[string]any{
            "part": Ref{id, p.Name}, "stock": stock, "uom": p.UOM,
        })
    })
}

// RestorePart undoes a soft delete.
func (s *Service) RestorePart(ctx context.Context, id int64) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        r, err := loadPartRow(ctx, tx, id)
        if err != nil {
            return err
        }
        if r.DeletedAt == nil {
            return Conflict("part_not_deleted", "part is not deleted")
        }
        if _, err := tx.ExecContext(ctx, `UPDATE parts SET deleted_at = NULL, version = version + 1, updated_at = ? WHERE id = ?`,
            db.Now(), id); err != nil {
            return err
        }
        if err := s.reindexParts(ctx, tx, []int64{id}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.restored", Subject{"part", id}, map[string]any{"part": Ref{id, r.Name}})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, id)
}

// PurgePart permanently removes a soft-deleted part. Its files become
// unreferenced and are removed by the next sweep.
func (s *Service) PurgePart(ctx context.Context, id int64) error {
    return s.DB.Tx(ctx, func(tx *sql.Tx) error {
        r, err := loadPartRow(ctx, tx, id)
        if err != nil {
            return err
        }
        if r.DeletedAt == nil {
            return Conflict("part_not_deleted", "only deleted parts can be purged")
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM parts WHERE id = ?`, id); err != nil {
            return err
        }
        if err := pruneTags(ctx, tx); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.purged", Subject{"part", id}, map[string]any{"part": Ref{id, r.Name}})
    })
}

// DuplicatePart copies a part's details, specification, tags, links and
// (optionally) images into a new part. Stock and documents are not copied.
func (s *Service) DuplicatePart(ctx context.Context, id int64, withImages bool) (*Part, error) {
    var newID int64
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        r, err := loadPartRow(ctx, tx, id)
        if err != nil {
            return err
        }
        f := r.partFields
        f.Name = truncateRunes(f.Name+" (copy)", 200)
        if newID, err = s.insertPart(ctx, tx, &f); err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `INSERT INTO part_links (part_id, url, description, sort_order)
                SELECT ?, url, description, sort_order FROM part_links WHERE part_id = ?`, newID, id); err != nil {
            return err
        }
        if withImages {
            if _, err := tx.ExecContext(ctx, `INSERT INTO part_images (part_id, file_id, caption, sort_order)
                    SELECT ?, file_id, caption, sort_order FROM part_images WHERE part_id = ? ORDER BY sort_order, id`, newID, id); err != nil {
                return err
            }
        }
        if err := s.reindexParts(ctx, tx, []int64{newID}); err != nil {
            return err
        }
        return s.event(ctx, tx, "part.created", Subject{"part", newID}, map[string]any{
            "part": Ref{newID, f.Name}, "uom": f.UOM, "duplicated_from": Ref{id, r.Name},
        }, Subject{"part", id})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, newID)
}

func truncateRunes(s string, n int) string {
    r := []rune(s)
    if len(r) > n {
        return string(r[:n])
    }
    return s
}

// PartFilter controls part listing and search.
type PartFilter struct {
    Q              string
    Tags           []string
    ManufacturerID *int64
    SupplierID     *int64
    LocationID     *int64
    CategoryID     *int64
    Uncategorised  bool
    Stock          string // "", in, out, low, none
    Deleted        bool
    Sort           string // relevance, name, updated, quantity
    Paging
}

const totalExpr = `(SELECT IFNULL(SUM(s.quantity_milli), 0) FROM stock s WHERE s.part_id = p.id)`

const lowExpr = `(EXISTS (SELECT 1 FROM stock s WHERE s.part_id = p.id AND s.min_quantity_milli IS NOT NULL
        AND s.quantity_milli < s.min_quantity_milli)
    OR (p.min_total_quantity_milli IS NOT NULL AND ` + totalExpr + ` < p.min_total_quantity_milli))`

// ListParts searches and filters parts.
func (s *Service) ListParts(ctx context.Context, f PartFilter) (*Page[PartSummary], error) {
    f.Paging = f.Paging.Normalise()
    var where []string
    var args []any
    from := `parts p`
    match := ftsQuery(f.Q)
    if match != "" {
        from += ` JOIN parts_fts ON parts_fts.rowid = p.id`
        where = append(where, `parts_fts MATCH ?`)
        args = append(args, match)
    }
    if f.Deleted {
        where = append(where, `p.deleted_at IS NOT NULL`)
    } else {
        where = append(where, `p.deleted_at IS NULL`)
    }
    tags, err := normaliseTags(f.Tags)
    if err != nil {
        return nil, err
    }
    for _, t := range tags {
        where = append(where, `p.id IN (SELECT pt.part_id FROM part_tags pt JOIN tags t ON t.id = pt.tag_id WHERE t.name = ?)`)
        args = append(args, t)
    }
    if f.ManufacturerID != nil {
        where = append(where, `p.manufacturer_id = ?`)
        args = append(args, *f.ManufacturerID)
    }
    if f.SupplierID != nil {
        where = append(where, `p.supplier_id = ?`)
        args = append(args, *f.SupplierID)
    }
    ix, err := loadLocations(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    if f.LocationID != nil {
        if _, ok := ix.byID[*f.LocationID]; !ok {
            return nil, Invalid("invalid_location", "location does not exist")
        }
        sub := ix.subtree(*f.LocationID)
        where = append(where, `p.id IN (SELECT part_id FROM stock WHERE location_id IN (`+placeholders(len(sub))+`))`)
        args = append(args, int64Args(sub)...)
    }
    cix, err := loadCategories(ctx, s.DB.R)
    if err != nil {
        return nil, err
    }
    if f.Uncategorised {
        where = append(where, `p.category_id IS NULL`)
    } else if f.CategoryID != nil {
        if _, ok := cix.byID[*f.CategoryID]; !ok {
            return nil, Invalid("invalid_category", "category does not exist")
        }
        sub := cix.subtree(*f.CategoryID)
        where = append(where, `p.category_id IN (`+placeholders(len(sub))+`)`)
        args = append(args, int64Args(sub)...)
    }
    switch f.Stock {
    case "":
    case "in":
        where = append(where, totalExpr+` > 0`)
    case "out":
        where = append(where, totalExpr+` = 0`)
    case "low":
        where = append(where, lowExpr)
    case "none":
        where = append(where, `NOT EXISTS (SELECT 1 FROM stock s WHERE s.part_id = p.id)`)
    default:
        return nil, Invalid("invalid_filter", "stock must be one of in, out, low, none")
    }
    order := `p.name COLLATE NOCASE, p.id`
    switch f.Sort {
    case "", "relevance":
        if match != "" {
            order = `bm25(parts_fts, 10, 10, 4, 3, 2, 1, 2, 3), p.name COLLATE NOCASE`
        }
    case "name":
    case "updated":
        order = `p.updated_at DESC, p.id DESC`
    case "quantity":
        order = `total DESC, p.name COLLATE NOCASE`
    default:
        return nil, Invalid("invalid_sort", "sort must be one of relevance, name, updated, quantity")
    }
    whereSQL := strings.Join(where, " AND ")
    out := &Page[PartSummary]{Items: []PartSummary{}}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+from+` WHERE `+whereSQL, args...).Scan(&out.Total); err != nil {
        return nil, fmt.Errorf("count parts: %w", err)
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT p.id, p.name, p.mpn, m.name, p.uom, `+totalExpr+` AS total, `+
        thumbnailExpr+`, `+lowExpr+`, p.updated_at, p.deleted_at, p.category_id
        FROM `+from+` LEFT JOIN manufacturers m ON m.id = p.manufacturer_id
        WHERE `+whereSQL+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
    if err != nil {
        return nil, fmt.Errorf("list parts: %w", err)
    }
    defer rows.Close()
    byID := map[int64]int{}
    var ids []int64
    for rows.Next() {
        var ps PartSummary
        var mfr, deleted sql.NullString
        var thumb, category sql.NullInt64
        if err := rows.Scan(&ps.ID, &ps.Name, &ps.MPN, &mfr, &ps.UOM, &ps.TotalQuantity, &thumb, &ps.Low,
            &ps.UpdatedAt, &deleted, &category); err != nil {
            return nil, err
        }
        ps.Manufacturer, ps.ThumbnailFileID, ps.DeletedAt = nullStr(mfr), nullInt(thumb), nullStr(deleted)
        ps.Category = cix.ref(nullInt(category))
        ps.Tags, ps.Locations = []string{}, []string{}
        byID[ps.ID] = len(out.Items)
        ids = append(ids, ps.ID)
        out.Items = append(out.Items, ps)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    rows.Close()
    if len(ids) == 0 {
        return out, nil
    }
    tagRows, err := s.DB.R.QueryContext(ctx, `SELECT pt.part_id, t.name FROM part_tags pt JOIN tags t ON t.id = pt.tag_id
        WHERE pt.part_id IN (`+placeholders(len(ids))+`) ORDER BY t.name`, int64Args(ids)...)
    if err != nil {
        return nil, err
    }
    for tagRows.Next() {
        var pid int64
        var name string
        if err := tagRows.Scan(&pid, &name); err != nil {
            tagRows.Close()
            return nil, err
        }
        it := &out.Items[byID[pid]]
        it.Tags = append(it.Tags, name)
    }
    tagRows.Close()
    locRows, err := s.DB.R.QueryContext(ctx, `SELECT part_id, location_id FROM stock
        WHERE part_id IN (`+placeholders(len(ids))+`) ORDER BY quantity_milli DESC`, int64Args(ids)...)
    if err != nil {
        return nil, err
    }
    defer locRows.Close()
    for locRows.Next() {
        var pid, lid int64
        if err := locRows.Scan(&pid, &lid); err != nil {
            return nil, err
        }
        it := &out.Items[byID[pid]]
        it.Locations = append(it.Locations, ix.path(lid))
    }
    return out, locRows.Err()
}

// LookupCode finds parts whose barcode, MPN or supplier SKU exactly
// matches code (case-insensitively).
func (s *Service) LookupCode(ctx context.Context, code string) ([]Ref, error) {
    code = strings.TrimSpace(code)
    out := []Ref{}
    if code == "" {
        return out, nil
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT id, name FROM parts WHERE deleted_at IS NULL AND
            (barcode = ? COLLATE NOCASE OR mpn = ? COLLATE NOCASE OR supplier_sku = ? COLLATE NOCASE)
        ORDER BY (barcode = ? COLLATE NOCASE) DESC, name LIMIT 20`, code, code, code, code)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    for rows.Next() {
        var r Ref
        if err := rows.Scan(&r.ID, &r.Name); err != nil {
            return nil, err
        }
        out = append(out, r)
    }
    return out, rows.Err()
}

// UOMs returns distinct units of measure in use, with common defaults.
func (s *Service) UOMs(ctx context.Context, q string) ([]string, error) {
    used, err := queryStrings(ctx, s.DB.R, `SELECT DISTINCT uom FROM parts WHERE deleted_at IS NULL ORDER BY uom`)
    if err != nil {
        return nil, err
    }
    seen := map[string]bool{}
    out := []string{}
    q = strings.ToLower(strings.TrimSpace(q))
    for _, u := range append([]string{"pcs", "m", "mm", "g", "kg", "ml", "l"}, used...) {
        if !seen[u] && strings.Contains(strings.ToLower(u), q) {
            seen[u] = true
            out = append(out, u)
        }
    }
    return out, nil
}
