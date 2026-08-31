// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *PackageRegistryHandler) listDependencies(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryDependencies,
		"GET /api/v0/package-registry/dependencies",
		packageRegistryDependenciesCapability,
	)
	defer span.End()

	if h.unsupported(w, r, packageRegistryDependenciesCapability) {
		return
	}
	limit, ok := requiredPackageRegistryLimit(w, r)
	if !ok {
		return
	}
	packageID := querycontract.QueryParam(r, "package_id")
	versionID := querycontract.QueryParam(r, "version_id")
	if packageID == "" && versionID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "package_id or version_id is required")
		return
	}
	afterVersionID := querycontract.QueryParam(r, "after_version_id")
	afterDependencyID := querycontract.QueryParam(r, "after_dependency_id")
	if (afterVersionID == "") != (afterDependencyID == "") {
		querycontract.WriteError(w, http.StatusBadRequest, "after_version_id and after_dependency_id must be provided together")
		return
	}
	if packageRegistryDependenciesGate(w, r, h, span, packageID, versionID, limit) {
		return
	}

	cypher, params := PackageRegistryDependenciesCypher(
		packageID,
		versionID,
		afterVersionID,
		afterDependencyID,
		limit+1,
	)
	queryCtx, cancel := context.WithTimeout(r.Context(), packageRegistryDependencyReadTimeout)
	defer cancel()
	rows, err := h.Neo4j.Run(queryCtx, cypher, params)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, packageRegistryDependenciesCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]PackageRegistryDependencyResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, PackageRegistryDependencyResult{
			DependencyID:         querycontract.StringVal(row, "dependency_id"),
			SourcePackageID:      querycontract.StringVal(row, "source_package_id"),
			SourceVersionID:      querycontract.StringVal(row, "source_version_id"),
			Version:              querycontract.StringVal(row, "version"),
			DependencyPackageID:  querycontract.StringVal(row, "dependency_package_id"),
			DependencyEcosystem:  querycontract.StringVal(row, "dependency_ecosystem"),
			DependencyRegistry:   querycontract.StringVal(row, "dependency_registry"),
			DependencyNamespace:  querycontract.StringVal(row, "dependency_namespace"),
			DependencyNormalized: querycontract.StringVal(row, "dependency_normalized"),
			DependencyPURL:       querycontract.StringVal(row, "dependency_purl"),
			DependencyBOMRef:     querycontract.StringVal(row, "dependency_bom_ref"),
			DependencyManager:    querycontract.StringVal(row, "dependency_manager"),
			DependencyRange:      querycontract.StringVal(row, "dependency_range"),
			DependencyType:       querycontract.StringVal(row, "dependency_type"),
			TargetFramework:      querycontract.StringVal(row, "target_framework"),
			Marker:               querycontract.StringVal(row, "marker"),
			Optional:             querycontract.BoolVal(row, "optional"),
			Excluded:             querycontract.BoolVal(row, "excluded"),
			SourceConfidence:     querycontract.StringVal(row, "source_confidence"),
			CollectorKind:        querycontract.StringVal(row, "collector_kind"),
			CollectorInstanceID:  querycontract.StringVal(row, "collector_instance_id"),
			CorrelationAnchors:   querycontract.StringSliceVal(row, "correlation_anchors"),
		})
	}
	body := map[string]any{
		"dependencies": results,
		"count":        len(results),
		"limit":        limit,
		"truncated":    truncated,
	}
	if truncated && len(results) > 0 {
		last := results[len(results)-1]
		body["next_cursor"] = map[string]string{
			"after_version_id":    last.SourceVersionID,
			"after_dependency_id": last.DependencyID,
		}
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, len(results), truncated)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		packageRegistryDependenciesCapability,
		querycontract.TruthBasisAuthoritativeGraph,
		"resolved from package-native dependency graph nodes",
	))
}
