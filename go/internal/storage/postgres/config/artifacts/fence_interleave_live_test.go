// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// Interleaving tests of protocol companion section 6. PR3b-1b ships no
// derive, so each test stands in for a derive with the two reads the derive
// protocol makes (section 3): the committed version v0, then the content
// list. A derive stores content_version = v0. The fence is correct when every
// content commit after that read moves the version past v0, so the
// production trust rule (artifacts.Classify) reports stale_version for the
// stored state, and a derive that reads after the last commit is trusted.

// deriveRead is what a derive reads before it writes: the version, then the
// number of content rows it lists for the repository.
type deriveRead struct {
	v0    int64
	files int
}

func readForDerive(ctx context.Context, t *testing.T, db *sql.DB, repo string) deriveRead {
	t.Helper()
	read := deriveRead{v0: versionOf(ctx, t, db, repo)}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_files WHERE repo_id = $1`, repo).Scan(&read.files); err != nil {
		t.Fatal(err)
	}
	return read
}

// trustOf classifies a successful derive that stored v0 against the current
// version, with the production reader rule.
func trustOf(ctx context.Context, t *testing.T, db *sql.DB, repo string, v0 int64) artifacts.Reason {
	t.Helper()
	rows := 1
	state := &artifacts.State{ContentVersion: &v0, ExtractorVersion: 1, RowCount: &rows}
	current := versionOf(ctx, t, db, repo)
	return artifacts.Classify(state, &current, 1)
}

// holdReferenceRows plants content_file_references rows for paths and holds
// them, so a Write batch that carries one of those paths stalls on its
// reference delete, before its content insert.
func holdReferenceRows(ctx context.Context, t *testing.T, db *sql.DB, repo string, paths []string) *rowLockHolder {
	t.Helper()
	for _, p := range paths {
		mustExec(ctx, t, db, fmt.Sprintf(`
INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
VALUES ('%s', '%s', 'zz_hold', 'x', now())`, repo, p))
	}
	return holdRows(ctx, t, db,
		`SELECT 1 FROM content_file_references
WHERE repo_id = $1 AND reference_kind = 'zz_hold' AND relative_path = ANY($2) FOR UPDATE`, repo, paths)
}

// pendingWrite is a content Write running in its own goroutine; err is
// valid after the WaitGroup it was started with is done.
type pendingWrite struct {
	err error
}

// goWrite starts the production ContentWriter.Write of mat under wg.
func goWrite(ctx context.Context, db *sql.DB, wg *sync.WaitGroup, mat content.Materialization) *pendingWrite {
	pending := &pendingWrite{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, pending.err = newWriter(db).Write(ctx, mat)
	}()
	return pending
}

// TestVersionFenceR1WriteBatchesAfterDeriveReadLive is the R-1 sequence: a
// Write with three batches and a derive read between batches 1 and 2. The
// derive sees only batch 1, so its stored state must read stale after the
// Write; a derive read after the Write is trusted and sees every file.
func TestVersionFenceR1WriteBatchesAfterDeriveReadLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const repo = "repo-r1"
	seedZeroVersions(ctx, t, db, repo)
	mat := fileMaterialization(repo, "scope-r1", "src", 1500)
	holder := holdReferenceRows(ctx, t, db, repo, []string{mat.Records[500].Path})
	var wg sync.WaitGroup
	write := goWrite(ctx, db, &wg, mat)
	waitForLockWaiters(ctx, t, db, "content_file_references", 1)

	mid := readForDerive(ctx, t, db, repo)
	if mid.v0 != 1 || mid.files != 500 {
		t.Fatalf("derive read between batches = version %d, %d files; want version 1, 500 files", mid.v0, mid.files)
	}
	holder.release(t)
	wg.Wait()
	if write.err != nil {
		t.Fatalf("write: %v", write.err)
	}
	if got := trustOf(ctx, t, db, repo, mid.v0); got != artifacts.ReasonStaleVersion {
		t.Fatalf("derive that read between batches classifies %q, want %q", got, artifacts.ReasonStaleVersion)
	}
	final := readForDerive(ctx, t, db, repo)
	if final.v0 != 3 || final.files != 1500 || trustOf(ctx, t, db, repo, final.v0) != artifacts.ReasonTrusted {
		t.Fatalf("derive after the Write = version %d, %d files; want version 3, 1500 files, trusted", final.v0, final.files)
	}
}

// TestVersionFenceTwoScopeWritesAfterDeriveReadLive is the two-scope
// sequence: two scopes write one repository concurrently, and a derive reads
// after the first scope's first batch while the second scope's first batch is
// held. Both Writes land after the read, so the stored state reads stale.
func TestVersionFenceTwoScopeWritesAfterDeriveReadLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const repo = "repo-two-scope"
	seedZeroVersions(ctx, t, db, repo)
	matX := fileMaterialization(repo, "scope-x", "x", 1000)
	matY := fileMaterialization(repo, "scope-y", "y", 1000)
	holdX := holdReferenceRows(ctx, t, db, repo, []string{matX.Records[500].Path})
	holdY := holdReferenceRows(ctx, t, db, repo, []string{matY.Records[0].Path})
	var wg sync.WaitGroup
	writes := []*pendingWrite{goWrite(ctx, db, &wg, matX), goWrite(ctx, db, &wg, matY)}
	waitForLockWaiters(ctx, t, db, "content_file_references", 2)

	mid := readForDerive(ctx, t, db, repo)
	if mid.v0 != 1 || mid.files != 500 {
		t.Fatalf("derive read = version %d, %d files; want version 1, 500 files (scope x batch 1)", mid.v0, mid.files)
	}
	holdY.release(t)
	holdX.release(t)
	wg.Wait()
	for i, w := range writes {
		if w.err != nil {
			t.Fatalf("write %d: %v", i, w.err)
		}
	}
	if got := trustOf(ctx, t, db, repo, mid.v0); got != artifacts.ReasonStaleVersion {
		t.Fatalf("mid derive classifies %q, want %q", got, artifacts.ReasonStaleVersion)
	}
	if final := readForDerive(ctx, t, db, repo); final.v0 != 4 || final.files != 2000 {
		t.Fatalf("after both Writes: version %d, %d files; want version 4, 2000 files", final.v0, final.files)
	}
}

// TestVersionFencePruneAfterDeriveReadLive is the prune sequence: a derive
// reads while a retention pass has deleted the repository's doomed rows but
// not committed. The derive still lists them; the prune's commit moves the
// version, so the stored state reads stale.
func TestVersionFencePruneAfterDeriveReadLive(t *testing.T) {
	ctx, db := openDatabase(t)
	const repo = "repo-prune-seq"
	seedZeroVersions(ctx, t, db, repo)
	seedRetentionScopes(ctx, t, db, []retentionScope{{"scope-prune-seq", repo, "old", 50}})
	writeContent(ctx, t, db, fileMaterialization(repo, "scope-live", "live", 5))
	start := versionOf(ctx, t, db, repo)

	holder := holdRows(ctx, t, db, `SELECT 1 FROM config_artifact_content_versions WHERE repo_id = $1 FOR UPDATE`, repo)
	var wg sync.WaitGroup
	var pruneErr error
	wg.Add(1)
	go func() { defer wg.Done(); _, pruneErr = prune(ctx, db) }()
	waitForLockWaiters(ctx, t, db, retentionCommitWaiter, 1)

	mid := readForDerive(ctx, t, db, repo)
	if mid.v0 != start || mid.files != 55 {
		t.Fatalf("derive read during the prune = version %d, %d files; want version %d, 55 files", mid.v0, mid.files, start)
	}
	holder.release(t)
	wg.Wait()
	if pruneErr != nil {
		t.Fatalf("prune: %v", pruneErr)
	}
	if got := trustOf(ctx, t, db, repo, mid.v0); got != artifacts.ReasonStaleVersion {
		t.Fatalf("derive during the prune classifies %q, want %q", got, artifacts.ReasonStaleVersion)
	}
	if final := readForDerive(ctx, t, db, repo); final.v0 != start+1 || final.files != 5 {
		t.Fatalf("after the prune: version %d, %d files; want version %d, 5 files", final.v0, final.files, start+1)
	}
}
