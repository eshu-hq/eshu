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
		impactRows, graphTruncated, err = h.changeSurfaceImpactRows(r.Context(), req, selected.ID)
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
	cypher := changeSurfaceResolverCypher(req.graphTargetType(), req.Limit+1)
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{"target": target})
	if err != nil {
		return nil, nil, fmt.Errorf("resolve change surface target: %w", err)
	}
	candidates := changeSurfaceCandidates(rows)
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
	switch {
	case totalCandidates == 0:
		return nil, resolution, nil
	case totalCandidates == 1:
		resolution["status"] = "resolved"
		resolution["selected"] = candidates[0].Map()
		return &candidates[0], resolution, nil
	default:
		resolution["status"] = "ambiguous"
		return nil, resolution, nil
	}
}

func changeSurfaceResolverCypher(targetType string, limit int) string {
	switch targetType {
	case "service", "workload":
		return fmt.Sprintf(`MATCH (n:Workload {id: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 0 as rank
UNION
MATCH (n:Workload {name: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 1 as rank
ORDER BY rank, name, id
LIMIT %d`, limit)
	case "workload_instance":
		return fmt.Sprintf(`MATCH (n:WorkloadInstance {id: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.workload_id as repo_id, n.environment as environment, 0 as rank
UNION
MATCH (n:WorkloadInstance {workload_id: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.workload_id as repo_id, n.environment as environment, 1 as rank
ORDER BY rank, name, id
LIMIT %d`, limit)
	case "repository", "repo":
		return fmt.Sprintf(`MATCH (n:Repository {id: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.id as repo_id, n.environment as environment, 0 as rank
UNION
MATCH (n:Repository {name: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.id as repo_id, n.environment as environment, 1 as rank
ORDER BY rank, name, id
LIMIT %d`, limit)
	case "resource", "cloud_resource":
		return fmt.Sprintf(`MATCH (n:CloudResource {id: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 0 as rank
UNION
MATCH (n:CloudResource {resource_id: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 1 as rank
UNION
MATCH (n:CloudResource {name: $target})
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 2 as rank
ORDER BY rank, name, id
LIMIT %d`, limit)
	case "terraform_module", "module":
		return fmt.Sprintf(`MATCH (n:TerraformModule {uid: $target})
RETURN n.uid as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 0 as rank
UNION
MATCH (n:TerraformModule {name: $target})
RETURN n.uid as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 1 as rank
ORDER BY rank, name, id
LIMIT %d`, limit)
	default:
		return fmt.Sprintf(`MATCH (n) WHERE n.id = $target
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, 0 as rank
ORDER BY rank, name, id
LIMIT %d`, limit)
	}
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
