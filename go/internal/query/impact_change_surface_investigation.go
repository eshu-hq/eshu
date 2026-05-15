package query

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	changeSurfaceInvestigationCapability   = "platform_impact.change_surface"
	changeSurfaceInvestigationDefaultLimit = 25
	changeSurfaceInvestigationMaxLimit     = 100
	changeSurfaceInvestigationMaxOffset    = 10000
	changeSurfaceInvestigationDefaultDepth = 4
	changeSurfaceInvestigationMaxDepth     = 8
)

type changeSurfaceInvestigationRequest struct {
	Target       string   `json:"target"`
	TargetType   string   `json:"target_type"`
	ServiceName  string   `json:"service_name"`
	WorkloadID   string   `json:"workload_id"`
	ResourceID   string   `json:"resource_id"`
	ModuleID     string   `json:"module_id"`
	RepoID       string   `json:"repo_id"`
	Topic        string   `json:"topic"`
	Query        string   `json:"query"`
	ChangedPaths []string `json:"changed_paths"`
	Environment  string   `json:"environment"`
	MaxDepth     int      `json:"max_depth"`
	Limit        int      `json:"limit"`
	Offset       int      `json:"offset"`
}

type changeSurfaceTargetCandidate struct {
	ID          string
	Name        string
	Labels      []string
	RepoID      string
	Environment string
	Rank        int
}

type changeSurfaceResolverQuery struct {
	cypher string
	params map[string]any
}

func (h *ImpactHandler) investigateChangeSurface(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryChangeSurfaceInvestigation,
		"POST /api/v0/impact/change-surface/investigate",
		changeSurfaceInvestigationCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), changeSurfaceInvestigationCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"change surface investigation requires authoritative platform truth",
			ErrorCodeUnsupportedCapability,
			changeSurfaceInvestigationCapability,
			h.profile(),
			requiredProfile(changeSurfaceInvestigationCapability),
		)
		return
	}

	var req changeSurfaceInvestigationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.normalize(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	codeSurface, err := h.changeSurfaceCodeSurface(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	var selected *changeSurfaceTargetCandidate
	resolution := changeSurfaceNoTargetResolution(req)
	if req.graphTarget() != "" {
		selected, resolution, err = h.resolveChangeSurfaceTarget(r.Context(), req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if selected == nil {
			resp := h.changeSurfaceResponse(req, resolution, codeSurface, nil, false)
			WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
				h.profile(),
				changeSurfaceInvestigationCapability,
				TruthBasisHybrid,
				"resolved target ambiguity before graph traversal",
			))
			return
		}
	}

	impactRows := []map[string]any(nil)
	graphTruncated := false
	if selected != nil {
		impactRows, graphTruncated, err = h.changeSurfaceImpactRows(r.Context(), req, *selected)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	resp := h.changeSurfaceResponse(req, resolution, codeSurface, impactRows, graphTruncated)
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
		h.profile(),
		changeSurfaceInvestigationCapability,
		TruthBasisHybrid,
		"resolved from bounded target resolution, content handles, and graph impact traversal",
	))
}

func (r *changeSurfaceInvestigationRequest) normalize() error {
	r.Target = strings.TrimSpace(r.Target)
	r.TargetType = normalizeChangeSurfaceTargetType(r.TargetType)
	r.ServiceName = strings.TrimSpace(r.ServiceName)
	r.WorkloadID = strings.TrimSpace(r.WorkloadID)
	r.ResourceID = strings.TrimSpace(r.ResourceID)
	r.ModuleID = strings.TrimSpace(r.ModuleID)
	r.RepoID = strings.TrimSpace(r.RepoID)
	r.Topic = strings.TrimSpace(r.topic())
	r.Environment = strings.TrimSpace(r.Environment)
	r.ChangedPaths = normalizeChangedPaths(r.ChangedPaths)
	if r.Limit <= 0 {
		r.Limit = changeSurfaceInvestigationDefaultLimit
	}
	if r.Limit > changeSurfaceInvestigationMaxLimit {
		r.Limit = changeSurfaceInvestigationMaxLimit
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > changeSurfaceInvestigationMaxOffset {
		return fmt.Errorf("offset must be <= %d", changeSurfaceInvestigationMaxOffset)
	}
	if r.MaxDepth <= 0 {
		r.MaxDepth = changeSurfaceInvestigationDefaultDepth
	}
	if r.MaxDepth > changeSurfaceInvestigationMaxDepth {
		r.MaxDepth = changeSurfaceInvestigationMaxDepth
	}
	if r.graphTarget() == "" && r.Topic == "" && len(r.ChangedPaths) == 0 {
		return fmt.Errorf("target, service_name, workload_id, resource_id, module_id, topic, or changed_paths is required")
	}
	if len(r.ChangedPaths) > 0 && r.RepoID == "" {
		return fmt.Errorf("repo_id is required when changed_paths are provided")
	}
	return nil
}

func (r changeSurfaceInvestigationRequest) topic() string {
	if r.Topic != "" {
		return r.Topic
	}
	return r.Query
}

func (r changeSurfaceInvestigationRequest) graphTarget() string {
	switch {
	case r.ServiceName != "":
		return r.ServiceName
	case r.WorkloadID != "":
		return r.WorkloadID
	case r.ResourceID != "":
		return r.ResourceID
	case r.ModuleID != "":
		return r.ModuleID
	default:
		return r.Target
	}
}

func (r changeSurfaceInvestigationRequest) graphTargetType() string {
	switch {
	case r.ServiceName != "":
		return "service"
	case r.WorkloadID != "":
		return "workload"
	case r.ResourceID != "":
		return "resource"
	case r.ModuleID != "":
		return "terraform_module"
	default:
		return r.TargetType
	}
}

func normalizeChangeSurfaceTargetType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "service", "workload", "workload_instance", "repository", "repo", "resource", "cloud_resource", "terraform_module", "module":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeChangedPaths(paths []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	slices.Sort(normalized)
	return normalized
}

func (h *ImpactHandler) resolveChangeSurfaceTarget(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) (*changeSurfaceTargetCandidate, map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil, fmt.Errorf("graph backend is unavailable")
	}
	target := req.graphTarget()
	candidates := make([]changeSurfaceTargetCandidate, 0, req.Limit+1)
	// Keep resolver probes separate so each graph read stays label/property
	// anchored and avoids backend-sensitive OR or UNION planning.
	for _, query := range changeSurfaceResolverQueries(req, req.Limit+1) {
		rows, err := h.Neo4j.Run(ctx, query.cypher, query.params)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve change surface target: %w", err)
		}
		candidates = appendChangeSurfaceCandidates(candidates, changeSurfaceCandidates(rows), req.Limit+1)
		if len(candidates) > 0 {
			break
		}
	}
	totalCandidates := len(candidates)
	truncated := len(candidates) > req.Limit
	if truncated {
		candidates = candidates[:req.Limit]
	}
	resolution := map[string]any{
		"input":       target,
		"target_type": req.graphTargetType(),
		"status":      "no_match",
		"candidates":  changeSurfaceCandidateMaps(candidates),
		"truncated":   truncated,
	}
	switch totalCandidates {
	case 0:
		return nil, resolution, nil
	case 1:
		resolution["status"] = "resolved"
		resolution["selected"] = candidates[0].Map()
		return &candidates[0], resolution, nil
	default:
		resolution["status"] = "ambiguous"
		return nil, resolution, nil
	}
}

func changeSurfaceResolverQueries(req changeSurfaceInvestigationRequest, limit int) []changeSurfaceResolverQuery {
	target := req.graphTarget()
	switch req.graphTargetType() {
	case "service", "workload":
		queries := []changeSurfaceResolverQuery{
			changeSurfaceWorkloadResolverQuery("id", target, 0, limit),
		}
		if canonicalID := canonicalWorkloadIDCandidate(target); canonicalID != target {
			queries = append(queries, changeSurfaceWorkloadResolverQuery("id", canonicalID, 1, limit))
		}
		queries = append(queries, changeSurfaceWorkloadResolverQuery("name", target, 2, limit))
		if req.RepoID != "" {
			queries = append(queries, changeSurfaceWorkloadResolverQuery("repo_id", req.RepoID, 3, limit))
		}
		return queries
	case "workload_instance":
		return []changeSurfaceResolverQuery{
			changeSurfaceWorkloadInstanceResolverQuery("id", target, 0, limit),
			changeSurfaceWorkloadInstanceResolverQuery("workload_id", target, 1, limit),
		}
	case "repository", "repo":
		return []changeSurfaceResolverQuery{
			changeSurfaceRepositoryResolverQuery("id", target, 0, limit),
			changeSurfaceRepositoryResolverQuery("name", target, 1, limit),
		}
	case "resource", "cloud_resource":
		return []changeSurfaceResolverQuery{
			changeSurfaceCloudResourceResolverQuery("id", target, 0, limit),
			changeSurfaceCloudResourceResolverQuery("resource_id", target, 1, limit),
			changeSurfaceCloudResourceResolverQuery("name", target, 2, limit),
		}
	case "terraform_module", "module":
		return []changeSurfaceResolverQuery{
			changeSurfaceTerraformModuleResolverQuery("uid", target, 0, limit),
			changeSurfaceTerraformModuleResolverQuery("name", target, 1, limit),
		}
	default:
		return changeSurfaceGenericResolverQueries(target, limit)
	}
}

func changeSurfaceGenericResolverQueries(target string, limit int) []changeSurfaceResolverQuery {
	queries := []changeSurfaceResolverQuery{
		changeSurfaceWorkloadResolverQuery("id", target, 0, limit),
	}
	if canonicalID := canonicalWorkloadIDCandidate(target); canonicalID != target {
		queries = append(queries, changeSurfaceWorkloadResolverQuery("id", canonicalID, 1, limit))
	}
	queries = append(queries,
		changeSurfaceWorkloadResolverQuery("name", target, 2, limit),
		changeSurfaceRepositoryResolverQuery("id", target, 3, limit),
		changeSurfaceRepositoryResolverQuery("name", target, 4, limit),
		changeSurfaceWorkloadInstanceResolverQuery("id", target, 5, limit),
		changeSurfaceWorkloadInstanceResolverQuery("workload_id", target, 6, limit),
		changeSurfaceCloudResourceResolverQuery("id", target, 7, limit),
		changeSurfaceCloudResourceResolverQuery("resource_id", target, 8, limit),
		changeSurfaceTerraformModuleResolverQuery("uid", target, 9, limit),
	)
	return queries
}

func canonicalWorkloadIDCandidate(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "workload:") {
		return target
	}
	return "workload:" + target
}

func changeSurfaceWorkloadResolverQuery(property string, target string, rank int, limit int) changeSurfaceResolverQuery {
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:Workload {%s: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, rank, limit),
		params: map[string]any{"target": target},
	}
}

func changeSurfaceWorkloadInstanceResolverQuery(property string, target string, rank int, limit int) changeSurfaceResolverQuery {
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:WorkloadInstance {%s: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, rank, limit),
		params: map[string]any{"target": target},
	}
}

func changeSurfaceRepositoryResolverQuery(property string, target string, rank int, limit int) changeSurfaceResolverQuery {
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:Repository {%s: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, rank, limit),
		params: map[string]any{"target": target},
	}
}

func changeSurfaceCloudResourceResolverQuery(property string, target string, rank int, limit int) changeSurfaceResolverQuery {
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:CloudResource {%s: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, rank, limit),
		params: map[string]any{"target": target},
	}
}

func changeSurfaceTerraformModuleResolverQuery(property string, target string, rank int, limit int) changeSurfaceResolverQuery {
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:TerraformModule {%s: $target})
RETURN n.uid as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, rank, limit),
		params: map[string]any{"target": target},
	}
}

func appendChangeSurfaceCandidates(
	existing []changeSurfaceTargetCandidate,
	incoming []changeSurfaceTargetCandidate,
	limit int,
) []changeSurfaceTargetCandidate {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, candidate := range existing {
		seen[candidate.ID] = struct{}{}
	}
	for _, candidate := range incoming {
		if _, ok := seen[candidate.ID]; ok {
			continue
		}
		seen[candidate.ID] = struct{}{}
		existing = append(existing, candidate)
		if limit > 0 && len(existing) >= limit {
			return existing
		}
	}
	return existing
}

func changeSurfaceCandidates(rows []map[string]any) []changeSurfaceTargetCandidate {
	candidates := make([]changeSurfaceTargetCandidate, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, changeSurfaceTargetCandidate{
			ID:          id,
			Name:        StringVal(row, "name"),
			Labels:      StringSliceVal(row, "labels"),
			RepoID:      StringVal(row, "repo_id"),
			Environment: StringVal(row, "environment"),
			Rank:        IntVal(row, "rank"),
		})
	}
	return candidates
}

func changeSurfaceCandidateMaps(candidates []changeSurfaceTargetCandidate) []map[string]any {
	values := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Map())
	}
	return values
}

func (c changeSurfaceTargetCandidate) Map() map[string]any {
	value := map[string]any{"id": c.ID, "name": c.Name, "labels": c.Labels}
	if c.RepoID != "" {
		value["repo_id"] = c.RepoID
	}
	if c.Environment != "" {
		value["environment"] = c.Environment
	}
	return value
}
