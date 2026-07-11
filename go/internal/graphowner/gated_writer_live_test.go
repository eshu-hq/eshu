// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestLiveGatedWriterEndToEnd proves the #5007 owner-ledger gate over a real
// graph write path (Postgres owner ledger + NornicDB graph): a single writer's
// non-contended batch lands byte-for-byte (the gate is a no-op), and two
// concurrent cross-scope writers racing the same uid converge the graph node to
// the max-(observed_at, source_fact_id) contributor with zero lost updates,
// over many trials. This is the production-shape concurrency proof for the
// gated writer, complementing the store-level proof.
//
// Skipped by default; set ESHU_OWNER_LEDGER_PROVE_LIVE=1, ESHU_OWNER_LEDGER_PG_DSN,
// and the ESHU_GRAPH_BACKEND/ESHU_NEO4J_URI env.
func TestLiveGatedWriterEndToEnd(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OWNER_LEDGER_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OWNER_LEDGER_PROVE_LIVE=1 (+ PG DSN + NornicDB env) to run the gated-writer end-to-end proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_OWNER_LEDGER_PG_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_OWNER_LEDGER_PG_DSN is required")
	}
	ctx := context.Background()

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = rawDB.Close() }()
	sqldb := postgres.SQLDB{DB: rawDB}
	if err := postgres.NewGraphNodeOwnerStore().EnsureSchema(ctx, sqldb); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		cc, ccl := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccl()
		_ = driver.Close(cc)
	}()
	if err := graphOwnerProbeExec(ctx, driver, cfg.DatabaseName,
		`CREATE CONSTRAINT owner_gate_probe_uid_unique IF NOT EXISTS FOR (n:OwnerGateProbe) REQUIRE n.uid IS UNIQUE`, nil); err != nil {
		t.Fatalf("create constraint: %v", err)
	}

	gate := NewGate(sqldb)
	// underlying is a representative canonical node write: a plain MERGE+SET of
	// each row's own values (no CASE), the shape every family's cypher writer
	// uses. The gate hands it only the owned rows.
	underlying := func(ctx context.Context, rows []map[string]any, _ string) error {
		for attempt := 0; attempt < 8; attempt++ {
			err := graphOwnerProbeExec(ctx, driver, cfg.DatabaseName,
				`UNWIND $rows AS row MERGE (n:OwnerGateProbe {uid: row.uid}) SET n.source_order_key = row.source_order_key, n.value = row.value`,
				map[string]any{"rows": rows})
			if err == nil {
				return nil
			}
			if !strings.Contains(err.Error(), "Outdated") && !strings.Contains(err.Error(), "already exists") {
				return err
			}
			time.Sleep(2 * time.Millisecond)
		}
		return fmt.Errorf("graph write exhausted retries")
	}
	writer := NewCloudResourceGatedWriter(gate, underlying)

	t.Run("non_contended_writes_own_row", func(t *testing.T) {
		uid := fmt.Sprintf("gate-solo-%d", time.Now().UnixNano())
		rows := []map[string]any{{"uid": uid, "source_order_key": "1000-a", "value": "solo"}}
		if err := writer.WriteCloudResourceNodes(ctx, rows, "reducer/aws-resources"); err != nil {
			t.Fatalf("write: %v", err)
		}
		k, v := graphOwnerProbeRead(t, driver, cfg.DatabaseName, uid)
		if k != "1000-a" || v != "solo" {
			t.Fatalf("non-contended graph = (%q,%q), want (1000-a, solo) — the gate must write the writer's own row unchanged", k, v)
		}
	})

	t.Run("concurrent_cross_scope_converges_to_max", func(t *testing.T) {
		const trials = 50
		var lost int
		for trial := 0; trial < trials; trial++ {
			uid := fmt.Sprintf("gate-conc-%d", trial)
			// two "scopes" racing the same uid with different order keys/values
			scopes := []struct{ key, value string }{{"1000-low", "low"}, {"2000-high", "high"}}
			var wg sync.WaitGroup
			errs := make([]error, len(scopes))
			wg.Add(len(scopes))
			for i, s := range scopes {
				i, s := i, s
				go func() {
					defer wg.Done()
					errs[i] = writer.WriteCloudResourceNodes(ctx,
						[]map[string]any{{"uid": uid, "source_order_key": s.key, "value": s.value}},
						"reducer/aws-resources")
				}()
			}
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatalf("trial %d scope %d: %v", trial, i, e)
				}
			}
			k, v := graphOwnerProbeRead(t, driver, cfg.DatabaseName, uid)
			if k != "2000-high" || v != "high" {
				lost++
				if lost <= 3 {
					t.Logf("trial %d graph = (%q,%q), want (2000-high, high)", trial, k, v)
				}
			}
		}
		t.Logf("gated-writer concurrent cross-scope: %d trials, %d graph lost", trials, lost)
		if lost > 0 {
			t.Fatalf("gated writer is NOT lost-update-free: %d/%d trials lost the max winner", lost, trials)
		}
	})
}

func graphOwnerProbeExec(ctx context.Context, driver neo4jdriver.DriverWithContext, database, cypher string, params map[string]any) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func graphOwnerProbeRead(t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) (string, string) {
	t.Helper()
	ctx := context.Background()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (n:OwnerGateProbe {uid: $uid}) RETURN n.source_order_key AS k, n.value AS v`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read graph single: %v", err)
	}
	k, _ := record.Get("k")
	v, _ := record.Get("v")
	ks, _ := k.(string)
	vs, _ := v.(string)
	return ks, vs
}
