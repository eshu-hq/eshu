// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// modulePolicy is the single-label sweep policy these tests drive.
func modulePolicy(clock time.Time) OrphanSweepPolicy {
	return OrphanSweepPolicy{
		OrphanTTL:  1 * time.Second,
		BatchLimit: 100,
		CountLimit: 1000,
		Labels:     []string{"Module"},
		Now:        clock,
	}
}

// TestOrphanSweepSweepsDisconnectedModuleWithConnectedSameNameSibling is the
// #6102 P2 defect. Canonical import Module identity is (name, lang), so a Go
// `time` and a Python `time` are separate nodes. While the sweep keyed on name
// alone, the pair shared one entry in its Go-side anti-join: the disconnected
// Python node counted as connected because its Go sibling was, so it was never
// marked, never deleted, and never stopped being counted as an orphan.
func TestOrphanSweepSweepsDisconnectedModuleWithConnectedSameNameSibling(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seedComposite("Module", []string{"time", "go"}, true, nil)
	graph.seedComposite("Module", []string{"time", "python"}, false, nil)

	store := NewOrphanSweepStore(graph, graph)
	clock := time.Unix(1_800_000_000, 0).UTC()

	for cycle := 0; cycle < 2; cycle++ {
		if _, err := store.SweepOrphanNodes(context.Background(), modulePolicy(clock)); err != nil {
			t.Fatalf("cycle %d SweepOrphanNodes: %v", cycle+1, err)
		}
		clock = clock.Add(2 * time.Second)
	}

	got := graph.compositeKeyRows("Module")
	want := [][]string{{"time", "go"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("surviving Module nodes = %v, want %v; the disconnected sibling must be swept "+
			"even though a same-named node in another language is still imported", got, want)
	}
}

// TestOrphanSweepNeverSweepsConnectedModule is the other direction of the same
// key change, and the one that must never regress: making the key exact must
// not turn a connected node into a delete candidate.
func TestOrphanSweepNeverSweepsConnectedModule(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seedComposite("Module", []string{"time", "go"}, true, nil)
	graph.seedComposite("Module", []string{"time", "python"}, true, nil)
	graph.seedComposite("Module", []string{"os", "go"}, true, nil)

	store := NewOrphanSweepStore(graph, graph)
	clock := time.Unix(1_800_000_000, 0).UTC()
	for cycle := 0; cycle < 3; cycle++ {
		if _, err := store.SweepOrphanNodes(context.Background(), modulePolicy(clock)); err != nil {
			t.Fatalf("cycle %d SweepOrphanNodes: %v", cycle+1, err)
		}
		clock = clock.Add(2 * time.Second)
	}

	got := graph.compositeKeyRows("Module")
	want := [][]string{{"os", "go"}, {"time", "go"}, {"time", "python"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("surviving Module nodes = %v, want all three %v", got, want)
	}
}

// TestOrphanSweepModuleGaugeCountsEachLanguageSeparately covers the gauge the
// review named. GraphOrphanNodeCounts runs the same anti-join without writes,
// so a name-keyed anti-join under-counted the same way it under-deleted.
func TestOrphanSweepModuleGaugeCountsEachLanguageSeparately(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seedComposite("Module", []string{"time", "go"}, true, nil)
	graph.seedComposite("Module", []string{"time", "python"}, false, nil)
	graph.seedComposite("Module", []string{"time", "ruby"}, false, nil)

	store := &OrphanSweepStore{
		Executor: graph,
		Reader:   graph,
		Labels:   []OrphanSweepLabel{OrphanSweepLabelModule},
	}
	counts, err := store.GraphOrphanNodeCounts(context.Background())
	if err != nil {
		t.Fatalf("GraphOrphanNodeCounts: %v", err)
	}
	if got, want := counts["Module"], int64(2); got != want {
		t.Fatalf("Module orphan count = %d, want %d", got, want)
	}
}

// TestOrphanSweepCompositeCursorVisitsEveryRowExactlyOnce is the paging proof.
// The composite cursor is the risky part of threading (name, lang) through the
// sweep: a cursor that only carried the name would resume strictly past a name
// and silently skip the rest of that name's languages, and one that resumed at
// the name would re-read rows forever.
//
// The label here holds five rows across three names, two of them multi-language,
// and CountLimit is 2 so a page boundary lands inside a name's group in both
// possible positions. The assertion is on the delete set, which is what the
// paging feeds: every disconnected row must be reached, and no connected row
// may be.
func TestOrphanSweepCompositeCursorVisitsEveryRowExactlyOnce(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	// aa: page 1 ends between its two languages.
	graph.seedComposite("Module", []string{"aa", "go"}, false, nil)
	graph.seedComposite("Module", []string{"aa", "python"}, false, nil)
	// ab: a lone row that straddles the next boundary.
	graph.seedComposite("Module", []string{"ab", "go"}, true, nil)
	// b: the trailing group, split across the last two pages.
	graph.seedComposite("Module", []string{"b", "go"}, true, nil)
	graph.seedComposite("Module", []string{"b", "ts"}, false, nil)

	store := NewOrphanSweepStore(graph, graph)
	policy := OrphanSweepPolicy{
		OrphanTTL:  1 * time.Second,
		BatchLimit: 100,
		CountLimit: 2,
		Labels:     []string{"Module"},
	}

	// Six cycles at two rows a page is three full passes over five rows: enough
	// for every row to be marked on one pass and swept on a later one, with the
	// cursor wrapping in between.
	clock := time.Unix(1_800_000_000, 0).UTC()
	for cycle := 0; cycle < 6; cycle++ {
		policy.Now = clock
		if _, err := store.SweepOrphanNodes(context.Background(), policy); err != nil {
			t.Fatalf("cycle %d SweepOrphanNodes: %v", cycle+1, err)
		}
		clock = clock.Add(2 * time.Second)
	}

	got := graph.compositeKeyRows("Module")
	want := [][]string{{"ab", "go"}, {"b", "go"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("surviving Module nodes = %v, want the two connected rows %v; "+
			"a row was skipped by the cursor or visited twice", got, want)
	}
}

// TestOrphanSweepCompositeCursorResumesMidName pins the exact resume case the
// paging has to get right, at the level of the emitted read rather than the
// whole cycle: handed a cursor of ("aa", "go"), the next page must start at
// ("aa", "python") and not skip to the next name.
func TestOrphanSweepCompositeCursorResumesMidName(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seedComposite("Module", []string{"aa", "go"}, false, nil)
	graph.seedComposite("Module", []string{"aa", "python"}, false, nil)
	graph.seedComposite("Module", []string{"ab", "go"}, false, nil)

	store := NewOrphanSweepStore(graph, graph)
	candidates, err := store.readCandidateOrphanNodes(context.Background(),
		OrphanSweepLabelModule, 10, orphanSweepKey{"aa", "go"})
	if err != nil {
		t.Fatalf("readCandidateOrphanNodes: %v", err)
	}

	got := make([][]string, 0, len(candidates))
	for _, candidate := range candidates {
		got = append(got, candidate.key)
	}
	want := [][]string{{"aa", "python"}, {"ab", "go"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates after cursor (aa, go) = %v, want %v", got, want)
	}
}

// TestOrphanSweepSinglePropertyQueriesAreUnchanged holds the blast radius down.
// Only Module gained a second identity property; every other label must emit
// byte-identical Cypher and the same $keys/$cursor parameter shapes, so this
// change cannot move a query-plan or replay artifact for them.
func TestOrphanSweepSinglePropertyQueriesAreUnchanged(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildCandidateOrphanNodesQuery(OrphanSweepLabelFile, 100, orphanSweepKey{"/a/b.go"})
	if !ok {
		t.Fatal("BuildCandidateOrphanNodesQuery(File) not ok")
	}
	wantS1 := `MATCH (n:File)
WHERE n.evidence_source IS NOT NULL
  AND n.path > $cursor
RETURN n.path AS key, n.eshu_orphan_observed_at_unix AS observed_at
ORDER BY n.path
LIMIT $limit`
	if stmt.Cypher != wantS1 {
		t.Fatalf("File S1 cypher =\n%s\nwant\n%s", stmt.Cypher, wantS1)
	}
	if got, want := stmt.Parameters["cursor"], "/a/b.go"; got != want {
		t.Fatalf("File S1 $cursor = %#v, want %q", got, want)
	}

	connected, ok := BuildConnectedKeysQuery(OrphanSweepLabelFile, []orphanSweepKey{{"/a/b.go"}})
	if !ok {
		t.Fatal("BuildConnectedKeysQuery(File) not ok")
	}
	wantS2 := `UNWIND $keys AS candidate_key
MATCH (n:File {path: candidate_key})-[r]-(m)
RETURN DISTINCT n.path AS key`
	if connected.Cypher != wantS2 {
		t.Fatalf("File S2 cypher =\n%s\nwant\n%s", connected.Cypher, wantS2)
	}
	if got, ok := connected.Parameters["keys"].([]string); !ok || !reflect.DeepEqual(got, []string{"/a/b.go"}) {
		t.Fatalf("File S2 $keys = %#v, want []string{\"/a/b.go\"}", connected.Parameters["keys"])
	}
}

// TestOrphanSweepModuleQueriesCarryBothProperties asserts the Module reads and
// all three key-anchored writes bind name AND lang. The lang comparison goes
// through coalesce(n.lang, ”) because a Module projected before the identity
// cutover can carry no lang property at all, and on the pinned backend a bare
// `n.lang > $cursor_1` drops such a node from every page.
func TestOrphanSweepModuleQueriesCarryBothProperties(t *testing.T) {
	t.Parallel()

	keys := []orphanSweepKey{{"time", "go"}}

	s1, ok := BuildCandidateOrphanNodesQuery(OrphanSweepLabelModule, 100, orphanSweepKey{"time", "go"})
	if !ok {
		t.Fatal("BuildCandidateOrphanNodesQuery(Module) not ok")
	}
	for _, want := range []string{
		"n.name > $cursor_0",
		"n.name = $cursor_0 AND coalesce(n.lang, '') > $cursor_1",
		"RETURN key_0, key_1, observed_at",
		"ORDER BY key_0, key_1",
	} {
		if !strings.Contains(s1.Cypher, want) {
			t.Fatalf("Module S1 cypher missing %q:\n%s", want, s1.Cypher)
		}
	}
	if got, want := s1.Parameters["cursor_0"], "time"; got != want {
		t.Fatalf("Module S1 $cursor_0 = %#v, want %q", got, want)
	}
	if got, want := s1.Parameters["cursor_1"], "go"; got != want {
		t.Fatalf("Module S1 $cursor_1 = %#v, want %q", got, want)
	}

	writes := map[string]Statement{}
	s2, ok := BuildConnectedKeysQuery(OrphanSweepLabelModule, keys)
	if !ok {
		t.Fatal("BuildConnectedKeysQuery(Module) not ok")
	}
	writes["S2 connected"] = s2
	clear, ok := BuildClearOrphanMarkerStatement(OrphanSweepLabelModule, keys)
	if !ok {
		t.Fatal("BuildClearOrphanMarkerStatement(Module) not ok")
	}
	writes["S3 clear"] = clear
	mark, ok := BuildMarkOrphanNodesStatement(OrphanSweepLabelModule, keys, 1)
	if !ok {
		t.Fatal("BuildMarkOrphanNodesStatement(Module) not ok")
	}
	writes["S4 mark"] = mark
	sweep, ok := BuildSweepOrphanNodesStatement(OrphanSweepLabelModule, keys, 1)
	if !ok {
		t.Fatal("BuildSweepOrphanNodesStatement(Module) not ok")
	}
	writes["S5 sweep"] = sweep

	for name, stmt := range writes {
		if !strings.Contains(stmt.Cypher, "MATCH (n:Module {name: candidate_key.key_0})") {
			t.Fatalf("%s does not anchor on the indexed name property:\n%s", name, stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "coalesce(n.lang, '') = candidate_key.key_1") {
			t.Fatalf("%s does not bind the language, so it would touch the same-named sibling too:\n%s",
				name, stmt.Cypher)
		}
		rows, ok := stmt.Parameters["keys"].([]map[string]any)
		if !ok {
			t.Fatalf("%s $keys = %#v, want []map[string]any", name, stmt.Parameters["keys"])
		}
		if len(rows) != 1 || rows[0]["key_0"] != "time" || rows[0]["key_1"] != "go" {
			t.Fatalf("%s $keys = %#v, want one {key_0: time, key_1: go} row", name, rows)
		}
	}
}
