// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"

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
	packageID := QueryParam(r, "package_id")
	versionID := QueryParam(r, "version_id")
	if packageID == "" && versionID == "" {
		WriteError(w, http.StatusBadRequest, "package_id or version_id is required")
		return
	}
	afterVersionID := QueryParam(r, "after_version_id")
	afterDependencyID := QueryParam(r, "after_dependency_id")
	if (afterVersionID == "") != (afterDependencyID == "") {
		WriteError(w, http.StatusBadRequest, "after_version_id and after_dependency_id must be provided together")
		return
	}
	if packageRegistryDependenciesGate(w, r, h, span, packageID, versionID, limit) {
		return
	}

	cypher, params := packageRegistryDependenciesCypher(
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
		if WriteGraphReadError(w, r, err, packageRegistryDependenciesCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]PackageRegistryDependencyResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, PackageRegistryDependencyResult{
			DependencyID:         StringVal(row, "dependency_id"),
			SourcePackageID:      StringVal(row, "source_package_id"),
			SourceVersionID:      StringVal(row, "source_version_id"),
			Version:              StringVal(row, "version"),
			DependencyPackageID:  StringVal(row, "dependency_package_id"),
			DependencyEcosystem:  StringVal(row, "dependency_ecosystem"),
			DependencyRegistry:   StringVal(row, "dependency_registry"),
			DependencyNamespace:  StringVal(row, "dependency_namespace"),
			DependencyNormalized: StringVal(row, "dependency_normalized"),
			DependencyPURL:       StringVal(row, "dependency_purl"),
			DependencyBOMRef:     StringVal(row, "dependency_bom_ref"),
			DependencyManager:    StringVal(row, "dependency_manager"),
			DependencyRange:      StringVal(row, "dependency_range"),
			DependencyType:       StringVal(row, "dependency_type"),
			TargetFramework:      StringVal(row, "target_framework"),
			Marker:               StringVal(row, "marker"),
			Optional:             BoolVal(row, "optional"),
			Excluded:             BoolVal(row, "excluded"),
			SourceConfidence:     StringVal(row, "source_confidence"),
			CollectorKind:        StringVal(row, "collector_kind"),
			CollectorInstanceID:  StringVal(row, "collector_instance_id"),
			CorrelationAnchors:   StringSliceVal(row, "correlation_anchors"),
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryDependenciesCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package-native dependency graph nodes",
	))
}
