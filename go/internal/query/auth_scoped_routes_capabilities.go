package query

import "net/http"

// scopedCapabilityCatalogRoute reports whether the request targets the capability
// catalog read. The catalog is the static, embedded artifact and carries no
// tenant-scoped data, so scoped tokens may read it unfiltered.
func scopedCapabilityCatalogRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/capabilities"
}

// scopedSurfaceInventoryRoute reports whether the request targets the static
// surface inventory. Like the capability catalog, this route serves an embedded
// generated artifact and carries no tenant-scoped data.
func scopedSurfaceInventoryRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/surface-inventory"
}
