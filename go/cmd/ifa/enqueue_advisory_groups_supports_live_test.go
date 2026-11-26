// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11201 advisory-group cell admission on real PostgreSQL: the committed advisory-group cassette's finding facts are admitted through the production impact writer's transaction shape, every live finding fact gets its support in the same transaction, a tombstoned fact gets none, the old-writer fence stays empty so the supports table can read ready, and the previous admission path (facts only) is shown to mark its scope dirty
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	advisoryCellScope      = "osv:eshu-fixture-advisory-findings"
	advisoryCellGeneration = "gen-ifa-advisory-group-family-findings-1"
)

func openAdvisoryCellDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set; skipping the advisory-group admission proof")
	}
	admin, err := postgresproof.WithDatabase(dsn, "postgres")
	if err != nil {
		t.Fatalf("derive administrative DSN: %v", err)
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, admin, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		})
	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'osv', $1, 'osv', $1, $2, $2, 'active', '{}'::jsonb)`, advisoryCellScope, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, advisoryCellGeneration, advisoryCellScope, now); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		advisoryCellGeneration, advisoryCellScope); err != nil {
		t.Fatalf("activate generation: %v", err)
	}
	return ctx, database
}

func advisoryCellOptions(t *testing.T) enqueueAdvisoryGroupsOptions {
	t.Helper()
	return enqueueAdvisoryGroupsOptions{
		cassettePath: filepath.Join("..", "..", "..", "testdata", "cassettes", "advisorygroup", "ifa-advisory-group-family.json"),
		scopeID:      advisoryCellScope, generationID: advisoryCellGeneration, sourceSystem: "osv",
	}
}

func advisoryCellCount(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

// firstSeenStampOfSupport returns the support payload's first_seen_at
// stamp, failing the proof when the payload shape cannot carry one or the
// stamp is malformed. The admission stamps every row it upserts
// (first_seen.go: firstSeenProvenanceKey), so a stamp-less or malformed
// support means the stamp step was skipped or regressed.
func firstSeenStampOfSupport(t *testing.T, factID string, payload any) string {
	t.Helper()
	doc, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("support payload for fact %s is not a JSON object", factID)
	}
	provenance, ok := doc["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("support payload for fact %s carries no provenance object", factID)
	}
	stamp, _ := provenance["first_seen_at"].(string)
	if stamp == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		t.Fatalf("support payload for fact %s carries malformed first_seen_at %q: %v", factID, stamp, err)
	}
	return stamp
}

// stripFirstSeenStampKeys deletes the admission's #11031 stamp keys
// (first_seen.go: firstSeenProvenanceKey, publishSourceProvenanceKey) from a
// decoded finding payload so the verbatim comparison sees the finding
// content. It tolerates payloads without them, such as the cassette side.
func stripFirstSeenStampKeys(payload any) {
	doc, ok := payload.(map[string]any)
	if !ok {
		return
	}
	provenance, ok := doc["provenance"].(map[string]any)
	if !ok {
		return
	}
	delete(provenance, "first_seen_at")
	delete(provenance, "selected_publish_source")
}

// advisoryCellSupportsEqualLiveRows proves every live cassette finding row
// has exactly one support in the scope and that each support's payload is the
// row's payload apart from the admission's #11031 first-seen stamp
// (provenance.first_seen_at plus provenance.selected_publish_source, claimed
// and stamped by admitFindingRowsInTx before the supports upsert). The
// comparison parses both sides: the supports table stores JSONB (whose text
// form orders keys its own way) while the rows carry the cassette's
// re-marshaled bytes, so a text comparison would be order-sensitive where the
// contract is content.
func advisoryCellSupportsEqualLiveRows(t *testing.T, ctx context.Context, database *sql.DB, rows []factwrite.VersionedRow) {
	t.Helper()
	live := map[string]json.RawMessage{}
	for _, row := range rows {
		if !row.IsTombstone {
			live[row.FactID] = json.RawMessage(row.Payload)
		}
	}
	stored, err := database.QueryContext(ctx,
		`SELECT fact_id, payload::text FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, advisoryCellScope)
	if err != nil {
		t.Fatalf("read supports: %v", err)
	}
	defer func() { _ = stored.Close() }()
	seen := map[string]bool{}
	for stored.Next() {
		var factID, payload string
		if err := stored.Scan(&factID, &payload); err != nil {
			t.Fatalf("scan support: %v", err)
		}
		if seen[factID] {
			t.Fatalf("duplicate support for fact %s: one live row must have one support", factID)
		}
		seen[factID] = true
		wantRaw, ok := live[factID]
		if !ok {
			t.Fatalf("support for fact %s names no live cassette row", factID)
		}
		var want, got any
		if err := json.Unmarshal(wantRaw, &want); err != nil {
			t.Fatalf("decode cassette payload %s: %v", factID, err)
		}
		if err := json.Unmarshal([]byte(payload), &got); err != nil {
			t.Fatalf("decode support payload %s: %v", factID, err)
		}
		// The admission stamps every row before the upsert, so the stamp
		// keys cannot equal the cassette's; pin them instead of comparing
		// them, then compare the rest verbatim.
		if stamp := firstSeenStampOfSupport(t, factID, got); stamp == "" {
			t.Fatalf("support payload for fact %s carries no first_seen_at stamp", factID)
		}
		stripFirstSeenStampKeys(want)
		stripFirstSeenStampKeys(got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("support payload for fact %s differs from the cassette finding payload", factID)
		}
	}
	if err := stored.Err(); err != nil {
		t.Fatalf("iterate supports: %v", err)
	}
	if len(seen) != len(live) {
		t.Fatalf("supports with a live cassette row = %d, want %d", len(seen), len(live))
	}
}

// TestAdvisoryGroupCellAdmitsFindingsWithSupportsLive proves #11201 on the
// committed cassette: the admission writes a support for every live finding
// fact in the same transaction and leaves the old-writer fence empty.
func TestAdvisoryGroupCellAdmitsFindingsWithSupportsLive(t *testing.T) {
	ctx, database := openAdvisoryCellDatabase(t)
	o := advisoryCellOptions(t)
	rows, err := cassetteFindingRows(o)
	if err != nil {
		t.Fatalf("cassetteFindingRows() error = %v, want nil", err)
	}
	live := 0
	for _, row := range rows {
		if !row.IsTombstone {
			live++
		}
	}
	if live == 0 || live == len(rows) {
		t.Fatalf("cassette rows = %d, live = %d: the proof needs live and tombstoned findings", len(rows), live)
	}

	admitted, err := admitCassetteFindings(ctx, postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: database}}, o)
	if err != nil {
		t.Fatalf("admitCassetteFindings() error = %v, want nil", err)
	}
	if admitted != len(rows) {
		t.Fatalf("admitted = %d, want %d facts", admitted, len(rows))
	}
	if got := advisoryCellCount(t, ctx, database, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, advisoryCellScope); got != live {
		t.Fatalf("supports rows = %d, want %d (one per live finding fact)", got, live)
	}
	// The payload is the finding payload apart from the #11031 first-seen
	// stamp: since PR-C2 the admission writes supports only and no finding
	// fact, so the check compares each live cassette row's payload against
	// its support's payload in Go (parsed JSON, keyed by fact id) instead of
	// joining the frozen fact table.
	advisoryCellSupportsEqualLiveRows(t, ctx, database, rows)
	if got := advisoryCellCount(t, ctx, database, `SELECT count(*) FROM supply_chain_impact_finding_supports_dirty`); got != 0 {
		t.Fatalf("dirty fence scopes = %d, want 0: the admission must run under the supports writer setting", got)
	}
	// A replay rewrites no support row version.
	before := advisoryCellCount(t, ctx, database, `SELECT COALESCE(sum(xmin::text::bigint), 0)::int FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, advisoryCellScope)
	if _, err := admitCassetteFindings(ctx, postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: database}}, o); err != nil {
		t.Fatalf("replay error = %v, want nil", err)
	}
	if after := advisoryCellCount(t, ctx, database, `SELECT COALESCE(sum(xmin::text::bigint), 0)::int FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, advisoryCellScope); after != before {
		t.Fatalf("support row versions changed on replay: %d -> %d", before, after)
	}
	// With the fence empty the backfill reaches ready.
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: database}}
	for step := 0; step < 100; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step: %v", err)
		}
		if report.Complete {
			break
		}
	}
	ready, err := postgres.SupplyChainImpactSupportsReady(ctx, postgres.SQLDB{DB: database})
	if err != nil || !ready {
		t.Fatalf("supports ready = %v, err = %v, want true after the backfill completes", ready, err)
	}
}

// TestAdvisoryGroupCellFactsOnlyAdmissionMarksTheScopeDirtyLive is the
// contrast for #11201: admitting the same rows with the facts-only statement
// (the cell's previous path) leaves no support and fences its scope dirty, so
// the supports table could never read ready on a drive that used it.
func TestAdvisoryGroupCellFactsOnlyAdmissionMarksTheScopeDirtyLive(t *testing.T) {
	ctx, database := openAdvisoryCellDatabase(t)
	rows, err := cassetteFindingRows(advisoryCellOptions(t))
	if err != nil {
		t.Fatalf("cassetteFindingRows() error = %v, want nil", err)
	}
	// The facts-only path is the cell's previous admission: it plants frozen
	// finding history, so the plant runs in a scoped SeedFindingFacts window
	// on the armed database while the dirty-fence assertion runs armed.
	postgresproof.SeedFindingFacts(t, database, func() {
		if err := factwrite.BatchInsertVersionedFacts(ctx, database, rows); err != nil {
			t.Fatalf("facts-only admission: %v", err)
		}
	})
	if got := advisoryCellCount(t, ctx, database, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, advisoryCellScope); got != 0 {
		t.Fatalf("supports after the facts-only admission = %d, want 0", got)
	}
	if got := advisoryCellCount(t, ctx, database, `SELECT count(*) FROM supply_chain_impact_finding_supports_dirty WHERE scope_id = $1`, advisoryCellScope); got != 1 {
		t.Fatalf("dirty fence rows for the scope = %d, want 1: the old admission path fences its scope", got)
	}
}
