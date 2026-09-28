// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"maps"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// TestRetentionBoundP3 is P3 of arbiter ruling arb-7127-3d, inverted by
// PR-3e (C1, C2): X is pruned, then the real link writer links G. The writer
// finds X gone and rebases instead of writing (X -> G), so the orphan probe
// stays at zero. get_changed_since from X answers retention_expired: X is
// resolved from scope_generations, so a pruned generation can never be the
// since generation. Once G is superseded and pruned, the probe is still zero
// and no row names X or G.
func TestRetentionBoundP3(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "scope-p3", "x0", "x1")

	if result := l.prune(t, l.ctx); result.GenerationsPruned != 1 {
		t.Fatalf("pruned %d, want 1 (x0)", result.GenerationsPruned)
	}
	link, err := w.LinkNext(l.ctx, "scope-p3")
	if err != nil || link.Kind != linksfreshnessstore.LinkKindRoot || link.Break != linksfreshnessstore.BreakPriorPruned ||
		link.RebasedFrom != "x0" || link.PriorGenerationID != "" {
		t.Fatalf("link after the prune = %+v, %v; want a root rebase x0 -> x1 (prior_pruned)", link, err)
	}
	if links, activations, headless := l.probe(t); links+activations+headless != 0 {
		t.Fatalf("probe = %d/%d/%d after the link, want zeros (no link names the pruned x0)",
			links, activations, headless)
	}

	summary, err := postgres.NewStatusStore(l.store).ComputeChangedSinceDelta(l.ctx, status.ChangedSinceFilter{
		ScopeID: "scope-p3", SinceGenerationID: "x0", SampleLimit: 5,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta: %v", err)
	}
	if !summary.Unavailable || summary.UnavailableReason != status.ChangedSinceUnavailableRetentionExpired {
		t.Fatalf("changed-since from the pruned x0 = unavailable %v reason %q, want retention_expired",
			summary.Unavailable, summary.UnavailableReason)
	}

	l.supersede(t, "scope-p3", "x1", "x2")
	if result := l.prune(t, l.ctx); result.GenerationsPruned != 1 {
		t.Fatalf("second prune = %d, want 1 (x1)", result.GenerationsPruned)
	}
	if links, activations, headless := l.probe(t); links+activations+headless != 0 {
		t.Fatalf("probe after x1's prune = %d/%d/%d, want zeros", links, activations, headless)
	}
	if n := l.rowsNaming(t, "x0", "x1"); n != 0 {
		t.Fatalf("%d ledger rows still name x0 or x1", n)
	}
	if n := l.headlessDeltas(t); n != 0 {
		t.Fatalf("%d headless delta rows", n)
	}
}

// TestRetentionActivationAboveCursorP4 is P4 of arbiter ruling arb-7127-3d.
// The state is at X; G1 is journaled and pruned before the writer reaches it;
// G2 is full. The prune deletes G1's activation row (it is above the cursor),
// the backlog drops by one without a link or a break, and the writer links G2
// as incremental from X with the state equal to G2's aggregate. That is the
// third way an activation can end: deleted by retention before it is linked.
func TestRetentionActivationAboveCursorP4(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	journal := linksfreshnessstore.NewJournalStore(l.store)
	l.seedChainAt(t, "scope-p4", []string{"x", "g1", "g2"},
		[]time.Time{time.Now().UTC(), fixtureEpoch.Add(-48 * time.Hour), {}})
	if root := mustLink(t, w, l, "scope-p4"); root.Kind != linksfreshnessstore.LinkKindRoot || root.GenerationID != "x" {
		t.Fatalf("first link = %+v, want root of x", root)
	}
	before, err := journal.Stats(l.ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if result := l.prune(t, l.ctx); result.GenerationsPruned != 1 || result.RowsPruned["changed_since_activations"] != 1 {
		t.Fatalf("prune = %d generations, %d activation rows; want g1 and its activation",
			result.GenerationsPruned, result.RowsPruned["changed_since_activations"])
	}
	after, err := journal.Stats(l.ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if after.BacklogRows != before.BacklogRows-1 {
		t.Fatalf("backlog %d -> %d, want one fewer", before.BacklogRows, after.BacklogRows)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE scope_id = 'scope-p4'`); n != 1 {
		t.Fatalf("%d links after the prune, want 1 (x's root; no link or break for g1)", n)
	}

	next := mustLink(t, w, l, "scope-p4")
	if next.Kind != linksfreshnessstore.LinkKindIncremental || next.GenerationID != "g2" || next.PriorGenerationID != "x" || next.Break != "" {
		t.Fatalf("next link = %+v, want incremental x -> g2 with no break", next)
	}
	got, n := l.stateDigest(t, "scope-p4")
	want, wantN := l.aggregateDigest(t, "scope-p4", "g2")
	if got != want || n != wantN {
		t.Fatalf("state after the link = %s (%d keys), aggregate of g2 = %s (%d keys)", got, n, want, wantN)
	}
}

// TestRetentionRowLimitDoesNotStarveLedgerHeavyGenerations is PB1 of arbiter
// ruling arb-7127-3d-b. BatchRowLimit counts ledger rows per candidate, and a
// link whose rows, plus the facts of either generation it names, exceed the
// limit used to be charged to whichever of its two generations a batch would
// prune: both were skipped on every pass (the RED record: at 4a3e229582 five
// passes left c1 and c2 in place). A generation over the limit only because of
// its ledger rows is now pruned in a batch of its own, so c0, c1 and c2 all go
// within three passes with the limit unchanged; the batch that deletes the
// oversized link holds one generation, and its event counts equal the rows
// deleted.
func TestRetentionRowLimitDoesNotStarveLedgerHeavyGenerations(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.linkChain(t, w, "scope-st", []string{"c0", "c1", "c2", "c3"}, fixtureEpoch.Add(-48*time.Hour))
	// Inflate (c1 -> c2) by 100 delta rows; delta_rows stays equal to the rows.
	l.exec(t, `
INSERT INTO changed_since_link_deltas (scope_id, generation_id, prior_generation_id, fact_category, classification,
    stable_fact_key, prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
SELECT 'scope-st', 'c2', 'c1', 'content_entities', 'updated', 'ent:big' || r, 'content_entity', 'content_entity',
       sha256(('p' || r)::bytea), sha256(('c' || r)::bytea), FALSE
FROM generate_series(1, 100) AS r`)
	l.exec(t, `UPDATE changed_since_links SET delta_rows = delta_rows + 100
WHERE scope_id = 'scope-st' AND generation_id = 'c2' AND prior_generation_id = 'c1'`)
	const limit = 60
	store := postgres.NewGenerationRetentionStore(l.store)
	bigLink := `SELECT count(*) FROM changed_since_links WHERE generation_id = 'c2' AND prior_generation_id = 'c1'`

	for pass := 0; pass < 3; pass++ {
		hadBigLink := l.queryInt(t, bigLink) == 1
		result, err := store.PruneSupersededGenerations(l.ctx, prunePolicy(limit))
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		t.Logf("pass %d: pruned %d, skipped %v, over limit by %d", pass, result.GenerationsPruned, result.Skipped, result.RowsOverLimit)
		if hadBigLink && l.queryInt(t, bigLink) == 0 {
			// This batch deleted the oversized link: it must be a batch of one,
			// over the limit only by its ledger rows, with exact event counts.
			if result.GenerationsPruned != 1 || result.RowsOverLimit <= 0 {
				t.Fatalf("pass %d deleted the oversized link in a batch of %d generations, over by %d; want one generation over the limit",
					pass, result.GenerationsPruned, result.RowsOverLimit)
			}
			if !maps.Equal(result.LedgerRowsCounted, result.LedgerRowsPruned) {
				t.Fatalf("pass %d: pre-count %v differs from the delete %v", pass, result.LedgerRowsCounted, result.LedgerRowsPruned)
			}
			if events := l.eventCounts(t, "c1"); !eventSumsMatch(events, result.LedgerRowsPruned) {
				t.Fatalf("pass %d: c1's event %v does not match the deletes %v", pass, events, result.LedgerRowsPruned)
			}
		}
	}
	if n := l.queryInt(t, `SELECT count(*) FROM scope_generations WHERE generation_id IN ('c0', 'c1', 'c2')`); n != 0 {
		t.Fatalf("%d of c0, c1, c2 remain after three passes at limit %d, want none", n, limit)
	}
	if n := l.rowsNaming(t, "c0", "c1", "c2"); n != 0 {
		t.Fatalf("%d ledger rows still name a pruned generation", n)
	}
	if n := l.headlessDeltas(t); n != 0 {
		t.Fatalf("%d headless delta rows", n)
	}
}
