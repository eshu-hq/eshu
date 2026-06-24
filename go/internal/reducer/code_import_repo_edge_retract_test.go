// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildCodeImportRepoEdgeRefreshIntentsEmitRetractForZeroOwners proves that
// when no import resolves to an owning repository (empty owner index), a
// consumer that appears in the file facts still gets a retract-only refresh
// intent so any stale projection/code-imports DEPENDS_ON edge from a prior
// generation is removed rather than left graph-visible (issue #3651, P1).
func TestBuildCodeImportRepoEdgeRefreshIntentsEmitRetractForZeroOwners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"express"}),
	}
	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-retract-1",
		GenerationID:  "gen-retract-1",
		SourceRunID:   "code_import_repo_dependency:scope-retract-1",
		CreatedAt:     now,
		FileEnvelopes: envelopes,
		Owners:        codeImportOwnerIndex{}, // no owners → upsert produces nothing
	}

	rows := BuildCodeImportRepoEdgeRefreshIntents(input)

	if len(rows) != 1 {
		t.Fatalf("BuildCodeImportRepoEdgeRefreshIntents() = %d rows, want 1", len(rows))
	}
	row := rows[0]
	if got := anyToString(row.Payload["action"]); got != "retract" {
		t.Errorf("action = %q, want retract", got)
	}
	if got := anyToString(row.Payload["evidence_source"]); got != codeImportEvidenceSource {
		t.Errorf("evidence_source = %q, want %q", got, codeImportEvidenceSource)
	}
	if got := anyToString(row.Payload["repo_id"]); got != "consumer-repo" {
		t.Errorf("repo_id = %q, want consumer-repo", got)
	}
	if row.SourceRunID != input.SourceRunID {
		t.Errorf("SourceRunID = %q, want %q", row.SourceRunID, input.SourceRunID)
	}
}

// TestBuildCodeImportRepoEdgeRefreshIntentsCoveredConsumerExcluded proves that
// a consumer which resolves at least one upsert edge is NOT emitted as a
// retract-only refresh intent: the upsert intent already drives the
// refresh-first reconstruction for the codeImportEvidenceSource lane (P1).
func TestBuildCodeImportRepoEdgeRefreshIntentsCoveredConsumerExcluded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	owners := codeImportTestOwners() // has npm/express → repo-express
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"express"}),
	}
	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-retract-2",
		GenerationID:  "gen-retract-2",
		SourceRunID:   "code_import_repo_dependency:scope-retract-2",
		CreatedAt:     now,
		FileEnvelopes: envelopes,
		Owners:        owners,
	}

	rows := BuildCodeImportRepoEdgeRefreshIntents(input)
	if len(rows) != 0 {
		t.Errorf("BuildCodeImportRepoEdgeRefreshIntents() = %d rows, want 0 (covered consumer excluded)", len(rows))
	}
}

// TestBuildCodeImportRepoEdgeRefreshIntentsSelfOnlyConsumerRetracted proves
// that a consumer whose only resolved import is a self-reference (consumer ==
// owner) emits a refresh intent, because no upsert is produced and a prior
// cross-repo edge must be retracted (mirrors #3598 self-ref retract logic).
func TestBuildCodeImportRepoEdgeRefreshIntentsSelfOnlyConsumerRetracted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	// consumer-repo is also the owner of express in this universe.
	owners := newCodeImportOwnerIndexForTest(map[ecoName]string{
		{"npm", "express"}: "consumer-repo",
	})
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"express"}),
	}
	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-retract-3",
		GenerationID:  "gen-retract-3",
		SourceRunID:   "code_import_repo_dependency:scope-retract-3",
		CreatedAt:     now,
		FileEnvelopes: envelopes,
		Owners:        owners,
	}

	rows := BuildCodeImportRepoEdgeRefreshIntents(input)
	if len(rows) != 1 {
		t.Fatalf("BuildCodeImportRepoEdgeRefreshIntents() = %d rows, want 1 (self-only retracted)", len(rows))
	}
	if got := anyToString(rows[0].Payload["action"]); got != "retract" {
		t.Errorf("action = %q, want retract", got)
	}
}

// TestBuildCodeImportRepoEdgeRefreshIntentsIdempotent proves that running the
// refresh builder twice with identical input produces rows with the same intent
// IDs, keeping the downstream lane replay-safe (P1 idempotency).
func TestBuildCodeImportRepoEdgeRefreshIntentsIdempotent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelope("consumer-repo", "src/app.js", "javascript", []string{"express"}),
	}
	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-retract-idem",
		GenerationID:  "gen-retract-idem",
		SourceRunID:   "code_import_repo_dependency:scope-retract-idem",
		CreatedAt:     now,
		FileEnvelopes: envelopes,
		Owners:        codeImportOwnerIndex{},
	}

	first := BuildCodeImportRepoEdgeRefreshIntents(input)
	second := BuildCodeImportRepoEdgeRefreshIntents(input)

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1 row each run, got first=%d second=%d", len(first), len(second))
	}
	if first[0].IntentID != second[0].IntentID {
		t.Errorf("IntentID first=%q second=%q, want equal", first[0].IntentID, second[0].IntentID)
	}
}
