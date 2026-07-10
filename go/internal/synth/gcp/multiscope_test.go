// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"bytes"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestGenerateMultiScopeIsByteIdenticalForSameInputs proves the multi-scope
// generator is deterministic: the same (Seed, Scopes, ResourceCount) always
// produces byte-identical output, mirroring the single-scope
// TestGenerateIsByteIdenticalForSameSeed contract this slice must preserve.
func TestGenerateMultiScopeIsByteIdenticalForSameInputs(t *testing.T) {
	optsA := MultiScopeOptions{Seed: 4396, Scopes: 4, ResourceCount: 10}
	optsB := MultiScopeOptions{Seed: 4396, Scopes: 4, ResourceCount: 10}

	outA, err := GenerateMultiScope(optsA)
	if err != nil {
		t.Fatalf("GenerateMultiScope run 1: %v", err)
	}
	outB, err := GenerateMultiScope(optsB)
	if err != nil {
		t.Fatalf("GenerateMultiScope run 2: %v", err)
	}
	if !bytes.Equal(outA, outB) {
		t.Fatalf("GenerateMultiScope is not deterministic for identical inputs: run1 %d bytes, run2 %d bytes differ", len(outA), len(outB))
	}
}

// TestGenerateMultiScopeDifferentSeedsDiffer proves the seed still drives
// content in the multi-scope path, the same non-vacuity guard
// TestGenerateDifferentSeedsDiffer proves for the single-scope generator.
func TestGenerateMultiScopeDifferentSeedsDiffer(t *testing.T) {
	outA, err := GenerateMultiScope(MultiScopeOptions{Seed: 1, Scopes: 3, ResourceCount: 8})
	if err != nil {
		t.Fatalf("GenerateMultiScope(seed=1): %v", err)
	}
	outB, err := GenerateMultiScope(MultiScopeOptions{Seed: 2, Scopes: 3, ResourceCount: 8})
	if err != nil {
		t.Fatalf("GenerateMultiScope(seed=2): %v", err)
	}
	if bytes.Equal(outA, outB) {
		t.Fatal("GenerateMultiScope(seed=1) and GenerateMultiScope(seed=2) produced identical bytes; the seed is not driving generation")
	}
}

// TestGenerateMultiScopeProducesKDistinctScopes proves the merged cassette
// carries exactly opts.Scopes scopes, each with a distinct scope_id — the
// "K genuinely independent work units" property the Ifá P3 determinism matrix
// needs to make `-workers N` non-inert.
func TestGenerateMultiScopeProducesKDistinctScopes(t *testing.T) {
	const k = 5
	out, err := GenerateMultiScope(MultiScopeOptions{Seed: 7, Scopes: k, ResourceCount: 6})
	if err != nil {
		t.Fatalf("GenerateMultiScope: %v", err)
	}
	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}
	if len(file.Scopes) != k {
		t.Fatalf("len(file.Scopes) = %d, want %d", len(file.Scopes), k)
	}
	seenScopeIDs := make(map[string]bool, k)
	for i, scope := range file.Scopes {
		if scope.ScopeID == "" {
			t.Fatalf("scope %d has an empty scope_id", i)
		}
		if seenScopeIDs[scope.ScopeID] {
			t.Fatalf("scope %d: duplicate scope_id %q", i, scope.ScopeID)
		}
		seenScopeIDs[scope.ScopeID] = true
	}
}

// TestGenerateMultiScopeScopesHaveDisjointFullResourceNames proves the
// critical correctness constraint the #4396 slice 6b architecture decision
// requires: every gcp_cloud_resource fact's full_resource_name is unique
// across the WHOLE merged cassette, not merely within one scope. The reducer
// keys the CloudResource node uid on full_resource_name (plus project/
// location/asset_type), not on scope_id
// (go/internal/reducer/gcp_resource_materialization.go's cloudResourceUID), so
// a full_resource_name collision across scopes would MERGE two scopes' facts
// onto one graph node and make last-writer-wins scope-derived properties
// (e.g. source_fact_id) legitimately order-dependent — a false red on the
// determinism matrix that is not a concurrency bug. This test decodes every
// payload through the real sdk/go/factschema decode seam rather than reading
// the raw map, so a schema/field-name drift would fail here too.
func TestGenerateMultiScopeScopesHaveDisjointFullResourceNames(t *testing.T) {
	const k = 6
	out, err := GenerateMultiScope(MultiScopeOptions{Seed: 99, Scopes: k, ResourceCount: 12})
	if err != nil {
		t.Fatalf("GenerateMultiScope: %v", err)
	}
	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}

	seenFullResourceNames := make(map[string]string, k*12) // full_resource_name -> owning scope_id
	seenUIDComponents := make(map[string]string, k*12)     // (project,location,asset_type,full_resource_name) -> owning scope_id
	resourceFactCount := 0
	for _, scope := range file.Scopes {
		for _, fact := range scope.Facts {
			if fact.FactKind != factschema.FactKindGCPCloudResource {
				continue
			}
			resourceFactCount++
			env := factschema.Envelope{
				FactKind:      fact.FactKind,
				SchemaVersion: fact.SchemaVersion,
				Payload:       fact.Payload,
			}
			resource, err := factschema.DecodeGCPCloudResource(env)
			if err != nil {
				t.Fatalf("scope %s: decode gcp_cloud_resource: %v", scope.ScopeID, err)
			}
			frn := resource.FullResourceName
			if owner, ok := seenFullResourceNames[frn]; ok {
				t.Fatalf("full_resource_name %q appears in both scope %q and scope %q; scopes are NOT disjoint", frn, owner, scope.ScopeID)
			}
			seenFullResourceNames[frn] = scope.ScopeID

			var project, location string
			if resource.ProjectID != nil {
				project = *resource.ProjectID
			}
			if resource.Location != nil {
				location = *resource.Location
			}
			uidKey := project + "|" + location + "|" + resource.AssetType + "|" + frn
			if owner, ok := seenUIDComponents[uidKey]; ok {
				t.Fatalf("CloudResource uid components %q collide between scope %q and scope %q", uidKey, owner, scope.ScopeID)
			}
			seenUIDComponents[uidKey] = scope.ScopeID
		}
	}
	if resourceFactCount == 0 {
		t.Fatal("no gcp_cloud_resource facts were generated to check for disjointness")
	}
}

// TestGenerateMultiScopeRejectsNonPositiveScopes proves the fail-closed
// validation guard: a caller cannot generate a degenerate zero- or
// negative-scope cassette.
func TestGenerateMultiScopeRejectsNonPositiveScopes(t *testing.T) {
	cases := []int{0, -1}
	for _, scopes := range cases {
		if _, err := GenerateMultiScope(MultiScopeOptions{Seed: 1, Scopes: scopes, ResourceCount: 4}); err == nil {
			t.Errorf("GenerateMultiScope(Scopes=%d) = nil error, want a fail-closed validation error", scopes)
		}
	}
}

// TestScopeProjectIDIsDeterministicAndDistinct proves scopeProjectID's own
// derivation is deterministic and injective over the small index range this
// generator supports.
func TestScopeProjectIDIsDeterministicAndDistinct(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		id := scopeProjectID(i)
		if id != scopeProjectID(i) {
			t.Fatalf("scopeProjectID(%d) is not deterministic", i)
		}
		if seen[id] {
			t.Fatalf("scopeProjectID(%d) = %q collides with an earlier index", i, id)
		}
		seen[id] = true
	}
}
