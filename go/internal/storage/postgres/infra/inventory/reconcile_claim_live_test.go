// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// isolatedDB creates a fresh database next to ESHU_POSTGRES_DSN's and applies
// the bootstrap schema, for a proof that needs to know every repository in
// it. It needs a URL-form DSN whose role may CREATE DATABASE.
func isolatedDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	base, ctx := liveDB(t)
	dsn, err := url.Parse(os.Getenv("ESHU_POSTGRES_DSN"))
	if err != nil || dsn.Scheme == "" {
		t.Skip("isolated database proofs need a URL-form ESHU_POSTGRES_DSN")
	}
	name := fmt.Sprintf("infra_claim_%d", time.Now().UnixNano())
	if _, err := base.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create an isolated database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = base.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	dsn.Path = "/" + name
	isolated, err := inventory.OpenWriterDB(dsn.String())
	if err != nil {
		t.Fatalf("open isolated db: %v", err)
	}
	t.Cleanup(func() { _ = isolated.Close() })
	schemaCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := postgres.ApplyBootstrap(schemaCtx, postgres.SQLDB{DB: isolated}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	isolatedDSNs.Store(isolated, dsn.String())
	return isolated, ctx
}

// isolatedDSNs remembers each isolated database's DSN so a test can open a
// second, unfenced connection to it.
var isolatedDSNs sync.Map

// isolatedPlainDB opens a connection to an isolatedDB database without the
// derive-aware writer setting, the way a pre-read-model binary connects.
func isolatedPlainDB(t *testing.T, isolated *sql.DB) *sql.DB {
	t.Helper()
	dsn, ok := isolatedDSNs.Load(isolated)
	if !ok {
		t.Fatal("isolatedPlainDB needs a database from isolatedDB")
	}
	plain, err := sql.Open("pgx", dsn.(string))
	if err != nil {
		t.Fatalf("open plain isolated db: %v", err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	return plain
}

// TestReconcileCycleLiveReplicasClaimDisjointPages proves the persisted walk
// is a shared work queue: two replicas that run a cycle at the same moment
// claim disjoint pages, together they cover every repository, and the claim
// that reaches the end wraps the walk to the start.
func TestReconcileCycleLiveReplicasClaimDisjointPages(t *testing.T) {
	sqlDB, ctx := isolatedDB(t)
	database := postgres.SQLDB{DB: sqlDB}
	recordMarker(t, ctx, database)
	repos := []string{"repo-a", "repo-b", "repo-c", "repo-d", "repo-e"}
	for _, repo := range repos {
		seedDerivedRepo(t, ctx, database, repo,
			contentRow{id: "e", path: "main.tf", entityType: "TerraformResource", name: "r"})
	}

	var seen []string
	wraps := 0
	for round := range 2 {
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			batches [][]string
		)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				batch, err := inventory.ReconcileCycle(ctx, database, inventory.ReconcileRequest{Budget: 2, Persist: true})
				if err != nil {
					t.Errorf("ReconcileCycle() error = %v", err)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				batches = append(batches, repoIDs(batch.Repos))
				if batch.NextCursor == "" {
					wraps++
				}
			}()
		}
		wg.Wait()
		if len(batches) != 2 {
			t.Fatalf("round %d: %d batches, want 2", round, len(batches))
		}
		for _, repo := range batches[0] {
			if slices.Contains(batches[1], repo) {
				t.Fatalf("round %d: replicas both claimed %s (%v vs %v)", round, repo, batches[0], batches[1])
			}
		}
		seen = append(seen, batches[0]...)
		seen = append(seen, batches[1]...)
	}
	// Claims in lock order: [a b] [c d] [e] (reaches the end, wraps) [a b].
	for _, repo := range repos {
		if !slices.Contains(seen, repo) {
			t.Fatalf("repository %s was never claimed across two rounds: %v", repo, seen)
		}
	}
	if len(seen) != 7 || wraps != 1 {
		t.Fatalf("claimed %v with %d wraps, want 7 claims ([a b] [c d] [e] [a b]) and exactly 1 wrap", seen, wraps)
	}
	stored, err := inventory.LoadCursor(ctx, database)
	if err != nil {
		t.Fatalf("LoadCursor() error = %v", err)
	}
	if stored != "repo-b" {
		t.Fatalf("stored cursor after the wrapped page = %q, want repo-b", stored)
	}
}
