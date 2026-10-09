// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CatalogScope builds a git repository scope whose partition key is the
// repository id. It merges the identical catalogTestScope twins from
// ingestion_catalog_cache_test.go and activation/fixtures_test.go (#7648).
func CatalogScope(scopeID, repoID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repoID,
	}
}

// CatalogGeneration builds a pending snapshot generation for scopeID. It
// merges the identical catalogTestGeneration twins from
// ingestion_catalog_cache_test.go and activation/fixtures_test.go (#7648).
func CatalogGeneration(scopeID, generationID string, now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}
