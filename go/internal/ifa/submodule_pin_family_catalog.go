// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
	submodulev1 "github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1"
)

const (
	submodulePinFamilyGenerationID = "gen-ifa-submodule-pin-family-1"
	// SubmodulePinFamilyRepoID is the parent repository ID every pin in this
	// Odù is scoped against. Exported so materializededges' moved
	// submodule-pin-family tests can build a synthetic Odù against this same
	// repository (TestResolveSubmodulePinMaterializedEdgesReturnsZeroRowsFalse's
	// empty-probe) and identify PIN A's row by parent_repo_id
	// (TestSubmodulePinFamilyCassettePinADupWinsOnPinnedSHA) without
	// duplicating the literal.
	SubmodulePinFamilyRepoID      = "repo-ifa-submodule-pin-family"
	submodulePinFamilyTargetFooID = "repo-ifa-submodule-pin-target-foo"
	submodulePinFamilyTargetBazID = "repo-ifa-submodule-pin-target-baz"
)

// SubmodulePinFamilyOdu returns the binary-portable catalog representation of
// the submodule_pin_edges cassette (#6002, under the #5543 umbrella). The
// checked-in cassette remains the live replay source;
// TestSubmodulePinFamilyIsCatalogedAndResolvable deeply pins this compiled
// literal to the same strict projection.
//
// Every PINS_SUBMODULE edge this Odù derives is exercised for a distinct
// reason, because the expected-edge-set fixture is asserted as an EXACT set:
// a silently-included edge fails as loudly as a missing one.
//
//   - PIN A (path "vendor/libfoo") and PIN B (path "vendor/libfoo-mirror")
//     both resolve to the SAME target repository
//     (repo-ifa-submodule-pin-target-foo): two DISTINCT PINS_SUBMODULE
//     relationships between the SAME (parent, target) pair, exercising the
//     relationship-MERGE-key-on-path shape canonical_submodule_edges.go
//     documents. A pair-only edge identity collapses these two onto one key
//     -- a bare `MERGE (parent)-[rel:PINS_SUBMODULE]->(target)` would
//     "silently collaps[e] two distinct submodule paths into one graph
//     edge" (canonical_submodule_edges.go's own words) -- so the declared
//     path identity (cypher.MaterializedEdgeIdentityProperties) is what lets
//     a dropped path stay visible when an unrelated duplicate masks the
//     count.
//   - PIN A-DUP repeats PIN A's exact (parent_repo_id, submodule_path) key
//     with a DIFFERENT pinned_sha: ExtractSubmodulePinEdgeRowsWithQuarantine
//     must collapse it into PIN A's row rather than emit a second edge for
//     the same path -- proving the dedup COUNT half of the last-match-wins
//     contract (rowIndexByKey in submodule_pin_materialization.go). The
//     offline guard's edge comparison drops pinned_sha (a SET-only
//     property, never part of the MERGE key), so it cannot see which
//     envelope's value survived; TestSubmodulePinFamilyCassettePinADupWinsOnPinnedSHA
//     (materialized_edges_submodule_pin_test.go) reads the raw row map to
//     prove the later envelope's value actually wins.
//   - PIN E (path "vendor/libbaz") resolves to a SECOND, DISTINCT target
//     repository (repo-ifa-submodule-pin-target-baz): proves the extractor
//     tracks per-path target identity correctly rather than only ever
//     wiring every pin to whichever target it saw first.
//   - PIN C (path "vendor/libunresolved") carries a submodule_url but NO
//     resolved_repo_id: the extractor must never guess a target Repository
//     id, so this fact projects NO edge at all -- proving the
//     resolved-repo-id gate, not merely a fourth entry here.
//
// Total: 3 PINS_SUBMODULE edges -- 2 from repo-ifa-submodule-pin-family to
// repo-ifa-submodule-pin-target-foo (PIN A, PIN B), 1 to
// repo-ifa-submodule-pin-target-baz (PIN E).
func SubmodulePinFamilyOdu() CatalogOdu {
	factsForOdu := []facts.Envelope{
		SubmodulePinFamilyRepositoryFact(SubmodulePinFamilyRepoID, "/repo-submodule-pin-parent"),
		// PIN A.
		submodulePinFamilyPinFact(submodulev1.Pin{
			ParentRepoID: SubmodulePinFamilyRepoID, SubmodulePath: "vendor/libfoo",
			SubmoduleURL:   strPtr("https://git.example.invalid/org/libfoo.git"),
			ResolvedRepoID: strPtr(submodulePinFamilyTargetFooID),
			PinnedSHA:      strPtr("sha-libfoo-pin-a"),
		}, "pin-a"),
		// PIN B: same target as PIN A, a different path.
		submodulePinFamilyPinFact(submodulev1.Pin{
			ParentRepoID: SubmodulePinFamilyRepoID, SubmodulePath: "vendor/libfoo-mirror",
			SubmoduleURL:   strPtr("https://git.example.invalid/org/libfoo-mirror.git"),
			ResolvedRepoID: strPtr(submodulePinFamilyTargetFooID),
			PinnedSHA:      strPtr("sha-libfoo-mirror-pin-b"),
		}, "pin-b"),
		// PIN A-DUP: last-match-wins duplicate of PIN A's (parent, path) key.
		submodulePinFamilyPinFact(submodulev1.Pin{
			ParentRepoID: SubmodulePinFamilyRepoID, SubmodulePath: "vendor/libfoo",
			SubmoduleURL:   strPtr("https://git.example.invalid/org/libfoo.git"),
			ResolvedRepoID: strPtr(submodulePinFamilyTargetFooID),
			PinnedSHA:      strPtr("sha-libfoo-pin-a-dup-newer"),
		}, "pin-a-dup"),
		// PIN E: a second, distinct target repository.
		submodulePinFamilyPinFact(submodulev1.Pin{
			ParentRepoID: SubmodulePinFamilyRepoID, SubmodulePath: "vendor/libbaz",
			SubmoduleURL:   strPtr("https://git.example.invalid/org/libbaz.git"),
			ResolvedRepoID: strPtr(submodulePinFamilyTargetBazID),
			PinnedSHA:      strPtr("sha-libbaz-pin-e"),
		}, "pin-e"),
		// PIN C: unresolved submodule URL -- no edge must project.
		submodulePinFamilyPinFact(submodulev1.Pin{
			ParentRepoID: SubmodulePinFamilyRepoID, SubmodulePath: "vendor/libunresolved",
			SubmoduleURL: strPtr("https://git.example.invalid/org/libunresolved.git"),
		}, "pin-c"),
		submodulePinFamilySharedFollowupFact(),
	}
	return CatalogOdu{
		Odu: Odu{Name: SubmodulePinFamilyOduName, Facts: factsForOdu},
		Detail: "one parent repository pinning two distinct target repositories across three " +
			"live paths (a same-target two-path collision PIN A/B, a last-match-wins " +
			"duplicate PIN A-DUP, and a second-target PIN E), plus an unresolved " +
			"submodule PIN C that must project no edge -- three PINS_SUBMODULE edges total",
	}
}

// submodulePinFamilySharedFollowupFact builds the shared_followup envelope
// that enqueues this family's reducer work item. WITHOUT IT THE LIVE GATES
// CANNOT DRIVE THIS HANDLER AT ALL, mirroring
// codeownersFamilySharedFollowupFact's doc comment exactly:
// scope_generation_intents.go's projector fan-out has no build*ReducerIntent
// probe for submodule_pin either, so this family is enqueued by a
// shared_followup fact instead of the projector's own fan-out.
//
// In production that fact comes from the git collector
// (go/internal/collector/git_followup_facts.go's submodulePinFactEnvelope),
// which emits one per repository unconditionally (not gated on ".gitmodules"
// presence) so the domain runs every generation and its delta-retract sweeps
// stale edges even when ".gitmodules" is removed.
func submodulePinFamilySharedFollowupFact() facts.Envelope {
	return facts.Envelope{
		ScopeID:          "scope-ifa-submodule-pin-family",
		GenerationID:     submodulePinFamilyGenerationID,
		FactKind:         "shared_followup",
		StableFactKey:    "shared_followup:" + SubmodulePinFamilyRepoID + ":submodule_pin",
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		Payload: map[string]any{
			"reducer_domain": "submodule_pin",
			"entity_key":     "submodule:" + SubmodulePinFamilyRepoID,
			"reason":         "repository snapshot emitted submodule pin follow-up",
			"repo_id":        SubmodulePinFamilyRepoID,
		},
	}
}

// SubmodulePinFamilyRepositoryFact builds a typed "repository" fact for
// repoID. Used for the parent repository this Odù's pins are scoped against.
// Exported so materializededges' moved
// TestResolveSubmodulePinMaterializedEdgesReturnsZeroRowsFalse can build a
// synthetic repository-only Odù (no pin facts) without duplicating the
// repository-fact-building logic.
func SubmodulePinFamilyRepositoryFact(repoID, localPath string) facts.Envelope {
	sourceRunID := "run-ifa-submodule-pin-family-1"
	repository := codegraphv1.Repository{
		RepoID: repoID, SourceRunID: &sourceRunID, LocalPath: &localPath,
	}
	payload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode submodule-pin catalog repository %q: %v", repoID, err))
	}
	return facts.Envelope{
		ScopeID: "scope-ifa-submodule-pin-family", GenerationID: submodulePinFamilyGenerationID,
		FactKind: factschema.FactKindCodegraphRepository, StableFactKey: "repository:" + repoID,
		SchemaVersion: "1.0.0", CollectorKind: "git", SourceConfidence: "observed", Payload: payload,
	}
}

// submodulePinFamilyPinFact builds one typed "submodule.pin" fact. pinLabel
// feeds the stable_fact_key so each pin has its own stable identity even
// though PIN A and PIN A-DUP otherwise share every field but pinned_sha.
func submodulePinFamilyPinFact(pin submodulev1.Pin, pinLabel string) facts.Envelope {
	payload, err := factschema.EncodeSubmodulePin(pin)
	if err != nil {
		panic(fmt.Sprintf("ifa: encode submodule-pin catalog pin %q/%q: %v", pin.ParentRepoID, pin.SubmodulePath, err))
	}
	return facts.Envelope{
		ScopeID: "scope-ifa-submodule-pin-family", GenerationID: submodulePinFamilyGenerationID,
		FactKind:         factschema.FactKindSubmodulePin,
		StableFactKey:    "submodule.pin:" + pin.ParentRepoID + ":" + pinLabel,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: "observed",
		Payload:          payload,
	}
}
