// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemareplay_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/schemareplay"
)

// frozenCassette is the committed historical-version compatibility corpus,
// relative to this package directory (go/internal/replay/schemareplay).
var frozenCassette = filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "replayschema", "historical-schema-versions.json")

// wantOutcome is the frozen, asserted outcome for one recorded historical fact.
type wantOutcome struct {
	admitted bool
	// rejectSubstr, when admitted is false, must appear in the refusal error so
	// the rejection is explicit and legible (never silent-wrong).
	rejectSubstr string
}

// frozenExpectations pins the admission outcome of every fact in the frozen
// cassette against the CURRENT admission code. Each key is the fact's
// stable_fact_key. These are the defined, asserted outcomes the issue requires:
// an old-schema fact is either admitted (still-supported / unknown-kind
// pass-through) or cleanly refused — never silently projected under the wrong
// interpretation.
var frozenExpectations = map[string]wantOutcome{
	// Exact supported version: admitted. When aws_resource's supported version is
	// later bumped, this same fact proves older-same-major backward compatibility
	// (or the registry-pin guard below forces an explicit decision).
	"aws_resource@1.0.0": {admitted: true},
	// Older MAJOR than current: a genuinely breaking old recording, refused.
	"aws_resource@0.9.0": {admitted: false, rejectSubstr: "unsupported"},
	// Unknown (out-of-tree / legacy) fact kind: core owns no versioned schema, so
	// it passes through admitted — the documented contract.
	"replay.unknown_legacy_kind@1.0.0": {admitted: true},
	// Newer than current code knows: refused, never silent-wrong.
	"aws_resource@1.2.0": {admitted: false, rejectSubstr: "unsupported"},
	// Non-canonical (pre-semver) historical version: fails closed.
	"aws_resource@legacy-2019": {admitted: false, rejectSubstr: "unsupported"},
}

// TestFrozenSchemaVersionCorpusAdmissionOutcomes replays the frozen old-schema
// cassette against the CURRENT admission code and asserts every recorded fact
// reaches its pinned outcome — admit or explicit refusal, never silent-wrong.
func TestFrozenSchemaVersionCorpusAdmissionOutcomes(t *testing.T) {
	t.Parallel()

	results, err := schemareplay.ReplayAdmission(frozenCassette)
	if err != nil {
		t.Fatalf("ReplayAdmission(%s): %v", frozenCassette, err)
	}
	if len(results) != len(frozenExpectations) {
		t.Fatalf("replayed %d facts, want %d (frozen corpus and expectations drifted)", len(results), len(frozenExpectations))
	}

	seen := map[string]bool{}
	for _, r := range results {
		want, ok := frozenExpectations[r.StableFactKey]
		if !ok {
			t.Fatalf("frozen fact %q has no pinned expectation", r.StableFactKey)
		}
		seen[r.StableFactKey] = true

		if r.Admitted != want.admitted {
			t.Fatalf("fact %q (kind=%q version=%q): admitted=%v, want %v (err=%v)",
				r.StableFactKey, r.FactKind, r.SchemaVersion, r.Admitted, want.admitted, r.Err)
		}
		if want.admitted {
			if r.Err != nil {
				t.Fatalf("fact %q admitted but carries error: %v", r.StableFactKey, r.Err)
			}
			continue
		}
		// Rejected: the refusal must be explicit and mention why.
		if r.Err == nil {
			t.Fatalf("fact %q expected refusal but was admitted (silent-wrong)", r.StableFactKey)
		}
		if !strings.Contains(r.Err.Error(), want.rejectSubstr) {
			t.Fatalf("fact %q refusal %q does not contain %q", r.StableFactKey, r.Err.Error(), want.rejectSubstr)
		}
	}
	for key := range frozenExpectations {
		if !seen[key] {
			t.Fatalf("frozen expectation %q was never replayed (missing from cassette)", key)
		}
	}
}

// pinnedSupportedVersions records the supported schema_version each frozen
// corpus kind was authored against. The guard below fails if the live registry
// drifts from these pins, forcing a contributor who bumps a fact_schema_version
// to either prove an older-version replay still admits (migration path) or add
// an explicit refusal case — the issue's second acceptance bullet.
var pinnedSupportedVersions = map[string]string{
	"aws_resource": "1.0.0",
}

// TestSchemaVersionRegistryPinForcesCompatibilityCase ties the frozen corpus to
// the central fact-schema-version registry (#3152). A version bump that lands
// without a corresponding frozen replay case trips this guard.
func TestSchemaVersionRegistryPinForcesCompatibilityCase(t *testing.T) {
	t.Parallel()

	for kind, pinned := range pinnedSupportedVersions {
		got, ok := facts.SchemaVersion(kind)
		if !ok {
			t.Fatalf("frozen corpus kind %q is no longer a registered versioned fact kind; update the frozen corpus and pins", kind)
		}
		if got != pinned {
			t.Fatalf("fact kind %q supported schema_version is now %q (frozen corpus pinned %q). "+
				"Adding a new fact_schema_version requires a frozen replay case proving the older version still admits "+
				"(migration path) OR an explicit asserted refusal. Update the frozen cassette + pins in the SAME change.",
				kind, got, pinned)
		}
	}
}

// TestAdmissionHookIsTheReplayedFunction confirms the central registry declares
// facts.ValidateSchemaVersion as the admission hook for each corpus kind, so a
// registry-level rename of the hook is caught here. This is a metadata check:
// the projector calls facts.ValidateSchemaVersion directly (via the thin
// projector/schema_version_admission.go wrapper) rather than dispatching on this
// string, so it documents the wiring rather than enforcing it at runtime — but
// ReplayAdmission calls that exact same leaf function, so the replay still
// asserts the real admission decision, not a re-implementation.
func TestAdmissionHookIsTheReplayedFunction(t *testing.T) {
	t.Parallel()

	for kind := range pinnedSupportedVersions {
		entry, ok := facts.FactKindRegistryEntryFor(kind)
		if !ok {
			t.Fatalf("fact kind %q not in registry", kind)
		}
		if entry.AdmissionHook != "facts.ValidateSchemaVersion" {
			t.Fatalf("fact kind %q admission hook = %q, want facts.ValidateSchemaVersion (this replay would assert against the wrong gate)", kind, entry.AdmissionHook)
		}
	}
}
