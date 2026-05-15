package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const (
	catalogCapability   = "platform_impact.catalog"
	defaultCatalogLimit = 2000
	maxCatalogLimit     = 5000
)

type catalogResponse struct {
	Repositories []catalogRepository `json:"repositories"`
	Workloads    []catalogWorkload   `json:"workloads"`
	Services     []catalogWorkload   `json:"services"`
	Counts       map[string]int      `json:"counts"`
	Count        int                 `json:"count"`
	Limit        int                 `json:"limit"`
	Truncated    bool                `json:"truncated"`
	Limitations  []string            `json:"limitations,omitempty"`
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
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repositories: %v", err))
		return
	}
	var workloadsTruncated bool
	response.Workloads, workloadsTruncated, err = h.listCatalogWorkloads(r.Context(), limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query workloads: %v", err))
		return
	}
	response.Truncated = response.Truncated || workloadsTruncated
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
		RETURN %s, coalesce(r.is_dependency, false) as is_dependency
		ORDER BY r.name, r.id
		LIMIT $limit
	`, RepoProjection("r"))
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
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (w:Workload)
		OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(w)
		OPTIONAL MATCH (inst:WorkloadInstance)-[:INSTANCE_OF]->(w)
		RETURN w.id AS id,
		       w.name AS name,
		       coalesce(w.kind, 'workload') AS kind,
		       collect(DISTINCT repo.id) AS repo_ids,
		       collect(DISTINCT repo.name) AS repo_names,
		       collect(DISTINCT inst.environment) AS environments,
		       count(DISTINCT inst) AS instance_count
		ORDER BY name, id
		LIMIT $limit
	`, map[string]any{"limit": limit + 1})
	if err != nil {
		return nil, false, err
	}
	rows, truncated := trimCatalogRows(rows, limit)
	workloads := make([]catalogWorkload, 0, len(rows))
	for _, row := range rows {
		workload := catalogWorkload{
			ID:            StringVal(row, "id"),
			Name:          StringVal(row, "name"),
			Kind:          normalizedCatalogWorkloadKind(StringVal(row, "kind")),
			RepoID:        firstNonEmptyCatalogValue(StringSliceVal(row, "repo_ids"), StringVal(row, "repo_id")),
			RepoName:      firstNonEmptyCatalogValue(StringSliceVal(row, "repo_names"), StringVal(row, "repo_name")),
			Environments:  compactStrings(StringSliceVal(row, "environments")),
			InstanceCount: IntVal(row, "instance_count"),
		}
		if workload.Name == "" {
			workload.Name = workload.ID
		}
		workloads = append(workloads, workload)
	}
	return workloads, truncated, nil
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

func firstNonEmptyCatalogValue(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
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
