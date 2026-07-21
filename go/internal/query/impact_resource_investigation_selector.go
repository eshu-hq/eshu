// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const resourceInvestigationSelectorConcurrency = 4

var resourceInvestigationExactSelectorPredicates = []string{
	"coalesce(n.id, '') = $selector",
	"coalesce(n.uid, '') = $selector",
	"coalesce(n.resource_id, '') = $selector",
	"coalesce(n.arn, '') = $selector",
	"coalesce(n.name, '') = $selector",
	"coalesce(n.kind, '') = $selector",
	"coalesce(n.resource_type, n.data_type, '') = $selector",
}

var resourceInvestigationFuzzySelectorPredicates = []string{
	"coalesce(n.name, '') CONTAINS $selector",
	"coalesce(n.resource_type, n.data_type, '') CONTAINS $selector",
	"coalesce(n.arn, '') CONTAINS $selector",
	"coalesce(n.source, '') CONTAINS $selector",
	"coalesce(n.config_path, '') CONTAINS $selector",
}

var resourceInvestigationDefaultLabels = []string{
	"CloudResource",
	"K8sResource",
	"TerraformResource",
	"TerraformDataSource",
	"TerraformModule",
	"CloudFormationResource",
	"ArgoCDApplication",
	"ArgoCDApplicationSet",
	"CrossplaneClaim",
	"CrossplaneXRD",
	"FluxHelmRelease",
}

func (h *ImpactHandler) resolveResourceInvestigationTarget(
	ctx context.Context,
	req resourceInvestigationRequest,
) (*resourceInvestigationCandidate, map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil, fmt.Errorf("graph backend is unavailable")
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return nil, resourceInvestigationEmptyGrantResolution(req), nil
	}
	started := time.Now()
	phase := "exact"
	candidates, err := h.resourceInvestigationSelectorCandidates(
		ctx,
		req,
		access,
		resourceInvestigationExactSelectorPredicates,
	)
	if err != nil {
		return nil, nil, err
	}
	if len(candidates) == 0 && req.ResourceID == "" {
		phase = "fuzzy"
		candidates, err = h.resourceInvestigationSelectorCandidates(
			ctx,
			req,
			access,
			resourceInvestigationFuzzySelectorPredicates,
		)
		if err != nil {
			return nil, nil, err
		}
	}
	return resourceInvestigationSelectorResolution(ctx, req, candidates, phase, time.Since(started))
}

func (h *ImpactHandler) resourceInvestigationSelectorCandidates(
	ctx context.Context,
	req resourceInvestigationRequest,
	access repositoryAccessFilter,
	predicates []string,
) ([]resourceInvestigationCandidate, error) {
	queries := resourceInvestigationSelectorCyphers(req, access, predicates)
	params := access.graphParams(map[string]any{
		"selector": req.selector(),
		"limit":    req.Limit + 1,
	})
	if req.Environment != "" {
		params["environment"] = req.Environment
	}
	rowGroups, err := runResourceInvestigationSelectorFanout(ctx, h.Neo4j, queries, params)
	if err != nil {
		return nil, fmt.Errorf("resolve resource investigation target: %w", err)
	}
	candidates := mergeResourceInvestigationCandidates(rowGroups)
	return filterResourceInvestigationCandidatesForAccess(candidates, access), nil
}

func runResourceInvestigationSelectorFanout(
	ctx context.Context,
	graph GraphQuery,
	queries []string,
	params map[string]any,
) ([][]map[string]any, error) {
	rowGroups := make([][]map[string]any, len(queries))
	errs := make([]error, len(queries))
	semaphore := make(chan struct{}, resourceInvestigationSelectorConcurrency)
	var wg sync.WaitGroup
	for index, cypher := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errs[index] = ctx.Err()
				return
			}
			rowGroups[index], errs[index] = graph.Run(ctx, cypher, params)
		}()
	}
	wg.Wait()
	joined := errors.Join(errs...)
	if joined != nil {
		return nil, joined
	}
	return rowGroups, nil
}

func resourceInvestigationSelectorCyphers(
	req resourceInvestigationRequest,
	access repositoryAccessFilter,
	predicates []string,
) []string {
	queries := make([]string, 0, len(predicates))
	for _, predicate := range predicates {
		queries = append(queries, resourceInvestigationSelectorPropertyCypher(req, access, predicate))
	}
	return queries
}

func resourceInvestigationSelectorPropertyCypher(
	req resourceInvestigationRequest,
	access repositoryAccessFilter,
	predicate string,
) string {
	labels := resourceInvestigationSelectorLabels(req.ResourceType)
	typeClause := ""
	if typePredicate := resourceInvestigationTypePredicate(req.ResourceType); typePredicate != "1 = 1" {
		typeClause = "\n  AND " + typePredicate
	}
	environmentClause := ""
	if req.Environment != "" {
		environmentClause = "\n  AND (coalesce(n.environment, '') = '' OR n.environment = $environment)"
	}
	branches := make([]string, 0, len(labels))
	for _, label := range labels {
		branches = append(branches, fmt.Sprintf(`MATCH (n:%s)
WHERE true%s
  AND %s%s%s
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
       coalesce(n.arn, '') AS arn,
       coalesce(n.kind, '') AS resource_kind,
       coalesce(n.resource_category, '') AS resource_class`,
			label,
			typeClause,
			predicate,
			environmentClause,
			access.graphPredicateOnProperty("n", "repo_id"),
		))
	}
	return `CALL {
` + strings.Join(branches, "\nUNION\n") + `
}
RETURN id, name, labels, resource_type, provider, environment, repo_id,
       config_path, source, resource_id, arn, resource_kind, resource_class
ORDER BY name, id
LIMIT $limit`
}

func resourceInvestigationSelectorLabels(resourceType string) []string {
	switch resourceType {
	case "cloud", "cloud_resource":
		return []string{"CloudResource"}
	case "k8s", "k8s_resource", "kubernetes":
		return []string{"K8sResource"}
	case "terraform", "terraform_resource":
		return []string{"TerraformResource", "TerraformDataSource"}
	case "module", "terraform_module":
		return []string{"TerraformModule"}
	default:
		return resourceInvestigationDefaultLabels
	}
}

func resourceInvestigationLabelPredicate(resourceType string) string {
	labels := resourceInvestigationSelectorLabels(resourceType)
	predicates := make([]string, 0, len(labels))
	for _, label := range labels {
		predicates = append(predicates, "n:"+label)
	}
	if len(predicates) == 1 {
		return predicates[0]
	}
	return "(" + strings.Join(predicates, " OR ") + ")"
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

func mergeResourceInvestigationCandidates(
	rowGroups [][]map[string]any,
) []resourceInvestigationCandidate {
	seen := make(map[string]resourceInvestigationCandidate)
	for _, rows := range rowGroups {
		for _, candidate := range resourceInvestigationCandidates(rows) {
			seen[resourceInvestigationCandidateKey(candidate)] = candidate
		}
	}
	candidates := make([]resourceInvestigationCandidate, 0, len(seen))
	for _, candidate := range seen {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return resourceInvestigationCandidateKey(candidates[i]) < resourceInvestigationCandidateKey(candidates[j])
	})
	return candidates
}

func resourceInvestigationCandidateKey(candidate resourceInvestigationCandidate) string {
	return strings.Join([]string{
		candidate.Name,
		candidate.ID,
		strings.Join(candidate.Labels, ","),
		candidate.ResourceType,
		candidate.Provider,
		candidate.Environment,
		candidate.RepoID,
		candidate.ConfigPath,
		candidate.Source,
		candidate.ResourceID,
		candidate.Arn,
		candidate.ResourceKind,
		candidate.ResourceClass,
	}, "\x00")
}

func resourceInvestigationSelectorResolution(
	ctx context.Context,
	req resourceInvestigationRequest,
	candidates []resourceInvestigationCandidate,
	phase string,
	duration time.Duration,
) (*resourceInvestigationCandidate, map[string]any, error) {
	totalCandidates := len(candidates)
	truncated := totalCandidates > req.Limit
	if truncated {
		candidates = candidates[:req.Limit]
	}
	ambiguous := totalCandidates > 1
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Float64("eshu.resource_investigation.selector_seconds", duration.Seconds()),
		attribute.String("eshu.resource_investigation.selector_phase", phase),
		attribute.Int("eshu.resource_investigation.selector_candidate_count", totalCandidates),
		attribute.Bool("eshu.resource_investigation.selector_ambiguous", ambiguous),
		attribute.Bool("eshu.resource_investigation.selector_truncated", truncated),
	)
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
