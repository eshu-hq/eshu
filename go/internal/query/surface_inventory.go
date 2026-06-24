// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

const (
	surfaceInventoryDefaultLimit = 200
	surfaceInventoryMaxLimit     = 1000
)

// SurfaceInventoryHandler serves the generated surface inventory at
// GET /api/v0/surface-inventory. The inventory is the embedded, generated
// artifact from the capabilitycatalog package, so the read is static, bounded,
// and exact in every profile. The same artifact backs the MCP
// get_surface_inventory tool and the console surface inventory page, which keeps
// the three surfaces in parity.
type SurfaceInventoryHandler struct {
	Profile QueryProfile

	once      sync.Once
	inventory capabilitycatalog.SurfaceInventory
	loadErr   error
}

func (h *SurfaceInventoryHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// load reads and caches the embedded surface inventory once for the handler's
// lifetime.
func (h *SurfaceInventoryHandler) load() (capabilitycatalog.SurfaceInventory, error) {
	h.once.Do(func() {
		h.inventory, h.loadErr = capabilitycatalog.LoadSurfaceInventory()
	})
	return h.inventory, h.loadErr
}

// Mount registers the surface inventory route.
func (h *SurfaceInventoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/surface-inventory", h.list)
}

// list returns the surface inventory with optional category and readiness
// filters and deterministic limit/offset paging.
// GET /api/v0/surface-inventory?category=&readiness=&limit=&offset=
func (h *SurfaceInventoryHandler) list(w http.ResponseWriter, r *http.Request) {
	inventory, err := h.load()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "surface inventory unavailable")
		return
	}

	limit, ok := parseBoundedLimit(w, r, surfaceInventoryDefaultLimit, surfaceInventoryMaxLimit)
	if !ok {
		return
	}
	offset, ok := parseOffset(w, r)
	if !ok {
		return
	}

	category := QueryParam(r, "category")
	readiness := QueryParam(r, "readiness")
	filtered := filterSurfaceRecords(inventory.Surfaces, category, readiness)

	total := len(filtered)
	page, truncated := pageSurfaceRecords(filtered, offset, limit)

	truth := BuildTruthEnvelope(h.profile(), surfaceInventoryCapability, TruthBasisRuntimeState,
		"embedded, generated surface inventory; no live backend read")
	truth.Freshness = TruthFreshness{State: FreshnessFresh}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"version":   inventory.Version,
		"surfaces":  page,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
		"truncated": truncated,
	}, truth)
}

// filterSurfaceRecords returns records matching the optional category and
// readiness filters, preserving the inventory's deterministic order.
func filterSurfaceRecords(records []capabilitycatalog.SurfaceRecord, category, readiness string) []capabilitycatalog.SurfaceRecord {
	if category == "" && readiness == "" {
		return records
	}
	out := make([]capabilitycatalog.SurfaceRecord, 0, len(records))
	for _, rec := range records {
		if category != "" && string(rec.Category) != category {
			continue
		}
		if readiness != "" && string(rec.Readiness) != readiness {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// pageSurfaceRecords applies offset and limit and reports whether more records
// remain past the returned page.
func pageSurfaceRecords(records []capabilitycatalog.SurfaceRecord, offset, limit int) ([]capabilitycatalog.SurfaceRecord, bool) {
	if offset >= len(records) {
		return []capabilitycatalog.SurfaceRecord{}, false
	}
	end := offset + limit
	truncated := end < len(records)
	if end > len(records) {
		end = len(records)
	}
	return records[offset:end], truncated
}
