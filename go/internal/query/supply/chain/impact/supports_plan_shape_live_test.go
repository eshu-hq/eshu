// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B1 disposable PostgreSQL 18 plan-shape guard: at the 400-scope grant shape over 144,000 supports in 2,400 scopes the candidate list read drives from the grant (index arms over the per-support table, no sequential scan of it), and the natural single-WHERE form of the same read is rejected by the same checker; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
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

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
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
	natural = strings.Replace(natural, "    AND ($16 = '' OR fact.image_ref = $16)\n)",
		"    AND ($16 = '' OR fact.image_ref = $16)\n    AND (fact.scope_id = ANY($23::text[]) OR fact.repository_id = ANY($22::text[]))\n)", 1)
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

	// The generation foreign key cascades. Its delete reads the supports by
	// generation_id; PostgreSQL 18 reaches them through the (scope_id,
	// generation_id) index by skip scan, so no separate generation_id index is
	// needed. A sequential scan here would make every generation delete scan the
	// whole table, so the plan is pinned.
	cascadeScans, err := explainSupportsScans(ctx, database,
		`DELETE FROM supply_chain_impact_finding_supports WHERE generation_id = 'gen:plan:7'`, nil)
	if err != nil {
		t.Fatalf("explain the cascade delete: %v", err)
	}
	t.Logf("cascade delete scans of the supports table: %v", cascadeScans)
	if len(cascadeScans) == 0 || strings.Contains(strings.Join(cascadeScans, " | "), "Seq Scan") {
		t.Fatalf("the generation delete reads the supports by sequential scan (scans %v): add a generation_id index", cascadeScans)
	}
}
