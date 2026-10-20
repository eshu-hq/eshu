// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Live backend proof for eshu-hq/eshu#8657 (writer-side conditional
// retract). Opt-in like every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (or ESHU_NEO4J_URI) to run against a running graph
// backend. Skipped otherwise.
//
// Proof matrix (eshu-correlation-truth), each with a direct graph check:
//   - positive: a file-less build over prior File nodes fails loud naming
//     generation, scope, and prior count, and the priors stay intact;
//   - negative: a zero-prior file-less build succeeds (and writes its
//     Repository node); a tombstone-driven mass delete succeeds and the
//     priors are gone; a file-less build on a chain without the count seam
//     proceeds unprobed (fail-safe direction, the production hole shape);
//   - ambiguous: concurrent file-less and file-bearing generations for one
//     scope never wipe silently -- every file-less attempt fails loud and
//     the final File count is exact.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// liveFilelessGuardExecutor adapts the Bolt test runner to the writer
// seams, including the #8657 CountProbeExecutor: it runs the guard's COUNT
// statement and reports the first column, exactly what the production
// projectorNeo4jExecutor does.
type liveFilelessGuardExecutor struct {
	runner *boltRetractTestRunner
}

func (e *liveFilelessGuardExecutor) Execute(ctx context.Context, stmt Statement) error {
	return e.runner.runCypherSingle(ctx, stmt)
}

func (e *liveFilelessGuardExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := e.runner.runCypherGroup(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (e *liveFilelessGuardExecutor) ExecuteCountProbe(ctx context.Context, stmt Statement) (int64, error) {
	rows, err := e.runner.runCypher(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("count probe returned no rows")
	}
	var first any
	for _, v := range rows[0] {
		first = v
		break
	}
	switch n := first.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float64:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("count probe first column has unexpected type %T", first)
	}
}

// liveFilelessGuardSeed creates a Repository with count root File nodes
// (production-shaped properties plus REPO_CONTAINS edges) stamped with
// genID. It returns a cleanup that removes exactly this repository's
// subgraph.
func liveFilelessGuardSeed(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, repoID, genID string, count int) {
	t.Helper()

	if err := boltWriteStatement(ctx, runner,
		`MERGE (r:Repository {id: $repo_id})`,
		map[string]any{"repo_id": repoID},
	); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	const batch = 50
	for lo := 0; lo < count; lo += batch {
		hi := lo + batch
		if hi > count {
			hi = count
		}
		paths := make([]string, 0, hi-lo)
		for i := lo; i < hi; i++ {
			paths = append(paths, fmt.Sprintf("/repos/%s/main-%04d.go", repoID, i))
		}
		if err := boltWriteStatement(ctx, runner,
			`MATCH (r:Repository {id: $repo_id})
UNWIND $paths AS p
CREATE (f:File {path: p, repo_id: $repo_id, evidence_source: 'projector/canonical', generation_id: $gen_id})
CREATE (r)-[:REPO_CONTAINS]->(f)`,
			map[string]any{"repo_id": repoID, "gen_id": genID, "paths": paths},
		); err != nil {
			t.Fatalf("seed files: %v", err)
		}
	}
	t.Cleanup(func() {
		if err := boltWriteStatement(context.Background(), runner,
			`MATCH (r:Repository {id: $repo_id}) OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(f:File) DETACH DELETE r, f`,
			map[string]any{"repo_id": repoID},
		); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
}

func liveFilelessGuardFileCount(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, repoID string) int64 {
	t.Helper()
	count, err := boltCount(ctx, runner,
		`MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File) RETURN count(f) AS count`,
		map[string]any{"repo_id": repoID},
	)
	if err != nil {
		t.Fatalf("count files: %v", err)
	}
	return count
}

// liveFilelessGuardMat builds the Repository-only materialization whose
// unbounded retract wipes every prior File node on success.
func liveFilelessGuardMat(scopeID, genID, repoID string) canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      scopeID,
		GenerationID: genID,
		RepoID:       repoID,
		Repository:   &canonical.RepositoryRow{RepoID: repoID, Name: repoID, Path: "/repos/" + repoID},
	}
}

type liveRetryabler interface{ Retryable() bool }

func liveIsRetryable(err error) bool {
	var r liveRetryabler
	if !errors.As(err, &r) {
		return false
	}
	return r.Retryable()
}

func TestLiveFilelessWipeRefusedPriorFilesIntact(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		repoID = "live-8657-refuse"
		scope  = "scope-live-8657-refuse"
		gen1   = "gen-live-8657-refuse-1"
		gen2   = "gen-live-8657-refuse-2"
	)
	liveFilelessGuardSeed(t, ctx, runner, repoID, gen1, 25)
	if got := liveFilelessGuardFileCount(t, ctx, runner, repoID); got != 25 {
		t.Fatalf("seeded file count = %d, want 25", got)
	}

	writer := NewCanonicalNodeWriter(&liveFilelessGuardExecutor{runner: runner}, 500, nil)
	err := writer.Write(ctx, liveFilelessGuardMat(scope, gen2, repoID))
	if err == nil {
		t.Fatal("Write() error = nil, want loud refusal; the unbounded retract would wipe 25 prior File nodes and report success")
	}
	for _, want := range []string{gen2, scope, "25"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Write() error = %v, want it to name %q (generation, scope, prior count)", err, want)
		}
	}
	if !liveIsRetryable(err) {
		t.Fatalf("Write() error = %T, want a retryable failure so the queue re-drives", err)
	}
	if got := liveFilelessGuardFileCount(t, ctx, runner, repoID); got != 25 {
		t.Fatalf("post-refusal file count = %d, want 25; a refused wipe must delete nothing", got)
	}
}

func TestLiveFilelessBuildSucceedsWithZeroPriors(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		repoID = "live-8657-clear"
		scope  = "scope-live-8657-clear"
		gen    = "gen-live-8657-clear-2"
	)
	liveFilelessGuardSeed(t, ctx, runner, repoID, "gen-live-8657-clear-1", 0)

	writer := NewCanonicalNodeWriter(&liveFilelessGuardExecutor{runner: runner}, 500, nil)
	if err := writer.Write(ctx, liveFilelessGuardMat(scope, gen, repoID)); err != nil {
		t.Fatalf("Write() error = %v, want nil; zero priors means the retract deletes nothing", err)
	}
	if got := liveFilelessGuardFileCount(t, ctx, runner, repoID); got != 0 {
		t.Fatalf("file count = %d, want 0", got)
	}
}

// A tombstone-driven mass delete is the retract's legitimate work: priors
// exist, the build carries tombstoned file intent, and the write succeeds
// with the priors gone.
func TestLiveTombstoneDrivenMassDeleteSucceeds(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		repoID = "live-8657-tombstone"
		scope  = "scope-live-8657-tombstone"
		gen    = "gen-live-8657-tombstone-2"
	)
	liveFilelessGuardSeed(t, ctx, runner, repoID, "gen-live-8657-tombstone-1", 10)

	mat := liveFilelessGuardMat(scope, gen, repoID)
	mat.FileIntent = canonical.FileIntentSummary{TombstonedFileFacts: 10}
	writer := NewCanonicalNodeWriter(&liveFilelessGuardExecutor{runner: runner}, 500, nil)
	if err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("Write() error = %v, want nil; tombstone-driven deletes stay exempt", err)
	}
	if got := liveFilelessGuardFileCount(t, ctx, runner, repoID); got != 0 {
		t.Fatalf("file count = %d, want 0; the exempt mass delete must still retract", got)
	}
}

// A file-less build on a chain without the count seam proceeds unprobed --
// the fail-safe direction. This pins the degraded path live and reproduces
// the original hole shape (success after wiping priors) exactly where no
// probe exists.
func TestLiveFilelessWipeProceedsUnprobedWithoutCountSeam(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		repoID = "live-8657-unprobed"
		scope  = "scope-live-8657-unprobed"
		gen    = "gen-live-8657-unprobed-2"
	)
	liveFilelessGuardSeed(t, ctx, runner, repoID, "gen-live-8657-unprobed-1", 10)

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil)
	if err := writer.Write(ctx, liveFilelessGuardMat(scope, gen, repoID)); err != nil {
		t.Fatalf("Write() error = %v, want nil; unknown probes proceed with the retract", err)
	}
	if got := liveFilelessGuardFileCount(t, ctx, runner, repoID); got != 0 {
		t.Fatalf("file count = %d, want 0; the unprobed retract still runs", got)
	}
}

// Concurrent file-less and file-bearing generations for one scope: every
// file-less attempt must fail loud and the final File count must be exact.
// The file-bearing generation lists every seeded path, so its bounded
// retract keeps them; the seeded priors therefore exist at every
// file-less probe, making refusal deterministic under overlap.
func TestLiveFilelessGuardConcurrentGenerations(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		repoID = "live-8657-concurrent"
		scope  = "scope-live-8657-concurrent"
	)
	const files = 20
	liveFilelessGuardSeed(t, ctx, runner, repoID, "gen-live-8657-concurrent-1", files)

	var fileRows []canonical.FileRow
	for i := 0; i < files; i++ {
		rel := fmt.Sprintf("main-%04d.go", i)
		fileRows = append(fileRows, canonical.FileRow{
			Path:         "/repos/" + repoID + "/" + rel,
			RelativePath: rel,
			Name:         rel,
			Language:     "go",
			RepoID:       repoID,
		})
	}
	bearing := liveFilelessGuardMat(scope, "gen-live-8657-concurrent-3", repoID)
	bearing.Files = fileRows
	bearing.FileIntent = canonical.FileIntentSummary{LiveFileFacts: files}

	const filelessAttempts = 4
	errs := make([]error, filelessAttempts)
	var wg sync.WaitGroup
	for i := 0; i < filelessAttempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := NewCanonicalNodeWriter(&liveFilelessGuardExecutor{runner: runner}, 500, nil)
			errs[i] = w.Write(context.Background(), liveFilelessGuardMat(scope, fmt.Sprintf("gen-live-8657-concurrent-2-%d", i), repoID))
		}(i)
	}
	wg.Add(1)
	var bearingErr error
	go func() {
		defer wg.Done()
		w := NewCanonicalNodeWriter(&liveFilelessGuardExecutor{runner: runner}, 500, nil)
		bearingErr = w.Write(context.Background(), bearing)
	}()
	wg.Wait()

	if bearingErr != nil {
		t.Fatalf("file-bearing Write() error = %v, want nil", bearingErr)
	}
	for i, err := range errs {
		if err == nil {
			t.Fatalf("file-less Write() %d error = nil, want loud refusal under overlap", i)
		}
		if !liveIsRetryable(err) {
			t.Fatalf("file-less Write() %d error = %T, want retryable", i, err)
		}
	}
	if got := liveFilelessGuardFileCount(t, ctx, runner, repoID); got != files {
		t.Fatalf("final file count = %d, want %d; no interleaving may wipe silently", got, files)
	}
}

// The guard's count probe stays exact as the repository grows.
// ESHU_8657_SCALE_ROWS sets the seeded File count (default 2000, cheap
// enough for CI live lanes; the Phase-1 theory proof ran 1,001,002 rows on
// the pinned image -- see evidence-8657-writer-conditional-retract.md).
// The anchored plan shape is pinned by the unit probe-shape assertion plus
// the schema tests pinning the UNIQUE Repository(id) index; this test pins
// the exact count the guard decides on. (EXPLAIN/PROFILE plans are not
// asserted here: this backend's Bolt driver surfaces no plan rows for
// EXPLAIN and drops PROFILE counters, so the plan tables live in the
// evidence note instead.)
func TestLiveFileCountProbeStaysAnchoredAtScale(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	rows := 2000
	if v := strings.TrimSpace(os.Getenv("ESHU_8657_SCALE_ROWS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			rows = parsed
		}
	}

	const repoID = "live-8657-scale"
	liveFilelessGuardSeed(t, ctx, runner, repoID, "gen-live-8657-scale-1", rows)

	exec := &liveFilelessGuardExecutor{runner: runner}
	count, err := exec.ExecuteCountProbe(ctx, Statement{
		Operation:  OperationCanonicalCountProbe,
		Cypher:     priorFileCountProbeCypher,
		Parameters: map[string]any{"repo_id": repoID},
	})
	if err != nil {
		t.Fatalf("ExecuteCountProbe() error = %v", err)
	}
	if count != int64(rows) {
		t.Fatalf("ExecuteCountProbe() = %d, want %d; the guard must see the exact prior count", count, rows)
	}
}
