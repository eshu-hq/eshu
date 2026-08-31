// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PackageDependencyChainPublisherResult is the wire shape of one provenance-only
// publisher leg. provenance_only is always true for these legs today and is
// surfaced explicitly so the console renders them as inferred links rather than
// asserted (exact) repository edges.
type PackageDependencyChainPublisherResult struct {
	CorrelationID    string `json:"correlation_id"`
	RelationshipKind string `json:"relationship_kind"`
	RepositoryID     string `json:"repository_id"`
	RepositoryName   string `json:"repository_name,omitempty"`
	SourceURL        string `json:"source_url,omitempty"`
	Outcome          string `json:"outcome,omitempty"`
	Reason           string `json:"reason,omitempty"`
	ProvenanceOnly   bool   `json:"provenance_only"`
	CanonicalWrites  int    `json:"canonical_writes"`
}

// PackageDependencyChainResult is the wire shape of one consumer-repo ->
// package -> publisher-repo chain. The consumption leg carries its own
// provenance_only/canonical_writes so the canonical consumption truth and the
// inferred publisher truth stay structurally distinct.
type PackageDependencyChainResult struct {
	ConsumerRepositoryID      string                                  `json:"consumer_repository_id"`
	ConsumerRepositoryName    string                                  `json:"consumer_repository_name,omitempty"`
	PackageID                 string                                  `json:"package_id"`
	PackageName               string                                  `json:"package_name,omitempty"`
	Ecosystem                 string                                  `json:"ecosystem,omitempty"`
	DependencyRange           string                                  `json:"dependency_range,omitempty"`
	ConsumptionCorrelationID  string                                  `json:"consumption_correlation_id"`
	ConsumptionProvenanceOnly bool                                    `json:"consumption_provenance_only"`
	ConsumptionCanonicalWrite int                                     `json:"consumption_canonical_writes"`
	Publishers                []PackageDependencyChainPublisherResult `json:"publishers"`
	Ambiguous                 bool                                    `json:"ambiguous"`
}

func (h *PackageRegistryHandler) listDependencyChains(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryDependencyChains,
		"GET /api/v0/package-registry/dependency-chains",
		packageRegistryDependencyChainsCapability,
	)
	defer span.End()

	if h.unsupported(w, r, packageRegistryDependencyChainsCapability) {
		return
	}
	limit, ok := requiredPackageRegistryLimit(w, r)
	if !ok {
		return
	}
	repositorySelector := querycontract.QueryParam(r, "repository_id")
	if repositorySelector == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "repository_id is required")
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyPackageDependencyChainPage(w, r, limit)
		return
	}
	repositoryID, ok := queryselector.ResolveForRequestWithAccess(
		w,
		r,
		h.Neo4j,
		h.Content,
		repositorySelector,
		access,
		packageRegistryDependencyChainsCapability,
	)
	if !ok {
		return
	}
	if h.Correlations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package dependency chains require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			packageRegistryDependencyChainsCapability,
			h.profile(),
			querycontract.RequiredProfile(packageRegistryDependencyChainsCapability),
		)
		return
	}

	afterCorrelationID := querycontract.QueryParam(r, "after_correlation_id")
	req := PackageDependencyChainRequest{
		RepositoryID:       repositoryID,
		AfterCorrelationID: afterCorrelationID,
		Limit:              limit,
	}
	if access.Scoped() {
		req.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
		req.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	}
	page, err := ResolvePackageDependencyChains(r.Context(), h.Correlations, req)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Truncated and the next cursor come from page (derived from the raw
	// fetched consumption fact count), never from len(page.Chains): a
	// malformed or unsupported-version consumption fact dropped mid-page must
	// not make a truncated page look complete or hide the chains beyond it
	// (#5816 finding on #5461). publishers_truncated is a separate,
	// additional signal about the phase-2 batched publisher leg -- it must
	// never be folded into truncated, which describes only the phase-1
	// consumption page.
	results := make([]PackageDependencyChainResult, 0, len(page.Chains))
	for _, chain := range page.Chains {
		results = append(results, packageDependencyChainResult(chain))
	}
	body := map[string]any{
		"chains":               results,
		"repository_id":        repositoryID,
		"count":                len(results),
		"limit":                limit,
		"truncated":            page.Truncated,
		"publishers_truncated": page.PublishersTruncated,
	}
	if page.Truncated && page.NextCursorCorrelationID != "" {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": page.NextCursorCorrelationID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		packageRegistryDependencyChainsCapability,
		querycontract.TruthBasisSemanticFacts,
		"canonical consumption correlations joined with provenance-only publisher correlations; publisher legs are inferred, not asserted repository edges",
	))
}

func (h *PackageRegistryHandler) writeEmptyPackageDependencyChainPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"chains":               []PackageDependencyChainResult{},
		"count":                0,
		"limit":                limit,
		"truncated":            false,
		"publishers_truncated": false,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		packageRegistryDependencyChainsCapability,
		querycontract.TruthBasisSemanticFacts,
		"canonical consumption correlations joined with provenance-only publisher correlations; publisher legs are inferred, not asserted repository edges",
	))
}

func packageDependencyChainResult(chain PackageDependencyChain) PackageDependencyChainResult {
	publishers := make([]PackageDependencyChainPublisherResult, 0, len(chain.Publishers))
	for _, pub := range chain.Publishers {
		publishers = append(publishers, PackageDependencyChainPublisherResult(pub))
	}
	return PackageDependencyChainResult{
		ConsumerRepositoryID:      chain.ConsumerRepositoryID,
		ConsumerRepositoryName:    chain.ConsumerRepositoryName,
		PackageID:                 chain.PackageID,
		PackageName:               chain.PackageName,
		Ecosystem:                 chain.Ecosystem,
		DependencyRange:           chain.DependencyRange,
		ConsumptionCorrelationID:  chain.ConsumptionCorrelationID,
		ConsumptionProvenanceOnly: chain.ConsumptionProvenanceOnly,
		ConsumptionCanonicalWrite: chain.ConsumptionCanonicalWrite,
		Publishers:                publishers,
		Ambiguous:                 chain.Ambiguous,
	}
}
