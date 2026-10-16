// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"sort"
	"testing"
	"time"
)

func deltaUnionTestEnvelope(kind, key, factID string, payload map[string]any) Envelope {
	return Envelope{
		FactID:        factID,
		ScopeID:       "scope-1",
		GenerationID:  "gen-delta-1",
		FactKind:      kind,
		StableFactKey: key,
		ObservedAt:    time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		Payload:       payload,
	}
}

func deltaUnionBaseEnvelope(kind, key, factID string, payload map[string]any) Envelope {
	env := deltaUnionTestEnvelope(kind, key, factID, payload)
	env.GenerationID = "gen-base-1"
	env.ObservedAt = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return env
}

func deltaUnionRepoPayload(delta bool, changed, deleted []string) map[string]any {
	payload := map[string]any{
		"repo_id":    "repo-1",
		"local_path": "/repos/alpha",
	}
	if delta {
		payload["delta_generation"] = true
	}
	if changed != nil {
		items := make([]any, 0, len(changed))
		for _, c := range changed {
			items = append(items, c)
		}
		payload["delta_relative_paths"] = items
	}
	if deleted != nil {
		items := make([]any, 0, len(deleted))
		for _, d := range deleted {
			items = append(items, d)
		}
		payload["delta_deleted_relative_paths"] = items
	}
	return payload
}

func deltaUnionFilePayload(rel string) map[string]any {
	return map[string]any{"relative_path": rel}
}

func deltaUnionEntityPayload(rel, name string) map[string]any {
	return map[string]any{
		"relative_path": rel,
		"entity_type":   "Function",
		"entity_name":   name,
	}
}

func deltaUnionKeys(envelopes []Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, env := range envelopes {
		keys = append(keys, env.FactKind+":"+env.StableFactKey)
	}
	sort.Strings(keys)
	return keys
}

func deltaUnionEqualKeys(t *testing.T, got []Envelope, want []string) {
	t.Helper()
	gotKeys := deltaUnionKeys(got)
	sort.Strings(want)
	if len(gotKeys) != len(want) {
		t.Fatalf("union keys = %q, want %q", gotKeys, want)
	}
	for i := range gotKeys {
		if gotKeys[i] != want[i] {
			t.Fatalf("union keys = %q, want %q", gotKeys, want)
		}
	}
}

// Full snapshot A {keep.go, w.go} then delta B {keep.go modified, w.go
// deleted, x.go added}: projecting the delta onto an empty graph with the
// union must yield the full tree B.
func TestOverlayDeltaBaseFullSnapshotThenDelta(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("file", "file:repo-1:keep.go", "base-keep", deltaUnionFilePayload("keep.go")),
		deltaUnionBaseEnvelope("file", "file:repo-1:w.go", "base-w", deltaUnionFilePayload("w.go")),
		deltaUnionBaseEnvelope("content_entity", "content_entity:keep-fn", "base-keep-fn", deltaUnionEntityPayload("keep.go", "keep")),
		deltaUnionBaseEnvelope("content_entity", "content_entity:w-fn", "base-w-fn", deltaUnionEntityPayload("w.go", "w")),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, []string{"keep.go", "x.go"}, []string{"w.go"})),
		deltaUnionTestEnvelope("file", "file:repo-1:keep.go", "delta-keep", deltaUnionFilePayload("keep.go")),
		deltaUnionTestEnvelope("file", "file:repo-1:x.go", "delta-x", deltaUnionFilePayload("x.go")),
		deltaUnionTestEnvelope("content_entity", "content_entity:keep-fn2", "delta-keep-fn", deltaUnionEntityPayload("keep.go", "keep2")),
		deltaUnionTestEnvelope("content_entity", "content_entity:x-fn", "delta-x-fn", deltaUnionEntityPayload("x.go", "x")),
	}
	scope := ParseDeltaScope(delta)
	if !scope.IsDelta {
		t.Fatal("ParseDeltaScope IsDelta = false, want true")
	}
	got := OverlayDeltaBase(base, delta, scope)
	deltaUnionEqualKeys(t, got, []string{
		"content_entity:content_entity:keep-fn2",
		"content_entity:content_entity:x-fn",
		"file:file:repo-1:keep.go",
		"file:file:repo-1:x.go",
		"repository:repository:repo-1",
	})
	for _, env := range got {
		if env.FactKind == "repository" && env.FactID != "delta-repo" {
			t.Fatalf("repository fact = %q, want delta repo fact", env.FactID)
		}
	}
}

// A delta that modifies one file keeps every untouched base file and entity.
func TestOverlayDeltaBaseModifyKeepsUntouchedBase(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("file", "file:repo-1:a.go", "base-a", deltaUnionFilePayload("a.go")),
		deltaUnionBaseEnvelope("file", "file:repo-1:b.go", "base-b", deltaUnionFilePayload("b.go")),
		deltaUnionBaseEnvelope("content_entity", "content_entity:a-fn", "base-a-fn", deltaUnionEntityPayload("a.go", "a")),
		deltaUnionBaseEnvelope("content_entity", "content_entity:b-fn", "base-b-fn", deltaUnionEntityPayload("b.go", "b")),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, []string{"a.go"}, nil)),
		deltaUnionTestEnvelope("file", "file:repo-1:a.go", "delta-a", deltaUnionFilePayload("a.go")),
		deltaUnionTestEnvelope("content_entity", "content_entity:a-fn-new", "delta-a-fn", deltaUnionEntityPayload("a.go", "aNew")),
	}
	got := OverlayDeltaBase(base, delta, ParseDeltaScope(delta))
	deltaUnionEqualKeys(t, got, []string{
		"content_entity:content_entity:a-fn-new",
		"content_entity:content_entity:b-fn",
		"file:file:repo-1:a.go",
		"file:file:repo-1:b.go",
		"repository:repository:repo-1",
	})
}

// A delta that deletes one file removes only that file's base rows.
func TestOverlayDeltaBaseDeleteRemovesOnlyThatFile(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("file", "file:repo-1:keep.go", "base-keep", deltaUnionFilePayload("keep.go")),
		deltaUnionBaseEnvelope("file", "file:repo-1:gone.go", "base-gone", deltaUnionFilePayload("gone.go")),
		deltaUnionBaseEnvelope("content_entity", "content_entity:keep-fn", "base-keep-fn", deltaUnionEntityPayload("keep.go", "keep")),
		deltaUnionBaseEnvelope("content_entity", "content_entity:gone-fn", "base-gone-fn", deltaUnionEntityPayload("gone.go", "gone")),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, nil, []string{"gone.go"})),
	}
	got := OverlayDeltaBase(base, delta, ParseDeltaScope(delta))
	deltaUnionEqualKeys(t, got, []string{
		"content_entity:content_entity:keep-fn",
		"file:file:repo-1:keep.go",
		"repository:repository:repo-1",
	})
}

// An empty delta (no changed or deleted paths) leaves the base set intact.
func TestOverlayDeltaBaseEmptyDeltaLeavesBaseUnchanged(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("file", "file:repo-1:keep.go", "base-keep", deltaUnionFilePayload("keep.go")),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, nil, nil)),
	}
	got := OverlayDeltaBase(base, delta, ParseDeltaScope(delta))
	deltaUnionEqualKeys(t, got, []string{
		"file:file:repo-1:keep.go",
		"repository:repository:repo-1",
	})
}

// Out-of-repo paths in the delta lists never remove base rows.
func TestOverlayDeltaBaseIgnoresOutOfRepoPaths(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("file", "file:repo-1:keep.go", "base-keep", deltaUnionFilePayload("keep.go")),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo",
			deltaUnionRepoPayload(true, []string{"../evil.go", "/abs.go", ""}, []string{"../../gone.go", "/etc/passwd"})),
	}
	got := OverlayDeltaBase(base, delta, ParseDeltaScope(delta))
	deltaUnionEqualKeys(t, got, []string{
		"file:file:repo-1:keep.go",
		"repository:repository:repo-1",
	})
}

// A delta tombstone removes the same-key base row even when no path list
// names it.
func TestOverlayDeltaBaseTombstoneRemovesSameKeyBase(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("codeowners.ownership", "codeowners:repo-1", "base-co", map[string]any{"rule": "old"}),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, nil, nil)),
		deltaUnionTestEnvelope("codeowners.ownership", "codeowners:repo-1", "delta-co", map[string]any{"rule": "new"}),
	}
	// Non-tombstone same-key replace.
	got := OverlayDeltaBase(base, delta, ParseDeltaScope(delta))
	deltaUnionEqualKeys(t, got, []string{
		"codeowners.ownership:codeowners:repo-1",
		"repository:repository:repo-1",
	})
	for _, env := range got {
		if env.FactKind == "codeowners.ownership" && env.FactID != "delta-co" {
			t.Fatalf("codeowners fact = %q, want delta fact", env.FactID)
		}
	}

	tombstoned := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, nil, nil)),
		deltaUnionTestEnvelope("codeowners.ownership", "codeowners:repo-1", "delta-co-tomb", nil),
	}
	tombstoned[1].IsTombstone = true
	got = OverlayDeltaBase(base, tombstoned, ParseDeltaScope(tombstoned))
	// The tombstone itself is kept (downstream stages take the deletion
	// path on IsTombstone); the base row it names is gone.
	deltaUnionEqualKeys(t, got, []string{
		"codeowners.ownership:codeowners:repo-1",
		"repository:repository:repo-1",
	})
	for _, env := range got {
		if env.FactKind == "codeowners.ownership" && !env.IsTombstone {
			t.Fatalf("codeowners fact IsTombstone = false, want tombstone kept")
		}
	}
}

// Content facts carry content_path instead of relative_path and are removed
// with their deleted file.
func TestOverlayDeltaBaseContentPathKind(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
		deltaUnionBaseEnvelope("content", "content:repo-1:gone.go", "base-content", map[string]any{"content_path": "gone.go"}),
		deltaUnionBaseEnvelope("content", "content:repo-1:keep.go", "base-content-keep", map[string]any{"content_path": "keep.go"}),
	}
	delta := []Envelope{
		deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo", deltaUnionRepoPayload(true, nil, []string{"gone.go"})),
	}
	got := OverlayDeltaBase(base, delta, ParseDeltaScope(delta))
	deltaUnionEqualKeys(t, got, []string{
		"content:content:repo-1:keep.go",
		"repository:repository:repo-1",
	})
}

// A non-delta scope overlays nothing: every row survives.
func TestOverlayDeltaBaseNonDeltaScopeKeepsAll(t *testing.T) {
	base := []Envelope{
		deltaUnionBaseEnvelope("file", "file:repo-1:a.go", "base-a", deltaUnionFilePayload("a.go")),
	}
	other := []Envelope{
		deltaUnionBaseEnvelope("file", "file:repo-1:a.go", "other-a", deltaUnionFilePayload("a.go")),
	}
	got := OverlayDeltaBase(base, other, ParseDeltaScope(other))
	if len(got) != 2 {
		t.Fatalf("union len = %d, want 2", len(got))
	}
}
