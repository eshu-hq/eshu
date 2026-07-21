// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build resource_selector_slo_live

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResourceInvestigationSelectorLabelFanoutTheory(t *testing.T) {
	if os.Getenv("ESHU_RESOURCE_SELECTOR_LABEL_THEORY") != "1" {
		t.Skip("set ESHU_RESOURCE_SELECTOR_LABEL_THEORY=1 to run the label-fanout theory")
	}
	if os.Getenv("ESHU_RESOURCE_SELECTOR_ISOLATED") != "1" {
		t.Fatal("ESHU_RESOURCE_SELECTOR_ISOLATED=1 is required because this proof replaces graph data")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	driver, database := openResourceSelectorSLOGraph(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedResourceSelectorSLOGraph(ctx, t, driver, database)
	reader := NewNeo4jReader(driver, database)

	exactReq := resourceInvestigationRequest{Query: "orders-db", Limit: 25}
	exactStarted := time.Now()
	exact := runResourceSelectorLabelTheory(
		ctx,
		t,
		reader,
		exactReq,
		repositoryAccessFilter{allScopes: true},
		resourceInvestigationExactSelectorPredicates,
	)
	exactDuration := time.Since(exactStarted)
	if got, want := resourceSelectorSLOCandidateIDs(exact), []string{
		"resource:authorized-exact",
		"resource:denied-exact",
		"resource:second-exact",
	}; !equalResourceSelectorSLOStrings(got, want) {
		t.Fatalf("label-fanout exact ids = %v, want %v", got, want)
	}

	fuzzyReq := resourceInvestigationRequest{Query: "fuzzy-only", Limit: 25}
	fuzzyStarted := time.Now()
	fuzzy := runResourceSelectorLabelTheory(
		ctx,
		t,
		reader,
		fuzzyReq,
		repositoryAccessFilter{allScopes: true},
		resourceInvestigationFuzzySelectorPredicates,
	)
	fuzzyDuration := time.Since(fuzzyStarted)
	if got, want := resourceSelectorSLOCandidateIDs(fuzzy), []string{"resource:fuzzy"}; !equalResourceSelectorSLOStrings(got, want) {
		t.Fatalf("label-fanout fuzzy ids = %v, want %v", got, want)
	}

	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-authorized"},
		allowed:              map[string]struct{}{"repo-authorized": {}},
	}
	scopedExact := runResourceSelectorLabelTheory(
		ctx,
		t,
		reader,
		exactReq,
		scoped,
		resourceInvestigationExactSelectorPredicates,
	)
	if got, want := resourceSelectorSLOCandidateIDs(scopedExact), []string{
		"resource:authorized-exact",
		"resource:second-exact",
	}; !equalResourceSelectorSLOStrings(got, want) {
		t.Fatalf("label-fanout scoped ids = %v, want %v", got, want)
	}
	t.Logf("label-fanout theory exact=%s fuzzy=%s", exactDuration, fuzzyDuration)
}

func runResourceSelectorLabelTheory(
	ctx context.Context,
	t *testing.T,
	graph GraphQuery,
	req resourceInvestigationRequest,
	access repositoryAccessFilter,
	predicates []string,
) []resourceInvestigationCandidate {
	t.Helper()
	queries := resourceSelectorLabelTheoryCyphers(req, access, predicates)
	params := access.graphParams(map[string]any{"selector": req.selector(), "limit": req.Limit + 1})
	if req.Environment != "" {
		params["environment"] = req.Environment
	}
	rows, err := runResourceInvestigationSelectorFanout(ctx, graph, queries, params)
	if err != nil {
		t.Fatalf("run label-fanout theory: %v", err)
	}
	return mergeResourceInvestigationCandidates(rows)
}

func resourceSelectorLabelTheoryCyphers(
	req resourceInvestigationRequest,
	access repositoryAccessFilter,
	predicates []string,
) []string {
	labels := resourceInvestigationSelectorLabels(req.ResourceType)
	queries := make([]string, 0, len(labels))
	for _, label := range labels {
		queries = append(queries, resourceSelectorLabelTheoryCypher(req, access, label, predicates))
	}
	return queries
}

func resourceSelectorLabelTheoryCypher(
	req resourceInvestigationRequest,
	access repositoryAccessFilter,
	label string,
	predicates []string,
) string {
	typeClause := ""
	if predicate := resourceInvestigationTypePredicate(req.ResourceType); predicate != "1 = 1" {
		typeClause = "\n  AND " + predicate
	}
	environmentClause := ""
	if req.Environment != "" {
		environmentClause = "\n  AND (coalesce(n.environment, '') = '' OR n.environment = $environment)"
	}
	return fmt.Sprintf(`MATCH (n:%s)
WHERE true%s
  AND (%s)%s%s
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
       coalesce(n.resource_category, '') AS resource_class
ORDER BY name, id
LIMIT $limit`,
		label,
		typeClause,
		strings.Join(predicates, " OR "),
		environmentClause,
		access.graphPredicateOnProperty("n", "repo_id"),
	)
}
