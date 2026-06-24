// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeImportTestOwners returns a stable owner index for intent builder tests.
func codeImportTestOwners() codeImportOwnerIndex {
	return newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"npm", "express"}:                    "repo-express",
		{"pypi", "requests"}:                  "repo-requests",
		{"gomod", "github.com/gin-gonic/gin"}: "repo-gin",
	})
}

// makeCodeImportFileEnvelope builds a minimal file-kind envelope with the given
// import sources under parsed_file_data.imports[].
func makeCodeImportFileEnvelope(repoID, relPath, language string, importSources []string) facts.Envelope {
	imports := make([]map[string]any, 0, len(importSources))
	for _, src := range importSources {
		imports = append(imports, map[string]any{"source": src})
	}
	return facts.Envelope{
		FactKind: factKindFile,
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relPath,
			"language":      language,
			"parsed_file_data": map[string]any{
				"imports": imports,
			},
		},
	}
}

// TestBuildCodeImportRepoDependencyIntentsPositive verifies that file facts
// with external imports whose package coordinates resolve to a known owner emit
// one upsert intent per distinct (consumer, owner) pair.
func TestBuildCodeImportRepoDependencyIntentsPositive(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/app.js", "javascript", []string{"express", "./local"}),
		makeCodeImportFileEnvelope("consumer-repo-a", "src/other.js", "javascript", []string{"express"}), // dedup
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-pkg-1",
		GenerationID:  "gen-1",
		SourceRunID:   "code_import_repo_dependency:scope-pkg-1",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)

	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1", len(intents))
	}
	intent := intents[0]
	if got := intent.Payload["evidence_source"]; got != codeImportEvidenceSource {
		t.Errorf("evidence_source = %v, want %q", got, codeImportEvidenceSource)
	}
	if got := intent.Payload["repo_id"]; got != "consumer-repo-a" {
		t.Errorf("repo_id = %v, want %q", got, "consumer-repo-a")
	}
	if got := intent.Payload["target_repo_id"]; got != "repo-express" {
		t.Errorf("target_repo_id = %v, want %q", got, "repo-express")
	}
}

// TestBuildCodeImportRepoDependencyIntentsSelfEdgeSkipped verifies that a
// consumer repository that imports a package it owns does not produce a
// self-referential DEPENDS_ON edge.
func TestBuildCodeImportRepoDependencyIntentsSelfEdgeSkipped(t *testing.T) {
	t.Parallel()

	// consumer-repo-a happens to own express in this test universe.
	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"npm", "express"}: "consumer-repo-a",
	})
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/index.js", "javascript", []string{"express"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-pkg-2",
		GenerationID:  "gen-2",
		SourceRunID:   "code_import_repo_dependency:scope-pkg-2",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 0 {
		t.Fatalf("len(intents) = %d, want 0 (self-edge skipped)", len(intents))
	}
}

// TestBuildCodeImportRepoDependencyIntentsRelativeSkipped verifies that
// relative import sources (intra-repo paths) produce no edges.
func TestBuildCodeImportRepoDependencyIntentsRelativeSkipped(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/app.py", "python", []string{"./local_mod", "../sibling"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-pkg-3",
		GenerationID:  "gen-3",
		SourceRunID:   "code_import_repo_dependency:scope-pkg-3",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 0 {
		t.Fatalf("len(intents) = %d, want 0 (relative imports skipped)", len(intents))
	}
}

// TestBuildCodeImportRepoDependencyIntentsUnresolvedSkipped verifies that
// import sources with no matching package owner produce no edges.
func TestBuildCodeImportRepoDependencyIntentsUnresolvedSkipped(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/app.js", "javascript", []string{"unknown-pkg-xyz"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-pkg-4",
		GenerationID:  "gen-4",
		SourceRunID:   "code_import_repo_dependency:scope-pkg-4",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 0 {
		t.Fatalf("len(intents) = %d, want 0 (unresolved import skipped)", len(intents))
	}
}

// TestBuildCodeImportRepoDependencyIntentsDedup verifies that multiple files
// importing the same external package collapse to a single (consumer, owner)
// edge intent.
func TestBuildCodeImportRepoDependencyIntentsDedup(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/a.js", "javascript", []string{"express"}),
		makeCodeImportFileEnvelope("consumer-repo-a", "src/b.js", "javascript", []string{"express"}),
		makeCodeImportFileEnvelope("consumer-repo-a", "src/c.ts", "typescript", []string{"express"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-pkg-5",
		GenerationID:  "gen-5",
		SourceRunID:   "code_import_repo_dependency:scope-pkg-5",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1 (dedup)", len(intents))
	}
}

// TestBuildCodeImportRepoDependencyIntentsMultiConsumer verifies that two
// different consumer repos produce independent intents.
func TestBuildCodeImportRepoDependencyIntentsMultiConsumer(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/app.js", "javascript", []string{"express"}),
		makeCodeImportFileEnvelope("consumer-repo-b", "main.py", "python", []string{"requests"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-pkg-6",
		GenerationID:  "gen-6",
		SourceRunID:   "code_import_repo_dependency:scope-pkg-6",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 2 {
		t.Fatalf("len(intents) = %d, want 2", len(intents))
	}

	// Verify both (consumer, owner) pairs appear.
	consumerOwnerPairs := make(map[string]string, 2)
	for _, intent := range intents {
		repoID, _ := intent.Payload["repo_id"].(string)
		targetRepoID, _ := intent.Payload["target_repo_id"].(string)
		consumerOwnerPairs[repoID] = targetRepoID
	}
	if got := consumerOwnerPairs["consumer-repo-a"]; got != "repo-express" {
		t.Errorf("consumer-repo-a → %q, want repo-express", got)
	}
	if got := consumerOwnerPairs["consumer-repo-b"]; got != "repo-requests" {
		t.Errorf("consumer-repo-b → %q, want repo-requests", got)
	}
}

// TestBuildCodeImportRepoDependencyIntentsGoSubpathResolvesToModule verifies
// that a Go import subpath (github.com/foo/bar/internal/thing) matches the
// registered module coordinate github.com/foo/bar.
func TestBuildCodeImportRepoDependencyIntentsGoSubpathResolvesToModule(t *testing.T) {
	t.Parallel()

	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"gomod", "github.com/gin-gonic/gin"}: "repo-gin",
	})
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-go", "main.go", "go", []string{
			"github.com/gin-gonic/gin/render", // subpath
		}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-go-1",
		GenerationID:  "gen-go-1",
		SourceRunID:   "code_import_repo_dependency:scope-go-1",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1 (go subpath resolves to module)", len(intents))
	}
	if got := intents[0].Payload["target_repo_id"]; got != "repo-gin" {
		t.Errorf("target_repo_id = %v, want repo-gin", got)
	}
}

// TestBuildCodeImportRepoDependencyIntentsGoVersionSuffixStripped verifies
// that a Go module import with a major version suffix (/v2, /v3) resolves
// to the base module coordinate.
func TestBuildCodeImportRepoDependencyIntentsGoVersionSuffixStripped(t *testing.T) {
	t.Parallel()

	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"gomod", "github.com/gin-gonic/gin"}: "repo-gin",
	})
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-go", "main.go", "go", []string{
			"github.com/gin-gonic/gin/v2",
		}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-go-2",
		GenerationID:  "gen-go-2",
		SourceRunID:   "code_import_repo_dependency:scope-go-2",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1 (version suffix stripped)", len(intents))
	}
	if got := intents[0].Payload["target_repo_id"]; got != "repo-gin" {
		t.Errorf("target_repo_id = %v, want repo-gin", got)
	}
}

// TestBuildCodeImportRepoDependencyIntentsIdempotent verifies that running the
// builder twice over identical input yields intents with identical IDs, so the
// downstream DEPENDS_ON MERGE stays idempotent under retries.
func TestBuildCodeImportRepoDependencyIntentsIdempotent(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/app.js", "javascript", []string{"express"}),
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-idem-1",
		GenerationID:  "gen-idem-1",
		SourceRunID:   "code_import_repo_dependency:scope-idem-1",
		CreatedAt:     now,
		FileEnvelopes: envelopes,
		Owners:        owners,
	}

	first := BuildCodeImportRepoDependencyIntents(input)
	second := BuildCodeImportRepoDependencyIntents(input)

	if len(first) != len(second) {
		t.Fatalf("len(first) = %d, len(second) = %d, want equal", len(first), len(second))
	}
	for i := range first {
		if first[i].IntentID != second[i].IntentID {
			t.Errorf("IntentID[%d]: first=%q second=%q, want equal", i, first[i].IntentID, second[i].IntentID)
		}
	}
}

// TestBuildCodeImportRepoDependencyIntentsEmptyNoOwners verifies that an empty
// owners map produces no intents even when file envelopes have external imports.
func TestBuildCodeImportRepoDependencyIntentsEmptyNoOwners(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo-a", "src/app.js", "javascript", []string{"express"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-no-owner",
		GenerationID:  "gen-no-owner",
		SourceRunID:   "code_import_repo_dependency:scope-no-owner",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        codeImportOwnerIndex{}, // no owners
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 0 {
		t.Fatalf("len(intents) = %d, want 0 (no owners)", len(intents))
	}
}

// TestBuildCodeImportRepoDependencyIntentsStdlibDropped verifies that Go
// stdlib imports (no dot in host segment) produce no edges.
func TestBuildCodeImportRepoDependencyIntentsStdlibDropped(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners()
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-go", "main.go", "go", []string{"fmt", "net/http", "encoding/json"}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-stdlib",
		GenerationID:  "gen-stdlib",
		SourceRunID:   "code_import_repo_dependency:scope-stdlib",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 0 {
		t.Fatalf("len(intents) = %d, want 0 (stdlib dropped)", len(intents))
	}
}

// TestBuildCodeImportRepoDependencyIntentsDeterministicOrder verifies that the
// returned intent slice is ordered deterministically, which is required for
// stable acceptance-row assignment in the shared projection lane.
func TestBuildCodeImportRepoDependencyIntentsDeterministicOrder(t *testing.T) {
	t.Parallel()

	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"npm", "express"}:   "repo-b",
		{"npm", "react"}:     "repo-a",
		{"pypi", "requests"}: "repo-c",
	})
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer", "c.py", "python", []string{"requests"}),
		makeCodeImportFileEnvelope("consumer", "b.ts", "typescript", []string{"react"}),
		makeCodeImportFileEnvelope("consumer", "a.js", "javascript", []string{"express"}),
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-order",
		GenerationID:  "gen-order",
		SourceRunID:   "code_import_repo_dependency:scope-order",
		CreatedAt:     now,
		FileEnvelopes: envelopes,
		Owners:        owners,
	}

	first := BuildCodeImportRepoDependencyIntents(input)
	second := BuildCodeImportRepoDependencyIntents(input)

	if len(first) != 3 {
		t.Fatalf("len(intents) = %d, want 3", len(first))
	}
	ids1 := make([]string, len(first))
	ids2 := make([]string, len(second))
	partitions1 := make([]string, len(first))
	for i := range first {
		ids1[i] = first[i].IntentID
		ids2[i] = second[i].IntentID
		partitions1[i] = first[i].PartitionKey
	}
	// Ordering is deterministic and stable across runs: rows are sorted by the
	// consumer->owner partition key, so the same input always yields the same
	// intent-id sequence and the downstream MERGE is replay-safe.
	if !sort.StringsAreSorted(partitions1) {
		t.Errorf("first run partition keys not sorted: %v", partitions1)
	}
	if !codeImportSlicesEqual(ids1, ids2) {
		t.Errorf("first=%v second=%v, want equal", ids1, ids2)
	}
}

func codeImportSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
