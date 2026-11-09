// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package group_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestAdvisoryGroupParityLive is the seeded bidirectional parity proof for
// #10411 criterion 9: winners seeded in Postgres (qualifying and excluded
// rows across two groups, one CVE-keyed and one advisory-keyed) project
// through the production handler, production reader, and production
// writer, and the graph (repository_id, package_id) pairs read back by
// direct Cypher must equal the reader's qualifying pairs for each group
// in both directions. It needs both backends:
//
//	cd go && ESHU_POSTGRES_TEST_DSN=postgres://... ESHU_NEO4J_URI=bolt://127.0.0.1:<port> \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j go test -tags live_nornicdb_answer_truth \
//	  ./internal/reducer/advisory/group -run '^TestAdvisoryGroupParityLive' -count=1 -v

const parityPrefix = "test-agp-10411-"

type parityBoltExecutor struct {
	driver neo4jdriver.DriverWithContext
}

func (e parityBoltExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	params := map[string]any{}
	for k, v := range stmt.Parameters {
		params[k] = v
	}
	result, err := session.Run(ctx, stmt.Cypher, params)
	if err != nil {
		return err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return err
	}
	c := summary.Counters()
	sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, sourcecypher.WriteCounters{
		NodesCreated:         int64(c.NodesCreated()),
		NodesDeleted:         int64(c.NodesDeleted()),
		RelationshipsCreated: int64(c.RelationshipsCreated()),
		RelationshipsDeleted: int64(c.RelationshipsDeleted()),
		PropertiesSet:        int64(c.PropertiesSet()),
	})
	return nil
}

func TestAdvisoryGroupParityLive(t *testing.T) {
	pgDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	neoURI := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if pgDSN == "" || neoURI == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN and ESHU_NEO4J_URI to run the advisory-group parity proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	driver, err := neo4jdriver.NewDriverWithContext(neoURI, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open neo4j: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(ctx) })
	exec := parityBoltExecutor{driver: driver}
	for _, stmt := range []string{
		"CREATE CONSTRAINT repository_id IF NOT EXISTS FOR (r:Repository) REQUIRE r.id IS UNIQUE",
		"CREATE CONSTRAINT advisory_group_id IF NOT EXISTS FOR (g:AdvisoryGroup) REQUIRE g.id IS UNIQUE",
	} {
		if err := exec.Execute(ctx, sourcecypher.Statement{Cypher: stmt, Parameters: map[string]any{}}); err != nil {
			t.Fatalf("apply DDL %q: %v", stmt, err)
		}
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	groupA := "CVE-10411-PARITY-" + suffix
	groupB := "GHSA-10411-parity-" + suffix
	repoA := parityPrefix + "repo-a-" + suffix
	repoB := parityPrefix + "repo-b-" + suffix
	seedParityWinners(t, ctx, db, groupA, groupB, repoA, repoB)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, q := range []string{
			`DELETE FROM supply_chain_impact_canonical_winners WHERE canonical_key LIKE '` + parityPrefix + `%'`,
			`DELETE FROM fact_records WHERE fact_id LIKE '` + parityPrefix + `%'`,
			`DELETE FROM scope_generations WHERE generation_id LIKE '` + parityPrefix + `%'`,
			`DELETE FROM ingestion_scopes WHERE scope_id LIKE '` + parityPrefix + `%'`,
		} {
			if _, err := db.ExecContext(cleanupCtx, q); err != nil {
				t.Errorf("pg cleanup %s: %v", q, err)
			}
		}
		_ = exec.Execute(cleanupCtx, sourcecypher.Statement{
			Cypher:     `MATCH (n) WHERE (n:Repository AND n.id IN $repos) OR (n:AdvisoryGroup AND n.id IN $groups) DETACH DELETE n`,
			Parameters: map[string]any{"repos": []string{repoA, repoB}, "groups": []string{groupA, groupB}},
		})
	})
	for _, repo := range []string{repoA, repoB} {
		if err := exec.Execute(ctx, sourcecypher.Statement{Cypher: `MERGE (:Repository {id: $id})`, Parameters: map[string]any{"id": repo}}); err != nil {
			t.Fatalf("seed repo: %v", err)
		}
	}

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	handler := advisorygroup.Handler{
		Winners: store,
		Writer:  edgewriter.NewAdvisoryGroupWriter(exec, 0),
	}
	for _, groupID := range []string{groupA, groupB} {
		if _, err := handler.Handle(ctx, reducercontract.Intent{
			IntentID:     "parity-" + groupID,
			Domain:       reducercontract.DomainAdvisoryGroupProjection,
			EntityKeys:   []string{advisorygroup.EntityKey(groupID)},
			EnqueuedAt:   time.Now().UTC().Add(-time.Minute),
			AttemptCount: 1,
		}); err != nil {
			t.Fatalf("Handle(%s): %v", groupID, err)
		}
		rows, err := store.QualifyingAdvisoryGroupWinners(ctx, groupID)
		if err != nil {
			t.Fatalf("reader(%s): %v", groupID, err)
		}
		want := map[string]bool{}
		for _, row := range rows {
			want[row.RepositoryID+"|"+row.PackageID] = true
		}
		got := parityGraphPairs(t, ctx, driver, groupID)
		if len(want) == 0 {
			t.Fatalf("group %s: reader returned no qualifying rows; seed is wrong", groupID)
		}
		for pair := range want {
			if !got[pair] {
				t.Errorf("group %s: Postgres pair %s missing from graph (graph=%v)", groupID, pair, keysOf(got))
			}
		}
		for pair := range got {
			if !want[pair] {
				t.Errorf("group %s: graph pair %s has no qualifying Postgres row (pg=%v)", groupID, pair, keysOf(want))
			}
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parityGraphPairs(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, groupID string) map[string]bool {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx,
		`MATCH (r:Repository)-[e:AFFECTED_BY]->(g:AdvisoryGroup {id: $id}) RETURN r.id + '|' + e.package_id AS pair`,
		map[string]any{"id": groupID})
	if err != nil {
		t.Fatalf("graph readback: %v", err)
	}
	out := map[string]bool{}
	for result.Next(ctx) {
		pair, _ := result.Record().Get("pair")
		if s, ok := pair.(string); ok {
			out[s] = true
		}
	}
	if err := result.Err(); err != nil {
		t.Fatalf("graph readback rows: %v", err)
	}
	return out
}

func seedParityWinners(t *testing.T, ctx context.Context, db *sql.DB, groupA, groupB, repoA, repoB string) {
	t.Helper()
	p := parityPrefix
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
		  VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', '{}'::jsonb)
		  ON CONFLICT (scope_id) DO NOTHING`, []any{p + "scope"}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
		  VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)
		  ON CONFLICT (generation_id) DO NOTHING`, []any{p + "gen", p + "scope"}},
		{`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
		  VALUES ($1, $2, $3, 'reducer_supply_chain_impact_finding', $1, 'synthetic', $1, NOW(), NOW(), $4::jsonb)
		  ON CONFLICT (fact_id) DO NOTHING`, []any{p + "fact-a", p + "scope", p + "gen", fmt.Sprintf(`{"purl": "pkg:npm/parity-a@1", "advisory_group_members": [%q, "GHSA-parity-m"]}`, groupA)}},
		{`INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at) VALUES (1, NOW())
		  ON CONFLICT (singleton) DO UPDATE SET materialized_at = NOW()`, nil},
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seed parity base: %v", err)
		}
	}
	type winner struct{ key, fact, status, repo, pkg, cve, advisory, suppression string }
	seed := []winner{
		{"a1", p + "fact-a", "affected_exact", repoA, "pkg:npm/parity-a", groupA, "GHSA-parity-a", "active"},
		{"a2", p + "missing", "possibly_affected", repoB, "pkg:npm/parity-a", groupA, "GHSA-parity-a", "active"},
		{"a3-fixed", p + "missing", "not_affected_known_fixed", repoA, "pkg:npm/parity-fixed", groupA, "GHSA-parity-a", "active"},
		{"a4-hidden", p + "missing", "affected_exact", repoB, "pkg:npm/parity-hidden", groupA, "GHSA-parity-a", "not_affected"},
		{"b1", p + "missing", "affected_derived", repoB, "pkg:npm/parity-b", "", groupB, "active"},
	}
	for _, row := range seed {
		if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_canonical_winners
    (canonical_key, winner_fact_id, winner_scope_id, finding_id, impact_status,
     repository_id, cve_id, advisory_id, package_id, ecosystem,
     suppression_state, suppression_expires_at, materialized_at)
    VALUES ($1, $2, 'static-scope', $3, $4, $5, $6, $7, $8, 'npm', $9, NULL, NOW())`,
			p+"winner-"+row.key, row.fact, p+"finding-"+row.key, row.status, row.repo, row.cve, row.advisory, row.pkg, row.suppression,
		); err != nil {
			t.Fatalf("seed parity winner %q: %v", row.key, err)
		}
	}
}
