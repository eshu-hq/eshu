// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build resource_selector_slo_live

package query

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	resourceSelectorSLONoiseNodes = 200_000
	resourceSelectorSLOLimit      = 2 * time.Second
)

func TestResourceInvestigationSelectorInteractiveSLO(t *testing.T) {
	if os.Getenv("ESHU_RESOURCE_SELECTOR_SLO_LIVE") != "1" {
		t.Skip("set ESHU_RESOURCE_SELECTOR_SLO_LIVE=1 to run the selector SLO proof")
	}
	if os.Getenv("ESHU_RESOURCE_SELECTOR_ISOLATED") != "1" {
		t.Fatal("ESHU_RESOURCE_SELECTOR_ISOLATED=1 is required because this proof replaces graph data")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	driver, database := openResourceSelectorSLOGraph(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedResourceSelectorSLOGraph(ctx, t, driver, database)
	if os.Getenv("ESHU_RESOURCE_SELECTOR_COMPARISON") == "1" {
		legacy := measureResourceSelectorSLOLegacy(ctx, t, driver, database)
		t.Logf(
			"selector OLD_CURRENT cold=%s warm_median=%s rows=%d ids=%v db_hits=%d",
			legacy.cold,
			legacy.warm,
			legacy.rows,
			legacy.ids,
			legacy.dbHits,
		)
	}

	handler := &ImpactHandler{Neo4j: NewNeo4jReader(driver, database)}
	exactReq := resourceInvestigationRequest{Query: "orders-db", Limit: 25}
	exactDuration, exactResolution := measureResourceSelectorSLOResolution(
		ctx,
		t,
		handler,
		exactReq,
	)
	if got, want := exactResolution["status"], "ambiguous"; got != want {
		t.Fatalf("exact resolution status = %#v, want %#v", got, want)
	}
	assertResourceSelectorSLOCandidateIDs(t, exactResolution, []string{
		"resource:authorized-exact",
		"resource:denied-exact",
		"resource:second-exact",
	})
	assertResourceSelectorSLODuration(t, "exact", exactDuration)

	fuzzyDuration, fuzzyResolution := measureResourceSelectorSLOResolution(
		ctx,
		t,
		handler,
		resourceInvestigationRequest{Query: "fuzzy-only", Limit: 25},
	)
	if got, want := fuzzyResolution["status"], "resolved"; got != want {
		t.Fatalf("fuzzy resolution status = %#v, want %#v", got, want)
	}
	assertResourceSelectorSLOCandidateIDs(t, fuzzyResolution, []string{"resource:fuzzy"})
	assertResourceSelectorSLODuration(t, "exact-miss plus fuzzy fallback", fuzzyDuration)

	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-authorized"},
		allowed:              map[string]struct{}{"repo-authorized": {}},
	}
	scopedCandidates, err := handler.resourceInvestigationSelectorCandidates(
		ctx,
		exactReq,
		scoped,
		resourceInvestigationExactSelectorPredicates,
	)
	if err != nil {
		t.Fatalf("scoped selector candidates: %v", err)
	}
	if got, want := resourceSelectorSLOCandidateIDs(scopedCandidates), []string{
		"resource:authorized-exact",
		"resource:second-exact",
	}; !equalResourceSelectorSLOStrings(got, want) {
		t.Fatalf("scoped candidate ids = %v, want %v", got, want)
	}
	if os.Getenv("ESHU_RESOURCE_SELECTOR_PROFILE") == "1" {
		exactHits := profileResourceSelectorSLOQueries(
			ctx,
			t,
			driver,
			database,
			exactReq,
			resourceInvestigationExactSelectorPredicates,
		)
		fuzzyHits := profileResourceSelectorSLOQueries(
			ctx,
			t,
			driver,
			database,
			resourceInvestigationRequest{Query: "fuzzy-only", Limit: 25},
			resourceInvestigationFuzzySelectorPredicates,
		)
		t.Logf("selector PROFILE exact_db_hits=%d fuzzy_db_hits=%d", exactHits, fuzzyHits)
	}
	if os.Getenv("ESHU_RESOURCE_SELECTOR_COMPARISON") == "1" {
		exactWarm := measureResourceSelectorSLOWarm(ctx, t, handler, exactReq)
		fuzzyWarm := measureResourceSelectorSLOWarm(
			ctx,
			t,
			handler,
			resourceInvestigationRequest{Query: "fuzzy-only", Limit: 25},
		)
		t.Logf("selector CANDIDATE warm_exact=%s warm_fuzzy=%s", exactWarm, fuzzyWarm)
	}

	t.Logf(
		"selector SLO exact=%s fuzzy=%s limit=%s noise_nodes=%d",
		exactDuration,
		fuzzyDuration,
		resourceSelectorSLOLimit,
		resourceSelectorSLONoiseNodes,
	)
}

type resourceSelectorSLOLegacyMeasurement struct {
	cold   time.Duration
	warm   time.Duration
	rows   int
	ids    []string
	dbHits int64
}

func measureResourceSelectorSLOLegacy(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
) resourceSelectorSLOLegacyMeasurement {
	t.Helper()
	req := resourceInvestigationRequest{Query: "orders-db", Limit: 25}
	cypher := resourceSelectorSLOLegacyCypher(req)
	params := map[string]any{"selector": req.selector(), "environment": "", "limit": req.Limit + 1}
	durations := make([]time.Duration, 0, 4)
	rows := 0
	var ids []string
	for range 4 {
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
		started := time.Now()
		result, err := session.Run(ctx, cypher, params)
		if err != nil {
			_ = session.Close(context.Background())
			t.Fatalf("run OLD/CURRENT selector: %v", err)
		}
		records, err := result.Collect(ctx)
		closeErr := session.Close(context.Background())
		if err != nil || closeErr != nil {
			t.Fatalf("collect/close OLD/CURRENT selector: collect=%v close=%v", err, closeErr)
		}
		durations = append(durations, time.Since(started))
		rows = len(records)
		if ids == nil {
			ids = make([]string, 0, len(records))
			for _, record := range records {
				value, ok := record.Get("id")
				if id, isString := value.(string); ok && isString {
					ids = append(ids, id)
				}
			}
			sort.Strings(ids)
		}
	}
	warm := append([]time.Duration(nil), durations[1:]...)
	sort.Slice(warm, func(i, j int) bool { return warm[i] < warm[j] })
	measurement := resourceSelectorSLOLegacyMeasurement{cold: durations[0], warm: warm[1], rows: rows, ids: ids}
	if os.Getenv("ESHU_RESOURCE_SELECTOR_PROFILE") == "1" {
		measurement.dbHits = profileResourceSelectorSLOQuery(ctx, t, driver, database, cypher, params)
	}
	return measurement
}

func measureResourceSelectorSLOWarm(
	ctx context.Context,
	t *testing.T,
	handler *ImpactHandler,
	req resourceInvestigationRequest,
) time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, 3)
	for range 3 {
		started := time.Now()
		if _, _, err := handler.resolveResourceInvestigationTarget(ctx, req); err != nil {
			t.Fatalf("warm selector %q: %v", req.Query, err)
		}
		durations = append(durations, time.Since(started))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations[1]
}

func resourceSelectorSLOLegacyCypher(req resourceInvestigationRequest) string {
	return fmt.Sprintf(`MATCH (n)
WHERE %s
  AND %s
  AND (n.id = $selector OR n.uid = $selector OR n.resource_id = $selector OR
       n.arn = $selector OR n.name = $selector OR n.name CONTAINS $selector OR
       n.kind = $selector OR coalesce(n.resource_type, n.data_type, '') = $selector OR
       coalesce(n.resource_type, n.data_type, '') CONTAINS $selector OR
       coalesce(n.arn, '') CONTAINS $selector OR coalesce(n.source, '') CONTAINS $selector OR
       coalesce(n.config_path, '') CONTAINS $selector)
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
       coalesce(n.arn, '') AS arn,
       coalesce(n.kind, '') AS resource_kind,
       coalesce(n.resource_category, '') AS resource_class
ORDER BY name, id
LIMIT $limit`, resourceInvestigationLabelPredicate(req.ResourceType), resourceInvestigationTypePredicate(req.ResourceType))
}

func profileResourceSelectorSLOQueries(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
	req resourceInvestigationRequest,
	predicates []string,
) int64 {
	t.Helper()
	queries := resourceInvestigationSelectorCyphers(
		req,
		repositoryAccessFilter{allScopes: true},
		predicates,
	)
	params := map[string]any{"selector": req.selector(), "limit": req.Limit + 1}
	var total int64
	for _, cypher := range queries {
		total += profileResourceSelectorSLOQuery(ctx, t, driver, database, cypher, params)
	}
	return total
}

func profileResourceSelectorSLOQuery(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
	cypher string,
	params map[string]any,
) int64 {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, "PROFILE "+cypher, params)
	if err != nil {
		t.Fatalf("PROFILE selector query: %v", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("consume selector PROFILE query: %v", err)
	}
	if summary.Profile() == nil {
		t.Fatal("selector PROFILE query returned no plan")
	}
	return resourceSelectorSLOPlanDBHits(summary.Profile())
}

func resourceSelectorSLOPlanDBHits(plan neo4jdriver.ProfiledPlan) int64 {
	total := plan.DbHits()
	for _, child := range plan.Children() {
		total += resourceSelectorSLOPlanDBHits(child)
	}
	return total
}

func openResourceSelectorSLOGraph(
	ctx context.Context,
	t *testing.T,
) (neo4jdriver.DriverWithContext, string) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	auth := neo4jdriver.NoAuth()
	if username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); username != "" {
		auth = neo4jdriver.BasicAuth(username, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver, strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
}

func measureResourceSelectorSLOResolution(
	ctx context.Context,
	t *testing.T,
	handler *ImpactHandler,
	req resourceInvestigationRequest,
) (time.Duration, map[string]any) {
	t.Helper()
	started := time.Now()
	_, resolution, err := handler.resolveResourceInvestigationTarget(ctx, req)
	duration := time.Since(started)
	if err != nil {
		t.Fatalf("resolve selector %q: %v", req.Query, err)
	}
	return duration, resolution
}

func assertResourceSelectorSLODuration(t *testing.T, phase string, duration time.Duration) {
	t.Helper()
	if duration > resourceSelectorSLOLimit {
		t.Fatalf("%s duration = %s, exceeds interactive SLO %s", phase, duration, resourceSelectorSLOLimit)
	}
}

func assertResourceSelectorSLOCandidateIDs(
	t *testing.T,
	resolution map[string]any,
	want []string,
) {
	t.Helper()
	rows, ok := resolution["candidates"].([]map[string]any)
	if !ok {
		t.Fatalf("resolution candidates = %#v, want []map[string]any", resolution["candidates"])
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, StringVal(row, "id"))
	}
	sort.Strings(got)
	if !equalResourceSelectorSLOStrings(got, want) {
		t.Fatalf("candidate ids = %v, want %v", got, want)
	}
}

func resourceSelectorSLOCandidateIDs(candidates []resourceInvestigationCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	sort.Strings(ids)
	return ids
}

func equalResourceSelectorSLOStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func seedResourceSelectorSLOGraph(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(context.Background()) }()
	if os.Getenv("ESHU_RESOURCE_SELECTOR_FRESH") != "1" {
		runResourceSelectorSLOCypher(ctx, t, session, "MATCH (n) DETACH DELETE n", nil)
	}
	const batchSize = 1_000
	for offset := 0; offset < resourceSelectorSLONoiseNodes; offset += batchSize {
		rows := make([]map[string]any, 0, batchSize)
		for index := offset; index < offset+batchSize; index++ {
			rows = append(rows, map[string]any{
				"uid":  fmt.Sprintf("noise:%06d", index),
				"name": fmt.Sprintf("unrelated-%06d", index),
			})
		}
		runResourceSelectorSLOCypher(
			ctx,
			t,
			session,
			"UNWIND $rows AS row CREATE (:Function {uid: row.uid, name: row.name})",
			map[string]any{"rows": rows},
		)
	}
	for _, label := range resourceInvestigationDefaultLabels {
		rows := make([]map[string]any, 0, 36)
		for index := 0; index < 36; index++ {
			rows = append(rows, map[string]any{
				"uid":           fmt.Sprintf("infra:%s:%03d", strings.ToLower(label), index),
				"id":            fmt.Sprintf("infra:%s:%03d", strings.ToLower(label), index),
				"name":          fmt.Sprintf("unrelated-%s-%03d", strings.ToLower(label), index),
				"repo_id":       "repo-authorized",
				"resource_type": "proof_resource",
			})
		}
		runResourceSelectorSLOCypher(
			ctx,
			t,
			session,
			fmt.Sprintf("UNWIND $rows AS row CREATE (n:%s) SET n = row", label),
			map[string]any{"rows": rows},
		)
	}
	for _, item := range []struct {
		label string
		row   map[string]any
	}{
		{label: "CloudResource", row: resourceSelectorSLORow("resource:authorized-exact", "orders-db", "repo-authorized")},
		{label: "K8sResource", row: resourceSelectorSLORow("resource:second-exact", "orders-db", "repo-authorized")},
		{label: "TerraformResource", row: resourceSelectorSLORow("resource:denied-exact", "orders-db", "repo-denied")},
		{label: "CloudResource", row: resourceSelectorSLORow("resource:prefix", "orders-db-shadow", "repo-authorized")},
		{label: "CloudResource", row: resourceSelectorSLORow("resource:fuzzy", "prefix-fuzzy-only-suffix", "repo-authorized")},
	} {
		runResourceSelectorSLOCypher(
			ctx,
			t,
			session,
			fmt.Sprintf("CREATE (n:%s) SET n = $row", item.label),
			map[string]any{"row": item.row},
		)
	}
}

func resourceSelectorSLORow(id, name, repoID string) map[string]any {
	return map[string]any{
		"uid": id, "id": id, "name": name, "repo_id": repoID, "resource_type": "database",
	}
}

func runResourceSelectorSLOCypher(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	cypher string,
	params map[string]any,
) {
	t.Helper()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run selector SLO Cypher: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume selector SLO Cypher: %v", err)
	}
}
