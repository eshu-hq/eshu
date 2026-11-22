// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11203 cross-scope and cross-generation fact_id collision proof on disposable PostgreSQL: a second scope or generation writing an id another scope or generation already holds must never overwrite that row, while a same-scope same-generation re-save stays idempotent and still lands changed content
//eshu:readiness:env ESHU_POSTGRES_DSN

// Regression proof for issue #11203. fact_records is keyed on fact_id alone,
// and upsertFactBatchSuffix's DO UPDATE sets neither scope_id nor
// generation_id. A second scope's write for an id the first scope holds
// therefore passed the fencing and unchanged guards and replaced the first
// row's payload while the row kept the first scope's identity. The owner guard
// skips such a row and the save reports it as a FactIDOwnerConflictError;
// selectUnchangedFactIDs must not call a foreign row "unchanged" even when its
// guarded columns and fencing token equal the owner's. Live-Postgres only;
// skipped without ESHU_POSTGRES_DSN.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// factIDOwnerRow reads the identity and payload a fact_id row holds.
func factIDOwnerRow(t *testing.T, ctx context.Context, store SQLDB, factID string) (scopeID, generationID, payload string) {
	t.Helper()
	if err := store.DB.QueryRowContext(
		ctx,
		`SELECT scope_id, generation_id, payload::text FROM fact_records WHERE fact_id = $1`, factID,
	).Scan(&scopeID, &generationID, &payload); err != nil {
		t.Fatalf("read fact_id owner row %q: %v", factID, err)
	}
	return scopeID, generationID, payload
}

// TestUpsertFactBatchRefusesForeignScopeOrGenerationLive pins the #11203
// contract: a write whose fact_id is already held by another scope, or by
// another generation of the same scope, never changes that row and is reported
// as a FactIDOwnerConflictError, never admitted. The "changed" cases carry a
// higher fencing token and different content (the predicates the fencing and
// unchanged legs accept); the "identical" cases carry the owner's exact guarded
// columns and the same token, which the unchanged-row discriminator alone would
// classify "unchanged" and admit for derivation.
func TestUpsertFactBatchRefusesForeignScopeOrGenerationLive(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11203 fact_id collision proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-a", "gen-a", now)
	seedUnchangedSkipScope(t, ctx, db, "scope-b", "gen-b", now)
	// A second generation of scope-a, for the same-scope cross-generation case.
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('gen-a2', 'scope-a', 'snapshot', 'unchanged-proof', $1, $1, 'pending', NULL, NULL, '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert second generation: %v", err)
	}

	cases := []struct {
		name         string
		factID       string
		scopeID      string
		generationID string
		identical    bool
		wantReason   string
	}{
		{"other scope, changed content, higher token", "fact-scope-changed", "scope-b", "gen-b", false, "scope"},
		{"other scope, identical columns, same token", "fact-scope-identical", "scope-b", "gen-b", true, "scope"},
		// Same generation id under another scope: only the scope leg can catch it.
		{"other scope, same generation id, changed content, higher token", "fact-scopeonly-changed", "scope-b", "gen-a", false, "scope"},
		{"other scope, same generation id, identical columns, same token", "fact-scopeonly-identical", "scope-b", "gen-a", true, "scope"},
		{"other generation, changed content, higher token", "fact-gen-changed", "scope-a", "gen-a2", false, "generation"},
		{"other generation, identical columns, same token", "fact-gen-identical", "scope-a", "gen-a2", true, "generation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := unchangedSkipEnvelope(tc.factID, "scope-a", "gen-a", 1, now)
			owner.Payload = map[string]any{"owner": "scope-a"}
			if _, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{owner}); err != nil {
				t.Fatalf("owner write error = %v, want nil", err)
			}
			beforeScope, beforeGen, beforePayload := factIDOwnerRow(t, ctx, store, tc.factID)
			beforeRow := readUnchangedSkipRow(t, ctx, db, tc.factID)
			t.Logf("before: scope=%s generation=%s payload=%s", beforeScope, beforeGen, beforePayload)

			intruder := unchangedSkipEnvelope(tc.factID, tc.scopeID, tc.generationID, 2, now.Add(time.Hour))
			intruder.Payload = map[string]any{"owner": tc.scopeID + "/" + tc.generationID}
			if tc.identical {
				intruder = owner
				intruder.ScopeID, intruder.GenerationID = tc.scopeID, tc.generationID
			}
			accepted, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{intruder})
			var conflict *FactIDOwnerConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("foreign write error = %v, want *FactIDOwnerConflictError", err)
			}
			if conflict.Count != 1 || conflict.ByReason[tc.wantReason] != 1 || len(conflict.Samples) != 1 {
				t.Fatalf("conflict = %+v, want one conflict with reason %q", conflict, tc.wantReason)
			}
			if sample := conflict.Samples[0]; sample.FactID != tc.factID || sample.StoredScopeID != "scope-a" ||
				sample.StoredGenerationID != "gen-a" || sample.IncomingScopeID != tc.scopeID ||
				sample.IncomingGenerationID != tc.generationID {
				t.Fatalf("sample = %+v, want stored scope-a/gen-a and incoming %s/%s", sample, tc.scopeID, tc.generationID)
			}
			if _, ok := accepted[tc.factID]; ok {
				t.Fatalf("foreign write for %q was admitted for derivation", tc.factID)
			}

			gotScope, gotGen, gotPayload := factIDOwnerRow(t, ctx, store, tc.factID)
			t.Logf("after:  scope=%s generation=%s payload=%s", gotScope, gotGen, gotPayload)
			if gotScope != beforeScope || gotGen != beforeGen || gotPayload != beforePayload {
				t.Fatalf("row changed by a foreign write: before (%s,%s,%s) after (%s,%s,%s)",
					beforeScope, beforeGen, beforePayload, gotScope, gotGen, gotPayload)
			}
			if afterRow := readUnchangedSkipRow(t, ctx, db, tc.factID); afterRow != beforeRow {
				t.Fatalf("row tuple changed by a foreign write: %+v -> %+v", beforeRow, afterRow)
			}
		})
	}
}

// TestUpsertFactBatchSameScopeGenerationResaveStaysIdempotentLive pins the
// legitimate path the #11203 guard must not disturb: the owning scope and
// generation re-saving its own id lands changed content and skips an identical
// replay (#10873) and is admitted either way.
func TestUpsertFactBatchSameScopeGenerationResaveStaysIdempotentLive(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11203 fact_id collision proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-a", "gen-a", now)

	const factID = "fact-same-owner"
	first := unchangedSkipEnvelope(factID, "scope-a", "gen-a", 1, now)
	first.Payload = map[string]any{"v": "one"}
	if _, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{first}); err != nil {
		t.Fatalf("first write error = %v", err)
	}

	changed := unchangedSkipEnvelope(factID, "scope-a", "gen-a", 2, now.Add(time.Hour))
	changed.Payload = map[string]any{"v": "two"}
	accepted, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{changed})
	if err != nil {
		t.Fatalf("changed re-save error = %v", err)
	}
	if _, ok := accepted[factID]; !ok {
		t.Fatalf("owner re-save with changed content was not admitted")
	}
	if _, _, payload := factIDOwnerRow(t, ctx, store, factID); payload != `{"v": "two"}` {
		t.Fatalf("owner re-save did not land: payload = %s", payload)
	}

	before := readUnchangedSkipRow(t, ctx, db, factID)
	replay := unchangedSkipEnvelope(factID, "scope-a", "gen-a", 2, now.Add(2*time.Hour))
	replay.Payload = map[string]any{"v": "two"}
	accepted, err = upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{replay})
	if err != nil {
		t.Fatalf("identical replay error = %v", err)
	}
	if _, ok := accepted[factID]; !ok {
		t.Fatalf("identical replay was not admitted for derivation")
	}
	if after := readUnchangedSkipRow(t, ctx, db, factID); after.xmin != before.xmin {
		t.Fatalf("identical replay rewrote the row: xmin %s -> %s", before.xmin, after.xmin)
	}
}

// TestUpsertStreamingFactsStopsAtTheFirstConflictingBatchLive drives the
// production streaming save with a batch that mixes a fresh id and two
// foreign-owned ids (one with different content, one identical to the owner's).
// The save must return the typed error, must not hand the conflicting batch to
// afterBatch (no derivation from a foreign write), and must leave both owner
// rows untouched. It stops at the first conflicting batch: the rest of the
// stream is not consumed by the save.
func TestUpsertStreamingFactsStopsAtTheFirstConflictingBatchLive(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11203 fact_id collision proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-a", "gen-a", now)
	seedUnchangedSkipScope(t, ctx, db, "scope-b", "gen-b", now)

	ownerDiff := unchangedSkipEnvelope("fact-owned-diff", "scope-a", "gen-a", 1, now)
	ownerSame := unchangedSkipEnvelope("fact-owned-same", "scope-a", "gen-a", 1, now)
	if err := upsertFacts(ctx, store, []facts.Envelope{ownerDiff, ownerSame}); err != nil {
		t.Fatalf("seed owner rows error = %v", err)
	}
	beforeDiff := readUnchangedSkipRow(t, ctx, db, "fact-owned-diff")
	beforeSame := readUnchangedSkipRow(t, ctx, db, "fact-owned-same")

	fresh := unchangedSkipEnvelope("fact-fresh-b", "scope-b", "gen-b", 1, now)
	foreignDiff := unchangedSkipEnvelope("fact-owned-diff", "scope-b", "gen-b", 9, now.Add(time.Hour))
	foreignDiff.Payload = map[string]any{"owner": "scope-b"}
	foreignSame := ownerSame
	foreignSame.ScopeID, foreignSame.GenerationID = "scope-b", "gen-b"

	stream := make(chan facts.Envelope, 3)
	stream <- fresh
	stream <- foreignDiff
	stream <- foreignSame
	close(stream)

	called := 0
	stats, err := upsertStreamingFacts(ctx, store, stream, "scope-b", "gen-b", func([]facts.Envelope, int) error {
		called++
		return nil
	})
	var conflict *FactIDOwnerConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("upsertStreamingFacts error = %v, want *FactIDOwnerConflictError", err)
	}
	if called != 0 {
		t.Fatalf("afterBatch ran %d time(s) for a batch with a foreign-owned id, want 0", called)
	}
	if stats.Rows != 0 || stats.Batches != 0 {
		t.Fatalf("stats = %+v, want the conflicting batch uncounted", stats)
	}
	if conflict.Count != 2 || conflict.ByReason[telemetry.FactIDOwnerConflictReasonScope] != 2 ||
		conflict.ScopeID != "scope-b" || conflict.GenerationID != "gen-b" {
		t.Fatalf("conflict = %+v, want 2 scope conflicts for scope-b/gen-b", conflict)
	}
	if got := readUnchangedSkipRow(t, ctx, db, "fact-owned-diff"); got != beforeDiff {
		t.Fatalf("owner row fact-owned-diff changed: %+v -> %+v", beforeDiff, got)
	}
	if got := readUnchangedSkipRow(t, ctx, db, "fact-owned-same"); got != beforeSame {
		t.Fatalf("owner row fact-owned-same changed: %+v -> %+v", beforeSame, got)
	}
}
