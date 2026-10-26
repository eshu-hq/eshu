// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const fallbackSeed = "fallbackseedcredential0123456789"

// runFallbackFixture streams one .npmrc content-file meta with no ContentStores
// map entry through the production fact stream and returns every envelope.
func runFallbackFixture(t *testing.T, snapshot RepositorySnapshot) []facts.Envelope {
	t.Helper()
	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, ".npmrc"),
		"password = \""+fallbackSeed+"\"\n")
	observedAt, err := time.Parse(time.RFC3339, policyObservedAt)
	if err != nil {
		t.Fatalf("parse observed at: %v", err)
	}
	collected := buildStreamingGeneration(repoPath,
		repositoryidentity.Metadata{ID: "repo://example/fallback", Name: "fallback"},
		"run", observedAt, snapshot, false, "")
	return drainFactChannel(collected.Facts)
}

// TestSnapshotMapMissOnGovernedPathFailsClosed proves a governed path in
// ContentFileMetas but absent from ContentStores never emits its raw body:
// the stream re-checks the snapshot registry and applies the policy instead
// of failing open (#8833 review F6).
func TestSnapshotMapMissOnGovernedPathFailsClosed(t *testing.T) {
	t.Parallel()

	// Control: without a registry to fall back to, the sparse map is the whole
	// contract, so the raw body streams (fail open). This proves the fixture
	// exercises the emit path and the assertion below is not vacuous.
	control := factKindsWithSeed(t, runFallbackFixture(t, RepositorySnapshot{
		ContentFileMetas: []model.ContentFileMeta{{RelativePath: ".npmrc", CommitSHA: "c0ffee"}},
	}), fallbackSeed)
	if !slices.Contains(control, "content") {
		t.Fatalf("control: the content fact does not hold the seed (got %v); the fixture is vacuous", control)
	}

	missed := factKindsWithSeed(t, runFallbackFixture(t, RepositorySnapshot{
		ContentFileMetas: []model.ContentFileMeta{{RelativePath: ".npmrc", CommitSHA: "c0ffee"}},
		// No ContentStores entry for the governed path.
		ContentStoreFallbackRegistry: parser.DefaultRegistry(),
	}), fallbackSeed)
	if len(missed) != 0 {
		t.Errorf("map miss on governed .npmrc emitted the raw body to fact kinds %v", missed)
	}
}
