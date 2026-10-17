// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestInputFingerprintReopenUnchangedItemStaysSucceeded is the negative case
// and the proof that the recorded value equals the listing-side value for the
// same snapshot: the fingerprint is taken in the fused fenced read, the
// listing recomputes it from the stored repo_ids, and a pass with no change in
// between must leave the item succeeded. Had the two sides disagreed, the
// item would reopen on every pass and the gate would save nothing.
func TestInputFingerprintReopenUnchangedItemStaysSucceeded(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)

	f.runPass(t)

	if got := f.status(t); got != "succeeded" {
		t.Fatalf("unchanged item status after a maintenance pass = %q, want succeeded", got)
	}
}

// TestInputFingerprintReopenMissingRowReopens pins fail-open: an item with no
// recorded fingerprint (first pass after the roll, a handler that never
// recorded) must reopen, exactly like the blanket reopen did.
func TestInputFingerprintReopenMissingRowReopens(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)

	f.runPass(t)

	if got := f.status(t); got != "pending" {
		t.Fatalf("item without a recorded fingerprint status = %q, want pending", got)
	}
}

// TestInputFingerprintReopenForeignActivationBeforeUpdatedAt is the issue's
// seeded RED case for a timestamp predicate. A foreign scope re-ingests and
// activates a new relationship generation touching the item's deployment
// repository. The activation is stamped now, the item's updated_at (its ACK)
// is an hour later, so activated_at < updated_at. An implementation that
// reopens only when activated_at > updated_at leaves the item succeeded and
// the graph wrong; the fingerprint must reopen it.
func TestInputFingerprintReopenForeignActivationBeforeUpdatedAt(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)

	f.reingestScope(t, "git:scope-b", "gen-b2", "repo-b", fingerprintDepRepoID)
	f.runPass(t)

	if got := f.status(t); got != "pending" {
		t.Fatalf("item status after a foreign activation stamped before its updated_at = %q, want pending", got)
	}
}

// TestInputFingerprintReopenForeignScopeOnly changes nothing in the item's own
// scope or relationship generation: only the foreign scope's generation is
// re-activated in place (the ON CONFLICT re-stamp), so an own-scope-only
// predicate would skip it.
func TestInputFingerprintReopenForeignScopeOnly(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)

	if err := f.relations.ActivateResolutionGeneration(f.ctx, "gen-b1", "git:scope-b"); err != nil {
		t.Fatalf("re-activate gen-b1: %v", err)
	}
	f.runPass(t)

	if got := f.status(t); got != "pending" {
		t.Fatalf("item status after a foreign-only re-activation = %q, want pending", got)
	}
}

// TestInputFingerprintReopenSupersedeWithoutReplacement retires the foreign
// relationship generation (the reset path) with no replacement. No new
// activated_at exists anywhere, so an activated_at-only predicate cannot see
// it; the set of touching active generations shrank, so the fingerprint moves.
func TestInputFingerprintReopenSupersedeWithoutReplacement(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)

	f.exec(t, `UPDATE relationship_generations SET status = 'superseded' WHERE generation_id = 'gen-b1'`)
	f.runPass(t)

	if got := f.status(t); got != "pending" {
		t.Fatalf("item status after a foreign supersede with no replacement = %q, want pending", got)
	}
}

// TestInputFingerprintReopenUnrelatedForeignScopeStaysSucceeded proves the
// fingerprint is scoped by the stored repository ids and is not a corpus-wide
// clock: a foreign scope that touches none of the item's repositories can
// re-ingest without reopening it.
func TestInputFingerprintReopenUnrelatedForeignScopeStaysSucceeded(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)

	f.reingestScope(t, "git:scope-c", "gen-c2", "repo-c", "repo-z")
	f.runPass(t)

	if got := f.status(t); got != "succeeded" {
		t.Fatalf("item status after an unrelated foreign re-ingest = %q, want succeeded", got)
	}
}

// TestInputFingerprintReopenOwnGenerationChange covers the own-relgen input:
// the item's own scope re-activates its relationship generation after the
// recorded read.
func TestInputFingerprintReopenOwnGenerationChange(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)

	if err := f.relations.ActivateResolutionGeneration(f.ctx, fingerprintOwnGen, fingerprintOwnScope); err != nil {
		t.Fatalf("re-activate own generation: %v", err)
	}
	f.runPass(t)

	if got := f.status(t); got != "pending" {
		t.Fatalf("item status after its own relationship generation re-activated = %q, want pending", got)
	}
}

// TestInputFingerprintReopenDeploymentRepositoryReingest re-ingests the scope
// that owns the candidate's deployment repository. Its new relationship
// generation touches that repository as a source, and must reopen the item.
func TestInputFingerprintReopenDeploymentRepositoryReingest(t *testing.T) {
	f := newFingerprintFixture(t)
	f.seedScope(t, "git:scope-dep", "gen-dep1", fingerprintDepRepoID, "repo-other")
	f.succeedItem(t)
	f.recordFingerprint(t)

	f.reingestScope(t, "git:scope-dep", "gen-dep2", fingerprintDepRepoID, "repo-other")
	f.runPass(t)

	if got := f.status(t); got != "pending" {
		t.Fatalf("item status after the deployment repository re-ingested = %q, want pending", got)
	}
}

// TestInputFingerprintReopenInFlightItemReopensOnNextPass walks the in-flight
// case: the item is claimed when the foreign activation commits, its read
// snapshot predates the activation, and its ACK lands afterwards. The pass
// that runs while the item is claimed must not touch it; the pass after the
// ACK must reopen it, because the recorded fingerprint is the pre-activation
// one.
func TestInputFingerprintReopenInFlightItemReopensOnNextPass(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.exec(t, `UPDATE fact_work_items SET status = 'claimed' WHERE work_item_id = $1`, fingerprintItemID)
	f.recordFingerprint(t)

	f.reingestScope(t, "git:scope-b", "gen-b2", "repo-b", fingerprintDepRepoID)
	f.runPass(t)
	if got := f.status(t); got != "claimed" {
		t.Fatalf("claimed item status after a pass = %q, want claimed (a pass must not touch in-flight work)", got)
	}

	f.exec(t, `UPDATE fact_work_items SET status = 'succeeded', updated_at = $2 WHERE work_item_id = $1`,
		fingerprintItemID, f.ackedAt)
	f.runPass(t)
	if got := f.status(t); got != "pending" {
		t.Fatalf("item status on the pass after its ACK = %q, want pending", got)
	}
}

// TestInputFingerprintReopenListingFailureFailsThePass is the error path:
// when the fingerprint table cannot be read the correlation listing fails, the
// pass returns the error, and the deployment_mapping reopen that ran earlier
// in the same transaction rolls back. After the table is back the next pass
// reopens both, so nothing is lost, only delayed.
func TestInputFingerprintReopenListingFailureFailsThePass(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)
	seedSucceededReopenWorkItem(t, f.ctx, f.database, "work-dm-a", fingerprintOwnScope, fingerprintOwnGen,
		"deployment_mapping", f.ackedAt)

	// Rename a column, not the table: a leftover table of the same name in public
	// (the hand-rolled proof schemas of older tests leak one) would otherwise
	// still resolve once the isolated schema's table is renamed away.
	f.exec(t, `ALTER TABLE reducer_work_input_fingerprints RENAME COLUMN repo_ids TO repo_ids_away`)
	err := f.ingestion.RunDeferredRelationshipMaintenance(f.ctx, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "deployable_unit_correlation") {
		t.Fatalf("pass error = %v, want the deployable_unit_correlation listing failure", err)
	}
	if got := workItemStatus(t, f.ctx, f.database, "work-dm-a"); got != "succeeded" {
		t.Fatalf("deployment_mapping item status after a failed pass = %q, want succeeded (reopen tx must roll back)", got)
	}

	f.exec(t, `ALTER TABLE reducer_work_input_fingerprints RENAME COLUMN repo_ids_away TO repo_ids`)
	f.runPass(t)
	if got := workItemStatus(t, f.ctx, f.database, "work-dm-a"); got != "pending" {
		t.Fatalf("deployment_mapping item status after the retry pass = %q, want pending", got)
	}
}

// TestInputFingerprintReopenRowsAddedToActiveGenerationWithoutRestamp covers the
// window the generation id and activated_at alone cannot see. The cross-repo
// resolver inserts resolved rows (ON CONFLICT DO NOTHING) into the scope's
// generation in one statement per row, then activates it in a later statement
// (cross_repo_resolution.go steps 4 and 6): resolution inserts and activation
// are NOT one transaction. A replay can therefore add rows to an already-active
// generation before the activation re-stamps it, and if the resolution item
// dies in between, no stamp ever follows. The row counts in the fingerprint
// must move the value anyway, for the foreign and for the item's own generation.
func TestInputFingerprintReopenRowsAddedToActiveGenerationWithoutRestamp(t *testing.T) {
	for name, generation := range map[string]string{
		"foreign generation": "gen-b1",
		"own generation":     fingerprintOwnGen,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFingerprintFixture(t)
			f.succeedItem(t)
			f.recordFingerprint(t)

			f.insertResolved(t, generation, "repo-new-source", fingerprintDepRepoID)
			f.runPass(t)

			if got := f.status(t); got != "pending" {
				t.Fatalf("item status after rows were added to %s with no re-stamp = %q, want pending", name, got)
			}
		})
	}
}

// TestInputFingerprintReopenOwnGenerationOnlySignals isolates the own half of
// the fingerprint. The item's candidate set is one repository that no resolved
// row touches, so the touching half is empty and cannot move. DUC still reads
// its whole own generation (listResolvedByGenerationSQL has no repository
// filter and no status filter), so each change below must reopen the item
// through the own component alone: rows added to the own generation that do not
// touch the candidates and carry no re-stamp, a status change with the same
// activated_at, and a bare re-stamp. The equal case must stay succeeded.
func TestInputFingerprintReopenOwnGenerationOnlySignals(t *testing.T) {
	untouched := []string{"repo-nobody-touches"}
	for name, tc := range map[string]struct {
		change func(t *testing.T, f *fingerprintFixture)
		want   string
	}{
		"nothing changed": {func(*testing.T, *fingerprintFixture) {}, "succeeded"},
		"rows added to the own generation, no re-stamp": {func(t *testing.T, f *fingerprintFixture) {
			f.insertResolved(t, fingerprintOwnGen, "repo-other-source", "repo-other-target")
		}, "pending"},
		"own status changed, same activated_at": {func(t *testing.T, f *fingerprintFixture) {
			f.exec(t, `UPDATE relationship_generations SET status = 'superseded' WHERE generation_id = $1`, fingerprintOwnGen)
		}, "pending"},
		"own generation re-stamped": {func(t *testing.T, f *fingerprintFixture) {
			if err := f.relations.ActivateResolutionGeneration(f.ctx, fingerprintOwnGen, fingerprintOwnScope); err != nil {
				t.Fatalf("re-activate own generation: %v", err)
			}
		}, "pending"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFingerprintFixture(t)
			f.succeedItem(t)
			f.recordFingerprintFor(t, untouched)

			tc.change(t, f)
			f.runPass(t)

			if got := f.status(t); got != tc.want {
				t.Fatalf("item status = %q, want %q", got, tc.want)
			}
		})
	}
}
