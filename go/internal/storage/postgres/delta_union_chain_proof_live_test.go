// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8780 disposable PostgreSQL proofs of cumulative chained deltas on the #8779 projector harness: the chained union equals a full built at the same head (content_files and the admin completeness check report complete), a burst delta diffed from a moved active is refused at preflight and the next cumulative re-diff activates, a delta fenced on an older full refuses, and a chained delta whose union base was drained fails closed with a reindex watermark; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// The #8780 AC2 matrix on the harness: F = {a, b, c, d}; d1 at B (fence A,
// union A) modifies a, adds e, deletes d; d2 at C (fence B, union A) reverts
// a, deletes e, modifies b, adds f, re-adds d with F's bytes. Diffed from F,
// d2 changes {b, f} and deletes D(A..C) + D(B..C) = {e}.
const (
	chainCommitA, chainCommitB, chainCommitC = "aaaa8780", "bbbb8780", "cccc8780"
)

func chainFile(name string) deltaProjFile {
	return deltaProjFile{name + ".go", "package p\nfunc " + name + "() {}\n", name}
}

var (
	chainFullF = deltaProjGen{
		id: "gen-8780-f", commit: chainCommitA,
		files: []deltaProjFile{chainFile("a"), chainFile("b"), chainFile("c"), chainFile("d")},
	}
	chainDeltaD1 = deltaProjGen{
		id: "gen-8780-d1", commit: chainCommitB, baseline: chainCommitA, unionBase: chainCommitA,
		delta: true, files: []deltaProjFile{{"a.go", "package p\nfunc aChanged() {}\n", "aChanged"}, chainFile("e")},
		changed: []string{"a.go", "e.go"}, deleted: []string{"d.go"},
	}
	chainDeltaD2 = deltaProjGen{
		id: "gen-8780-d2", commit: chainCommitC, baseline: chainCommitB, unionBase: chainCommitA,
		delta: true, files: []deltaProjFile{{"b.go", "package p\nfunc bChanged() {}\n", "bChanged"}, chainFile("f")},
		changed: []string{"b.go", "f.go"}, deleted: []string{"e.go"},
	}
	chainFullAtC = deltaProjGen{id: "gen-8780-r", commit: chainCommitC, files: []deltaProjFile{
		chainFile("a"), {"b.go", "package p\nfunc bChanged() {}\n", "bChanged"}, chainFile("c"), chainFile("d"), chainFile("f"),
	}}
)

// contentRows returns "<path>=<body>" for every content_files row.
func (h *deltaProjHarness) contentRows() []string {
	h.t.Helper()
	return h.queryStrings(`SELECT relative_path || '=' || content FROM content_files WHERE repo_id = $1 ORDER BY relative_path`, h.repo.ID)
}

// lastMaterialization returns the newest recorded canonical materialization.
func (h *deltaProjHarness) lastMaterialization() canonical.CanonicalMaterialization {
	h.t.Helper()
	if len(h.canonical.calls) == 0 {
		h.t.Fatal("no canonical materialization recorded")
	}
	return h.canonical.calls[len(h.canonical.calls)-1]
}

// materializedEntitiesByFile returns "<path>:<entity,...>" per file.
func materializedEntitiesByFile(mat canonical.CanonicalMaterialization) []string {
	byFile := map[string][]string{}
	for _, entity := range mat.Entities {
		byFile[entity.RelativePath] = append(byFile[entity.RelativePath], entity.EntityName)
	}
	out := make([]string, 0, len(byFile))
	for file, names := range byFile {
		sort.Strings(names)
		out = append(out, file+":"+strings.Join(names, ","))
	}
	sort.Strings(out)
	return out
}

// requireRefused asserts a delta was refused with class, wrote nothing to the
// canonical writer after canonicalBefore, and left active as the active one.
func (h *deltaProjHarness) requireRefused(generationID, class string, canonicalBefore int, active string) {
	h.t.Helper()
	row := h.projectorRow(generationID)
	if row.failureClass != class || row.generationStatus != "superseded" {
		h.t.Fatalf("%s = %+v, want superseded with failure class %q", generationID, row, class)
	}
	if len(h.canonical.calls) != canonicalBefore {
		h.t.Fatalf("refused %s wrote a canonical materialization", generationID)
	}
	if got := h.projectorRow(active); got.generationStatus != "active" {
		h.t.Fatalf("%s generation status = %q after %s refused, want active", active, got.generationStatus, generationID)
	}
}

// TestChainedDeltaUnionMatchesFullAtHeadLive is the live half of the #8780
// AC2 oracle: after F, d1, and d2 project, content_files and the canonical
// materialization equal those of a full generation projected at C, and the
// admin completeness check (ACTIVE read, goldengate assembly) reports the
// scope complete against the materialized graph side.
func TestChainedDeltaUnionMatchesFullAtHeadLive(t *testing.T) {
	h := newDeltaProjHarness(t)
	for _, gen := range []deltaProjGen{chainFullF, chainDeltaD1, chainDeltaD2} {
		h.commit(gen)
		h.project()
		if row := h.projectorRow(gen.id); row.generationStatus != "active" {
			t.Fatalf("%s = %+v, want active", gen.id, row)
		}
	}
	reference := newDeltaProjHarness(t)
	reference.commit(chainFullAtC)
	reference.project()

	if got, want := strings.Join(h.contentRows(), "|"), strings.Join(reference.contentRows(), "|"); got != want {
		t.Fatalf("content_files after the chain = %s\nfull at C = %s", got, want)
	}
	mat, refMat := h.lastMaterialization(), reference.lastMaterialization()
	if got, want := materializedEntitiesByFile(mat), materializedEntitiesByFile(refMat); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("materialized entities after the chain = %v, full at C = %v", got, want)
	}
	if len(mat.DeltaDeletedFilePaths) != 1 || !strings.HasSuffix(mat.DeltaDeletedFilePaths[0], "e.go") {
		t.Fatalf("delta deleted file paths = %v, want only e.go (deleted since the active commit)", mat.DeltaDeletedFilePaths)
	}

	snapshot, err := NewFactStore(SQLDB{DB: h.proof.db}).ReadDeltaUnionActive(context.Background(), []string{h.proof.scope.ScopeID})
	if err != nil {
		t.Fatalf("ReadDeltaUnionActive() = %v", err)
	}
	expectations := goldengate.AssembleDeltaUnionExpectations(snapshot.Rows, snapshot.Deltas)
	if len(expectations) != 1 || expectations[0].BaseGenerationID != chainFullF.id {
		t.Fatalf("expectations = %+v, want one scope whose base is %s (resolved at the union-base commit)", expectations, chainFullF.id)
	}
	observed := goldengate.DeltaUnionObservation{RepoID: expectations[0].RepoID, Files: map[string]int64{}}
	for _, file := range mat.Files {
		observed.Files[file.RelativePath] = 0
	}
	for _, entity := range mat.Entities {
		observed.Files[entity.RelativePath]++
	}
	if result := goldengate.CompareDeltaUnionRepo(expectations[0], observed); result.Outcome != goldengate.DeltaUnionRepoComplete {
		t.Fatalf("completeness = %+v, want complete", result)
	}
}

// TestBurstDeltaRefusedThenCumulativeRediffActivatesLive is the #8780 burst:
// d1 and d2 were both diffed while F was active; d1 activates, so d2 is
// refused at preflight with no write and no watermark. The next sync diffs
// from F with d1 as its fence commit, and that delta activates.
func TestBurstDeltaRefusedThenCumulativeRediffActivatesLive(t *testing.T) {
	h := newDeltaProjHarness(t)
	h.commit(chainFullF)
	h.project()
	h.commit(chainDeltaD1)
	h.project()

	burst := chainDeltaD2
	burst.id, burst.baseline = "gen-8780-burst", chainCommitA
	burst.deleted = nil
	before := len(h.canonical.calls)
	h.commit(burst)
	h.project()
	h.requireRefused(burst.id, projector.DeltaBaselineMismatchClass, before, chainDeltaD1.id)
	if got := h.reindexRequests(); got != 0 {
		t.Fatalf("repository_reindex_requests rows = %d after an active-differs refusal, want 0", got)
	}

	h.commit(chainDeltaD2)
	h.project()
	if row := h.projectorRow(chainDeltaD2.id); row.generationStatus != "active" {
		t.Fatalf("re-diffed cumulative delta = %+v, want active", row)
	}
	if got, want := strings.Join(h.contentFiles(), ","), "a.go,b.go,c.go,d.go,f.go"; got != want {
		t.Fatalf("content_files = %s, want the tree at C %s", got, want)
	}
}

// TestDeltaFencedOnOlderFullRefusesLive: a delta diffed from F1 while a newer
// full F2 at another commit became active refuses on the fence, whatever its
// union base.
func TestDeltaFencedOnOlderFullRefusesLive(t *testing.T) {
	h := newDeltaProjHarness(t)
	h.commit(chainFullF)
	h.project()
	newer := chainFullAtC
	newer.id, newer.commit = "gen-8780-f2", "eeee8780"
	h.commit(newer)
	h.project()

	stale := chainDeltaD1
	stale.id = "gen-8780-stale"
	before := len(h.canonical.calls)
	h.commit(stale)
	h.project()
	h.requireRefused(stale.id, projector.DeltaBaselineMismatchClass, before, newer.id)
}

// TestChainedDeltaWithDrainedBaseFailsClosedLive: when the union base F is
// drained after d2 is stored, d2 refuses base-missing before any write, the
// active delta d1 stays active, and a reindex watermark forces the full.
func TestChainedDeltaWithDrainedBaseFailsClosedLive(t *testing.T) {
	h := newDeltaProjHarness(t)
	h.commit(chainFullF)
	h.project()
	h.commit(chainDeltaD1)
	h.project()
	h.commit(chainDeltaD2)
	if _, err := h.proof.db.ExecContext(context.Background(),
		`DELETE FROM fact_records WHERE generation_id = $1`, chainFullF.id); err != nil {
		t.Fatalf("drain base facts: %v", err)
	}
	before := len(h.canonical.calls)
	h.project()
	h.requireRefused(chainDeltaD2.id, projector.DeltaBaselineBaseMissingClass, before, chainDeltaD1.id)
	if got := h.reindexRequests(); got != 1 {
		t.Fatalf("repository_reindex_requests rows = %d, want 1 (the forced full that restores the base)", got)
	}
}
