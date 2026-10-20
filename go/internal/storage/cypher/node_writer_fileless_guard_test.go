// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Regression tests for eshu-hq/eshu#8657: a file-less build (zero live and
// zero tombstoned file facts, zero File rows) over a scope that already has
// File nodes must fail loud instead of letting the unbounded retract wipe
// them and reporting success. Only the writer sees both halves (the
// materialization's file intent plus the graph's prior File nodes), so the
// check lives in CanonicalNodeWriter.Write, ahead of the retract phase.
//
// Proof matrix (eshu-correlation-truth):
//   - positive: prior-File wipe fails loud, names generation/scope/count,
//     is retryable, and writes nothing;
//   - negative: zero-prior build succeeds; tombstone-driven mass delete,
//     first-generation, delta, non-repository, and file-bearing builds never
//     probe;
//   - ambiguous: unknown probe (no seam, error) proceeds with the retract
//     (fail-safe direction per the ProbeExecutor contract) and counts the
//     outcome; a declared-empty scope skips the probe (escape hatch #8668,
//     wired after that lane lands).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// filelessGuardFakeExecutor is a write-only Executor double with an
// optional CountProbeExecutor capability. writes counts Execute calls so
// tests prove the refused wipe issues no backend write at all.
type filelessGuardFakeExecutor struct {
	count     int64
	countErr  error
	hasCount  bool
	probes    int
	writes    int
	lastProbe Statement
}

func (f *filelessGuardFakeExecutor) Execute(context.Context, Statement) error {
	f.writes++
	return nil
}

func (f *filelessGuardFakeExecutor) ExecuteCountProbe(_ context.Context, stmt Statement) (int64, error) {
	if !f.hasCount {
		return 0, errors.New("inner executor does not support ExecuteCountProbe")
	}
	f.probes++
	f.lastProbe = stmt
	return f.count, f.countErr
}

// filelessGuardMat builds a non-first, non-delta, repository-scoped
// materialization carrying only a Repository row: the exact shape whose
// unbounded file retract wipes every prior File node on success.
func filelessGuardMat() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope-8657",
		GenerationID: "gen-8657-fileless",
		RepoID:       "repo-8657",
		Repository:   &canonical.RepositoryRow{RepoID: "repo-8657", Name: "p", Path: "/repos/p"},
	}
}

type retryabler interface{ Retryable() bool }

func isRetryableErr(t *testing.T, err error) bool {
	t.Helper()
	var r retryabler
	if !errors.As(err, &r) {
		return false
	}
	return r.Retryable()
}

func TestFilelessWipeGuardRefusesPriorFiles(t *testing.T) {
	t.Parallel()

	exec := &filelessGuardFakeExecutor{hasCount: true, count: 41}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	err := writer.Write(context.Background(), filelessGuardMat())
	if err == nil {
		t.Fatal("Write() error = nil, want loud refusal; the unbounded retract would wipe 41 prior File nodes and report success")
	}
	for _, want := range []string{"gen-8657-fileless", "scope-8657", "41"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Write() error = %v, want it to name %q (generation, scope, prior count)", err, want)
		}
	}
	if !isRetryableErr(t, err) {
		t.Fatalf("Write() error = %v, want a retryable failure so the queue re-drives instead of dead-lettering", err)
	}
	if exec.probes != 1 {
		t.Fatalf("probes = %d, want 1; the guard must probe before deleting", exec.probes)
	}
	if exec.writes != 0 {
		t.Fatalf("writes = %d, want 0; a refused wipe must reach no backend write", exec.writes)
	}
	// The probe shape is the cost contract: an anchored equality on the
	// UNIQUE Repository(id) index plus a one-hop REPO_CONTAINS expand, so
	// the probe costs at most the retract's own match phase. Pin the text
	// here (the schema tests pin the index, the Phase-1 PROFILE in
	// evidence-8657-writer-conditional-retract.md pins the plan they make).
	if exec.lastProbe.Operation != OperationCanonicalCountProbe {
		t.Fatalf("probe operation = %q, want %q", exec.lastProbe.Operation, OperationCanonicalCountProbe)
	}
	for _, want := range []string{
		`MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)`,
		`count(f)`,
	} {
		if !strings.Contains(exec.lastProbe.Cypher, want) {
			t.Fatalf("probe cypher missing %q; the guard must probe the anchored count shape, got:\n%s", want, exec.lastProbe.Cypher)
		}
	}
	if exec.lastProbe.Parameters["repo_id"] != "repo-8657" {
		t.Fatalf("probe repo_id = %v, want repo-8657; the probe must scope to this build's repository", exec.lastProbe.Parameters["repo_id"])
	}
}

func TestFilelessWipeGuardProceedsWhenZeroPriors(t *testing.T) {
	t.Parallel()

	exec := &filelessGuardFakeExecutor{hasCount: true, count: 0}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	if err := writer.Write(context.Background(), filelessGuardMat()); err != nil {
		t.Fatalf("Write() error = %v, want nil; zero priors means the retract deletes nothing", err)
	}
	if exec.probes != 1 {
		t.Fatalf("probes = %d, want 1", exec.probes)
	}
	if exec.writes == 0 {
		t.Fatal("writes = 0, want the normal write path to run once the probe clears it")
	}
}

func TestFilelessWipeGuardProceedsWhenProbeUnsupported(t *testing.T) {
	t.Parallel()

	exec := &filelessGuardFakeExecutor{hasCount: false}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	if err := writer.Write(context.Background(), filelessGuardMat()); err != nil {
		t.Fatalf("Write() error = %v, want nil; an unknown probe is never zero rows (ProbeExecutor contract), so the retract runs as before", err)
	}
	if exec.writes == 0 {
		t.Fatal("writes = 0, want the status-quo retract path on an unprobed backend")
	}
}

func TestFilelessWipeGuardProceedsWhenProbeFails(t *testing.T) {
	t.Parallel()

	exec := &filelessGuardFakeExecutor{hasCount: true, countErr: errors.New("boom")}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	if err := writer.Write(context.Background(), filelessGuardMat()); err != nil {
		t.Fatalf("Write() error = %v, want nil; a probe-only failure cannot fail a projection whose write would succeed", err)
	}
}

// A tombstone-driven mass delete is the retract's legitimate work: the
// guard must not probe it even when priors would match.
func TestFilelessWipeGuardSkipsTombstoneDrivenBuild(t *testing.T) {
	t.Parallel()

	exec := &filelessGuardFakeExecutor{hasCount: true, count: 99}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := filelessGuardMat()
	mat.FileIntent = canonical.FileIntentSummary{TombstonedFileFacts: 3}
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil; tombstone-driven deletes stay exempt", err)
	}
	if exec.probes != 0 {
		t.Fatalf("probes = %d, want 0; exempt builds must not pay for a probe", exec.probes)
	}
}

func TestFilelessWipeGuardSkipsNonWipeShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*canonical.CanonicalMaterialization)
	}{
		{"first generation", func(m *canonical.CanonicalMaterialization) { m.FirstGeneration = true }},
		{"delta projection", func(m *canonical.CanonicalMaterialization) { m.DeltaProjection = true }},
		{"non-repository scope", func(m *canonical.CanonicalMaterialization) { m.RepoID = "" }},
		{"file-bearing", func(m *canonical.CanonicalMaterialization) {
			m.Files = []canonical.FileRow{{Path: "/repos/p/a.go", RelativePath: "a.go", Name: "a.go", RepoID: "repo-8657"}}
		}},
		{"live file facts", func(m *canonical.CanonicalMaterialization) {
			m.FileIntent = canonical.FileIntentSummary{LiveFileFacts: 2}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exec := &filelessGuardFakeExecutor{hasCount: true, count: 99}
			writer := NewCanonicalNodeWriter(exec, 500, nil)
			mat := filelessGuardMat()
			tc.mutate(&mat)
			if err := writer.Write(context.Background(), mat); err != nil {
				t.Fatalf("Write() error = %v, want nil; %s never selects the unbounded file retract", err, tc.name)
			}
			if exec.probes != 0 {
				t.Fatalf("probes = %d, want 0; %s must not pay for a probe", exec.probes, tc.name)
			}
		})
	}
}

// The empty-scope marker (#8668) is the escape hatch: a scope that
// declared filelessness skips the probe and lets the retract authorize the
// wipe. The declaration rides the materialization after that lane lands;
// this pins the guard half of the contract against the seam it will read.
func TestFilelessWipeGuardHonorsDeclaredEmptyScope(t *testing.T) {
	t.Parallel()

	exec := &filelessGuardFakeExecutor{hasCount: true, count: 99}
	outcome, err := checkFilelessWipeGuard(context.Background(), exec, filelessGuardMat(), true)
	if err != nil {
		t.Fatalf("checkFilelessWipeGuard(declaredEmpty=true) error = %v, want nil; the marker authorizes the wipe", err)
	}
	if outcome != filelessWipeDeclaredEmpty {
		t.Fatalf("checkFilelessWipeGuard(declaredEmpty=true) outcome = %q, want %q", outcome, filelessWipeDeclaredEmpty)
	}
	if exec.probes != 0 {
		t.Fatalf("probes = %d, want 0; a declared-empty scope skips the probe", exec.probes)
	}
}

// The guard trigger must stay byte-identical to the statement selection in
// buildRetractStatements: the unbounded RetractFilesCypher runs exactly
// when the predicate holds, the bounded RetractRemovedFilesCypher exactly
// when the materialization carries files.
func TestUsesUnboundedFileRetractMatchesStatementSelection(t *testing.T) {
	t.Parallel()

	// Nil receiver: the non-delta retract path never dereferences w.
	var w *CanonicalNodeWriter
	fileless := filelessGuardMat()
	if !usesUnboundedFileRetract(fileless) {
		t.Fatal("usesUnboundedFileRetract(file-less) = false, want true")
	}
	if got := w.buildRetractStatements(fileless); !containsCypher(got, "DETACH DELETE f") || containsCypher(got, "$file_paths") {
		t.Fatalf("file-less retract statements select %q, want the unbounded shape", summarizeStatementCyphers(got))
	}
	withFiles := filelessGuardMat()
	withFiles.Files = []canonical.FileRow{{Path: "/repos/p/a.go", RelativePath: "a.go", Name: "a.go", RepoID: "repo-8657"}}
	if usesUnboundedFileRetract(withFiles) {
		t.Fatal("usesUnboundedFileRetract(with files) = true, want false")
	}
	if got := w.buildRetractStatements(withFiles); !containsCypher(got, "NOT (f.path IN $file_paths)") {
		t.Fatalf("file-bearing retract statements select %q, want the bounded positive-list shape", summarizeStatementCyphers(got))
	}
}

func containsCypher(stmts []Statement, substr string) bool {
	for _, s := range stmts {
		if strings.Contains(s.Cypher, substr) {
			return true
		}
	}
	return false
}

func summarizeStatementCyphers(stmts []Statement) string {
	kinds := make([]string, 0, len(stmts))
	for _, s := range stmts {
		first := strings.SplitN(strings.TrimSpace(s.Cypher), "\n", 2)[0]
		kinds = append(kinds, first)
	}
	return strings.Join(kinds, " | ")
}
