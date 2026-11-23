// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B1 disposable PostgreSQL 18 plan-shape guard: at the 400-scope grant shape over 144,000 supports in 2,400 scopes the granted list, count, inventory, grouped-inventory and explain reads (#10880 PR-B2a) drive from the grant, the public-id explain probes the finding index, and the advisory evidence read probes the repository index or walks the scope-and-generation index (index arms over the per-support table, no sequential scan of it), and the natural single-WHERE form of the same read is rejected by the same checker; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	supportsPlanScopes        = 2400
	supportsPlanPerScope      = 60
	supportsPlanGrantedScopes = 400
	supportsPlanTable         = "supply_chain_impact_finding_supports"
)

// seedSupportsPlanCorpus writes supportsPlanScopes active scopes with
// supportsPlanPerScope supports each, shaped like the theory proof's corpus:
// one repository per scope, distinct canonical keys, no operator rows.
func seedSupportsPlanCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope:plan:' || n, 'synthetic', 'synthetic', 'k' || n, 'synthetic', 'k' || n,
       NOW(), NOW(), 'active', 'gen:plan:' || n, '{}'::jsonb
FROM generate_series(1, $1) AS n`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:plan:' || n, 'scope:plan:' || n, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb
FROM generate_series(1, $1) AS n`,
		`INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id,
  has_payload_finding_id, priority_score, source_confidence, suppression_state, impact_status, repository_id,
  cve_id, package_id, priority_bucket, severity_bucket, payload)
SELECT 'fact:plan:' || n || ':' || k, 'key:' || n || ':' || k, 'scope:plan:' || n, 'gen:plan:' || n,
       'finding:plan:' || n || ':' || k, 1, (k * 7) % 100, 'observed', 'active', 'affected_exact',
       'repository:plan:' || n, 'CVE-2026-' || k, 'pkg:npm/p' || k, 'high', 'high',
       jsonb_build_object('finding_id', 'finding:plan:' || n || ':' || k, 'impact_status', 'affected_exact',
                          'priority_score', (k * 7) % 100)
FROM generate_series(1, $1) AS n, generate_series(1, $2) AS k`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement, supportsPlanScopes, supportsPlanPerScope); err != nil {
			// Statements that bind only $1 reject the extra argument; retry with one.
			if _, retry := database.ExecContext(ctx, statement, supportsPlanScopes); retry != nil {
				t.Fatalf("seed plan corpus: %v", err)
			}
		}
	}
	if _, err := database.ExecContext(ctx, `ANALYZE supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, table := range []string{"ingestion_scopes", "scope_generations"} {
		if _, err := database.ExecContext(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}

// planNode is the part of one EXPLAIN (FORMAT JSON) node the checker reads.
type planNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name"`
	IndexName    string     `json:"Index Name"`
	IndexCond    string     `json:"Index Cond"`
	Plans        []planNode `json:"Plans"`
}

// supportsScans returns one label per access to the supports table: a Seq Scan,
// an Index Scan with its index, and for a Bitmap Heap Scan every Bitmap Index
// Scan beneath it with its index.
func supportsScans(node planNode) []string {
	var out []string
	if node.RelationName == supportsPlanTable {
		label := strings.TrimSpace(node.NodeType + " " + node.IndexName)
		if node.NodeType == "Bitmap Heap Scan" {
			label = node.NodeType + " via " + strings.Join(bitmapIndexes(node), "+")
		}
		out = append(out, label)
	}
	for _, child := range node.Plans {
		out = append(out, supportsScans(child)...)
	}
	return out
}

// bitmapIndexes lists the indexes of the Bitmap Index Scan nodes under node.
func bitmapIndexes(node planNode) []string {
	var out []string
	for _, child := range node.Plans {
		if child.NodeType == "Bitmap Index Scan" {
			out = append(out, child.IndexName)
		}
		out = append(out, bitmapIndexes(child)...)
	}
	return out
}

func explainSupportsScans(ctx context.Context, database *sql.DB, query string, args []any) ([]string, error) {
	var raw []byte
	if err := database.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return nil, err
	}
	var plan []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	if len(plan) != 1 {
		return nil, fmt.Errorf("explain returned %d plans", len(plan))
	}
	return supportsScans(plan[0].Plan), nil
}

// supportsPlanIsGrantDriven reports whether the plan reaches the supports table
// through both grant indexes (the scope-and-generation index and the repository
// index, one per grant arm) and never by a sequential scan; it returns the
// reason when not. The natural single-WHERE form uses one index and filters the
// other grant, which the theory proofs measured as the slow shape.
func supportsPlanIsGrantDriven(scans []string) (bool, string) {
	joined := strings.Join(scans, " | ")
	if len(scans) == 0 {
		return false, "the plan never scans the supports table"
	}
	if strings.Contains(joined, "Seq Scan") {
		return false, "sequential scan of the supports table"
	}
	for _, index := range []string{"supply_chain_impact_finding_supports_scope_generation_idx", "supply_chain_impact_finding_supports_repository_idx"} {
		if !strings.Contains(joined, index) {
			return false, "the plan never uses grant index " + index
		}
	}
	return true, ""
}

// TestSupportsCandidatePlanDrivesFromTheGrantLive pins the raw-first plan shape
// the theory proofs measured. At the 400-scope grant shape the planner must
// reach the supports table through the grant indexes. The same checker rejects
// the natural single-WHERE form (a seeded violation), so a rewrite that loses
// the shape fails here instead of in production.
func TestSupportsCandidatePlanDrivesFromTheGrantLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		10*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedSupportsPlanCorpus(t, ctx, database)
	scopes := make([]string, 0, supportsPlanGrantedScopes)
	for n := 1; n <= supportsPlanGrantedScopes; n++ {
		scopes = append(scopes, fmt.Sprintf("scope:plan:%d", n))
	}
	filter := FindingFilter{
		Limit: 50, Status: "affected_exact", AllowedScopeIDs: scopes,
		AllowedRepositoryIDs: []string{"repository:plan:2000", "repository:plan:2001"},
	}
	args := multiSupportWinnersListArgs(ctx, filter)

	candidate := supportsCandidateListQuery(t)
	scans, err := explainSupportsScans(ctx, database, candidate, args)
	if err != nil {
		t.Fatalf("explain candidate: %v", err)
	}
	t.Logf("candidate scans of the supports table: %v", scans)
	if ok, reason := supportsPlanIsGrantDriven(scans); !ok {
		t.Fatalf("candidate plan is not grant-driven: %s (scans %v)", reason, scans)
	}

	// Seeded violation: the natural single-WHERE form.
	const arms = `FROM (SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY($23::text[])
        UNION ALL
        SELECT * FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($22::text[])
          AND NOT COALESCE(scope_id = ANY($23::text[]), FALSE)) AS fact`
	if !strings.Contains(candidate, arms) {
		t.Fatal("candidate lost its grant arms: update the seeded violation")
	}
	natural := strings.Replace(candidate, arms, "FROM supply_chain_impact_finding_supports AS fact", 1)
	natural = strings.Replace(natural, "    AND ($16 = '' OR fact.image_ref = $16)\n",
		"    AND ($16 = '' OR fact.image_ref = $16)\n    AND (fact.scope_id = ANY($23::text[]) OR fact.repository_id = ANY($22::text[]))\n", 1)
	if !strings.Contains(natural, "OR fact.repository_id = ANY($22::text[]))") {
		t.Fatal("the seeded violation did not apply")
	}
	naturalScans, err := explainSupportsScans(ctx, database, natural, args)
	if err != nil {
		t.Fatalf("explain natural form: %v", err)
	}
	t.Logf("natural form scans of the supports table: %v", naturalScans)
	if ok, _ := supportsPlanIsGrantDriven(naturalScans); ok {
		t.Fatalf("the checker accepted the natural single-WHERE form (scans %v): it would not catch a rewrite", naturalScans)
	}

	// The granted aggregates and the grouped inventory read the supports table
	// through the same grant arms, and the grouped hidden-bucket probe reaches
	// it through the repository index. Each must be grant-driven at the same
	// grant shape, and the checker must reject the natural single-WHERE form of
	// the aggregate.
	aggregateArgs := []any{
		"", "", "", "", "affected_exact", "", "", "", "", "", "", "", "", 0, "", false, "",
		array.Of(filter.AllowedRepositoryIDs), array.Of(filter.AllowedScopeIDs), time.Now().UTC(),
	}
	countArgs := append(append([]any{}, aggregateArgs...), supplyChainImpactCallerAuthorizationArgs(ctx)...)
	inventoryQuery := InventorySupportsQuery("COALESCE(NULLIF(fact.impact_status, ''), 'unknown')")
	groupedQuery, err := groupedInventorySupportsQuery(GroupedInventoryByService)
	if err != nil {
		t.Fatalf("build the grouped supports query: %v", err)
	}
	for name, read := range map[string]struct {
		query string
		args  []any
	}{
		"count":     {AggregateSupportsCombinedCountQuery, countArgs},
		"inventory": {inventoryQuery, append(append([]any{}, countArgs...), 50, 0)},
		"grouped":   {groupedQuery, append(append([]any{}, aggregateArgs...), 50, 0)},
	} {
		readScans, err := explainSupportsScans(ctx, database, read.query, read.args)
		if err != nil {
			t.Fatalf("explain the granted %s read: %v", name, err)
		}
		t.Logf("granted %s scans of the supports table: %v", name, readScans)
		if ok, reason := supportsPlanIsGrantDriven(readScans); !ok {
			t.Fatalf("granted %s plan is not grant-driven: %s (scans %v)", name, reason, readScans)
		}
	}
	const aggregateArms = `FROM (SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY($19::text[])
        UNION ALL
        SELECT * FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($18::text[])
          AND NOT COALESCE(scope_id = ANY($19::text[]), FALSE)) AS fact`
	if !strings.Contains(AggregateSupportsCombinedCountQuery, aggregateArms) {
		t.Fatal("the aggregate lost its grant arms: update the seeded violation")
	}
	naturalCount := strings.Replace(AggregateSupportsCombinedCountQuery, aggregateArms, "FROM supply_chain_impact_finding_supports AS fact", 1)
	naturalCount = strings.Replace(naturalCount, "    AND ($17 = '' OR fact.image_ref = $17)\n",
		"    AND ($17 = '' OR fact.image_ref = $17)\n    AND (fact.scope_id = ANY($19::text[]) OR fact.repository_id = ANY($18::text[]))\n", 1)
	if !strings.Contains(naturalCount, "OR fact.repository_id = ANY($18::text[]))") {
		t.Fatal("the aggregate seeded violation did not apply")
	}
	naturalCountScans, err := explainSupportsScans(ctx, database, naturalCount, countArgs)
	if err != nil {
		t.Fatalf("explain the natural aggregate form: %v", err)
	}
	t.Logf("natural aggregate form scans of the supports table: %v", naturalCountScans)
	if ok, _ := supportsPlanIsGrantDriven(naturalCountScans); ok {
		t.Fatalf("the checker accepted the natural single-WHERE aggregate (scans %v)", naturalCountScans)
	}

	// The explain reads: a public finding id probes the finding index (no
	// sequential scan, the grant applies to the few rows it names), and a granted
	// selector lookup drives from the grant arms.
	explainArgs := []any{
		FindingFactKind, "finding:plan:7:7", "", "", "", "", "", "", "", "",
		array.Of(filter.AllowedRepositoryIDs), array.Of(filter.AllowedScopeIDs), time.Now().UTC(),
	}
	explainArgs = append(explainArgs, supplyChainImpactCallerAuthorizationArgs(ctx)...)
	idScans, err := explainSupportsScans(ctx, database, ExplainFindingByPublicIDFromSupportsQuery, explainArgs)
	if err != nil {
		t.Fatalf("explain the public-id read: %v", err)
	}
	t.Logf("public-id explain scans of the supports table: %v", idScans)
	if joined := strings.Join(idScans, " | "); strings.Contains(joined, "Seq Scan") ||
		!strings.Contains(joined, "supply_chain_impact_finding_supports_finding_idx") {
		t.Fatalf("the public-id explain does not probe the finding index (scans %v)", idScans)
	}
	selectorArgs := append([]any{}, explainArgs...)
	selectorArgs[1], selectorArgs[3] = "", "CVE-2026-7"
	selectorScans, err := explainSupportsScans(ctx, database, ExplainFindingFromSupportsQuery, selectorArgs)
	if err != nil {
		t.Fatalf("explain the granted selector read: %v", err)
	}
	t.Logf("granted selector explain scans of the supports table: %v", selectorScans)
	if ok, reason := supportsPlanIsGrantDriven(selectorScans); !ok {
		t.Fatalf("granted selector explain plan is not grant-driven: %s (scans %v)", reason, selectorScans)
	}

	// The advisory evidence read derives its anchors from the supports of one
	// selector: a repository anchor probes the repository index, and a
	// service-only anchor walks the active scopes through the scope-and-generation
	// index. Neither may read the table sequentially.
	for name, tc := range map[string]struct {
		repository, service, index string
	}{
		"repository anchor":   {"repository:plan:7", "", "supply_chain_impact_finding_supports_repository_idx"},
		"service-only anchor": {"", "service:plan", "supply_chain_impact_finding_supports_scope_generation_idx"},
	} {
		evidenceArgs := []any{
			array.Of([]string{"vulnerability.cve"}), array.Of([]string{}), array.Of([]string{}), "", 50,
			tc.repository, tc.service, "", array.Of([]string{}),
		}
		evidenceScans, err := explainSupportsScans(ctx, database, advisory.ListEvidenceFromSupportsQuery, evidenceArgs)
		if err != nil {
			t.Fatalf("explain the advisory evidence %s: %v", name, err)
		}
		t.Logf("advisory evidence %s scans of the supports table: %v", name, evidenceScans)
		if joined := strings.Join(evidenceScans, " | "); strings.Contains(joined, "Seq Scan") || !strings.Contains(joined, tc.index) {
			t.Fatalf("advisory evidence %s does not read the supports table through %s (scans %v)", name, tc.index, evidenceScans)
		}
	}

	// The generation foreign key cascades. The trigger runs
	// DELETE FROM ONLY <table> WHERE generation_id = $1 once per deleted
	// generation, as a prepared statement that reuses a generic plan. Without a
	// generation_id index PostgreSQL 18 reads the whole (scope_id, generation_id)
	// index once per generation (Index Searches: 1, every leaf page), so the cost
	// grows with the table. Pin the index by name and the key in its Index Cond,
	// not the absence of a sequential scan.
	cascadePlan, err := explainSupportsCascadePlan(ctx, database, false)
	if err != nil {
		t.Fatalf("explain the cascade delete: %v", err)
	}
	t.Logf("cascade delete scans of the supports table: %v", cascadePlan.scans())
	if ok, reason := supportsCascadeUsesGenerationIndex(cascadePlan); !ok {
		t.Fatalf("the generation delete does not use %s: %s (scans %v)", supportsGenerationIndex, reason, cascadePlan.scans())
	}

	// Seeded violation: the same plan with the index dropped inside a
	// transaction that rolls back. The checker must reject it, and the index
	// must still exist afterward.
	droppedPlan, err := explainSupportsCascadePlan(ctx, database, true)
	if err != nil {
		t.Fatalf("explain the cascade delete without the index: %v", err)
	}
	t.Logf("cascade delete scans without the generation index: %v", droppedPlan.scans())
	if ok, _ := supportsCascadeUsesGenerationIndex(droppedPlan); ok {
		t.Fatalf("the checker accepted the cascade plan without %s (scans %v): it would not catch a missing index",
			supportsGenerationIndex, droppedPlan.scans())
	}
	if again, err := explainSupportsCascadePlan(ctx, database, false); err != nil {
		t.Fatalf("explain after the rollback: %v", err)
	} else if ok, reason := supportsCascadeUsesGenerationIndex(again); !ok {
		t.Fatalf("the rolled-back drop leaked: %s", reason)
	}
}
