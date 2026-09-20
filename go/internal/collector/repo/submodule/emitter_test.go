// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

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
		SourceURI:    ".gitmodules",
	}
}

func TestEmit(t *testing.T) {
	t.Parallel()

	ctx := testFixtureContext()
	body := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n" +
		"[submodule \"libbar\"]\n\tpath = lib/bar\n\turl = ../libbar.git\n"

	envelopes := Emit(ctx, "repo-1", ".gitmodules", body)

	if got, want := len(envelopes), 2; got != want {
		t.Fatalf("Emit() returned %d envelopes, want %d", got, want)
	}

	for i, envelope := range envelopes {
		if got, want := envelope.FactKind, facts.SubmodulePinFactKind; got != want {
			t.Errorf("envelope[%d].FactKind = %q, want %q", i, got, want)
		}
		if got, want := envelope.SchemaVersion, facts.SubmoduleSchemaVersionV1; got != want {
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
		if got, want := envelope.SourceRef.SourceURI, ".gitmodules"; got != want {
			t.Errorf("envelope[%d].SourceRef.SourceURI = %q, want %q", i, got, want)
		}
		if envelope.FactID == "" {
			t.Errorf("envelope[%d].FactID is blank", i)
		}
		if envelope.StableFactKey == "" {
			t.Errorf("envelope[%d].StableFactKey is blank", i)
		}
		if _, hasPinnedSHA := envelope.Payload["pinned_sha"]; hasPinnedSHA {
			t.Errorf("envelope[%d].Payload unexpectedly includes pinned_sha (Phase 2b): %#v", i, envelope.Payload)
		}
	}

	first := envelopes[0].Payload
	if got, want := first["parent_repo_id"], "repo-1"; got != want {
		t.Errorf("envelopes[0].Payload[parent_repo_id] = %#v, want %#v", got, want)
	}
	if got, want := first["submodule_path"], "lib/foo"; got != want {
		t.Errorf("envelopes[0].Payload[submodule_path] = %#v, want %#v", got, want)
	}
	if got, want := first["submodule_url"], "https://github.com/example/libfoo.git"; got != want {
		t.Errorf("envelopes[0].Payload[submodule_url] = %#v, want %#v", got, want)
	}
	resolvedRepoID, ok := first["resolved_repo_id"].(string)
	if !ok || resolvedRepoID == "" {
		t.Errorf("envelopes[0].Payload[resolved_repo_id] = %#v, want a non-empty resolved id", first["resolved_repo_id"])
	}

	second := envelopes[1].Payload
	if got, want := second["submodule_path"], "lib/bar"; got != want {
		t.Errorf("envelopes[1].Payload[submodule_path] = %#v, want %#v", got, want)
	}
	if got, want := second["submodule_url"], "../libbar.git"; got != want {
		t.Errorf("envelopes[1].Payload[submodule_url] = %#v, want %#v", got, want)
	}
	if _, hasResolved := second["resolved_repo_id"]; hasResolved {
		t.Errorf("envelopes[1].Payload unexpectedly includes resolved_repo_id for a relative url: %#v", second)
	}

	if envelopes[0].StableFactKey == envelopes[1].StableFactKey {
		t.Fatalf("two distinct submodules must not share a stable fact key")
	}
	if envelopes[0].FactID == envelopes[1].FactID {
		t.Fatalf("two distinct submodules must not share a fact id")
	}
}

func TestEmitStableIDIsKeyedByRepoAndSubmodulePath(t *testing.T) {
	t.Parallel()

	ctx := testFixtureContext()
	body := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n"

	base := Emit(ctx, "repo-1", ".gitmodules", body)[0]

	// Re-emitting the identical entry (same repo, submodule path) must reuse
	// the same stable key so the fact store upserts instead of duplicating.
	repeat := Emit(ctx, "repo-1", ".gitmodules", body)[0]
	if base.StableFactKey != repeat.StableFactKey {
		t.Fatalf("StableFactKey changed on identical re-emission: %q vs %q", base.StableFactKey, repeat.StableFactKey)
	}

	// A different repo must change the stable key even for an otherwise
	// identical entry.
	otherRepo := Emit(ctx, "repo-2", ".gitmodules", body)[0]
	if base.StableFactKey == otherRepo.StableFactKey {
		t.Fatalf("StableFactKey must differ across parent_repo_id")
	}

	// A different submodule path must change the stable key even when the
	// URL is unchanged.
	otherPathBody := "[submodule \"libfoo\"]\n\tpath = lib/other\n\turl = https://github.com/example/libfoo.git\n"
	otherPath := Emit(ctx, "repo-1", ".gitmodules", otherPathBody)[0]
	if base.StableFactKey == otherPath.StableFactKey {
		t.Fatalf("StableFactKey must differ across submodule_path")
	}

	// A changed URL for the SAME path reuses the same stable key (upsert on
	// re-pointing, not a new fact identity) — the join identity is
	// (parent_repo_id, submodule_path) only, per
	// sdk/go/factschema/submodule/v1.Pin's doc comment.
	changedURLBody := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo-renamed.git\n"
	changedURL := Emit(ctx, "repo-1", ".gitmodules", changedURLBody)[0]
	if base.StableFactKey != changedURL.StableFactKey {
		t.Fatalf("StableFactKey must stay stable across a url change for the same submodule_path: %q vs %q", base.StableFactKey, changedURL.StableFactKey)
	}
}

func TestEmitEmptyBodyReturnsNoEnvelopes(t *testing.T) {
	t.Parallel()

	envelopes := Emit(testFixtureContext(), "repo-1", ".gitmodules", "")
	if len(envelopes) != 0 {
		t.Fatalf("Emit() with empty body returned %d envelopes, want 0", len(envelopes))
	}
}

func TestEmitIncompleteSectionEmitsNoFact(t *testing.T) {
	t.Parallel()

	body := "[submodule \"incomplete\"]\n\tpath = lib/incomplete\n" +
		"[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n"

	envelopes := Emit(testFixtureContext(), "repo-1", ".gitmodules", body)
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("Emit() returned %d envelopes, want %d", got, want)
	}
	if got, want := envelopes[0].Payload["submodule_path"], "lib/foo"; got != want {
		t.Fatalf("Payload[submodule_path] = %#v, want %#v", got, want)
	}
}

func TestEmitCarriesCollectorInstanceIDWhenSet(t *testing.T) {
	t.Parallel()

	ctx := testFixtureContext()
	instanceID := "git-submodule-1"
	ctx.CollectorInstanceID = &instanceID

	body := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n"
	envelopes := Emit(ctx, "repo-1", ".gitmodules", body)
	if got, want := envelopes[0].Payload["collector_instance_id"], instanceID; got != want {
		t.Fatalf("Payload[collector_instance_id] = %#v, want %#v", got, want)
	}
}

func TestEmitOmitsCollectorInstanceIDWhenUnset(t *testing.T) {
	t.Parallel()

	body := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n"
	envelopes := Emit(testFixtureContext(), "repo-1", ".gitmodules", body)
	if _, ok := envelopes[0].Payload["collector_instance_id"]; ok {
		t.Fatalf("Payload unexpectedly includes collector_instance_id: %#v", envelopes[0].Payload)
	}
}
