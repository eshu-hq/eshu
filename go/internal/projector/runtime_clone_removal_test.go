// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// mixedProjectorFixture builds one inputFacts slice that exercises every
// per-fact consumer buildProjection's loop calls: a repository fact (content
// materialization gate + buildRepositoryRefs via git_refs), a content-record
// fact (buildContentRecord), a content-entity fact that is also a semantic
// entity (buildContentEntityRecord + buildSemanticEntityReducerIntent), a
// generic reducer-signal fact (buildReducerIntent), and a malformed
// codegraph_repository fact that buildCanonicalMaterialization quarantines
// (missing its required repo_id). It backs both the #4854 mutation-safety
// regression test and the clone-vs-borrow equivalence test so both prove the
// same representative shape.
func mixedProjectorFixture(scopeValue scope.IngestionScope, generation scope.ScopeGeneration) []facts.Envelope {
	repoID := scopeValue.Metadata["repo_id"]
	now := generation.ObservedAt

	return []facts.Envelope{
		{
			FactID:        "fact-repository",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "repository",
			SchemaVersion: "1.0.0",
			ObservedAt:    now,
			Payload: map[string]any{
				"repo_id":    repoID,
				"name":       "clone-removal-test-repo",
				"local_path": "/tmp/repos/clone-removal-test-repo",
				"remote_url": "https://github.com/org/clone-removal-test-repo.git",
				"repo_slug":  "org/clone-removal-test-repo",
				"has_remote": true,
				"git_refs": []any{
					map[string]any{"name": "main", "kind": "branch", "head_sha": "deadbeef00", "is_default": true},
					map[string]any{"name": "feature-x", "kind": "branch", "head_sha": "cafef00d00", "is_default": false},
				},
			},
		},
		{
			FactID:       "fact-content-record",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			FactKind:     "file",
			ObservedAt:   now,
			Payload: map[string]any{
				"content_path": "src/README.md",
				"content_body": "# Title\n\nBody text.",
				"language":     "markdown",
			},
		},
		{
			FactID:       "fact-content-entity-semantic",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			FactKind:     "content_entity",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"entity_id":     "entity-annotation-1",
				"entity_type":   "Annotation",
				"entity_name":   "Deprecated",
				"relative_path": "src/api/handler.go",
				"start_line":    float64(3),
				"end_line":      float64(3),
				"language":      "go",
			},
		},
		{
			FactID:       "fact-reducer-intent",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			FactKind:     "custom_reducer_signal",
			ObservedAt:   now,
			Payload: map[string]any{
				"reducer_domain": "workload_identity",
				"entity_key":     "repo:" + repoID,
				"reason":         "test reducer intent",
			},
		},
		{
			FactID:        "fact-quarantined-repository",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      factschema.FactKindCodegraphRepository,
			SchemaVersion: "1.0.0",
			ObservedAt:    now,
			Payload: map[string]any{
				// "repo_id" intentionally absent so buildCanonicalMaterialization
				// quarantines this fact as input_invalid.
				"name": "unattributed",
			},
		},
	}
}

// deepCopyPayloadValue recursively copies a fact payload value so a snapshot
// taken before buildProjection runs cannot alias any map or slice the loop
// might (incorrectly) write through. Scalars (string, bool, numeric, nil) are
// already immutable value copies once read out of the map, so they pass
// through unchanged.
func deepCopyPayloadValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, inner := range typed {
			out[key] = deepCopyPayloadValue(inner)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, inner := range typed {
			out[i] = deepCopyPayloadValue(inner)
		}
		return out
	default:
		return value
	}
}

func deepCopyPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	return deepCopyPayloadValue(payload).(map[string]any)
}

// TestBuildProjectionDoesNotMutateInputFactPayloads is the #4854
// mutation-safety regression test: buildProjection's per-fact loop now
// borrows inputFacts[i] instead of deep-cloning it (runtime.go), so every
// consumer in that loop (validateFactBoundary, validateFactSchemaVersion,
// buildContentRecord, buildContentEntityRecord, buildRepositoryRefs,
// buildSemanticEntityReducerIntent, buildReducerIntent) now shares the same
// Payload map as the caller's inputFacts slice. This snapshots every input
// fact's Payload before the call and asserts it is byte-identical after,
// proving none of those consumers writes through the shared map.
func TestBuildProjectionDoesNotMutateInputFactPayloads(t *testing.T) {
	t.Parallel()

	scopeValue, generation := makeTestScope("scope-clone-removal", "repo-clone-removal", "org/clone-removal-repo")
	inputFacts := mixedProjectorFixture(scopeValue, generation)

	before := make([]map[string]any, len(inputFacts))
	for i, fact := range inputFacts {
		before[i] = deepCopyPayload(fact.Payload)
	}

	if _, err := buildProjection(scopeValue, generation, inputFacts); err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	for i, fact := range inputFacts {
		if !reflect.DeepEqual(fact.Payload, before[i]) {
			t.Errorf("input fact %q Payload mutated by buildProjection:\nbefore = %#v\nafter  = %#v", fact.FactID, before[i], fact.Payload)
		}
	}
}

// buildProjectionClonePathForEquivalenceTest replicates buildProjection's
// pre-#4854 behavior verbatim: it deep-clones each fact envelope
// (inputFacts[i].Clone()) before the per-fact loop consumes it, exactly as
// runtime.go did before the borrow change. It must never diverge from
// buildProjection in runtime.go except on that one line — it exists solely so
// TestBuildProjectionBorrowMatchesClonePathEquivalence can prove the borrow
// path's output is byte-identical to the old clone path's output on the same
// fixture.
func buildProjectionClonePathForEquivalenceTest(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, inputFacts []facts.Envelope) (projection, error) {
	repoID := scopeRepoID(scopeValue)
	contentMaterialization := content.Materialization{
		RepoID:       repoID,
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		SourceSystem: scopeValue.SourceSystem,
	}
	materializeContent := repoID != ""

	intents := make([]ReducerIntent, 0, len(inputFacts))
	for i := range inputFacts {
		fact := inputFacts[i].Clone()
		if err := validateFactBoundary(scopeValue, generation, fact); err != nil {
			return projection{}, err
		}
		if err := validateFactSchemaVersion(fact); err != nil {
			return projection{}, err
		}

		if materializeContent {
			if record, ok := buildContentRecord(fact); ok {
				contentMaterialization.Records = append(contentMaterialization.Records, record)
			}
			if entity, ok := buildContentEntityRecord(repoID, fact); ok {
				contentMaterialization.Entities = append(contentMaterialization.Entities, entity)
			}
			if refs := buildRepositoryRefs(fact); len(refs) > 0 {
				contentMaterialization.RepositoryRefs = append(contentMaterialization.RepositoryRefs, refs...)
			}
		}
		if intent, ok := buildSemanticEntityReducerIntent(fact); ok {
			intents = append(intents, intent)
		}
		if intent, ok := buildReducerIntent(fact); ok {
			intents = append(intents, intent)
		}
	}
	intents = appendScopeGenerationReducerIntents(intents, scopeValue, generation, inputFacts)

	sort.SliceStable(intents, func(i, j int) bool {
		left := intents[i]
		right := intents[j]
		if left.Domain != right.Domain {
			return left.Domain < right.Domain
		}
		if left.EntityKey != right.EntityKey {
			return left.EntityKey < right.EntityKey
		}
		return left.FactID < right.FactID
	})

	canonical, quarantined := buildCanonicalMaterialization(scopeValue, generation, inputFacts)

	return projection{
		canonical:              canonical,
		contentMaterialization: contentMaterialization,
		reducerIntents:         intents,
		quarantinedFacts:       quarantined,
	}, nil
}

// TestBuildProjectionBorrowMatchesClonePathEquivalence is the #4854
// full-projection equivalence test: it proves buildProjection's borrow-based
// production path (runtime.go) returns a byte-identical projection to the old
// clone-based path (buildProjectionClonePathForEquivalenceTest above) on the
// same mixed fixture, so dropping the per-fact Clone() call did not change
// canonical, content, intent, or quarantine output.
func TestBuildProjectionBorrowMatchesClonePathEquivalence(t *testing.T) {
	t.Parallel()

	scopeValue, generation := makeTestScope("scope-clone-removal-equiv", "repo-clone-removal-equiv", "org/clone-removal-equiv-repo")
	inputFacts := mixedProjectorFixture(scopeValue, generation)

	borrowed, err := buildProjection(scopeValue, generation, inputFacts)
	if err != nil {
		t.Fatalf("buildProjection() (borrow path) error = %v, want nil", err)
	}

	cloned, err := buildProjectionClonePathForEquivalenceTest(scopeValue, generation, inputFacts)
	if err != nil {
		t.Fatalf("buildProjectionClonePathForEquivalenceTest() (clone path) error = %v, want nil", err)
	}

	if !reflect.DeepEqual(borrowed, cloned) {
		t.Fatalf("borrow-path projection does not match clone-path projection:\nborrowed = %#v\ncloned   = %#v", borrowed, cloned)
	}
}

// buildLargeMixedProjectorFixture builds n facts.Envelope entries alternating
// between a content-record shape and a content-entity (semantic) shape — the
// two heaviest per-fact loop consumers in buildProjection — each carrying a
// nested map and slice in its Payload so a deep clone (facts.Envelope.Clone,
// which recursively copies nested maps/slices via cloneMap) has real work to
// do. It appends one repository fact (git_refs, for buildRepositoryRefs) and
// one malformed codegraph_repository fact (for the quarantine path) so the
// #4854 performance proof benchmark exercises the same consumer set as
// TestBuildProjectionBorrowMatchesClonePathEquivalence, just at scale.
func buildLargeMixedProjectorFixture(n int) (scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) {
	scopeValue, generation := makeTestScope("scope-clone-removal-bench", "repo-clone-removal-bench", "org/clone-removal-bench-repo")
	repoID := scopeValue.Metadata["repo_id"]
	now := generation.ObservedAt

	inputFacts := make([]facts.Envelope, 0, n)
	for i := 0; i < n-2; i++ {
		if i%2 == 0 {
			inputFacts = append(inputFacts, facts.Envelope{
				FactID:       fmt.Sprintf("fact-content-record-%d", i),
				ScopeID:      scopeValue.ScopeID,
				GenerationID: generation.GenerationID,
				FactKind:     "file",
				ObservedAt:   now,
				Payload: map[string]any{
					"content_path":   fmt.Sprintf("src/pkg%d/file%d.go", i%50, i),
					"content_body":   fmt.Sprintf("package pkg%d\n\nfunc Handler%d() {}\n", i%50, i),
					"content_digest": fmt.Sprintf("sha256:%040d", i),
					"language":       "go",
					"extra_meta": map[string]any{
						"size_bytes": float64(1024 + i),
						"tags":       []any{"generated", "benchmark"},
					},
				},
			})
			continue
		}
		inputFacts = append(inputFacts, facts.Envelope{
			FactID:       fmt.Sprintf("fact-content-entity-%d", i),
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			FactKind:     "content_entity",
			ObservedAt:   now,
			Payload: map[string]any{
				"repo_id":       repoID,
				"entity_id":     fmt.Sprintf("entity-%d", i),
				"entity_type":   "Annotation",
				"entity_name":   fmt.Sprintf("Entity%d", i),
				"relative_path": fmt.Sprintf("src/pkg%d/file%d.go", i%50, i),
				"start_line":    float64(i % 500),
				"end_line":      float64(i%500 + 10),
				"language":      "go",
				"entity_metadata": map[string]any{
					"docstring":  "generated benchmark entity",
					"decorators": []any{"a", "b"},
				},
			},
		})
	}

	inputFacts = append(inputFacts, facts.Envelope{
		FactID:        "fact-repository-bench",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      "repository",
		SchemaVersion: "1.0.0",
		ObservedAt:    now,
		Payload: map[string]any{
			"repo_id":    repoID,
			"name":       "clone-removal-bench-repo",
			"local_path": "/tmp/repos/clone-removal-bench-repo",
			"has_remote": true,
			"git_refs": []any{
				map[string]any{"name": "main", "kind": "branch", "head_sha": "deadbeef00", "is_default": true},
			},
		},
	})
	inputFacts = append(inputFacts, facts.Envelope{
		FactID:        "fact-quarantined-repository-bench",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      factschema.FactKindCodegraphRepository,
		SchemaVersion: "1.0.0",
		ObservedAt:    now,
		Payload: map[string]any{
			// "repo_id" intentionally absent so buildCanonicalMaterialization
			// quarantines this fact as input_invalid.
			"name": "unattributed-bench",
		},
	})

	return scopeValue, generation, inputFacts
}

// BenchmarkProjectionCloneRemovalProof is the #4854 Prove-The-Theory-First
// performance proof: it times buildProjection's per-fact loop over a
// 5,000-fact mixed fixture both in its old shape (deep-cloning every fact via
// buildProjectionClonePathForEquivalenceTest) and its new borrow shape
// (buildProjection in runtime.go), so `go test -bench
// BenchmarkProjectionCloneRemovalProof -benchmem` reports ns/op, B/op, and
// allocs/op for both directly comparable.
func BenchmarkProjectionCloneRemovalProof(b *testing.B) {
	const factCount = 5000
	scopeValue, generation, inputFacts := buildLargeMixedProjectorFixture(factCount)

	b.Run("Clone", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := buildProjectionClonePathForEquivalenceTest(scopeValue, generation, inputFacts); err != nil {
				b.Fatalf("buildProjectionClonePathForEquivalenceTest() error = %v", err)
			}
		}
	})

	b.Run("Borrow", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := buildProjection(scopeValue, generation, inputFacts); err != nil {
				b.Fatalf("buildProjection() error = %v", err)
			}
		}
	})
}
