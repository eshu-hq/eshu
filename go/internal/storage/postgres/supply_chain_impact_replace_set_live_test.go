// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Issue #6831: the supply-chain impact writer must treat each pass's finding
// set as the complete truth for its (scope, generation). A finding an earlier
// pass emitted that the current pass no longer derives -- the repo-less twin a
// pass without repository anchoring evidence produced -- must be tombstoned,
// or it stays visible on the query surface forever and defeats a suppression
// scoped to the anchored repository.
const (
	replaceSetLiveScope      = "scope:6831:replace-set"
	replaceSetLiveGeneration = "generation:6831:replace-set"
	replaceSetLiveCVE        = "CVE-2026-68310"
	replaceSetLivePackage    = "pkg:deb/debian/openssl"
	replaceSetLiveRepository = "repository:r_6831_anchor"
)

func TestSupplyChainImpactWriterRetractsSupersededFindingsLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)

	repoLess := replaceSetFinding("")
	anchored := replaceSetFinding(replaceSetLiveRepository)
	other := replaceSetFinding("")
	other.PackageID = "pkg:deb/debian/zlib"

	first := replaceSetWrite("intent:6831:first", repoLess, other)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, first); err != nil {
		t.Fatalf("first write: %v", err)
	}
	second := replaceSetWrite("intent:6831:second", anchored)
	result, err := writer.WriteSupplyChainImpactFindings(ctx, second)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	active := replaceSetActiveRepositories(t, ctx, db)
	if len(active) != 1 || active[0] != replaceSetLiveRepository {
		t.Fatalf("active finding repositories = %q, want only %q", active, replaceSetLiveRepository)
	}
	if got := replaceSetTombstoneCount(t, ctx, db); got != 2 {
		t.Fatalf("tombstoned finding rows = %d, want 2 (rows are retracted, never deleted)", got)
	}

	store := query.NewPostgresSupplyChainImpactFindingStore(db)
	findings, err := store.ListSupplyChainImpactFindings(ctx, query.SupplyChainImpactFindingFilter{
		CVEID:             replaceSetLiveCVE,
		IncludeSuppressed: true,
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("query findings: %v", err)
	}
	if len(findings) != 1 || findings[0].RepositoryID != replaceSetLiveRepository {
		t.Fatalf("query surface findings = %#v, want only the anchored finding", findings)
	}
	if result.FactsRetracted != 2 {
		t.Fatalf("FactsRetracted = %d, want 2 (repo-less twin + dropped package)", result.FactsRetracted)
	}

	// Re-emitting a retracted finding revives it: the upsert resets
	// is_tombstone, and the absent anchored row is retracted in turn.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, first); err != nil {
		t.Fatalf("re-emit write: %v", err)
	}
	active = replaceSetActiveRepositories(t, ctx, db)
	if len(active) != 2 || active[0] != "" || active[1] != "" {
		t.Fatalf("active repositories after re-emit = %q, want the two repo-less rows", active)
	}
}

func TestSupplyChainImpactWriterRetractionIsIdempotentLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	write := replaceSetWrite("intent:6831:replay", replaceSetFinding(replaceSetLiveRepository))

	for attempt := 1; attempt <= 2; attempt++ {
		result, err := writer.WriteSupplyChainImpactFindings(ctx, write)
		if err != nil {
			t.Fatalf("write attempt %d: %v", attempt, err)
		}
		if result.FactsRetracted != 0 {
			t.Fatalf("attempt %d FactsRetracted = %d, want 0 (redelivery of the same pass)", attempt, result.FactsRetracted)
		}
	}
	if active := replaceSetActiveRepositories(t, ctx, db); len(active) != 1 {
		t.Fatalf("active rows after replay = %q, want 1", active)
	}
}

func TestSupplyChainImpactWriterRetractionHonorsBoundaryLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)

	// A row carrying a higher fencing token than this writer's pass was
	// written by a fresher writer and must never be retracted by this one.
	replaceSetPlantRow(t, ctx, db, "fenced", replaceSetLiveScope, replaceSetLiveGeneration,
		facts.ReducerSupplyChainImpactFindingFactKind, 7)
	// Rows outside the (scope, generation, fact_kind) conflict domain are
	// never this pass's to retract.
	replaceSetPlantRow(t, ctx, db, "other-kind", replaceSetLiveScope, replaceSetLiveGeneration,
		"reducer_other_finding", 0)
	replaceSetPlantRow(t, ctx, db, "other-generation", replaceSetLiveScope, replaceSetLiveGeneration+":prior",
		facts.ReducerSupplyChainImpactFindingFactKind, 0)

	if _, err := writer.WriteSupplyChainImpactFindings(ctx,
		replaceSetWrite("intent:6831:boundary", replaceSetFinding(replaceSetLiveRepository))); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, factID := range []string{"fenced", "other-kind", "other-generation"} {
		if replaceSetIsTombstone(t, ctx, db, factID) {
			t.Fatalf("row %q was retracted; it is outside this pass's fenced conflict domain", factID)
		}
	}
}

func TestSupplyChainImpactWriterPartialEvidenceSkipsRetractionLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)

	if _, err := writer.WriteSupplyChainImpactFindings(ctx,
		replaceSetWrite("intent:6831:full", replaceSetFinding(""))); err != nil {
		t.Fatalf("full write: %v", err)
	}
	// A pass whose active-evidence expansion was truncated has not seen the
	// whole evidence set, so its finding set is not authoritative: absent
	// rows must stay visible rather than be hidden by a bounded load.
	partial := replaceSetWrite("intent:6831:partial", replaceSetFinding(replaceSetLiveRepository))
	partial.PartialEvidence = true
	result, err := writer.WriteSupplyChainImpactFindings(ctx, partial)
	if err != nil {
		t.Fatalf("partial write: %v", err)
	}
	if result.FactsRetracted != 0 {
		t.Fatalf("partial pass FactsRetracted = %d, want 0", result.FactsRetracted)
	}
	if active := replaceSetActiveRepositories(t, ctx, db); len(active) != 2 {
		t.Fatalf("active rows after partial pass = %q, want 2", active)
	}
}

func openReplaceSetLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #6831 replace-set proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t, ctx, dsn)
	t.Cleanup(func() { _ = db.Close() })
	replaceSetSeedScope(t, ctx, db)
	return ctx, db
}

func replaceSetFinding(repositoryID string) reducer.SupplyChainImpactFinding {
	return reducer.SupplyChainImpactFinding{
		CVEID:           replaceSetLiveCVE,
		PackageID:       replaceSetLivePackage,
		Ecosystem:       "deb",
		ObservedVersion: "3.0.11-1",
		Status:          reducer.SupplyChainImpactAffectedExact,
		RepositoryID:    repositoryID,
	}
}

func replaceSetWrite(intentID string, findings ...reducer.SupplyChainImpactFinding) reducer.SupplyChainImpactWrite {
	return reducer.SupplyChainImpactWrite{
		IntentID:     intentID,
		ScopeID:      replaceSetLiveScope,
		GenerationID: replaceSetLiveGeneration,
		SourceSystem: "vulnerability_intelligence",
		Cause:        "synthetic #6831 replace-set proof",
		Findings:     findings,
	}
}
