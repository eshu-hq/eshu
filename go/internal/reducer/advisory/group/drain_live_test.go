// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package group_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
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

// TestAdvisoryGroupQueueDrainLive is the queue-drain half of #10411
// criterion 9: winners seeded in Postgres are enqueued through the
// production AdvisoryGroupProjectionEnqueuer, claimed through the
// production reducer queue (conflict-key partition included), handled by
// the production handler, and acked, until the queue drains empty and the
// graph readback matches the qualifying winners. It needs both backends:
//
//	cd go && ESHU_POSTGRES_TEST_DSN=postgres://... ESHU_NEO4J_URI=bolt://127.0.0.1:<port> \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j go test -tags live_nornicdb_answer_truth \
//	  ./internal/reducer/advisory/group -run '^TestAdvisoryGroupQueueDrainLive' -count=1 -v
func TestAdvisoryGroupQueueDrainLive(t *testing.T) {
	pgDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	neoURI := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if pgDSN == "" || neoURI == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN and ESHU_NEO4J_URI to run the advisory-group queue-drain proof")
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

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	groupA := "CVE-10411-DRAIN-" + suffix
	repo := parityPrefix + "drain-repo-" + suffix
	seedParityWinners(t, ctx, db, groupA, "GHSA-10411-drain-"+suffix, repo, repo)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, q := range []string{
			`DELETE FROM fact_work_items WHERE scope_id LIKE '` + parityPrefix + `%'`,
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
			Cypher:     `MATCH (n) WHERE (n:Repository AND n.id = $repo) OR (n:AdvisoryGroup AND n.id = $group) DETACH DELETE n`,
			Parameters: map[string]any{"repo": repo, "group": groupA},
		})
	})
	if err := exec.Execute(ctx, sourcecypher.Statement{Cypher: `MERGE (:Repository {id: $id})`, Parameters: map[string]any{"id": repo}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "advisory-drain-proof-"+suffix, time.Minute)
	queue.ClaimDomains = []reducercontract.Domain{reducercontract.DomainAdvisoryGroupProjection}
	enqueuer := postgres.NewAdvisoryGroupProjectionEnqueuer(queue)
	origin := reducercontract.Intent{ScopeID: parityPrefix + "scope", GenerationID: parityPrefix + "gen"}
	if err := enqueuer.EnqueueAdvisoryGroupProjections(ctx, origin, []string{groupA}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// The winners watermark must pass the intent's enqueue time before the
	// handler acknowledges; the production maintainer advances it on its
	// cadence, so advance it here the same way after the enqueue.
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_winners_materialization SET materialized_at = NOW() + INTERVAL '2 seconds' WHERE singleton = 1`); err != nil {
		t.Fatalf("advance winners watermark: %v", err)
	}

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	handler := advisorygroup.Handler{Winners: store, Writer: edgewriter.NewAdvisoryGroupWriter(exec, 0)}
	drained := 0
	for range 5 {
		intent, ok, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			break
		}
		result, err := handler.Handle(ctx, intent)
		if err != nil {
			t.Fatalf("handle claimed intent: %v", err)
		}
		if err := queue.Ack(ctx, intent, result); err != nil {
			t.Fatalf("ack: %v", err)
		}
		drained++
	}
	if drained != 1 {
		t.Fatalf("drained = %d intents, want exactly 1 (queue must empty after ack)", drained)
	}
	got := parityGraphPairs(t, ctx, driver, groupA)
	if !got[repo+"|pkg:npm/parity-a"] {
		t.Fatalf("graph pairs = %v, want the qualifying pair for %s", keysOf(got), groupA)
	}
}
