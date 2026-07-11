// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"bytes"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// TestGenerateOverlappingScopeIsByteIdenticalForSameInputs proves the contention
// fixture is deterministic, so the #5007 determinism matrix drives the same
// fixture into every worker-count cell.
func TestGenerateOverlappingScopeIsByteIdenticalForSameInputs(t *testing.T) {
	t.Parallel()

	opts := OverlappingScopeOptions{Seed: 5007, Scopes: 4, ResourceCount: 8}
	a, err := GenerateOverlappingScope(opts)
	if err != nil {
		t.Fatalf("GenerateOverlappingScope() error = %v", err)
	}
	b, err := GenerateOverlappingScope(opts)
	if err != nil {
		t.Fatalf("GenerateOverlappingScope() second call error = %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("GenerateOverlappingScope() is not byte-identical for identical inputs")
	}
}

// TestGenerateOverlappingScopeScopesShareResourceIdentity proves the fixture is
// actually a contention fixture: every generated scope carries a DISTINCT
// scope_id (so they do not fence each other) but the SAME set of resource
// full_resource_names (so every scope folds to the same CloudResource node uid).
// This is the inverse of TestGenerateMultiScopeScopesHaveDisjointFullResourceNames.
func TestGenerateOverlappingScopeScopesShareResourceIdentity(t *testing.T) {
	t.Parallel()

	raw, err := GenerateOverlappingScope(OverlappingScopeOptions{Seed: 5007, Scopes: 3, ResourceCount: 8})
	if err != nil {
		t.Fatalf("GenerateOverlappingScope() error = %v", err)
	}
	file, err := cassette.ParseAndValidate(raw)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}
	if len(file.Scopes) != 3 {
		t.Fatalf("len(scopes) = %d, want 3", len(file.Scopes))
	}

	scopeIDs := make(map[string]struct{}, len(file.Scopes))
	var firstNames []string
	for i, scope := range file.Scopes {
		if _, dup := scopeIDs[scope.ScopeID]; dup {
			t.Fatalf("scope[%d] reuses scope_id %q — contending scopes must be distinct", i, scope.ScopeID)
		}
		scopeIDs[scope.ScopeID] = struct{}{}

		names := fullResourceNames(scope)
		if i == 0 {
			firstNames = names
			continue
		}
		if len(names) != len(firstNames) {
			t.Fatalf("scope[%d] has %d resource names, scope[0] has %d — identity sets must match", i, len(names), len(firstNames))
		}
		for j := range names {
			if names[j] != firstNames[j] {
				t.Fatalf("scope[%d] resource name %q != scope[0] %q — every scope must share the same uid set", i, names[j], firstNames[j])
			}
		}
	}
	if len(firstNames) == 0 {
		t.Fatal("contention fixture produced zero resource names")
	}
}

// TestGenerateOverlappingScopeDivergentMutatesStateNotIdentity proves the
// divergent mode changes only OBSERVED STATE across scopes while preserving the
// resource identity, so the shared uid still collides but the payloads differ.
func TestGenerateOverlappingScopeDivergentMutatesStateNotIdentity(t *testing.T) {
	t.Parallel()

	raw, err := GenerateOverlappingScope(OverlappingScopeOptions{Seed: 5007, Scopes: 2, ResourceCount: 8, Divergent: true})
	if err != nil {
		t.Fatalf("GenerateOverlappingScope(divergent) error = %v", err)
	}
	file, err := cassette.ParseAndValidate(raw)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}
	if len(file.Scopes) != 2 {
		t.Fatalf("len(scopes) = %d, want 2", len(file.Scopes))
	}
	n0 := fullResourceNames(file.Scopes[0])
	n1 := fullResourceNames(file.Scopes[1])
	if len(n0) != len(n1) {
		t.Fatalf("divergent scopes have mismatched resource counts %d vs %d", len(n0), len(n1))
	}
	for i := range n0 {
		if n0[i] != n1[i] {
			t.Fatalf("divergent mode must preserve identity: scope0 %q != scope1 %q", n0[i], n1[i])
		}
	}
	if !anyStateDiffers(file.Scopes[0], file.Scopes[1]) {
		t.Fatal("divergent mode did not mutate any resource state across scopes")
	}
}

func fullResourceNames(scope cassette.Scope) []string {
	names := make([]string, 0, len(scope.Facts))
	for _, f := range scope.Facts {
		if name, ok := f.Payload["full_resource_name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func anyStateDiffers(a, b cassette.Scope) bool {
	byName := make(map[string]string, len(a.Facts))
	for _, f := range a.Facts {
		name, _ := f.Payload["full_resource_name"].(string)
		state, _ := f.Payload["state"].(string)
		byName[name] = state
	}
	for _, f := range b.Facts {
		name, _ := f.Payload["full_resource_name"].(string)
		state, _ := f.Payload["state"].(string)
		if prev, ok := byName[name]; ok && prev != state {
			return true
		}
	}
	return false
}
