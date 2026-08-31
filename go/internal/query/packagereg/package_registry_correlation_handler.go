// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PackageRegistryCorrelationResult is one reducer-owned package ownership,
// publication, or consumption correlation read from durable reducer facts.
type PackageRegistryCorrelationResult struct {
	CorrelationID          string   `json:"correlation_id"`
	RelationshipKind       string   `json:"relationship_kind"`
	PackageID              string   `json:"package_id"`
	VersionID              string   `json:"version_id,omitempty"`
	Version                string   `json:"version,omitempty"`
	PublishedAt            string   `json:"published_at,omitempty"`
	Ecosystem              string   `json:"ecosystem,omitempty"`
	PackageName            string   `json:"package_name,omitempty"`
	RepositoryID           string   `json:"repository_id,omitempty"`
	RepositoryName         string   `json:"repository_name,omitempty"`
	SourceURL              string   `json:"source_url,omitempty"`
	CandidateRepositoryIDs []string `json:"candidate_repository_ids,omitempty"`
	RelativePath           string   `json:"relative_path,omitempty"`
	ManifestSection        string   `json:"manifest_section,omitempty"`
	DependencyRange        string   `json:"dependency_range,omitempty"`
	Outcome                string   `json:"outcome"`
	Reason                 string   `json:"reason,omitempty"`
	ProvenanceOnly         bool     `json:"provenance_only"`
	CanonicalWrites        int      `json:"canonical_writes"`
	EvidenceFactIDs        []string `json:"evidence_fact_ids,omitempty"`
}

func (h *PackageRegistryHandler) listCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryCorrelations,
		"GET /api/v0/package-registry/correlations",
		packageRegistryCorrelationsCapability,
	)
	defer span.End()

	if h.unsupported(w, r, packageRegistryCorrelationsCapability) {
		return
	}
	limit, ok := requiredPackageRegistryLimit(w, r)
	if !ok {
		return
	}
	packageID := querycontract.QueryParam(r, "package_id")
	repositorySelector := querycontract.QueryParam(r, "repository_id")
	if packageID == "" && repositorySelector == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "package_id or repository_id is required")
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyPackageRegistryCorrelationPage(w, r, limit)
		return
	}
	repositoryID, ok := queryselector.ResolveForRequestWithAccess(
		w,
		r,
		h.Neo4j,
		h.Content,
		repositorySelector,
		access,
		packageRegistryCorrelationsCapability,
	)
	if !ok {
		return
	}
	if h.Correlations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry correlations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			packageRegistryCorrelationsCapability,
			h.profile(),
			querycontract.RequiredProfile(packageRegistryCorrelationsCapability),
		)
		return
	}

	filter := PackageRegistryCorrelationFilter{
		PackageID:          packageID,
		RepositoryID:       repositoryID,
		RelationshipKind:   querycontract.QueryParam(r, "relationship_kind"),
		AfterCorrelationID: querycontract.QueryParam(r, "after_correlation_id"),
		Limit:              limit,
	}
	filter = packageRegistryCorrelationFilterWithRepositoryAccess(filter, access)
	page, err := h.Correlations.ListPackageRegistryCorrelations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Truncated and the next cursor come from page (derived from the raw
	// fetched fact count), never from len(page.Rows): a fact dropped mid-page
	// by a failed typed decode must not make a truncated page look complete or
	// hide the correlations beyond it (#5816 finding on #5461).
	results := make([]PackageRegistryCorrelationResult, 0, len(page.Rows))
	for _, row := range page.Rows {
		results = append(results, PackageRegistryCorrelationResult(row))
	}
	body := map[string]any{
		"correlations": results,
		"count":        len(results),
		"limit":        limit,
		"truncated":    page.Truncated,
	}
	if page.Truncated && page.NextCursorCorrelationID != "" {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": page.NextCursorCorrelationID,
		}
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, len(results), page.Truncated)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		packageRegistryCorrelationsCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned package ownership, publication, and consumption correlation facts",
	))
}

func (h *PackageRegistryHandler) writeEmptyPackageRegistryCorrelationPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	body := map[string]any{
		"correlations": []PackageRegistryCorrelationResult{},
		"count":        0,
		"limit":        limit,
		"truncated":    false,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, 0, false)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		packageRegistryCorrelationsCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned package ownership, publication, and consumption correlation facts",
	))
}

func packageRegistryCorrelationFilterWithRepositoryAccess(
	filter PackageRegistryCorrelationFilter,
	access querycontract.RepositoryAccessFilter,
) PackageRegistryCorrelationFilter {
	if !access.Scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter
}
