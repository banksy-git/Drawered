// Package perm defines the fixed, code-defined permission set.
package perm

// Permission is a single capability that may be granted through a role.
type Permission string

const (
    PartsRead        Permission = "parts:read"
    LocationsRead    Permission = "locations:read"
    EventsRead       Permission = "events:read"
    StockMove        Permission = "stock:move"
    StockAdjust      Permission = "stock:adjust"
    PartsCreate      Permission = "parts:create"
    PartsEdit        Permission = "parts:edit"
    PartsDelete      Permission = "parts:delete"
    LocationsCreate  Permission = "locations:create"
    LocationsEdit    Permission = "locations:edit"
    LocationsDelete  Permission = "locations:delete"
    CatalogueManage  Permission = "catalogue:manage"
    CategoriesManage Permission = "categories:manage"
    UsersManage      Permission = "users:manage"
    RolesManage      Permission = "roles:manage"
    SystemAdmin      Permission = "system:admin"
)

// Info describes a permission for the role editor.
type Info struct {
    Name        Permission `json:"name"`
    Description string     `json:"description"`
}

// All lists every permission in display order.
var All = []Info{
    {PartsRead, "View parts, stock, files, tags, manufacturers and suppliers; search."},
    {LocationsRead, "View the location tree and location details."},
    {EventsRead, "View history tabs and the global activity feed."},
    {StockMove, "Add, remove and move stock; add and remove empty stock entries."},
    {StockAdjust, "Set absolute quantities (stock-take corrections)."},
    {PartsCreate, "Create parts and their stock entries."},
    {PartsEdit, "Edit part details, tags, images, documents and links."},
    {PartsDelete, "Delete and restore parts."},
    {LocationsCreate, "Create locations."},
    {LocationsEdit, "Edit and move locations, including description and colour."},
    {LocationsDelete, "Delete empty locations."},
    {CatalogueManage, "Rename, merge and delete manufacturers, suppliers and tags."},
    {CategoriesManage, "Create, edit (including icon), move and delete part categories."},
    {UsersManage, "View users, assign roles, disable users and revoke sessions."},
    {RolesManage, "Manage roles, claim mappings and the default role."},
    {SystemAdmin, "Reindex, back up, purge, view system information. Implies every permission."},
}

// Valid reports whether p is a known permission.
func Valid(p Permission) bool {
    for _, i := range All {
        if i.Name == p {
            return true
        }
    }
    return false
}

// Set is a user's effective permission set.
type Set map[Permission]bool

// Has reports whether the set grants p. system:admin implies everything.
func (s Set) Has(p Permission) bool {
    return s[p] || s[SystemAdmin]
}

// List returns the effective permissions, expanding system:admin.
func (s Set) List() []Permission {
    out := []Permission{}
    for _, i := range All {
        if s.Has(i.Name) {
            out = append(out, i.Name)
        }
    }
    return out
}

// Seed role definitions created on first start.
var (
    ReadOnlyPerms = []Permission{PartsRead, LocationsRead, EventsRead}
    EngineerPerms = append(append([]Permission{}, ReadOnlyPerms...), StockMove, PartsCreate, PartsEdit)
    StoresPerms   = append(append([]Permission{}, EngineerPerms...), StockAdjust, PartsDelete,
        LocationsCreate, LocationsEdit, LocationsDelete, CatalogueManage, CategoriesManage)
    AdminPerms = []Permission{SystemAdmin}
)
