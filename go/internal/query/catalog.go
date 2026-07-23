// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const (
	catalogCapability   = "platform_impact.catalog"
	defaultCatalogLimit = 2000
	maxCatalogLimit     = 5000
)

type catalogResponse struct {
	Repositories       []catalogRepository `json:"repositories"`
	Workloads          []catalogWorkload   `json:"workloads"`
	Services           []catalogWorkload   `json:"services"`
	Counts             map[string]int      `json:"counts"`
	Count              int                 `json:"count"`
	Limit              int                 `json:"limit"`
	Truncated          bool                `json:"truncated"`
	WorkloadsTruncated bool                `json:"workloads_truncated"`
	Limitations        []string            `json:"limitations,omitempty"`
}

type catalogRepository struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path,omitempty"`
	LocalPath    string `json:"local_path,omitempty"`
	RemoteURL    string `json:"remote_url,omitempty"`
	RepoSlug     string `json:"repo_slug,omitempty"`
	HasRemote    bool   `json:"has_remote"`
	IsDependency bool   `json:"is_dependency,omitempty"`
}

type catalogWorkload struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Kind                  string   `json:"kind"`
	RepoID                string   `json:"repo_id,omitempty"`
	RepoName              string   `json:"repo_name,omitempty"`
	Environments          []string `json:"environments,omitempty"`
	InstanceCount         int      `json:"instance_count"`
	MaterializationStatus string   `json:"materialization_status,omitempty"`
	// Tier, Category, Domain, and Language are declared in service-catalog
	// manifests (Backstage, Cortex, OpsLevel) and joined from reducer-owned
	// correlation facts when ServiceCatalogCorrelations is wired. Empty when
	// no correlated manifest is found for this workload or repository.
	Tier     string `json:"tier,omitempty"`
	Category string `json:"category,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Language string `json:"language,omitempty"`
}

type catalogWorkloadIdentityStore interface {
	ListWorkloadIdentities(ctx context.Context, limit int) ([]CatalogWorkloadIdentityEntry, bool, error)
}

// CatalogWorkloadIdentityEntry is a repository read-model workload handle.
type CatalogWorkloadIdentityEntry struct {
	Name     string
	RepoID   string
	RepoName string
}

// listCatalog returns bounded entity handles for the console catalog.
func (h *RepositoryHandler) listCatalog(w http.ResponseWriter, r *http.Request) {
	limit := catalogLimitFromRequest(r)
	response := catalogResponse{
		Repositories: []catalogRepository{},
		Workloads:    []catalogWorkload{},
		Services:     []catalogWorkload{},
		Limit:        limit,
		Counts:       map[string]int{},
	}

	if h == nil {
		response.finish()
		WriteSuccess(w, r, http.StatusOK, response, catalogTruth(ProfileProduction, TruthBasisHybrid))
		return
	}

	var err error
	if h.Neo4j == nil {
		response.Repositories, response.Truncated, err = h.listCatalogRepositoriesFromContent(r.Context(), limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
		response.Limitations = append(response.Limitations, "workload and service catalog rows require an authoritative graph backend")
		response.finish()
		WriteSuccess(w, r, http.StatusOK, response, catalogTruth(h.profile(), TruthBasisContentIndex))
		return
	}

	response.Repositories, response.Truncated, err = h.listCatalogRepositoriesFromGraph(r.Context(), limit)
	if err != nil {
		if WriteGraphReadError(w, r, err, catalogCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repositories: %v", err))
		return
	}
	response.Workloads, response.WorkloadsTruncated, err = h.listCatalogWorkloads(r.Context(), limit)
	if err != nil {
		if WriteGraphReadError(w, r, err, catalogCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query workloads: %v", err))
		return
	}
	response.Truncated = response.Truncated || response.WorkloadsTruncated
	// Enrich workloads with tier, category, domain, and language from
	// service-catalog correlation facts. The lookup is bounded to the repo IDs
	// assembled above; a nil store or empty id set is a no-op so the response
	// degrades gracefully when Postgres is unavailable.
	response.Workloads = h.enrichCatalogWorkloadsFromCorrelations(r.Context(), response.Workloads)
	response.Services = serviceCatalogRows(response.Workloads)
	response.finish()

	WriteSuccess(w, r, http.StatusOK, response, catalogTruth(h.profile(), TruthBasisAuthoritativeGraph))
}

func (h *RepositoryHandler) listCatalogRepositoriesFromGraph(
	ctx context.Context,
	limit int,
) ([]catalogRepository, bool, error) {
	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		RETURN %s, %s
		ORDER BY r.name, r.id
		LIMIT $limit
	`, RepoProjection("r"), repositoryDependencyMarkerProjection("r", repositoryAccessFilter{allScopes: true}))
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{"limit": limit + 1})
	if err != nil {
		return nil, false, err
	}
	rows, truncated := trimCatalogRows(rows, limit)
	repositories := make([]catalogRepository, 0, len(rows))
	for _, row := range rows {
		repositories = append(repositories, catalogRepositoryFromRow(row))
	}
	return repositories, truncated, nil
}

func (h *RepositoryHandler) listCatalogRepositoriesFromContent(
	ctx context.Context,
	limit int,
) ([]catalogRepository, bool, error) {
	repositories, err := h.listRepositoriesFromContent(ctx)
	if err != nil {
		return nil, false, err
	}
	truncated := len(repositories) > limit
	if truncated {
		repositories = repositories[:limit]
	}
	rows := make([]catalogRepository, 0, len(repositories))
	for _, repository := range repositories {
		rows = append(rows, catalogRepositoryFromRow(repository))
	}
	return rows, truncated, nil
}

func (h *RepositoryHandler) listCatalogWorkloads(
	ctx context.Context,
	limit int,
) ([]catalogWorkload, bool, error) {
	graphWorkloads, graphTruncated, err := h.listCatalogWorkloadsFromGraph(ctx, limit)
	if err != nil {
		return nil, false, err
	}
	identityWorkloads, identityTruncated, err := h.listCatalogWorkloadIdentitiesFromContent(ctx, limit)
	if err != nil {
		return nil, false, err
	}
	merged, mergeTruncated := mergeCatalogWorkloads(graphWorkloads, identityWorkloads, limit)
	return merged, graphTruncated || identityTruncated || mergeTruncated, nil
}

func (h *RepositoryHandler) listCatalogWorkloadsFromGraph(
	ctx context.Context,
	limit int,
) ([]catalogWorkload, bool, error) {
	return h.assembleCatalogWorkloadsFromGraph(ctx, limit)
}

func (h *RepositoryHandler) listCatalogWorkloadIdentitiesFromContent(
	ctx context.Context,
	limit int,
) ([]catalogWorkload, bool, error) {
	store, ok := h.Content.(catalogWorkloadIdentityStore)
	if !ok {
		return nil, false, nil
	}
	identities, truncated, err := store.ListWorkloadIdentities(ctx, limit)
	if err != nil {
		return nil, false, err
	}
	workloads := make([]catalogWorkload, 0, len(identities))
	for _, identity := range identities {
		if strings.TrimSpace(identity.Name) == "" {
			continue
		}
		workloads = append(workloads, catalogWorkload{
			ID:                    "workload:" + strings.TrimPrefix(identity.Name, "workload:"),
			Name:                  strings.TrimPrefix(identity.Name, "workload:"),
			Kind:                  "service",
			RepoID:                identity.RepoID,
			RepoName:              identity.RepoName,
			MaterializationStatus: "identity_only",
		})
	}
	return workloads, truncated, nil
}

// enrichCatalogWorkloadsFromCorrelations joins service-catalog correlation facts
// onto the assembled workload set. It collects the non-empty repo IDs from
// workloads, issues a single bounded Postgres query filtered to those IDs via
// AllowedRepositoryIDs, then stamps Tier/Category/Domain/Language on each
// workload whose repo has a correlated manifest. The lookup is capped at
// serviceCatalogCorrelationMaxLimit to stay within the few-seconds SLA; extra
// workloads beyond that cap keep their zero values. Failures are swallowed so
// the catalog endpoint never errors on a missing or unavailable store.
func (h *RepositoryHandler) enrichCatalogWorkloadsFromCorrelations(
	ctx context.Context,
	workloads []catalogWorkload,
) []catalogWorkload {
	if h.ServiceCatalogCorrelations == nil || len(workloads) == 0 {
		return workloads
	}

	// Collect unique non-empty repo IDs for the bounded Postgres predicate.
	repoIDs := make([]string, 0, len(workloads))
	seen := make(map[string]struct{}, len(workloads))
	for _, w := range workloads {
		if w.RepoID != "" {
			if _, ok := seen[w.RepoID]; !ok {
				seen[w.RepoID] = struct{}{}
				repoIDs = append(repoIDs, w.RepoID)
			}
		}
	}
	if len(repoIDs) == 0 {
		return workloads
	}

	rows, err := h.ServiceCatalogCorrelations.ListServiceCatalogCorrelations(ctx, ServiceCatalogCorrelationFilter{
		AllowedRepositoryIDs: repoIDs,
		Limit:                serviceCatalogCorrelationMaxLimit,
	})
	if err != nil {
		// Non-fatal: catalog still returns graph data; enrichment is best-effort.
		return workloads
	}

	// Index correlations by repository_id for O(1) join. When multiple
	// correlations exist for the same repo, first-wins preserves the
	// highest-confidence declaration (facts are ordered by fact_id ASC which
	// corresponds to insertion time).
	byRepo := make(map[string]ServiceCatalogCorrelationRow, len(rows))
	for _, row := range rows {
		if row.RepositoryID != "" {
			if _, exists := byRepo[row.RepositoryID]; !exists {
				byRepo[row.RepositoryID] = row
			}
		}
	}

	// Stamp matching enrichment fields onto each workload.
	for i, w := range workloads {
		corr, ok := byRepo[w.RepoID]
		if !ok {
			continue
		}
		workloads[i].Tier = corr.Tier
		// EntityType carries the service-catalog category (e.g. "service",
		// "library", "website") as declared in the manifest.
		workloads[i].Category = corr.EntityType
		// OwnerRef carries the team/domain owner slug from the manifest; it
		// doubles as a domain label for the catalog view.
		workloads[i].Domain = corr.OwnerRef
	}
	return workloads
}

func (r *catalogResponse) finish() {
	r.Counts = map[string]int{
		"repositories": len(r.Repositories),
		"workloads":    len(r.Workloads),
		"services":     len(r.Services),
	}
	r.Count = len(r.Repositories) + len(r.Workloads)
}

func mergeCatalogWorkloads(
	graphWorkloads []catalogWorkload,
	identityWorkloads []catalogWorkload,
	limit int,
) ([]catalogWorkload, bool) {
	seen := make(map[string]struct{}, len(graphWorkloads))
	merged := make([]catalogWorkload, 0, len(graphWorkloads)+len(identityWorkloads))
	for _, workload := range graphWorkloads {
		seen[catalogWorkloadKey(workload)] = struct{}{}
		merged = append(merged, workload)
	}
	for _, workload := range identityWorkloads {
		key := catalogWorkloadKey(workload)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, workload)
	}
	if len(merged) <= limit {
		return merged, false
	}
	return merged[:limit], true
}

func catalogWorkloadKey(workload catalogWorkload) string {
	if workload.Name != "" {
		return strings.ToLower(workload.Name)
	}
	return strings.ToLower(strings.TrimPrefix(workload.ID, "workload:"))
}

func serviceCatalogRows(workloads []catalogWorkload) []catalogWorkload {
	services := make([]catalogWorkload, 0)
	for _, workload := range workloads {
		if normalizedCatalogWorkloadKind(workload.Kind) != "service" {
			continue
		}
		services = append(services, workload)
	}
	return services
}

func catalogLimitFromRequest(r *http.Request) int {
	limit := QueryParamInt(r, "limit", defaultCatalogLimit)
	if limit <= 0 {
		return defaultCatalogLimit
	}
	if limit > maxCatalogLimit {
		return maxCatalogLimit
	}
	return limit
}

func catalogTruth(profile QueryProfile, basis TruthBasis) *TruthEnvelope {
	return BuildTruthEnvelope(
		profile,
		catalogCapability,
		basis,
		"resolved from bounded repository and workload catalog handles",
	)
}

func catalogRepositoryFromRow(row map[string]any) catalogRepository {
	return catalogRepository{
		ID:           StringVal(row, "id"),
		Name:         StringVal(row, "name"),
		Path:         StringVal(row, "path"),
		LocalPath:    StringVal(row, "local_path"),
		RemoteURL:    StringVal(row, "remote_url"),
		RepoSlug:     StringVal(row, "repo_slug"),
		HasRemote:    BoolVal(row, "has_remote"),
		IsDependency: BoolVal(row, "is_dependency"),
	}
}

func trimCatalogRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

func normalizedCatalogWorkloadKind(kind string) string {
	normalized := strings.TrimSpace(strings.ToLower(kind))
	if normalized == "" {
		return "workload"
	}
	return normalized
}

// mergeCatalogEnvironments unions a workload's environment evidence from
// WorkloadInstance.environment and TARGETS_ENVIRONMENT deployment evidence into
// a deduplicated, deterministically ordered set. Both sources are derived from
// graph edges, so a workload that materializes no WorkloadInstance still
// reports the environments resolved through its repository's deployment
// evidence. An empty result means no environment edge exists; it is never
// fabricated from names.
func mergeCatalogEnvironments(sources ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, source := range sources {
		for _, value := range source {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			merged = append(merged, trimmed)
		}
	}
	sort.Strings(merged)
	return merged
}

func compactStrings(values []string) []string {
	compacted := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			compacted = append(compacted, trimmed)
		}
	}
	return compacted
}
