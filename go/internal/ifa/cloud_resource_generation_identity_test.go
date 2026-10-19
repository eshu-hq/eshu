// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"sort"
	"testing"
)

// TestCloudResourceCassetteScopesEachResolveADeltaBaseline pins the database
// contract behind the #7319 preflight fence for the cloud_resource family
// cassette (#8358): every scope the cassette drives must resolve its own
// delta-baseline row.
//
// scope_generations.generation_id is TEXT PRIMARY KEY
// (go/internal/storage/postgres/migrations/002_scope_generations.sql), so one
// generation id can back exactly one scope's row. The fence resolves the
// baseline per scope (go/internal/projector/delta_baseline_preflight.go), so
// N scopes sharing one id leave N-1 projector items with no baseline and the
// fence correctly refuses them. The cassette must therefore carry one
// generation id per scope, matching the compiled Odù's per-scope ids.
func TestCloudResourceCassetteScopesEachResolveADeltaBaseline(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	envelopes := loadCassetteEnvelopes(t, CloudResourceFamilyCassetteFullPath(repoRoot))
	if len(envelopes) == 0 {
		t.Fatal("cloud-resource cassette contains no facts; an empty fixture makes this proof vacuous")
	}

	scopes := map[string]bool{}
	for _, envelope := range envelopes {
		if envelope.ScopeID == "" || envelope.GenerationID == "" {
			t.Fatal("cloud-resource cassette contains a blank scope/generation coordinate")
		}
		scopes[envelope.ScopeID] = true
	}

	// Emulate scope_generations' PRIMARY KEY on generation_id: one row per
	// id, so when several scopes share an id only one scope keeps a row.
	// First write wins here; on a live stack the later inserts are rejected
	// instead — either way a single row survives per shared id.
	generationScope := map[string]string{}
	for _, envelope := range envelopes {
		if _, exists := generationScope[envelope.GenerationID]; !exists {
			generationScope[envelope.GenerationID] = envelope.ScopeID
		}
	}

	var missing []string
	for scope := range scopes {
		found := false
		for _, rowScope := range generationScope {
			if rowScope == scope {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, scope)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		shared := map[string][]string{}
		for _, envelope := range envelopes {
			shared[envelope.GenerationID] = append(shared[envelope.GenerationID], envelope.ScopeID)
		}
		t.Fatalf("%d of %d cloud-resource scopes resolve no delta-baseline row (missing scopes %q); scope_generations holds one row per generation_id, so the shared id in %v leaves every non-first scope's projector item without a baseline and the #7319 fence refuses it",
			len(missing), len(scopes), missing, shared)
	}
}
