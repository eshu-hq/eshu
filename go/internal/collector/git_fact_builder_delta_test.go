// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildStreamingGenerationEmitsDeltaMetadataAndDeletedTombstones(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 13, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-changed")
	snapshot.Delta = true
	snapshot.DeltaRelativePaths = []string{"app.py", "old/deleted.py"}
	snapshot.DeletedRelativePaths = []string{"old/deleted.py"}

	collected := buildStreamingGeneration(repoPath, repo, "run-delta", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := len(envelopes), collected.FactCount(); got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d", got, want)
	}

	var repositoryPayload map[string]any
	var fileTombstoneSeen bool
	var contentTombstoneSeen bool
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case "repository":
			repositoryPayload = envelope.Payload
		case "file":
			if envelope.IsTombstone && envelope.Payload["relative_path"] == "old/deleted.py" {
				fileTombstoneSeen = true
			}
		case "content":
			if envelope.IsTombstone && envelope.Payload["content_path"] == "old/deleted.py" {
				contentTombstoneSeen = true
			}
		}
	}
	if repositoryPayload == nil {
		t.Fatal("missing repository fact")
	}
	if got, want := repositoryPayload["delta_generation"], true; got != want {
		t.Fatalf("delta_generation = %#v, want %#v", got, want)
	}
	if got, want := repositoryPayload["delta_relative_paths"], []string{"app.py", "old/deleted.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delta_relative_paths = %#v, want %#v", got, want)
	}
	if got, want := repositoryPayload["delta_deleted_relative_paths"], []string{"old/deleted.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delta_deleted_relative_paths = %#v, want %#v", got, want)
	}
	if !fileTombstoneSeen {
		t.Fatal("missing file tombstone for deleted path")
	}
	if !contentTombstoneSeen {
		t.Fatal("missing content tombstone for deleted path")
	}
}

func TestBuildStreamingGenerationPreservesDeltaPathWhitespace(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		Delta:                true,
		DeltaRelativePaths:   []string{"dir/ file.go", "dir/deleted .go"},
		DeletedRelativePaths: []string{"dir/deleted .go"},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-delta", time.Now().UTC(), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	var repositoryPayload map[string]any
	var fileTombstonePath string
	var contentTombstonePath string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case "repository":
			repositoryPayload = envelope.Payload
		case "file":
			if envelope.IsTombstone {
				fileTombstonePath, _ = envelope.Payload["relative_path"].(string)
			}
		case "content":
			if envelope.IsTombstone {
				contentTombstonePath, _ = envelope.Payload["content_path"].(string)
			}
		}
	}

	if got, want := repositoryPayload["delta_relative_paths"], []string{"dir/ file.go", "dir/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delta_relative_paths = %#v, want %#v", got, want)
	}
	if got, want := fileTombstonePath, "dir/deleted .go"; got != want {
		t.Fatalf("file tombstone relative_path = %q, want %q", got, want)
	}
	if got, want := contentTombstonePath, "dir/deleted .go"; got != want {
		t.Fatalf("content tombstone content_path = %q, want %q", got, want)
	}
}

func TestBuildStreamingGenerationDeltaChangedFileFactsMatchFullSnapshot(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	observedAt := time.Date(2026, time.June, 13, 6, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, time.June, 13, 5, 59, 0, 0, time.UTC)
	fullSnapshot := RepositorySnapshot{
		FileCount: 2,
		FileData: []map[string]any{
			{
				"lang": "go",
				"path": filepath.Join(repoPath, "changed.go"),
				"functions": []any{
					map[string]any{"name": "Run", "line_number": 1, "uid": "entity-changed"},
				},
			},
			{
				"lang": "go",
				"path": filepath.Join(repoPath, "unchanged.go"),
				"functions": []any{
					map[string]any{"name": "Idle", "line_number": 1, "uid": "entity-unchanged"},
				},
			},
		},
		ContentFiles: []ContentFileSnapshot{
			{RelativePath: "changed.go", Body: "package app\nfunc Run() {}\n", Digest: "digest-changed", Language: "go"},
			{RelativePath: "unchanged.go", Body: "package app\nfunc Idle() {}\n", Digest: "digest-unchanged", Language: "go"},
		},
		ContentEntities: []ContentEntitySnapshot{
			{
				EntityID:     "entity-changed",
				RelativePath: "changed.go",
				EntityType:   "Function",
				EntityName:   "Run",
				StartLine:    1,
				EndLine:      2,
				Language:     "go",
				SourceCache:  "package app\nfunc Run() {}\n",
				IndexedAt:    indexedAt,
			},
			{
				EntityID:     "entity-unchanged",
				RelativePath: "unchanged.go",
				EntityType:   "Function",
				EntityName:   "Idle",
				StartLine:    1,
				EndLine:      2,
				Language:     "go",
				SourceCache:  "package app\nfunc Idle() {}\n",
				IndexedAt:    indexedAt,
			},
		},
	}
	deltaSnapshot := RepositorySnapshot{
		FileCount:          1,
		Delta:              true,
		DeltaRelativePaths: []string{"changed.go"},
		FileData:           []map[string]any{cloneAnyMap(fullSnapshot.FileData[0])},
		ContentFiles:       []ContentFileSnapshot{fullSnapshot.ContentFiles[0]},
		ContentEntities:    []ContentEntitySnapshot{fullSnapshot.ContentEntities[0]},
	}

	fullFacts := drainFactChannel(buildStreamingGeneration(repoPath, repo, "run-full", observedAt, fullSnapshot, false, "").Facts)
	deltaFacts := drainFactChannel(buildStreamingGeneration(repoPath, repo, "run-delta", observedAt, deltaSnapshot, false, "").Facts)

	for _, kind := range []string{"file", "content", "content_entity"} {
		fullPayload, ok := factPayloadForRelativePath(fullFacts, kind, "changed.go")
		if !ok {
			t.Fatalf("full facts missing %s payload for changed.go", kind)
		}
		deltaPayload, ok := factPayloadForRelativePath(deltaFacts, kind, "changed.go")
		if !ok {
			t.Fatalf("delta facts missing %s payload for changed.go", kind)
		}
		if !reflect.DeepEqual(deltaPayload, fullPayload) {
			t.Fatalf("delta %s payload = %#v, want full payload %#v", kind, deltaPayload, fullPayload)
		}
		if _, ok := factPayloadForRelativePath(deltaFacts, kind, "unchanged.go"); ok {
			t.Fatalf("delta facts unexpectedly included %s payload for unchanged.go", kind)
		}
	}
}

// TestBuildStreamingGenerationDeltaEmitsWholeRepoResolveFollowups proves a
// delta generation emits the shared_followup marker for every reducer domain
// that re-resolves the whole-repo candidate set from current disk state
// (codex P1 finding on #5420/#5419: codeowners_ownership and submodule_pin
// carry dedicated delta-scope retract logic that is dead unless their marker
// fires on delta, same as shell_exec_materialization already did). Without
// this, a delta that removes CODEOWNERS/.gitmodules never re-projects and
// leaves stale edges in the graph.
func TestBuildStreamingGenerationDeltaEmitsWholeRepoResolveFollowups(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-changed")
	snapshot.Delta = true
	snapshot.DeltaRelativePaths = []string{"app.py"}

	collected := buildStreamingGeneration(repoPath, repo, "run-delta", time.Now().UTC(), snapshot, false, "")
	domainCounts := map[string]int{}
	for _, envelope := range drainFactChannel(collected.Facts) {
		if envelope.FactKind != "shared_followup" {
			continue
		}
		domain, _ := envelope.Payload["reducer_domain"].(string)
		domainCounts[domain]++
	}

	want := map[string]int{
		"shell_exec_materialization": 1,
		"codeowners_ownership":       1,
		"submodule_pin":              1,
	}
	if !reflect.DeepEqual(domainCounts, want) {
		t.Fatalf("delta generation shared_followup reducer_domain counts = %#v, want %#v", domainCounts, want)
	}
}

// TestBuildStreamingGenerationEmitsCodeownersOwnershipFollowup proves the git
// collector wires the codeowners_ownership shared_followup marker (issue
// #5419 Phase 3b) so the reducer domain the Phase 3 handler registered
// actually receives an intent. Without this marker the handler is dead code:
// no fact ever carries reducer_domain "codeowners_ownership".
func TestBuildStreamingGenerationEmitsCodeownersOwnershipFollowup(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-full")

	collected := buildStreamingGeneration(repoPath, repo, "run-full", time.Now().UTC(), snapshot, false, "")
	var marker facts.Envelope
	var found bool
	for _, envelope := range drainFactChannel(collected.Facts) {
		if envelope.FactKind != "shared_followup" {
			continue
		}
		if domain, _ := envelope.Payload["reducer_domain"].(string); domain == "codeowners_ownership" {
			marker = envelope
			found = true
		}
	}
	if !found {
		t.Fatal("full generation missing shared_followup marker for reducer_domain codeowners_ownership")
	}
	if got, want := marker.Payload["entity_key"], "codeowners:"+filepath.Base(repoPath); got != want {
		t.Fatalf("codeowners_ownership followup entity_key = %#v, want %#v", got, want)
	}
	if got, want := marker.StableFactKey, "shared_followup:"+repo.ID+":codeowners_ownership"; got != want {
		t.Fatalf("codeowners_ownership followup StableFactKey = %q, want %q", got, want)
	}
	if reason, _ := marker.Payload["reason"].(string); reason == "" {
		t.Fatal("codeowners_ownership followup reason is empty, want non-empty")
	}
}

// TestBuildStreamingGenerationEmitsSubmodulePinFollowup proves the git
// collector wires the submodule_pin shared_followup marker (issue #5420
// Phase 3) so the reducer domain the Phase 3 handler registered actually
// receives an intent. Without this marker the handler is dead code: no fact
// ever carries reducer_domain "submodule_pin". Mirrors
// TestBuildStreamingGenerationEmitsCodeownersOwnershipFollowup.
func TestBuildStreamingGenerationEmitsSubmodulePinFollowup(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-full")

	collected := buildStreamingGeneration(repoPath, repo, "run-full", time.Now().UTC(), snapshot, false, "")
	var marker facts.Envelope
	var found bool
	for _, envelope := range drainFactChannel(collected.Facts) {
		if envelope.FactKind != "shared_followup" {
			continue
		}
		if domain, _ := envelope.Payload["reducer_domain"].(string); domain == "submodule_pin" {
			marker = envelope
			found = true
		}
	}
	if !found {
		t.Fatal("full generation missing shared_followup marker for reducer_domain submodule_pin")
	}
	if got, want := marker.Payload["entity_key"], "submodule:"+filepath.Base(repoPath); got != want {
		t.Fatalf("submodule_pin followup entity_key = %#v, want %#v", got, want)
	}
	if got, want := marker.StableFactKey, "shared_followup:"+repo.ID+":submodule_pin"; got != want {
		t.Fatalf("submodule_pin followup StableFactKey = %q, want %q", got, want)
	}
	if reason, _ := marker.Payload["reason"].(string); reason == "" {
		t.Fatal("submodule_pin followup reason is empty, want non-empty")
	}
}

func factPayloadForRelativePath(envelopes []facts.Envelope, kind string, relativePath string) (map[string]any, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != kind || envelope.IsTombstone {
			continue
		}
		switch kind {
		case "file", "content_entity":
			if envelope.Payload["relative_path"] == relativePath {
				return envelope.Payload, true
			}
		case "content":
			if envelope.Payload["content_path"] == relativePath {
				return envelope.Payload, true
			}
		}
	}
	return nil, false
}
