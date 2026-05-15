package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PackageRegistryCorrelationResult is one reducer-owned package ownership or
// consumption correlation read from durable reducer facts.
type PackageRegistryCorrelationResult struct {
	CorrelationID    string   `json:"correlation_id"`
	RelationshipKind string   `json:"relationship_kind"`
	PackageID        string   `json:"package_id"`
	VersionID        string   `json:"version_id,omitempty"`
	Ecosystem        string   `json:"ecosystem,omitempty"`
	PackageName      string   `json:"package_name,omitempty"`
	RepositoryID     string   `json:"repository_id,omitempty"`
	RepositoryName   string   `json:"repository_name,omitempty"`
	SourceURL        string   `json:"source_url,omitempty"`
	RelativePath     string   `json:"relative_path,omitempty"`
	ManifestSection  string   `json:"manifest_section,omitempty"`
	DependencyRange  string   `json:"dependency_range,omitempty"`
	Outcome          string   `json:"outcome"`
	Reason           string   `json:"reason,omitempty"`
	ProvenanceOnly   bool     `json:"provenance_only"`
	CanonicalWrites  int      `json:"canonical_writes"`
	EvidenceFactIDs  []string `json:"evidence_fact_ids,omitempty"`
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
	packageID := QueryParam(r, "package_id")
	repositoryID := QueryParam(r, "repository_id")
	if packageID == "" && repositoryID == "" {
		WriteError(w, http.StatusBadRequest, "package_id or repository_id is required")
		return
	}
	if h.Correlations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry correlations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			packageRegistryCorrelationsCapability,
			h.profile(),
			requiredProfile(packageRegistryCorrelationsCapability),
		)
		return
	}

	rows, err := h.Correlations.ListPackageRegistryCorrelations(r.Context(), PackageRegistryCorrelationFilter{
		PackageID:          packageID,
		RepositoryID:       repositoryID,
		RelationshipKind:   QueryParam(r, "relationship_kind"),
		AfterCorrelationID: QueryParam(r, "after_correlation_id"),
		Limit:              limit + 1,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]PackageRegistryCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, PackageRegistryCorrelationResult(row))
	}
	body := map[string]any{
		"correlations": results,
		"count":        len(results),
		"limit":        limit,
		"truncated":    truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": results[len(results)-1].CorrelationID,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryCorrelationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned package ownership and consumption correlation facts",
	))
}
