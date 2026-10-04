package api

import (
    "net/http"
    "strings"

    "drawered/internal/perm"
    "drawered/internal/service"
)

func (s *Server) routes(mux *http.ServeMux) {
    mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
    mux.HandleFunc("GET /readyz", s.ready)

    mux.HandleFunc("GET /auth/login", s.login)
    mux.HandleFunc("GET /auth/callback", s.callback)
    s.handle(mux, "POST /auth/logout", "", s.logout)

    mux.HandleFunc("GET /files/{id}", s.serveFile)
    mux.HandleFunc("GET /files/{id}/{rendition}", s.serveFile)

    h := func(pattern string, p perm.Permission, fn apiFunc) { s.handle(mux, pattern, p, fn) }
    const v = "/api/v1"

    h("GET "+v+"/me", "", s.me)
    h("GET "+v+"/permissions", "", func(w http.ResponseWriter, r *http.Request) error { return ok(w, perm.All) })

    // Parts.
    h("GET "+v+"/parts", perm.PartsRead, s.listParts)
    h("POST "+v+"/parts", perm.PartsCreate, s.createPart)
    h("GET "+v+"/parts/lookup", perm.PartsRead, s.lookupPart)
    h("GET "+v+"/parts/{id}", perm.PartsRead, s.getPart)
    h("PATCH "+v+"/parts/{id}", perm.PartsEdit, s.updatePart)
    h("DELETE "+v+"/parts/{id}", perm.PartsDelete, s.deletePart)
    h("POST "+v+"/parts/{id}/restore", perm.PartsDelete, s.restorePart)
    h("POST "+v+"/parts/{id}/purge", perm.SystemAdmin, s.purgePart)
    h("POST "+v+"/parts/{id}/duplicate", perm.PartsCreate, s.duplicatePart)
    h("GET "+v+"/parts/{id}/events", perm.EventsRead, s.subjectEvents("part"))
    h("GET "+v+"/parts/{id}/stock", perm.PartsRead, s.partStock)

    h("POST "+v+"/parts/{id}/images", perm.PartsEdit, s.addImage)
    h("PATCH "+v+"/parts/{id}/images/{sub}", perm.PartsEdit, s.updateImage)
    h("DELETE "+v+"/parts/{id}/images/{sub}", perm.PartsEdit, s.deleteImage)
    h("PUT "+v+"/parts/{id}/thumbnail", perm.PartsEdit, s.setThumbnail)
    h("POST "+v+"/parts/{id}/documents", perm.PartsEdit, s.addDocument)
    h("PATCH "+v+"/parts/{id}/documents/{sub}", perm.PartsEdit, s.updateDocument)
    h("DELETE "+v+"/parts/{id}/documents/{sub}", perm.PartsEdit, s.deleteDocument)
    h("POST "+v+"/parts/{id}/links", perm.PartsEdit, s.addLink)
    h("PATCH "+v+"/parts/{id}/links/{sub}", perm.PartsEdit, s.updateLink)
    h("DELETE "+v+"/parts/{id}/links/{sub}", perm.PartsEdit, s.deleteLink)

    // Stock.
    h("POST "+v+"/stock/add", perm.StockMove, s.stockAdjust("add"))
    h("POST "+v+"/stock/remove", perm.StockMove, s.stockAdjust("remove"))
    h("POST "+v+"/stock/set", perm.StockAdjust, s.stockAdjust("set"))
    h("POST "+v+"/stock/move", perm.StockMove, s.stockMove)
    h("POST "+v+"/stock/entries", perm.StockMove, s.addStockEntry)
    h("PATCH "+v+"/stock/{part}/{loc}", perm.StockMove, s.updateStockEntry)
    h("DELETE "+v+"/stock/{part}/{loc}", perm.StockMove, s.removeStockEntry)

    // Locations.
    h("GET "+v+"/locations", perm.LocationsRead, s.listLocations)
    h("POST "+v+"/locations", perm.LocationsCreate, s.createLocation)
    h("GET "+v+"/locations/search", perm.LocationsRead, s.searchLocations)
    h("GET "+v+"/locations/{id}", perm.LocationsRead, s.getLocation)
    h("PATCH "+v+"/locations/{id}", perm.LocationsEdit, s.updateLocation)
    h("DELETE "+v+"/locations/{id}", perm.LocationsDelete, s.deleteLocation)
    h("GET "+v+"/locations/{id}/stock", perm.PartsRead, s.locationStock)
    h("GET "+v+"/locations/{id}/events", perm.EventsRead, s.subjectEvents("location"))

    // Part categories.
    h("GET "+v+"/categories", perm.PartsRead, s.listCategories)
    h("POST "+v+"/categories", perm.CategoriesManage, s.createCategory)
    h("GET "+v+"/categories/{id}", perm.PartsRead, s.getCategory)
    h("PATCH "+v+"/categories/{id}", perm.CategoriesManage, s.updateCategory)
    h("DELETE "+v+"/categories/{id}", perm.CategoriesManage, s.deleteCategory)
    h("GET "+v+"/categories/{id}/events", perm.EventsRead, s.subjectEvents("category"))

    // Catalogue.
    h("GET "+v+"/tags", perm.PartsRead, s.listTags)
    h("PATCH "+v+"/tags/{id}", perm.CatalogueManage, s.renameTag)
    h("POST "+v+"/tags/{id}/merge", perm.CatalogueManage, s.mergeTag)
    h("DELETE "+v+"/tags/{id}", perm.CatalogueManage, s.deleteTag)
    h("GET "+v+"/uoms", perm.PartsRead, s.listUOMs)
    for _, k := range []service.NamedKind{service.Manufacturers, service.Suppliers} {
        base := v + "/" + string(k) + "s"
        h("GET "+base, perm.PartsRead, s.listNamed(k))
        h("POST "+base, perm.CatalogueManage, s.createNamed(k))
        h("PATCH "+base+"/{id}", perm.CatalogueManage, s.updateNamed(k))
        h("POST "+base+"/{id}/merge", perm.CatalogueManage, s.mergeNamed(k))
        h("DELETE "+base+"/{id}", perm.CatalogueManage, s.deleteNamed(k))
        h("GET "+base+"/{id}/events", perm.EventsRead, s.subjectEvents(string(k)))
    }

    // Events.
    h("GET "+v+"/events", perm.EventsRead, s.listEvents)

    // Administration.
    h("GET "+v+"/roles", "", s.requireAny(s.listRoles, perm.UsersManage, perm.RolesManage))
    h("POST "+v+"/roles", perm.RolesManage, s.createRole)
    h("GET "+v+"/roles/{id}", "", s.requireAny(s.getRole, perm.UsersManage, perm.RolesManage))
    h("PATCH "+v+"/roles/{id}", perm.RolesManage, s.updateRole)
    h("DELETE "+v+"/roles/{id}", perm.RolesManage, s.deleteRole)
    h("GET "+v+"/roles/{id}/events", perm.EventsRead, s.subjectEvents("role"))
    h("GET "+v+"/role-mappings", perm.RolesManage, s.listMappings)
    h("POST "+v+"/role-mappings", perm.RolesManage, s.createMapping)
    h("POST "+v+"/role-mappings/test", perm.RolesManage, s.testMappings)
    h("PATCH "+v+"/role-mappings/{id}", perm.RolesManage, s.updateMapping)
    h("DELETE "+v+"/role-mappings/{id}", perm.RolesManage, s.deleteMapping)
    h("GET "+v+"/settings", perm.RolesManage, s.getSettings)
    h("PATCH "+v+"/settings", perm.RolesManage, s.updateSettings)
    h("GET "+v+"/users", perm.UsersManage, s.listUsers)
    h("GET "+v+"/users/{id}", perm.UsersManage, s.getUser)
    h("PUT "+v+"/users/{id}/roles", perm.UsersManage, s.setUserRoles)
    h("POST "+v+"/users/{id}/disable", perm.UsersManage, s.setUserDisabled(true))
    h("POST "+v+"/users/{id}/enable", perm.UsersManage, s.setUserDisabled(false))
    h("GET "+v+"/users/{id}/sessions", perm.UsersManage, s.listSessions)
    h("DELETE "+v+"/users/{id}/sessions/{sub}", perm.UsersManage, s.revokeSession)
    h("GET "+v+"/users/{id}/events", perm.EventsRead, s.subjectEvents("user"))
    h("GET "+v+"/system", perm.SystemAdmin, s.systemInfo)
    h("POST "+v+"/system/reindex", perm.SystemAdmin, s.reindex)
    h("GET "+v+"/system/backup", perm.SystemAdmin, s.backup)

    mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
        writeError(w, &service.Error{Status: http.StatusNotFound, Code: "not_found", Message: "no such endpoint"})
    })
    mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/auth/") || strings.HasPrefix(r.URL.Path, "/files/") {
            http.NotFound(w, r)
            return
        }
        s.spa.ServeHTTP(w, r)
    })
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
    if err := s.svc.DB.R.PingContext(r.Context()); err != nil {
        http.Error(w, "database unavailable", http.StatusServiceUnavailable)
        return
    }
    w.Write([]byte("ok\n"))
}
