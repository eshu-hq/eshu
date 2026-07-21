// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestStreamFactBodyReadCountBeforeAfter is the measured before/after for
// #4877: it counts physical content-body reads through the
// streamContentBodyReadFile seam and proves the removed pre-stream count pass
// eliminated one body read per candidate file.
//
// Before #4877, three count functions (serviceCatalogFactCount,
// gitDocumentationFactCount, workflowImageEvidenceFactCount) each read a
// candidate file body once before streaming, and streamFacts then read the same
// body again at emit time — 2 reads per candidate. After #4877, only the emit
// read remains — 1 read per candidate. The fixture below uses documentation
// candidate files (which the removed gitDocumentationFactCount read), so the
// removed pass would have read all N once; the test reconstructs that removed
// pass faithfully (one body read per candidate) to measure the BEFORE, and runs
// the real current path to measure the AFTER.
//
// Not parallel: it swaps the package-level streamContentBodyReadFile seam.
func TestStreamFactBodyReadCountBeforeAfter(t *testing.T) {
	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)

	// Documentation candidate files: the removed gitDocumentationFactCount read
	// each of these bodies once pre-stream.
	relPaths := []string{"README.md", "docs/guide.md", "docs/api/reference.md"}
	for i, rel := range relPaths {
		writeCollectorTestFile(t, filepath.Join(repoPath, rel),
			"# Doc "+rel+"\n\nBody paragraph "+string(rune('a'+i))+" with enough text to parse.\n")
	}
	metas := make([]ContentFileMeta, len(relPaths))
	for i, rel := range relPaths {
		metas[i] = ContentFileMeta{RelativePath: rel, Digest: "sha256:doc", Language: "markdown", CommitSHA: "abc123"}
	}
	repo := testCollectorRepositoryMetadata(repoPath)

	// buildStreamingGeneration consumes/nils the snapshot as it streams
	// (two-phase memory), so each run needs a fresh snapshot.
	freshSnapshot := func() RepositorySnapshot {
		fresh := make([]ContentFileMeta, len(metas))
		copy(fresh, metas)
		return RepositorySnapshot{FileCount: len(fresh), ContentFileMetas: fresh}
	}

	// Install a counting seam over the single stream-time body read.
	var mu sync.Mutex
	reads := map[string]int{}
	original := streamContentBodyReadFile
	streamContentBodyReadFile = func(path string) ([]byte, error) {
		mu.Lock()
		reads[filepath.Clean(path)]++
		mu.Unlock()
		return original(path)
	}
	defer func() { streamContentBodyReadFile = original }()

	absOf := func(rel string) string { return filepath.Clean(filepath.Join(repoPath, rel)) }

	// AFTER (current code): buildStreamingGeneration + drain reads each content
	// body exactly once.
	for k := range reads {
		delete(reads, k)
	}
	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, freshSnapshot(), false, "")
	_ = drainFactChannel(collected.Facts)
	afterTotal := 0
	for _, rel := range relPaths {
		c := reads[absOf(rel)]
		if c != 1 {
			t.Fatalf("AFTER: %s read %d times during stream, want exactly 1", rel, c)
		}
		afterTotal += c
	}
	if afterTotal != len(relPaths) {
		t.Fatalf("AFTER: total body reads = %d, want %d (one per candidate)", afterTotal, len(relPaths))
	}

	// BEFORE (reconstructed): the removed pre-stream count pass read each
	// candidate body once, then the stream read it again — 2 per candidate.
	for k := range reads {
		delete(reads, k)
	}
	for _, rel := range relPaths { // removed count pass: one read per candidate
		if _, err := streamContentBodyReadFile(absOf(rel)); err != nil {
			t.Fatalf("reconstructed count read %s: %v", rel, err)
		}
	}
	collectedBefore := buildStreamingGeneration(repoPath, repo, "run-2", observedAt, freshSnapshot(), false, "")
	_ = drainFactChannel(collectedBefore.Facts)
	beforeTotal := 0
	for _, rel := range relPaths {
		c := reads[absOf(rel)]
		if c != 2 {
			t.Fatalf("BEFORE: %s read %d times (count pass + stream), want exactly 2", rel, c)
		}
		beforeTotal += c
	}

	// Measured delta: the removed count pass eliminated one body read per
	// candidate (2N -> N).
	if beforeTotal != 2*afterTotal {
		t.Fatalf("expected BEFORE (%d) == 2x AFTER (%d): the fix removes one read per candidate", beforeTotal, afterTotal)
	}
	t.Logf("body reads per candidate: BEFORE=2 (count pass + stream), AFTER=1 (stream only); total %d -> %d for %d candidates",
		beforeTotal, afterTotal, len(relPaths))
}
