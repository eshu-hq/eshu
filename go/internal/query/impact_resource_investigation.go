package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	resourceInvestigationCapability   = "platform_impact.resource_investigation"
	resourceInvestigationDefaultLimit = 25
	resourceInvestigationMaxLimit     = 100
	resourceInvestigationDefaultDepth = 4
	resourceInvestigationMaxDepth     = 8
)

type resourceInvestigationRequest struct {
	Query        string `json:"query"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	Environment  string `json:"environment"`
	MaxDepth     int    `json:"max_depth"`
	Limit        int    `json:"limit"`
}

type resourceInvestigationCandidate struct {
	ID            string
	Name          string
	Labels        []string
	ResourceType  string
	Provider      string
	Environment   string
	RepoID        string
	ConfigPath    string
	Source        string
	ResourceID    string
	ResourceKind  string
	ResourceClass string
}

func (h *ImpactHandler) investigateResource(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryResourceInvestigation,
		"POST /api/v0/impact/resource-investigation",
		resourceInvestigationCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), resourceInvestigationCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"resource investigation requires authoritative platform truth",
			ErrorCodeUnsupportedCapability,
			resourceInvestigationCapability,
			h.profile(),
			requiredProfile(resourceInvestigationCapability),
		)
		return
	}

	var req resourceInvestigationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.normalize(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	selected, resolution, err := h.resolveResourceInvestigationTarget(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if selected == nil {
		resp := resourceInvestigationResponse(req, resolution, nil, nil, nil, nil, false)
		WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
			h.profile(),
			resourceInvestigationCapability,
			TruthBasisHybrid,
			"resolved resource ambiguity before graph traversal",
		))
		return
	}

	workloads, workloadsTruncated, incomingPaths, incomingTruncated, outgoingPaths, outgoingTruncated, err := h.loadResourceInvestigationSections(r.Context(), req, selected.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := workloadsTruncated || incomingTruncated || outgoingTruncated
	resp := resourceInvestigationResponse(req, resolution, selected, workloads, incomingPaths, outgoingPaths, truncated)
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
		h.profile(),
		resourceInvestigationCapability,
		TruthBasisHybrid,
		"resolved from bounded resource resolution, workload usage, and repository provenance paths",
	))
}

func (h *ImpactHandler) loadResourceInvestigationSections(
	ctx context.Context,
	req resourceInvestigationRequest,
	resourceID string,
) (
	[]map[string]any,
	bool,
	[]map[string]any,
	bool,
	[]map[string]any,
	bool,
	error,
) {
	var (
		workloads          []map[string]any
		workloadsTruncated bool
		incomingPaths      []map[string]any
		incomingTruncated  bool
		outgoingPaths      []map[string]any
		outgoingTruncated  bool
		wg                 sync.WaitGroup
	)
	errCh := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		var err error
		workloads, workloadsTruncated, err = h.resourceInvestigationWorkloads(ctx, req, resourceID)
		if err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		incomingPaths, incomingTruncated, err = h.resourceInvestigationRepoPaths(ctx, req, resourceID, "incoming")
		if err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		outgoingPaths, outgoingTruncated, err = h.resourceInvestigationRepoPaths(ctx, req, resourceID, "outgoing")
		if err != nil {
			errCh <- err
		}
	}()
	wg.Wait()
	close(errCh)
	var sectionErrs []error
	for err := range errCh {
		sectionErrs = append(sectionErrs, err)
	}
	if len(sectionErrs) > 0 {
		return nil, false, nil, false, nil, false, errors.Join(sectionErrs...)
	}
	return workloads, workloadsTruncated, incomingPaths, incomingTruncated, outgoingPaths, outgoingTruncated, nil
}

func (r *resourceInvestigationRequest) normalize() error {
	r.Query = strings.TrimSpace(r.Query)
	r.ResourceID = strings.TrimSpace(r.ResourceID)
	r.ResourceType = normalizeResourceInvestigationType(r.ResourceType)
	r.Environment = strings.TrimSpace(r.Environment)
	if r.Limit <= 0 {
		r.Limit = resourceInvestigationDefaultLimit
	}
	if r.Limit > resourceInvestigationMaxLimit {
		r.Limit = resourceInvestigationMaxLimit
	}
	if r.MaxDepth <= 0 {
		r.MaxDepth = resourceInvestigationDefaultDepth
	}
	if r.MaxDepth > resourceInvestigationMaxDepth {
		r.MaxDepth = resourceInvestigationMaxDepth
	}
	if r.selector() == "" {
		return fmt.Errorf("query or resource_id is required")
	}
	return nil
}

func (r resourceInvestigationRequest) selector() string {
	if r.ResourceID != "" {
		return r.ResourceID
	}
	return r.Query
}

func normalizeResourceInvestigationType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "queue", "database", "db", "cloud_resource", "cloud", "k8s", "k8s_resource", "kubernetes", "terraform", "terraform_resource", "module", "terraform_module":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func (h *ImpactHandler) resolveResourceInvestigationTarget(
	ctx context.Context,
	req resourceInvestigationRequest,
) (*resourceInvestigationCandidate, map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil, fmt.Errorf("graph backend is unavailable")
	}
	params := map[string]any{"selector": req.selector(), "environment": req.Environment, "limit": req.Limit + 1}
	rows, err := h.Neo4j.Run(ctx, resourceInvestigationResolverCypher(req), params)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve resource investigation target: %w", err)
	}
	candidates := resourceInvestigationCandidates(rows)
	totalCandidates := len(candidates)
	truncated := len(candidates) > req.Limit
	if truncated {
		candidates = candidates[:req.Limit]
	}
	resolution := map[string]any{
		"input":         req.selector(),
		"resource_type": req.ResourceType,
		"status":        "no_match",
		"candidates":    resourceInvestigationCandidateMaps(candidates),
		"truncated":     truncated,
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

func resourceInvestigationResolverCypher(req resourceInvestigationRequest) string {
	labelPredicate := resourceInvestigationLabelPredicate(req.ResourceType)
	typePredicate := resourceInvestigationTypePredicate(req.ResourceType)
	matchPredicate := `(
  n.id = $selector OR
  n.uid = $selector OR
  n.resource_id = $selector OR
  n.name = $selector OR
  n.kind = $selector OR
  coalesce(n.resource_type, n.data_type, '') = $selector
)`
	if req.ResourceID == "" {
		matchPredicate = `(
  n.id = $selector OR
  n.uid = $selector OR
  n.resource_id = $selector OR
  n.name = $selector OR
  n.name CONTAINS $selector OR
  n.kind = $selector OR
  coalesce(n.resource_type, n.data_type, '') = $selector OR
  coalesce(n.resource_type, n.data_type, '') CONTAINS $selector OR
  coalesce(n.source, '') CONTAINS $selector OR
  coalesce(n.config_path, '') CONTAINS $selector
)`
	}
	return fmt.Sprintf(`MATCH (n)
WHERE %s
  AND %s
  AND %s
  AND ($environment = '' OR coalesce(n.environment, '') = '' OR n.environment = $environment)
RETURN coalesce(n.id, n.uid, n.resource_id, n.name) AS id,
       n.name AS name,
       labels(n) AS labels,
       coalesce(n.resource_type, n.data_type, n.kind, '') AS resource_type,
       coalesce(n.provider, '') AS provider,
       coalesce(n.environment, '') AS environment,
       coalesce(n.repo_id, '') AS repo_id,
       coalesce(n.config_path, '') AS config_path,
       coalesce(n.source, '') AS source,
       coalesce(n.resource_id, '') AS resource_id,
       coalesce(n.kind, '') AS resource_kind,
       coalesce(n.resource_category, '') AS resource_class
ORDER BY name, id
LIMIT $limit`, labelPredicate, typePredicate, matchPredicate)
}

func resourceInvestigationLabelPredicate(resourceType string) string {
	switch resourceType {
	case "cloud", "cloud_resource":
		return "n:CloudResource"
	case "k8s", "k8s_resource", "kubernetes":
		return "n:K8sResource"
	case "terraform", "terraform_resource":
		return "(n:TerraformResource OR n:TerraformDataSource)"
	case "module", "terraform_module":
		return "n:TerraformModule"
	default:
		return "(n:CloudResource OR n:K8sResource OR n:TerraformResource OR n:TerraformDataSource OR n:TerraformModule OR n:CloudFormationResource OR n:ArgoCDApplication OR n:ArgoCDApplicationSet OR n:CrossplaneClaim OR n:CrossplaneXRD OR n:HelmRelease)"
	}
}

func resourceInvestigationTypePredicate(resourceType string) string {
	resourceTypeExpr := "toLower(coalesce(n.resource_type, n.data_type, n.kind, n.resource_category, ''))"
	switch resourceType {
	case "queue":
		return fmt.Sprintf("(%s CONTAINS 'queue' OR %s CONTAINS 'sqs')", resourceTypeExpr, resourceTypeExpr)
	case "database", "db":
		return fmt.Sprintf(
			"(%s CONTAINS 'database' OR %s CONTAINS 'db' OR %s CONTAINS 'rds' OR %s CONTAINS 'sql' OR %s CONTAINS 'postgres' OR %s CONTAINS 'mysql' OR %s CONTAINS 'dynamodb')",
			resourceTypeExpr,
			resourceTypeExpr,
			resourceTypeExpr,
			resourceTypeExpr,
			resourceTypeExpr,
			resourceTypeExpr,
			resourceTypeExpr,
		)
	default:
		return "1 = 1"
	}
}

func (h *ImpactHandler) resourceInvestigationWorkloads(
	ctx context.Context,
	req resourceInvestigationRequest,
	resourceID string,
) ([]map[string]any, bool, error) {
	cypher := `MATCH (resource) WHERE coalesce(resource.id, resource.uid, resource.resource_id, resource.name) = $resource_id
MATCH (instance:WorkloadInstance)-[rel:USES]->(resource)
OPTIONAL MATCH (instance)-[:INSTANCE_OF]->(workload:Workload)
WITH resource, instance, workload, rel
WHERE $environment = '' OR coalesce(instance.environment, resource.environment, '') = '' OR instance.environment = $environment
RETURN DISTINCT coalesce(workload.id, instance.workload_id, instance.id) AS workload_id,
       coalesce(workload.name, instance.name, instance.workload_id) AS workload_name,
       instance.id AS instance_id,
       coalesce(instance.environment, resource.environment, '') AS environment,
       type(rel) AS relationship_type,
       coalesce(rel.reason, rel.evidence_type, '') AS relationship_reason,
       rel.confidence AS confidence
ORDER BY workload_name, workload_id, instance_id
LIMIT $limit`
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{
		"resource_id": resourceID,
		"environment": req.Environment,
		"limit":       req.Limit + 1,
	})
	if err != nil {
		return nil, false, fmt.Errorf("load resource workloads: %w", err)
	}
	rows, truncated := trimImpactRows(rows, req.Limit)
	workloads := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		workload := compactStringMap(map[string]any{
			"workload_id":         StringVal(row, "workload_id"),
			"workload_name":       StringVal(row, "workload_name"),
			"instance_id":         StringVal(row, "instance_id"),
			"environment":         StringVal(row, "environment"),
			"relationship_type":   StringVal(row, "relationship_type"),
			"relationship_reason": StringVal(row, "relationship_reason"),
		})
		if confidence := row["confidence"]; confidence != nil {
			workload["confidence"] = confidence
		}
		workloads = append(workloads, workload)
	}
	return workloads, truncated, nil
}

func (h *ImpactHandler) resourceInvestigationRepoPaths(
	ctx context.Context,
	req resourceInvestigationRequest,
	resourceID string,
	direction string,
) ([]map[string]any, bool, error) {
	pattern := fmt.Sprintf("(resource)-[rels*1..%d]->(repo:Repository)", req.MaxDepth)
	if direction == "incoming" {
		pattern = fmt.Sprintf("(resource)<-[rels*1..%d]-(repo:Repository)", req.MaxDepth)
	}
	cypher := fmt.Sprintf(`MATCH (resource) WHERE coalesce(resource.id, resource.uid, resource.resource_id, resource.name) = $resource_id
MATCH path = %s
RETURN DISTINCT repo.id AS repo_id,
       repo.name AS repo_name,
       %q AS direction,
       length(path) AS depth,
       [rel IN relationships(path) | {type: type(rel), confidence: rel.confidence, reason: coalesce(rel.reason, rel.evidence_type, '')}] AS hops
ORDER BY depth, repo_name, repo_id
LIMIT $limit`, pattern, direction)
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{"resource_id": resourceID, "limit": req.Limit + 1})
	if err != nil {
		return nil, false, fmt.Errorf("load resource repository paths: %w", err)
	}
	rows, truncated := trimImpactRows(rows, req.Limit)
	paths := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, map[string]any{
			"repo_id":   StringVal(row, "repo_id"),
			"repo_name": StringVal(row, "repo_name"),
			"direction": StringVal(row, "direction"),
			"depth":     IntVal(row, "depth"),
			"hops":      row["hops"],
		})
	}
	return paths, truncated, nil
}
