// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestWriteContentProjectionEnqueuesDriftedIntent proves the #6837 trigger:
// a content Write that (re)published fingerprint side-table truth enqueues
// exactly one code_drifted intent for the materialization's
// (scope, generation) carrying the repo_id payload. A fingerprint-free
// Write enqueues nothing.
func TestWriteContentProjectionEnqueuesDriftedIntent(t *testing.T) {
	t.Parallel()

	mat := content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "repo:repo-1",
		GenerationID: "gen-9",
		SourceSystem: "collector/git",
		Records:      []content.Record{{Path: "a.go", Body: "package p\n", Digest: "d1"}},
	}
	scopeValue := scope.IngestionScope{ScopeID: "repo:repo-1"}

	changed := &recordingContentWriter{result: content.Result{FingerprintsChanged: true}}
	intents := &recordingIntentWriter{result: IntentResult{Count: 1}}
	runtime := Runtime{ContentWriter: changed, IntentWriter: intents}
	if _, err := runtime.writeContentProjection(context.Background(), scopeValue, mat); err != nil {
		t.Fatalf("writeContentProjection() error = %v, want nil", err)
	}
	if got := len(intents.calls); got != 1 {
		t.Fatalf("intent writer call count = %d, want 1", got)
	}
	if got := len(intents.calls[0]); got != 1 {
		t.Fatalf("enqueued intent count = %d, want exactly one drifted intent", got)
	}
	got := intents.calls[0][0]
	if got.Domain != reducer.DomainCodeDrifted {
		t.Fatalf("intent.Domain = %q, want %q", got.Domain, reducer.DomainCodeDrifted)
	}
	if got.ScopeID != "repo:repo-1" || got.GenerationID != "gen-9" {
		t.Fatalf("intent scope/generation = %q/%q, want repo:repo-1/gen-9", got.ScopeID, got.GenerationID)
	}
	if got.Payload["repo_id"] != "repo-1" {
		t.Fatalf("intent repo_id payload = %v, want repo-1", got.Payload["repo_id"])
	}

	quiet := &recordingContentWriter{result: content.Result{}}
	none := &recordingIntentWriter{result: IntentResult{}}
	runtime = Runtime{ContentWriter: quiet, IntentWriter: none}
	if _, err := runtime.writeContentProjection(context.Background(), scopeValue, mat); err != nil {
		t.Fatalf("writeContentProjection() error = %v, want nil", err)
	}
	if got := len(none.calls); got != 0 {
		t.Fatalf("intent writer call count = %d, want 0 for fingerprint-free write", got)
	}
}

// TestWriteContentProjectionFailsClosedWithoutIntentWriter proves the
// fail-closed leg: fingerprints changed but no queue is wired is a wiring
// gap, surfaced as an error rather than a silently dropped generation.
func TestWriteContentProjectionFailsClosedWithoutIntentWriter(t *testing.T) {
	t.Parallel()

	runtime := Runtime{
		ContentWriter: &recordingContentWriter{result: content.Result{FingerprintsChanged: true}},
	}
	mat := content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "repo:repo-1",
		GenerationID: "gen-9",
		SourceSystem: "collector/git",
		Records:      []content.Record{{Path: "a.go", Body: "package p\n", Digest: "d1"}},
	}
	if _, err := runtime.writeContentProjection(
		context.Background(), scope.IngestionScope{ScopeID: "repo:repo-1"}, mat,
	); err == nil {
		t.Fatal("writeContentProjection() error = nil, want fail-closed without intent writer")
	}
}
