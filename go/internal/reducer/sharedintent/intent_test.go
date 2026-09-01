// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"testing"
	"time"
)

// TestStableIntentIDIsPinnedToAnExactDigest pins the derivation byte-for-byte.
//
// This is deliberately a golden value rather than a round-trip check. The digest
// keys every intent already persisted in Postgres, so a change to the
// serialization -- key order, separators, the {"identity":{...}} wrapper, the
// hash, or the hex case -- silently orphans in-flight rows instead of updating
// them, which breaks idempotency under retry. A round-trip test would stay green
// through exactly that change. It also matches the original Python
// _stable_intent_id, so the constant below is a cross-implementation contract.
func TestStableIntentIDIsPinnedToAnExactDigest(t *testing.T) {
	t.Parallel()

	got := StableIntentID(map[string]string{
		"acceptance_unit_id": "repo-1",
		"generation_id":      "gen-1",
		"partition_key":      "part-1",
		"projection_domain":  "repo_dependency",
		"repository_id":      "repo-1",
		"scope_id":           "scope-1",
		"source_run_id":      "run-1",
	})

	const want = "df2cf9f13bbed659e8aafcdd7f869c83ab2c8a48574c86af9e4af44d2fc35d35"
	if got != want {
		t.Fatalf("StableIntentID() = %q, want %q -- the persisted identity contract changed", got, want)
	}
	// Recompute to prove the function is deterministic within one process.
	again := StableIntentID(map[string]string{
		"acceptance_unit_id": "repo-1",
		"generation_id":      "gen-1",
		"partition_key":      "part-1",
		"projection_domain":  "repo_dependency",
		"repository_id":      "repo-1",
		"scope_id":           "scope-1",
		"source_run_id":      "run-1",
	})
	if got != again {
		t.Fatalf("StableIntentID() not deterministic: %q then %q", got, again)
	}
}

// TestStableIntentIDIsInsensitiveToMapInsertionOrder proves the sorted-key
// serialization actually sorts. Go randomizes map iteration, but json.Marshal
// sorts map keys, so this would only fail if the implementation stopped going
// through json.Marshal -- which is the change most likely to be made for
// "performance" and most likely to break persisted identity.
func TestStableIntentIDIsInsensitiveToMapInsertionOrder(t *testing.T) {
	t.Parallel()

	a := map[string]string{}
	for _, k := range []string{"scope_id", "generation_id", "repository_id"} {
		a[k] = k + "-value"
	}
	b := map[string]string{}
	for _, k := range []string{"repository_id", "generation_id", "scope_id"} {
		b[k] = k + "-value"
	}

	if StableIntentID(a) != StableIntentID(b) {
		t.Fatal("StableIntentID() depends on map insertion order")
	}
}

func TestBuildFallsBackToRepositoryIDForAcceptanceUnit(t *testing.T) {
	t.Parallel()

	row := Build(Input{
		ProjectionDomain: "repo_dependency",
		PartitionKey:     "part-1",
		RepositoryID:     "repo-1",
		GenerationID:     "gen-1",
		SourceRunID:      "run-1",
		ScopeID:          "  scope-1  ",
	})

	if row.AcceptanceUnitID != "repo-1" {
		t.Fatalf("AcceptanceUnitID = %q, want the repository ID as fallback", row.AcceptanceUnitID)
	}
	if row.ScopeID != "scope-1" {
		t.Fatalf("ScopeID = %q, want it trimmed", row.ScopeID)
	}
	if row.CompletedAt != nil {
		t.Fatal("a freshly built row must not be pre-completed")
	}
}

// TestBuildIdentityKeyChangesIdentityButNotStoredPartition pins the one subtle
// behaviour in Build: IdentityKey overrides the partition key for the hashed
// identity ONLY. The stored PartitionKey must keep its original value, because
// several rows deliberately share one stored partition while needing distinct
// intent IDs. A change that let IdentityKey leak into the stored column would
// pass any test that only checked the digest.
func TestBuildIdentityKeyChangesIdentityButNotStoredPartition(t *testing.T) {
	t.Parallel()

	base := Input{
		ProjectionDomain: "repo_dependency",
		PartitionKey:     "shared-partition",
		RepositoryID:     "repo-1",
		AcceptanceUnitID: "unit-1",
		GenerationID:     "gen-1",
		SourceRunID:      "run-1",
		ScopeID:          "scope-1",
	}
	withKey := base
	withKey.IdentityKey = "distinct-identity"

	plain := Build(base)
	keyed := Build(withKey)

	if plain.IntentID == keyed.IntentID {
		t.Fatal("IdentityKey did not change the derived intent ID")
	}
	if keyed.PartitionKey != "shared-partition" {
		t.Fatalf("PartitionKey = %q, want the stored partition unchanged", keyed.PartitionKey)
	}
}

func TestAcceptanceKeyReadsPayloadWhenColumnsAreEmpty(t *testing.T) {
	t.Parallel()

	row := Row{
		SourceRunID: "run-1",
		Payload: map[string]any{
			"scope_id":           "scope-from-payload",
			"acceptance_unit_id": "unit-from-payload",
		},
		CreatedAt: time.Now(),
	}

	key, ok := row.AcceptanceKey()
	if !ok {
		t.Fatal("AcceptanceKey() = false, want the payload fallback to supply the slice")
	}
	if key.ScopeID != "scope-from-payload" || key.AcceptanceUnitID != "unit-from-payload" {
		t.Fatalf("AcceptanceKey() = %+v, want both values read from the payload", key)
	}
}

// TestAcceptanceKeyReportsFalseRatherThanAZeroKey pins the contract that a row
// which cannot name a slice reports false. Returning a zero-value key with true
// would make every such row collide into one bogus acceptance slice.
func TestAcceptanceKeyReportsFalseRatherThanAZeroKey(t *testing.T) {
	t.Parallel()

	for name, row := range map[string]Row{
		"no source run": {ScopeID: "scope-1", AcceptanceUnitID: "unit-1"},
		"no scope":      {AcceptanceUnitID: "unit-1", SourceRunID: "run-1"},
		"nothing":       {},
	} {
		key, ok := row.AcceptanceKey()
		if ok {
			t.Fatalf("%s: AcceptanceKey() = true, want false", name)
		}
		if key != (AcceptanceKey{}) {
			t.Fatalf("%s: AcceptanceKey() returned %+v with ok=false", name, key)
		}
	}
}

// TestBuildProducesThePinnedIntentIDForItsFieldSet pins the end-to-end
// Build -> IntentID derivation, not just the hash function underneath it.
//
// TestStableIntentIDIsPinnedToAnExactDigest above calls StableIntentID directly,
// so it locks the serialization -- key order, the wrapper, the hash, the hex
// case -- but says nothing about WHICH fields Build feeds in. Dropping
// projection_domain from Build's identity map, adding a field, or trimming a
// field that is currently untrimmed would silently re-key every intent already
// persisted in Postgres while that test stayed green. This one fails instead.
func TestBuildProducesThePinnedIntentIDForItsFieldSet(t *testing.T) {
	t.Parallel()

	row := Build(Input{
		ProjectionDomain: "repo_dependency",
		PartitionKey:     "part-1",
		RepositoryID:     "repo-1",
		GenerationID:     "gen-1",
		SourceRunID:      "run-1",
		ScopeID:          "scope-1",
	})

	// Same digest as the direct-call pin above: with AcceptanceUnitID empty it
	// falls back to RepositoryID, and with IdentityKey empty the partition key
	// passes through, so Build feeds exactly that map. Their agreement is the
	// point -- it is what ties the field set to the serialization contract.
	const want = "df2cf9f13bbed659e8aafcdd7f869c83ab2c8a48574c86af9e4af44d2fc35d35"
	if row.IntentID != want {
		t.Fatalf("Build().IntentID = %q, want %q -- Build's identity field set changed", row.IntentID, want)
	}
}
