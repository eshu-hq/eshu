// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const packageRegistryAggregateCapability = "package_registry.packages.aggregate"

// packageRegistryAggregateRoutes registers the cheap-summary aggregate routes
// alongside the existing package registry list routes. Mount in
// package_registry.go invokes it.
func (h *PackageRegistryHandler) packageRegistryAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/package-registry/packages/count", h.countPackageRegistryPackages)
	mux.HandleFunc("GET /api/v0/package-registry/packages/inventory", h.packageRegistryPackageInventory)
}

func (h *PackageRegistryHandler) countPackageRegistryPackages(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryAggregate,
		"GET /api/v0/package-registry/packages/count",
		packageRegistryAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), packageRegistryAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"package registry aggregates require the authoritative graph",
			ErrorCodeUnsupportedCapability,
			packageRegistryAggregateCapability,
			h.profile(),
			requiredProfile(packageRegistryAggregateCapability),
		)
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry aggregates require the authoritative graph",
			ErrorCodeBackendUnavailable,
			packageRegistryAggregateCapability,
			h.profile(),
			requiredProfile(packageRegistryAggregateCapability),
		)
		return
	}

	filter := packageRegistryAggregateFilterFromRequest(r)
	if !validatePackageRegistryAggregateVisibility(w, filter) {
		return
	}
	filter, emptyResult := packageRegistryAggregateVisibilityGate(r.Context(), span, filter)
	if emptyResult {
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"total_packages": 0,
			"by_ecosystem":   map[string]int{},
			"scope":          packageRegistryAggregateScope(filter),
		}, BuildTruthEnvelope(
			h.profile(),
			packageRegistryAggregateCapability,
			TruthBasisAuthoritativeGraph,
			"resolved from the authoritative (:Package) corpus; per-ecosystem rollup uses the package_ecosystem graph index",
		))
		return
	}
	count, err := h.Aggregates.CountPackageRegistryPackages(r.Context(), filter)
	if err != nil {
		if WriteGraphReadError(w, r, err, packageRegistryAggregateCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_packages": count.TotalPackages,
		"by_ecosystem":   count.ByEcosystem,
		"scope":          packageRegistryAggregateScope(filter),
	}, BuildTruthEnvelope(
		h.profile(),
		packageRegistryAggregateCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from the authoritative (:Package) corpus; per-ecosystem rollup uses the package_ecosystem graph index",
	))
}

func (h *PackageRegistryHandler) packageRegistryPackageInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryAggregate,
		"GET /api/v0/package-registry/packages/inventory",
		packageRegistryAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), packageRegistryAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"package registry aggregates require the authoritative graph",
			ErrorCodeUnsupportedCapability,
			packageRegistryAggregateCapability,
			h.profile(),
			requiredProfile(packageRegistryAggregateCapability),
		)
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry aggregates require the authoritative graph",
			ErrorCodeBackendUnavailable,
			packageRegistryAggregateCapability,
			h.profile(),
			requiredProfile(packageRegistryAggregateCapability),
		)
		return
	}

	dimension := PackageRegistryInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = PackageRegistryInventoryByEcosystem
	}
	if !isSupportedPackageRegistryInventoryDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of ecosystem, registry, namespace, package_manager, visibility")
		return
	}
	limit, ok := parsePackageRegistryAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parsePackageRegistryAggregateOffset(w, r)
	if !ok {
		return
	}
	filter := packageRegistryAggregateFilterFromRequest(r)
	if !validatePackageRegistryAggregateVisibility(w, filter) {
		return
	}
	filter, emptyResult := packageRegistryAggregateVisibilityGate(r.Context(), span, filter)
	if emptyResult {
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"buckets":     []PackageRegistryInventoryRow{},
			"count":       0,
			"limit":       limit,
			"offset":      offset,
			"group_by":    string(dimension),
			"truncated":   false,
			"next_offset": nil,
			"scope":       packageRegistryAggregateScope(filter),
		}, BuildTruthEnvelope(
			h.profile(),
			packageRegistryAggregateCapability,
			TruthBasisAuthoritativeGraph,
			"resolved from the authoritative (:Package) corpus; one grouped bucket per row, ordered by count desc",
		))
		return
	}

	rows, err := h.Aggregates.PackageRegistryPackageInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		if WriteGraphReadError(w, r, err, packageRegistryAggregateCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"buckets":     rows,
		"count":       len(rows),
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   truncated,
		"next_offset": nextPackageRegistryAggregateOffset(offset, limit, truncated),
		"scope":       packageRegistryAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryAggregateCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from the authoritative (:Package) corpus; one grouped bucket per row, ordered by count desc",
	))
}

func packageRegistryAggregateFilterFromRequest(r *http.Request) PackageRegistryAggregateFilter {
	return PackageRegistryAggregateFilter{
		Ecosystem:      QueryParam(r, "ecosystem"),
		Registry:       QueryParam(r, "registry"),
		Namespace:      QueryParam(r, "namespace"),
		PackageManager: QueryParam(r, "package_manager"),
		Visibility:     QueryParam(r, "visibility"),
	}
}

func packageRegistryAggregateScope(filter PackageRegistryAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.Ecosystem != "" {
		out["ecosystem"] = filter.Ecosystem
	}
	if filter.Registry != "" {
		out["registry"] = filter.Registry
	}
	if filter.Namespace != "" {
		out["namespace"] = filter.Namespace
	}
	if filter.PackageManager != "" {
		out["package_manager"] = filter.PackageManager
	}
	if filter.Visibility != "" {
		out["visibility"] = filter.Visibility
	}
	return out
}

func isSupportedPackageRegistryInventoryDimension(d PackageRegistryInventoryDimension) bool {
	switch d {
	case PackageRegistryInventoryByEcosystem,
		PackageRegistryInventoryByRegistry,
		PackageRegistryInventoryByNamespace,
		PackageRegistryInventoryByPackageManager,
		PackageRegistryInventoryByVisibility:
		return true
	default:
		return false
	}
}

// validatePackageRegistryAggregateVisibility rejects out-of-contract
// visibility filters with a 400 so a typo does not silently return zero
// counts. The closed enum matches the ingestion contract emitted by
// `parseVisibility` in `go/internal/collector/packageregistry/`: a missing
// or unrecognized value normalizes to `unknown`, so the aggregate must
// accept `unknown` to let callers filter to the unresolved slice.
func validatePackageRegistryAggregateVisibility(w http.ResponseWriter, filter PackageRegistryAggregateFilter) bool {
	if filter.Visibility == "" {
		return true
	}
	switch filter.Visibility {
	case "public", "private", "unknown":
		return true
	default:
		WriteError(w, http.StatusBadRequest, "visibility must be one of public, private, unknown")
		return false
	}
}

const (
	packageRegistryAggregateDefaultLimit = 100
	packageRegistryAggregateMinLimit     = 1
	// packageRegistryAggregateMaxOffset matches the OpenAPI offset bound
	// and keeps SKIP scans bounded. Past this point callers should narrow
	// scope (ecosystem, registry, package_manager) or fall back to the
	// list endpoint with anchored pagination.
	packageRegistryAggregateMaxOffset = 10000
)

func parsePackageRegistryAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return packageRegistryAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < packageRegistryAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > PackageRegistryAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parsePackageRegistryAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > packageRegistryAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextPackageRegistryAggregateOffset returns the next offset when a truncated
// page can be continued without exceeding the documented offset bound, and
// nil otherwise. Callers serialize the nil as JSON null so generated clients
// see a clean end-of-stream marker.
func nextPackageRegistryAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > packageRegistryAggregateMaxOffset {
		return nil
	}
	return next
}
