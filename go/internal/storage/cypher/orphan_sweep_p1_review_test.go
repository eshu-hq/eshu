// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestOrphanSweepPagesPastAllConnectedWindow proves the #5313 review finding
// that S1's LIMIT-before-connectivity must not stall: when a label's first
// candidate window is entirely connected, the cursor still advances so the next
// cycle reaches true orphans beyond that window instead of re-reading the same
// connected rows forever.
func TestOrphanSweepPagesPastAllConnectedWindow(t *testing.T) {
	t.Parallel()

	g := newFakeOrphanGraph()
	// CountLimit is 2. The first ordered window {/a,/b} is entirely connected;
	// the true orphans {/c,/d} sort after it, outside the first window.
	g.seed("File", "/a", true, nil)
	g.seed("File", "/b", true, nil)
	g.seed("File", "/c", false, nil)
	g.seed("File", "/d", false, nil)

	store := &OrphanSweepStore{Executor: g, Reader: g, Labels: []OrphanSweepLabel{OrphanSweepLabelFile}}
	policy := OrphanSweepPolicy{
		CountLimit: 2,
		BatchLimit: 100,
		OrphanTTL:  time.Hour,
		Now:        time.Unix(1_000_000, 0).UTC(),
	}

	// Cycle 1: window {/a,/b}, both connected -> zero orphans marked, but the
	// cursor advances past /b instead of getting stuck.
	r1, err := store.SweepOrphanNodes(context.Background(), policy)
	if err != nil {
		t.Fatalf("cycle 1 SweepOrphanNodes: %v", err)
	}
	if r1.Marked["File"] != 0 {
		t.Fatalf("cycle 1 marked = %d, want 0 (first window is all connected)", r1.Marked["File"])
	}

	// Cycle 2: window advances to {/c,/d} -> both orphan -> both marked. This is
	// the forward-progress guarantee: orphans past an all-connected window are
	// reached rather than invisible.
	r2, err := store.SweepOrphanNodes(context.Background(), policy)
	if err != nil {
		t.Fatalf("cycle 2 SweepOrphanNodes: %v", err)
	}
	if r2.Marked["File"] != 2 {
		t.Fatalf("cycle 2 marked = %d, want 2 (orphans beyond the first window reached)", r2.Marked["File"])
	}
}

// TestOrphanSweepWritesReapplyOwnershipAndMarkerGuard proves the #5313 review
// finding that the key-anchored writes must re-apply the ownership and marker
// predicates, not only the earlier read. Without this a Repository key selected
// by S1 that races with canonical projection (re-created as
// evidence_source='projector/canonical') could be deleted by the label+key-only
// write during the window before its relationships attach.
func TestOrphanSweepWritesReapplyOwnershipAndMarkerGuard(t *testing.T) {
	t.Parallel()

	clear, ok := BuildClearOrphanMarkerStatement(OrphanSweepLabelRepository, []string{"r1"})
	if !ok {
		t.Fatal("BuildClearOrphanMarkerStatement ok = false")
	}
	for _, want := range []string{
		"n.evidence_source <> 'projector/canonical'",
		"n.eshu_orphan_observed_at_unix IS NOT NULL",
	} {
		if !strings.Contains(clear.Cypher, want) {
			t.Fatalf("clear missing %q:\n%s", want, clear.Cypher)
		}
	}

	mark, ok := BuildMarkOrphanNodesStatement(OrphanSweepLabelRepository, []string{"r1"}, 100)
	if !ok {
		t.Fatal("BuildMarkOrphanNodesStatement ok = false")
	}
	for _, want := range []string{
		"n.evidence_source <> 'projector/canonical'",
		"n.eshu_orphan_observed_at_unix IS NULL",
	} {
		if !strings.Contains(mark.Cypher, want) {
			t.Fatalf("mark missing %q:\n%s", want, mark.Cypher)
		}
	}

	sweep, ok := BuildSweepOrphanNodesStatement(OrphanSweepLabelRepository, []string{"r1"}, 500)
	if !ok {
		t.Fatal("BuildSweepOrphanNodesStatement ok = false")
	}
	for _, want := range []string{
		"n.evidence_source <> 'projector/canonical'",
		"n.eshu_orphan_observed_at_unix IS NOT NULL",
		"n.eshu_orphan_observed_at_unix <= $cutoff_unix",
	} {
		if !strings.Contains(sweep.Cypher, want) {
			t.Fatalf("sweep missing %q:\n%s", want, sweep.Cypher)
		}
	}
}

// TestModuleAntiJoinRestrictsToCanonicalImportClass proves the #5313 review
// finding that Module.name is not unique across node classes (canonical
// imported modules are MERGEd on {name} with no uid; semantic module entities
// are MERGEd on {uid} and also carry a name). The sweep owns only the canonical
// imports, so S1, S2, and the writes must restrict to `n.uid IS NULL`;
// otherwise a connected same-name semantic module masks a canonical orphan and
// the writes target the semantic node too. Unique-key labels must not be
// restricted.
func TestModuleAntiJoinRestrictsToCanonicalImportClass(t *testing.T) {
	t.Parallel()

	s1, _ := BuildCandidateOrphanNodesQuery(OrphanSweepLabelModule, 10, "")
	if !strings.Contains(s1.Cypher, "n.uid IS NULL") {
		t.Fatalf("Module S1 missing class predicate:\n%s", s1.Cypher)
	}
	s2, _ := BuildConnectedKeysQuery(OrphanSweepLabelModule, []string{"index"})
	if !strings.Contains(s2.Cypher, "n.uid IS NULL") {
		t.Fatalf("Module S2 missing class predicate:\n%s", s2.Cypher)
	}
	sweep, _ := BuildSweepOrphanNodesStatement(OrphanSweepLabelModule, []string{"index"}, 0)
	if !strings.Contains(sweep.Cypher, "n.uid IS NULL") {
		t.Fatalf("Module sweep missing class predicate:\n%s", sweep.Cypher)
	}

	// A unique-key label (File) must NOT carry the class predicate.
	fileS2, _ := BuildConnectedKeysQuery(OrphanSweepLabelFile, []string{"/a"})
	if strings.Contains(fileS2.Cypher, "n.uid IS NULL") {
		t.Fatalf("File S2 wrongly restricted to a class predicate:\n%s", fileS2.Cypher)
	}
}
