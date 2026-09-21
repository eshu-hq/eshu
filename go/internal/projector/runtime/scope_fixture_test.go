// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "github.com/eshu-hq/eshu/go/internal/scope"

// testScope is the single-repository ingestion scope the projection tests in
// this package drive. It mirrors the fixture the canonical package's own tests
// use, because a test helper cannot cross a package boundary and the two suites
// must assert against the same scope identity.
func testScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-1",
		SourceSystem:  "test",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "part-1",
		Metadata: map[string]string{
			"repo_id":   "repo-abc",
			"repo_path": "/repos/my-project",
		},
	}
}

// testGeneration is the active generation of [testScope].
func testGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "gen-1",
		ScopeID:      "scope-1",
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}
