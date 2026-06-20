package query

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

const (
	capabilitiesDefaultLimit = 200
	capabilitiesMaxLimit     = 500
)

// CapabilitiesHandler serves the reconciled capability catalog at
// GET /api/v0/capabilities. The catalog is the embedded, generated artifact from
// the capabilitycatalog package, so the read is static, bounded, and exact in
// every profile. The same artifact backs the MCP get_capability_catalog tool and
// the console capability matrix, which keeps the three surfaces in parity.
type CapabilitiesHandler struct {
	Profile QueryProfile

	once    sync.Once
	catalog capabilitycatalog.Catalog
	loadErr error
}

func (h *CapabilitiesHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// load reads and caches the embedded catalog once for the handler's lifetime.
func (h *CapabilitiesHandler) load() (capabilitycatalog.Catalog, error) {
	h.once.Do(func() {
		h.catalog, h.loadErr = capabilitycatalog.Load()
	})
	return h.catalog, h.loadErr
}

// Mount registers the capability catalog route.
func (h *CapabilitiesHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/capabilities", h.list)
}

// list returns the capability catalog with optional maturity and owner_package
// filters and deterministic limit/offset paging.
// GET /api/v0/capabilities?maturity=&owner=&limit=&offset=
func (h *CapabilitiesHandler) list(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.load()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "capability catalog unavailable")
		return
	}

	limit, ok := parseBoundedLimit(w, r, capabilitiesDefaultLimit, capabilitiesMaxLimit)
	if !ok {
		return
	}
	offset, ok := parseOffset(w, r)
	if !ok {
		return
	}

	maturity := QueryParam(r, "maturity")
	owner := QueryParam(r, "owner")
	filtered := filterCatalogEntries(catalog.Entries, maturity, owner)

	total := len(filtered)
	page, truncated := pageEntries(filtered, offset, limit)

	truth := BuildTruthEnvelope(h.profile(), capabilityCatalogCapability, TruthBasisRuntimeState,
		"embedded, generated capability catalog; no live backend read")
	truth.Freshness = TruthFreshness{State: FreshnessFresh}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"version":      catalog.Version,
		"capabilities": page,
		"total":        total,
		"limit":        limit,
		"offset":       offset,
		"truncated":    truncated,
	}, truth)
}

// filterCatalogEntries returns entries matching the optional maturity and
// owner_package filters, preserving the catalog's deterministic order.
func filterCatalogEntries(entries []capabilitycatalog.Entry, maturity, owner string) []capabilitycatalog.Entry {
	if maturity == "" && owner == "" {
		return entries
	}
	out := make([]capabilitycatalog.Entry, 0, len(entries))
	for _, entry := range entries {
		if maturity != "" && string(entry.Maturity) != maturity {
			continue
		}
		if owner != "" && entry.OwnerPackage != owner {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// pageEntries applies offset and limit and reports whether more entries remain
// past the returned page.
func pageEntries(entries []capabilitycatalog.Entry, offset, limit int) ([]capabilitycatalog.Entry, bool) {
	if offset >= len(entries) {
		return []capabilitycatalog.Entry{}, false
	}
	end := offset + limit
	truncated := end < len(entries)
	if end > len(entries) {
		end = len(entries)
	}
	return entries[offset:end], truncated
}

// parseBoundedLimit reads the limit query param, applying the default when blank
// and rejecting values outside [1, max].
func parseBoundedLimit(w http.ResponseWriter, r *http.Request, def, max int) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return def, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > max {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer in [1, %d]", max))
		return 0, false
	}
	return limit, true
}

// parseOffset reads the offset query param, defaulting to 0 and rejecting
// negative values.
func parseOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	return offset, true
}
