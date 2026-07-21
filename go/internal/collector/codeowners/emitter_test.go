// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testFixtureContext() FixtureContext {
	return FixtureContext{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		ObservedAt:   time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		SourceURI:    ".github/CODEOWNERS",
	}
}

func TestEmit(t *testing.T) {
	t.Parallel()

	ctx := testFixtureContext()
	body := "*.go @octocat\n/services/payments/ @org/payments-team\n"

	envelopes := Emit(ctx, "repo-1", ".github/CODEOWNERS", body)

	if got, want := len(envelopes), 2; got != want {
		t.Fatalf("Emit() returned %d envelopes, want %d", got, want)
	}

	for i, envelope := range envelopes {
		if got, want := envelope.FactKind, facts.CodeownersOwnershipFactKind; got != want {
			t.Errorf("envelope[%d].FactKind = %q, want %q", i, got, want)
		}
		if got, want := envelope.SchemaVersion, facts.CodeownersSchemaVersionV1; got != want {
			t.Errorf("envelope[%d].SchemaVersion = %q, want %q", i, got, want)
		}
		if got, want := envelope.ScopeID, ctx.ScopeID; got != want {
			t.Errorf("envelope[%d].ScopeID = %q, want %q", i, got, want)
		}
		if got, want := envelope.GenerationID, ctx.GenerationID; got != want {
			t.Errorf("envelope[%d].GenerationID = %q, want %q", i, got, want)
		}
		if got, want := envelope.SourceConfidence, facts.SourceConfidenceObserved; got != want {
			t.Errorf("envelope[%d].SourceConfidence = %q, want %q", i, got, want)
		}
		if got, want := envelope.SourceRef.SourceURI, ".github/CODEOWNERS"; got != want {
			t.Errorf("envelope[%d].SourceRef.SourceURI = %q, want %q", i, got, want)
		}
		if envelope.FactID == "" {
			t.Errorf("envelope[%d].FactID is blank", i)
		}
		if envelope.StableFactKey == "" {
			t.Errorf("envelope[%d].StableFactKey is blank", i)
		}
	}

	first := envelopes[0].Payload
	if got, want := first["repo_id"], "repo-1"; got != want {
		t.Errorf("envelopes[0].Payload[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := first["source_path"], ".github/CODEOWNERS"; got != want {
		t.Errorf("envelopes[0].Payload[source_path] = %#v, want %#v", got, want)
	}
	if got, want := first["pattern"], "*.go"; got != want {
		t.Errorf("envelopes[0].Payload[pattern] = %#v, want %#v", got, want)
	}
	// The payload round-trips through JSON (factschema.EncodeCodeownersOwnership
	// marshals the typed struct then unmarshals into map[string]any), so a JSON
	// number decodes as float64 and a JSON array decodes as []any rather than
	// the original Go int/[]string.
	if got, want := first["order_index"], float64(0); got != want {
		t.Errorf("envelopes[0].Payload[order_index] = %#v, want %#v", got, want)
	}
	owners, ok := first["owners"].([]any)
	if !ok || len(owners) != 1 || owners[0] != "@octocat" {
		t.Errorf("envelopes[0].Payload[owners] = %#v, want [@octocat]", first["owners"])
	}

	second := envelopes[1].Payload
	if got, want := second["pattern"], "/services/payments/"; got != want {
		t.Errorf("envelopes[1].Payload[pattern] = %#v, want %#v", got, want)
	}
	if got, want := second["order_index"], float64(1); got != want {
		t.Errorf("envelopes[1].Payload[order_index] = %#v, want %#v", got, want)
	}

	if envelopes[0].StableFactKey == envelopes[1].StableFactKey {
		t.Fatalf("two distinct rules must not share a stable fact key")
	}
	if envelopes[0].FactID == envelopes[1].FactID {
		t.Fatalf("two distinct rules must not share a fact id")
	}
}

func TestEmitStableIDIsKeyedByRepoSourcePathPatternAndOrderIndex(t *testing.T) {
	t.Parallel()

	ctx := testFixtureContext()

	base := Emit(ctx, "repo-1", ".github/CODEOWNERS", "*.go @octocat\n")[0]

	// Re-emitting the identical rule (same repo, source path, pattern, and
	// order index) must reuse the same stable key so the fact store upserts
	// instead of duplicating.
	repeat := Emit(ctx, "repo-1", ".github/CODEOWNERS", "*.go @octocat\n")[0]
	if base.StableFactKey != repeat.StableFactKey {
		t.Fatalf("StableFactKey changed on identical re-emission: %q vs %q", base.StableFactKey, repeat.StableFactKey)
	}

	// A different repo must change the stable key even for an otherwise
	// identical rule.
	otherRepo := Emit(ctx, "repo-2", ".github/CODEOWNERS", "*.go @octocat\n")[0]
	if base.StableFactKey == otherRepo.StableFactKey {
		t.Fatalf("StableFactKey must differ across repo_id")
	}

	// A different source path must change the stable key.
	otherPath := Emit(ctx, "repo-1", "CODEOWNERS", "*.go @octocat\n")[0]
	if base.StableFactKey == otherPath.StableFactKey {
		t.Fatalf("StableFactKey must differ across source_path")
	}

	// A different pattern must change the stable key.
	otherPattern := Emit(ctx, "repo-1", ".github/CODEOWNERS", "*.md @octocat\n")[0]
	if base.StableFactKey == otherPattern.StableFactKey {
		t.Fatalf("StableFactKey must differ across pattern")
	}

	// A different order index (a second rule in the same file) must change
	// the stable key even when the pattern text coincidentally repeats.
	twoRules := Emit(ctx, "repo-1", ".github/CODEOWNERS", "*.go @octocat\n*.go @writer\n")
	if twoRules[0].StableFactKey == twoRules[1].StableFactKey {
		t.Fatalf("StableFactKey must differ across order_index")
	}
}

func TestEmitEmptyBodyReturnsNoEnvelopes(t *testing.T) {
	t.Parallel()

	envelopes := Emit(testFixtureContext(), "repo-1", ".github/CODEOWNERS", "")
	if len(envelopes) != 0 {
		t.Fatalf("Emit() with empty body returned %d envelopes, want 0", len(envelopes))
	}
}

func TestEmitPatternOnlyLineEmitsNoFact(t *testing.T) {
	t.Parallel()

	envelopes := Emit(testFixtureContext(), "repo-1", ".github/CODEOWNERS", "*.md\n*.go @octocat\n")
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("Emit() returned %d envelopes, want %d", got, want)
	}
	if got, want := envelopes[0].Payload["pattern"], "*.go"; got != want {
		t.Fatalf("Payload[pattern] = %#v, want %#v", got, want)
	}
}

func TestEmitCarriesCollectorInstanceIDWhenSet(t *testing.T) {
	t.Parallel()

	ctx := testFixtureContext()
	instanceID := "git-codeowners-1"
	ctx.CollectorInstanceID = &instanceID

	envelopes := Emit(ctx, "repo-1", ".github/CODEOWNERS", "*.go @octocat\n")
	if got, want := envelopes[0].Payload["collector_instance_id"], instanceID; got != want {
		t.Fatalf("Payload[collector_instance_id] = %#v, want %#v", got, want)
	}
}

func TestEmitOmitsCollectorInstanceIDWhenUnset(t *testing.T) {
	t.Parallel()

	envelopes := Emit(testFixtureContext(), "repo-1", ".github/CODEOWNERS", "*.go @octocat\n")
	if _, ok := envelopes[0].Payload["collector_instance_id"]; ok {
		t.Fatalf("Payload unexpectedly includes collector_instance_id: %#v", envelopes[0].Payload)
	}
}
