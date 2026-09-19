// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"reflect"
	"strconv"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	capabilitiesDefaultLimit = 12
	capabilitiesMaxLimit     = 500
)

// capabilityCompactEntry is the default (compact) view's per-entry shape. It
// hand-picks a subset of capabilitycatalog.Entry's fields, omitting Profiles
// and ProofSignals, which are the bulk of an entry's serialized size (#6795).
// view=full instead serializes capabilitycatalog.Entry directly (see list),
// so the full-view shape is guaranteed byte-identical to the pre-#6795
// response and cannot silently drop a field added to Entry later.
type capabilityCompactEntry struct {
	Capability      string                                    `json:"capability"`
	DisplayName     string                                    `json:"display_name"`
	OwnerPackage    string                                    `json:"owner_package,omitempty"`
	Maturity        capabilitycatalog.Maturity                `json:"maturity"`
	DerivedMaturity capabilitycatalog.Maturity                `json:"derived_maturity"`
	MaturityReason  string                                    `json:"maturity_reason,omitempty"`
	Surfaces        []capabilitycatalog.Surface               `json:"surfaces"`
	KnownGaps       []string                                  `json:"known_gaps,omitempty"`
	LinkedIssues    []int                                     `json:"linked_issues,omitempty"`
	Docs            []string                                  `json:"docs,omitempty"`
	Console         bool                                      `json:"console"`
	Authorization   capabilitycatalog.CapabilityAuthorization `json:"authorization"`
}

// toCapabilityCompactEntry projects entry into its compact wire shape.
func toCapabilityCompactEntry(entry capabilitycatalog.Entry) capabilityCompactEntry {
	return capabilityCompactEntry{
		Capability:      entry.Capability,
		DisplayName:     entry.DisplayName,
		OwnerPackage:    entry.OwnerPackage,
		Maturity:        entry.Maturity,
		DerivedMaturity: entry.DerivedMaturity,
		MaturityReason:  entry.MaturityReason,
		Surfaces:        entry.Surfaces,
		KnownGaps:       entry.KnownGaps,
		LinkedIssues:    entry.LinkedIssues,
		Docs:            entry.Docs,
		Console:         entry.Console,
		Authorization:   entry.Authorization,
	}
}

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
// filters and deterministic limit/offset paging. The default response is the
// compact view: entries omit profiles and proof_signals, and the top-level
// authorization catalog is empty. Pass view=full for the complete entry shape
// and include_authorization=true for the full role/grant/data-class catalog
// (#6795 -- these are the two largest contributors to default payload size).
// GET /api/v0/capabilities?maturity=&owner=&limit=&offset=&view=&include_authorization=
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
	full, ok := parseCatalogView(w, r)
	if !ok {
		return
	}
	includeAuthorization, ok := parseIncludeAuthorization(w, r)
	if !ok {
		return
	}

	maturity := QueryParam(r, "maturity")
	owner := QueryParam(r, "owner")
	filtered := filterCatalogEntries(catalog.Entries, maturity, owner)

	total := len(filtered)
	page, truncated := pageEntries(filtered, offset, limit)

	// view=full serializes the catalog entries unchanged (byte-identical to
	// the pre-#6795 response); the compact default projects a hand-picked
	// subset. Never wrap page in an intermediate struct for the full case --
	// that would silently drop a field added to capabilitycatalog.Entry later.
	var wirePage any
	if full {
		wirePage = page
	} else {
		compact := make([]capabilityCompactEntry, len(page))
		for i, entry := range page {
			compact[i] = toCapabilityCompactEntry(entry)
		}
		wirePage = compact
	}

	authorization := emptyAuthorizationCatalog()
	if includeAuthorization {
		authorization = catalog.Authorization
	}

	truth := BuildTruthEnvelope(h.profile(), capabilityCatalogCapability, TruthBasisRuntimeState,
		"embedded, generated capability catalog; no live backend read")
	truth.Freshness = TruthFreshness{State: FreshnessFresh}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"version":       catalog.Version,
		"authorization": authorization,
		"capabilities":  wirePage,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
		"truncated":     truncated,
		"next_offset":   nextOffset(offset, limit, truncated),
	}, truth)
}

// nextOffset returns the offset value a caller should pass to fetch the next
// page, or nil when the current page is not truncated. This mirrors the
// next_offset convention used across the other paginated list handlers in
// this package (e.g. nextInfraResourceAggregateOffset).
func nextOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + limit
}

// parseCatalogView reads the view query param, defaulting to the compact view
// (full=false). An unrecognized value is a bounded 400, not a silent default.
func parseCatalogView(w http.ResponseWriter, r *http.Request) (full bool, ok bool) {
	switch raw := QueryParam(r, "view"); raw {
	case "", "compact":
		return false, true
	case "full":
		return true, true
	default:
		WriteError(w, http.StatusBadRequest, "view must be compact or full")
		return false, false
	}
}

// parseIncludeAuthorization reads the include_authorization query param,
// defaulting to false. An unrecognized value is a bounded 400.
func parseIncludeAuthorization(w http.ResponseWriter, r *http.Request) (bool, bool) {
	switch raw := QueryParam(r, "include_authorization"); raw {
	case "", "false":
		return false, true
	case "true":
		return true, true
	default:
		WriteError(w, http.StatusBadRequest, "include_authorization must be true or false")
		return false, false
	}
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
// and rejecting values outside [1, max]. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func parseBoundedLimit(w http.ResponseWriter, r *http.Request, def, max int) (int, bool) {
	return querycontract.ParseBoundedLimit(w, r, def, max)
}

// emptyAuthorizationCatalog returns an AuthorizationCatalog whose slice
// fields -- including nested ones such as bootstrap_owner.delegable_roles --
// are non-nil, zero-length slices instead of Go's zero-value nil. The
// OpenAPI fragment (openapi/paths/catalog/capabilities.go) declares roles,
// data_classes, permission_families, and bootstrap_owner.delegable_roles as
// non-nullable JSON arrays; a bare capabilitycatalog.AuthorizationCatalog{}
// would instead serialize those fields as null (#6795 review finding). Uses
// reflection so a slice field added later to AuthorizationCatalog, at any
// nesting depth, is covered automatically instead of silently regressing to
// null.
func emptyAuthorizationCatalog() capabilitycatalog.AuthorizationCatalog {
	var catalog capabilitycatalog.AuthorizationCatalog
	nilSlicesToEmpty(reflect.ValueOf(&catalog).Elem())
	return catalog
}

// nilSlicesToEmpty walks the struct value v and replaces every nil slice
// field with a non-nil, zero-length slice of the same element type,
// recursing into nested struct fields.
func nilSlicesToEmpty(v reflect.Value) {
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		switch field.Kind() {
		case reflect.Slice:
			if field.IsNil() {
				field.Set(reflect.MakeSlice(field.Type(), 0, 0))
			}
		case reflect.Struct:
			nilSlicesToEmpty(field)
		default:
			// Non-slice, non-struct fields (string, bool, ...) need no
			// nil-to-empty normalization.
		}
	}
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
