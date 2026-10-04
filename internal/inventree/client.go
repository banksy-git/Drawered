// Package inventree is a minimal read-only client for the InvenTree REST
// API, covering what the Drawered importer needs.
package inventree

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "mime"
    "net/http"
    "net/url"
    "path"
    "strconv"
    "strings"
    "time"
)

// Client talks to one InvenTree server.
type Client struct {
    BaseURL string
    Token   string
    HTTP    *http.Client
}

// New creates a client for baseURL (with or without a trailing slash).
func New(baseURL string) *Client {
    return &Client{
        BaseURL: strings.TrimRight(baseURL, "/"),
        HTTP:    &http.Client{Timeout: 60 * time.Second},
    }
}

// Error is a non-success HTTP response.
type Error struct {
    Status int
    URL    string
    Body   string
}

func (e *Error) Error() string {
    return fmt.Sprintf("inventree: %s returned %d: %s", e.URL, e.Status, e.Body)
}

func (c *Client) do(ctx context.Context, rawURL string, user, pass string) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Accept", "application/json")
    if user != "" {
        req.SetBasicAuth(user, pass)
    } else if c.Token != "" {
        req.Header.Set("Authorization", "Token "+c.Token)
    }
    resp, err := c.HTTP.Do(req)
    if err != nil {
        return nil, err
    }
    if resp.StatusCode >= 300 {
        defer resp.Body.Close()
        b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
        return nil, &Error{Status: resp.StatusCode, URL: rawURL, Body: strings.TrimSpace(string(b))}
    }
    return resp, nil
}

// Login fetches an API token with a username and password, trying the
// current endpoint first and then the pre-1.0 one.
func (c *Client) Login(ctx context.Context, user, pass string) error {
    var lastErr error
    for _, p := range []string{"/api/user/me/token/", "/api/user/token/"} {
        resp, err := c.do(ctx, c.BaseURL+p, user, pass)
        if err != nil {
            var he *Error
            if errors.As(err, &he) && he.Status == http.StatusNotFound {
                lastErr = err
                continue
            }
            return err
        }
        var body struct {
            Token string `json:"token"`
        }
        err = json.NewDecoder(resp.Body).Decode(&body)
        resp.Body.Close()
        if err != nil {
            return fmt.Errorf("inventree: decode token: %w", err)
        }
        if body.Token == "" {
            return errors.New("inventree: login returned no token")
        }
        c.Token = body.Token
        return nil
    }
    return lastErr
}

// pageSize is the number of records requested per page.
const pageSize = 500

// List fetches every record of a list endpoint, following limit/offset
// paging. A bare JSON array (paging disabled on the server) is accepted.
func List[T any](ctx context.Context, c *Client, endpoint string, params url.Values) ([]T, error) {
    var out []T
    offset := 0
    for {
        q := url.Values{}
        for k, v := range params {
            q[k] = v
        }
        q.Set("limit", strconv.Itoa(pageSize))
        q.Set("offset", strconv.Itoa(offset))
        resp, err := c.do(ctx, c.BaseURL+endpoint+"?"+q.Encode(), "", "")
        if err != nil {
            return nil, err
        }
        body, err := io.ReadAll(resp.Body)
        resp.Body.Close()
        if err != nil {
            return nil, err
        }
        body = bytes.TrimSpace(body)
        if len(body) > 0 && body[0] == '[' {
            var all []T
            if err := json.Unmarshal(body, &all); err != nil {
                return nil, fmt.Errorf("inventree: decode %s: %w", endpoint, err)
            }
            return append(out, all...), nil
        }
        var page struct {
            Count   int             `json:"count"`
            Next    *string         `json:"next"`
            Results json.RawMessage `json:"results"`
        }
        if err := json.Unmarshal(body, &page); err != nil {
            return nil, fmt.Errorf("inventree: decode %s: %w", endpoint, err)
        }
        var items []T
        if err := json.Unmarshal(page.Results, &items); err != nil {
            return nil, fmt.Errorf("inventree: decode %s results: %w", endpoint, err)
        }
        out = append(out, items...)
        offset += len(items)
        if len(items) == 0 || page.Next == nil || offset >= page.Count {
            return out, nil
        }
    }
}

// Download fetches a media file such as a part image. rawURL may be
// absolute or relative to the server. The caller closes the body.
func (c *Client) Download(ctx context.Context, rawURL string) (io.ReadCloser, string, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return nil, "", err
    }
    base, err := url.Parse(c.BaseURL + "/")
    if err != nil {
        return nil, "", err
    }
    abs := base.ResolveReference(u)
    resp, err := c.do(ctx, abs.String(), "", "")
    if err != nil {
        return nil, "", err
    }
    name := path.Base(abs.Path)
    if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
        name = params["filename"]
    }
    return resp.Body, name, nil
}

// Number accepts a JSON number or a numeric string (InvenTree serialises
// decimals as strings) and keeps its exact text.
type Number string

func (n *Number) UnmarshalJSON(b []byte) error {
    s := string(bytes.TrimSpace(b))
    if s == "null" {
        *n = ""
        return nil
    }
    if strings.HasPrefix(s, `"`) {
        var v string
        if err := json.Unmarshal(b, &v); err != nil {
            return err
        }
        s = v
    }
    *n = Number(strings.TrimSpace(s))
    return nil
}

// Float returns the value as a float64 (0 if empty or invalid).
func (n Number) Float() float64 {
    f, _ := strconv.ParseFloat(string(n), 64)
    return f
}

// Text is a string that may be null in the JSON.
type Text string

func (t *Text) UnmarshalJSON(b []byte) error {
    if string(bytes.TrimSpace(b)) == "null" {
        *t = ""
        return nil
    }
    var s string
    if err := json.Unmarshal(b, &s); err != nil {
        return err
    }
    *t = Text(s)
    return nil
}

// Location is a stock location.
type Location struct {
    PK          int64  `json:"pk"`
    Name        Text   `json:"name"`
    Description Text   `json:"description"`
    Parent      *int64 `json:"parent"`
    Structural  bool   `json:"structural"`
}

// Category is a part category.
type Category struct {
    PK          int64  `json:"pk"`
    Name        Text   `json:"name"`
    Description Text   `json:"description"`
    Parent      *int64 `json:"parent"`
    Structural  bool   `json:"structural"`
    Icon        Text   `json:"icon"`
}

// Part is a part, with only the fields the importer uses.
type Part struct {
    PK              int64    `json:"pk"`
    Name            Text     `json:"name"`
    Description     Text     `json:"description"`
    IPN             Text     `json:"IPN"`
    Revision        Text     `json:"revision"`
    Keywords        Text     `json:"keywords"`
    Category        *int64   `json:"category"`
    Units           Text     `json:"units"`
    MinimumStock    Number   `json:"minimum_stock"`
    Link            Text     `json:"link"`
    Image           Text     `json:"image"`
    Active          *bool    `json:"active"`
    Tags            []string `json:"tags"`
    DefaultSupplier *int64   `json:"default_supplier"`
}

// IsActive reports whether the part is active (missing means active).
func (p Part) IsActive() bool { return p.Active == nil || *p.Active }

// CompanyBrief is the nested company detail on supplier and manufacturer parts.
type CompanyBrief struct {
    PK   int64 `json:"pk"`
    Name Text  `json:"name"`
}

// ManufacturerPart links a part to a manufacturer and MPN.
type ManufacturerPart struct {
    PK                 int64         `json:"pk"`
    Part               int64         `json:"part"`
    Manufacturer       int64         `json:"manufacturer"`
    ManufacturerDetail *CompanyBrief `json:"manufacturer_detail"`
    MPN                Text          `json:"MPN"`
    Link               Text          `json:"link"`
}

// SupplierPart links a part to a supplier and SKU.
type SupplierPart struct {
    PK                     int64             `json:"pk"`
    Part                   int64             `json:"part"`
    Supplier               int64             `json:"supplier"`
    SupplierDetail         *CompanyBrief     `json:"supplier_detail"`
    SKU                    Text              `json:"SKU"`
    Link                   Text              `json:"link"`
    Active                 *bool             `json:"active"`
    Primary                bool              `json:"primary"`
    ManufacturerPart       *int64            `json:"manufacturer_part"`
    ManufacturerPartDetail *ManufacturerPart `json:"manufacturer_part_detail"`
}

// IsActive reports whether the supplier part is active (missing means active).
func (s SupplierPart) IsActive() bool { return s.Active == nil || *s.Active }

// PriceBreak is a supplier part price at a quantity.
type PriceBreak struct {
    PK       int64  `json:"pk"`
    Part     int64  `json:"part"` // the supplier part
    Quantity Number `json:"quantity"`
    Price    Number `json:"price"`
    Currency Text   `json:"price_currency"`
}

// StockItem is a quantity of a part at a location.
type StockItem struct {
    PK       int64  `json:"pk"`
    Part     int64  `json:"part"`
    Location *int64 `json:"location"`
    Quantity Number `json:"quantity"`
}

// Company is a supplier, manufacturer or customer.
type Company struct {
    PK   int64 `json:"pk"`
    Name Text  `json:"name"`
}

// Data is everything the importer reads, fetched up front.
type Data struct {
    Companies         []Company
    Locations         []Location
    Categories        []Category
    Parts             []Part
    SupplierParts     []SupplierPart
    ManufacturerParts []ManufacturerPart
    PriceBreaks       []PriceBreak
    Stock             []StockItem
}

// FetchAll reads every record the importer needs. withStock controls
// whether stock items are fetched.
func (c *Client) FetchAll(ctx context.Context, withStock bool, progress func(string, int)) (*Data, error) {
    d := &Data{}
    var err error
    step := func(name string, n int) {
        if progress != nil {
            progress(name, n)
        }
    }
    if d.Companies, err = List[Company](ctx, c, "/api/company/", nil); err != nil {
        return nil, err
    }
    step("companies", len(d.Companies))
    if d.Locations, err = List[Location](ctx, c, "/api/stock/location/", nil); err != nil {
        return nil, err
    }
    step("stock locations", len(d.Locations))
    if d.Categories, err = List[Category](ctx, c, "/api/part/category/", nil); err != nil {
        return nil, err
    }
    step("part categories", len(d.Categories))
    if d.Parts, err = List[Part](ctx, c, "/api/part/", url.Values{"tags": {"true"}}); err != nil {
        return nil, err
    }
    step("parts", len(d.Parts))
    if d.SupplierParts, err = List[SupplierPart](ctx, c, "/api/company/part/", url.Values{
        "supplier_detail": {"true"}, "manufacturer_part_detail": {"true"}, "manufacturer_detail": {"true"},
    }); err != nil {
        return nil, err
    }
    step("supplier parts", len(d.SupplierParts))
    if d.ManufacturerParts, err = List[ManufacturerPart](ctx, c, "/api/company/part/manufacturer/", url.Values{
        "manufacturer_detail": {"true"},
    }); err != nil {
        return nil, err
    }
    step("manufacturer parts", len(d.ManufacturerParts))
    if d.PriceBreaks, err = List[PriceBreak](ctx, c, "/api/company/price-break/", nil); err != nil {
        return nil, err
    }
    step("price breaks", len(d.PriceBreaks))
    if withStock {
        if d.Stock, err = List[StockItem](ctx, c, "/api/stock/", url.Values{"in_stock": {"true"}}); err != nil {
            return nil, err
        }
        step("stock items", len(d.Stock))
    }
    return d, nil
}
