// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8838 disposable PostgreSQL proof of the security-alert target-set read on the bootstrapped schema: generations committed through the real IngestionStore record the collected repository set in the payload, a requested repository outside the latest committed generation's set is reported uncollected until a generation that holds it commits, an equal freshness hint skips the commit so the set must move the hint, a newer pending generation is ignored, and a generation committed before the record existed reports nothing uncollected; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// commitTargetGeneration commits one security-alert generation, with no facts,
// through the production ingestion store.
func (f *collectionStateLiveFixture) commitTargetGeneration(
	scopeID, generationID, hint string,
	ingestedAt time.Time,
	set scope.SecurityAlertTargetSet,
) {
	f.t.Helper()
	store := storagepostgres.NewIngestionStore(storagepostgres.SQLDB{DB: f.database})
	store.Now = func() time.Time { return ingestedAt }
	factStream := make(chan facts.Envelope)
	close(factStream)
	err := store.CommitScopeGeneration(f.ctx, scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  string(scope.CollectorSecurityAlert),
		ScopeKind:     scope.KindSecurityAlert,
		CollectorKind: scope.CollectorSecurityAlert,
		PartitionKey:  scopeID,
	}, scope.ScopeGeneration{
		GenerationID:           generationID,
		ScopeID:                scopeID,
		ObservedAt:             ingestedAt,
		IngestedAt:             ingestedAt,
		Status:                 scope.GenerationStatusPending,
		TriggerKind:            scope.TriggerKindSnapshot,
		FreshnessHint:          hint,
		SecurityAlertTargetSet: set,
	}, factStream)
	if err != nil {
		f.t.Fatalf("CommitScopeGeneration(%s) error = %v", generationID, err)
	}
}

// activate stands in for the projector: it supersedes the scope's active
// generation and activates generationID.
func (f *collectionStateLiveFixture) activate(scopeID, generationID string) {
	f.t.Helper()
	f.exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = $2
WHERE scope_id = $1 AND status = 'active'`, scopeID, f.asOf)
	f.exec(`UPDATE scope_generations SET status = 'active', activated_at = $2 WHERE generation_id = $1`, generationID, f.asOf)
	f.exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
}

func (f *collectionStateLiveFixture) generationCount(scopeID string) int {
	f.t.Helper()
	var count int
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT count(*) FROM scope_generations WHERE scope_id = $1`, scopeID).Scan(&count); err != nil {
		f.t.Fatalf("count generations: %v", err)
	}
	return count
}

func (f *collectionStateLiveFixture) uncollected(scopeID string, slugs []string) (bool, []string) {
	f.t.Helper()
	store := NewPostgresSecurityAlertCollectionStateStoreWithReadStore(storagepostgres.SQLDB{DB: f.database})
	states, err := store.SecurityAlertTargetStates(f.ctx, []string{scopeID}, slugs)
	if err != nil {
		f.t.Fatalf("SecurityAlertTargetStates() error = %v", err)
	}
	if len(states) != 1 {
		f.t.Fatalf("states = %+v, want one", states)
	}
	return states[0].CommittedGeneration, states[0].UncollectedRequested
}

// TestSecurityAlertTargetSetAllowlistChangeLive walks the #8838 acceptance
// sequence on real Postgres: an allowlist change between two cycles, checked
// before and after the second projection.
func TestSecurityAlertTargetSetAllowlistChangeLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t, "sa_target_set")
	requested := []string{"acme/a", "acme/new"}
	cycle1 := scope.NewSecurityAlertTargetSet([]string{"acme/a"})
	cycle2 := scope.NewSecurityAlertTargetSet([]string{"acme/a", "acme/new"})

	// Cycle 1 projects with the allowlist before acme/new was added.
	f.commitTargetGeneration("t-org", "gen-1", "sha256:alerts-a", f.asOf.Add(-time.Hour), cycle1)
	f.activate("t-org", "gen-1")
	committed, uncollected := f.uncollected("t-org", requested)
	if !committed || !reflect.DeepEqual(uncollected, []string{"acme/new"}) {
		t.Fatalf("after cycle 1: committed=%v uncollected=%v, want true [acme/new]", committed, uncollected)
	}

	// The theory behind moving the freshness hint: an equal hint with a new
	// set is skipped as unchanged, so the set alone never reaches Postgres.
	f.commitTargetGeneration("t-org", "gen-1b", "sha256:alerts-a", f.asOf.Add(-30*time.Minute), cycle2)
	if got := f.generationCount("t-org"); got != 1 {
		t.Fatalf("generations after an equal-hint commit = %d, want 1 (skipped as unchanged)", got)
	}

	// Cycle 2 commits with a hint the set moved, but has not projected: a
	// pending generation is not committed, so acme/new is still uncollected.
	f.commitTargetGeneration("t-org", "gen-2", "sha256:alerts-a+set", f.asOf.Add(-10*time.Minute), cycle2)
	committed, uncollected = f.uncollected("t-org", requested)
	if !committed || !reflect.DeepEqual(uncollected, []string{"acme/new"}) {
		t.Fatalf("cycle 2 pending: committed=%v uncollected=%v, want true [acme/new]", committed, uncollected)
	}

	// Cycle 2 projects: the repository is collected.
	f.activate("t-org", "gen-2")
	committed, uncollected = f.uncollected("t-org", requested)
	if !committed || len(uncollected) != 0 {
		t.Fatalf("after cycle 2: committed=%v uncollected=%v, want true []", committed, uncollected)
	}

	// An unanchored read requests no slug and reports none.
	if _, uncollected = f.uncollected("t-org", nil); len(uncollected) != 0 {
		t.Fatalf("unanchored read uncollected = %v, want none", uncollected)
	}
}

// TestSecurityAlertTargetSetLegacyAndEmptyLive pins the two edges: a
// generation committed before the record existed reports nothing uncollected,
// and a recorded empty set reports every requested repository.
func TestSecurityAlertTargetSetLegacyAndEmptyLive(t *testing.T) {
	f := newCollectionStateLiveFixture(t, "sa_target_set_edges")

	f.commitTargetGeneration("t-legacy", "gen-legacy", "sha256:legacy", f.asOf.Add(-time.Hour), scope.SecurityAlertTargetSet{})
	f.activate("t-legacy", "gen-legacy")
	committed, uncollected := f.uncollected("t-legacy", []string{"acme/a"})
	if !committed || len(uncollected) != 0 {
		t.Fatalf("legacy: committed=%v uncollected=%v, want true []", committed, uncollected)
	}

	f.commitTargetGeneration("t-empty", "gen-empty", "sha256:empty", f.asOf.Add(-time.Hour), scope.NewSecurityAlertTargetSet(nil))
	f.activate("t-empty", "gen-empty")
	committed, uncollected = f.uncollected("t-empty", []string{"acme/b", "acme/a"})
	if !committed || !reflect.DeepEqual(uncollected, []string{"acme/a", "acme/b"}) {
		t.Fatalf("empty set: committed=%v uncollected=%v, want true [acme/a acme/b]", committed, uncollected)
	}
}
