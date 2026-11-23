// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11213 and #11215 disposable PostgreSQL 18 proofs: the reducer single-row, batch and versioned fact statements never rewrite a row another scope holds and report it as a typed non-retryable owner conflict with the stored scope, while an own-scope re-save with a newer generation still lands (no generation leg) and a fenced-out stale write is not a conflict; two writers racing one new fact id from two scopes in both orders leave one owner and one typed conflict with no deadlock; enqueue refuses a reducer intent whose non-injective work item id another intent holds while a plain duplicate stays a benign coalesce; reuses the #7978 bootstrapped proof schema; wall time logged not gated
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

func ownerLiveRow(factID, scopeID, generationID string, token int64, tag string, at time.Time) factwrite.Row {
	return factwrite.Row{
		FactID: factID, ScopeID: scopeID, GenerationID: generationID, FactKind: "reducer_owner_proof",
		StableFactKey: "k:" + factID, CollectorKind: "git", SourceConfidence: "inferred",
		SourceSystem: "git", SourceFactKey: "src", ObservedAt: at, IngestedAt: at,
		Payload: `{"owner":"` + tag + `"}`, FencingToken: token,
	}
}

func ownerLiveVersioned(row factwrite.Row) factwrite.VersionedRow {
	return factwrite.VersionedRow{
		FactID: row.FactID, ScopeID: row.ScopeID, GenerationID: row.GenerationID, FactKind: row.FactKind,
		StableFactKey: row.StableFactKey, SchemaVersion: "1.0.0", CollectorKind: row.CollectorKind,
		SourceConfidence: row.SourceConfidence, SourceSystem: row.SourceSystem, SourceFactKey: row.SourceFactKey,
		ObservedAt: row.ObservedAt, IngestedAt: row.IngestedAt, Payload: row.Payload, FencingToken: row.FencingToken,
	}
}

// ownerLiveWriters are the three reducer statements under one signature so a
// single proof drives all of them.
func ownerLiveWriters(store SQLDB) map[string]func(context.Context, factwrite.Row) error {
	single := func(ctx context.Context, row factwrite.Row) error {
		result, err := store.ExecContext(ctx, factwrite.SingleInsertQuery,
			row.FactID, row.ScopeID, row.GenerationID, row.FactKind, row.StableFactKey, row.CollectorKind,
			row.SourceConfidence, row.SourceSystem, row.SourceFactKey, nil, nil, row.ObservedAt, row.IngestedAt, false, row.Payload)
		if err != nil {
			return err
		}
		return factwrite.CheckOwned(ctx, store, result, factwrite.WriterSingle, row.FactID, row.ScopeID, row.GenerationID, row.FactKind)
	}
	return map[string]func(context.Context, factwrite.Row) error{
		"single": single,
		"batch": func(ctx context.Context, row factwrite.Row) error {
			return factwrite.BatchInsertFacts(ctx, store, []factwrite.Row{row})
		},
		"versioned": func(ctx context.Context, row factwrite.Row) error {
			return factwrite.BatchInsertVersionedFacts(ctx, store, []factwrite.VersionedRow{ownerLiveVersioned(row)})
		},
	}
}

// TestReducerFactStatementsRefuseAForeignScopeRowLive is the #11213 live proof
// for all three reducer statements.
func TestReducerFactStatementsRefuseAForeignScopeRowLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	store := SQLDB{DB: p.db}
	now := time.Now().UTC().Truncate(time.Second)
	for _, scopeGen := range [][2]string{{"scope-a", "gen-a1"}, {"scope-b", "gen-b1"}} {
		seedUnchangedSkipScope(t, ctx, p.db, scopeGen[0], scopeGen[1], now)
	}
	if _, err := p.db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at, ingested_at, status, payload)
VALUES ('gen-a2', 'scope-a', 'snapshot', 'owner-proof', $1, $1, 'pending', '{}'::jsonb)`, now); err != nil {
		t.Fatalf("seed second generation: %v", err)
	}

	for name, write := range ownerLiveWriters(store) {
		t.Run(name, func(t *testing.T) {
			id := "owned-" + name
			if err := write(ctx, ownerLiveRow(id, "scope-a", "gen-a1", 1, "a1", now)); err != nil {
				t.Fatalf("owner write: %v", err)
			}
			before := p.queryString(`SELECT row_to_json(f)::text FROM fact_records f WHERE fact_id = $1`, id)

			// Another scope, higher token and different content: refused, row untouched.
			err := write(ctx, ownerLiveRow(id, "scope-b", "gen-b1", 9, "intruder", now.Add(time.Hour)))
			var conflict *factwrite.OwnerConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("foreign write error = %v, want *factwrite.OwnerConflictError", err)
			}
			if conflict.Retryable() || conflict.Count != 1 || conflict.Samples[0].FactID != id ||
				conflict.Samples[0].IncomingScopeID != "scope-b" || conflict.Samples[0].StoredScopeID != "scope-a" {
				t.Fatalf("conflict = %+v, want one non-retryable conflict held by scope-a", conflict)
			}
			if got := p.queryString(`SELECT row_to_json(f)::text FROM fact_records f WHERE fact_id = $1`, id); got != before {
				t.Fatalf("the owner row changed:\n before %s\n after  %s", before, got)
			}

			// The owning scope with a NEWER generation still updates its row: there is
			// no generation leg, so a stable id is not frozen at its first generation.
			if err := write(ctx, ownerLiveRow(id, "scope-a", "gen-a2", 2, "a2", now.Add(2*time.Hour))); err != nil {
				t.Fatalf("own-scope re-save with a newer generation: %v", err)
			}
			if got := p.queryString(`SELECT payload::text FROM fact_records WHERE fact_id = $1`, id); got != `{"owner": "a2"}` {
				t.Fatalf("own-scope re-save did not land: payload = %s", got)
			}

			// A stale write by the owner (lower token) is fenced out, which is not a conflict.
			if name != "single" { // the single statement carries no fencing token
				if err := write(ctx, ownerLiveRow(id, "scope-a", "gen-a2", 1, "stale", now.Add(3*time.Hour))); err != nil {
					t.Fatalf("a fenced-out stale write reported %v, want no error", err)
				}
				if got := p.queryString(`SELECT payload::text FROM fact_records WHERE fact_id = $1`, id); got != `{"owner": "a2"}` {
					t.Fatalf("the stale write landed: payload = %s", got)
				}
			}
		})
	}
}

// TestReducerFactOwnerRaceTwoScopesLive: two transactions write one brand-new
// fact id from two scopes through the reducer batch path, in both orders. The
// second statement waits on the first's uncommitted insert, then re-evaluates the
// conflict WHERE: exactly one scope owns the row, the other gets the typed
// conflict, and nothing deadlocks.
func TestReducerFactOwnerRaceTwoScopesLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedUnchangedSkipScope(t, ctx, p.db, "scope-r1", "gen-r1", now)
	seedUnchangedSkipScope(t, ctx, p.db, "scope-r2", "gen-r2", now)
	sides := [2][2]string{{"scope-r1", "gen-r1"}, {"scope-r2", "gen-r2"}}

	save := func(scopeID, generationID, factID string, release <-chan struct{}, started chan<- struct{}) error {
		tx, err := SQLDB{DB: p.db}.Begin(ctx)
		if err != nil {
			return err
		}
		if started != nil {
			close(started)
		}
		saveErr := factwrite.BatchInsertFacts(ctx, tx, []factwrite.Row{ownerLiveRow(factID, scopeID, generationID, 1, scopeID, now)})
		if release != nil {
			<-release
		}
		if saveErr != nil {
			_ = tx.Rollback()
			return saveErr
		}
		return tx.Commit()
	}

	wins := map[string]int{}
	const rounds = 6
	for round := 0; round < rounds; round++ {
		first, second := sides[round%2], sides[(round+1)%2]
		factID := fmt.Sprintf("reducer-race-%d", round)
		started, release := make(chan struct{}), make(chan struct{})
		firstErr := make(chan error, 1)
		go func() { firstErr <- save(first[0], first[1], factID, release, started) }()
		<-started
		time.Sleep(150 * time.Millisecond)
		secondErr := make(chan error, 1)
		go func() { secondErr <- save(second[0], second[1], factID, nil, nil) }()
		waitForSecondFactWriterBlocked(t, ctx, p)
		close(release)
		if err := <-firstErr; err != nil {
			t.Fatalf("round %d: the first writer failed: %v", round, err)
		}
		var conflict *factwrite.OwnerConflictError
		if err := <-secondErr; !errors.As(err, &conflict) || conflict.Writer != factwrite.WriterBatch || conflict.Count != 1 {
			t.Fatalf("round %d: the second writer error = %v, want one reducer_batch conflict", round, err)
		}
		owner := p.queryString(`SELECT scope_id FROM fact_records WHERE fact_id = $1`, factID)
		if owner != first[0] {
			t.Fatalf("round %d: owner = %q, want the first committer %q", round, owner, first[0])
		}
		wins[owner]++
	}
	if wins["scope-r1"] != rounds/2 || wins["scope-r2"] != rounds/2 {
		t.Fatalf("wins = %v, want each scope to win %d rounds (both orders)", wins, rounds/2)
	}
}

// TestEnqueueRefusesAForeignIntentWorkItemIDLive is the live proof for #11215
// on the real queue: two intents whose scope ids differ only in '/' versus '_'
// share one work item id. The second enqueue is refused with the typed error and
// the first row is untouched; enqueuing the first intent again is a benign
// coalesce, not an error.
func TestEnqueueRefusesAForeignIntentWorkItemIDLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	now := time.Now().UTC()
	held := runtime.ReducerIntent{
		ScopeID: "git-repository-scope:grp/sub_proj", GenerationID: "gen-q1", Domain: reducer.DomainWorkloadMaterialization,
		EntityKey: "workload_materialization:git-repository-scope:grp/sub_proj", Reason: "proof", SourceSystem: "git",
	}
	incoming := held
	incoming.ScopeID = "git-repository-scope:grp_sub/proj"
	incoming.EntityKey = "workload_materialization:git-repository-scope:grp_sub/proj"
	if reducerWorkItemID(held) != reducerWorkItemID(incoming) {
		t.Fatalf("test bug: the intents must share a work item id")
	}
	seedUnchangedSkipScope(t, ctx, p.db, held.ScopeID, held.GenerationID, now)
	seedUnchangedSkipScope(t, ctx, p.db, incoming.ScopeID, "gen-q2", now)
	incoming.GenerationID = held.GenerationID

	queue := NewReducerQueue(SQLDB{DB: p.db}, "owner-proof", time.Minute)
	if result, err := queue.Enqueue(ctx, []runtime.ReducerIntent{held}); err != nil || result.Count != 1 {
		t.Fatalf("first enqueue = %+v, %v, want one inserted", result, err)
	}
	before := p.queryString(`SELECT row_to_json(w)::text FROM fact_work_items w WHERE work_item_id = $1`, reducerWorkItemID(held))

	_, err := queue.Enqueue(ctx, []runtime.ReducerIntent{incoming})
	var collision *ReducerWorkItemCollisionError
	if !errors.As(err, &collision) || collision.Count != 1 || collision.Samples[0].StoredScopeID != held.ScopeID ||
		collision.Samples[0].IncomingScopeID != incoming.ScopeID || collision.Retryable() {
		t.Fatalf("colliding enqueue error = %v, want one non-retryable collision held by %q", err, held.ScopeID)
	}
	if got := p.queryString(`SELECT row_to_json(w)::text FROM fact_work_items w WHERE work_item_id = $1`, reducerWorkItemID(held)); got != before {
		t.Fatalf("the queued row changed:\n before %s\n after  %s", before, got)
	}
	if result, err := queue.Enqueue(ctx, []runtime.ReducerIntent{held}); err != nil || result.Count != 0 {
		t.Fatalf("duplicate enqueue = %+v, %v, want a benign zero-count coalesce", result, err)
	}
}
