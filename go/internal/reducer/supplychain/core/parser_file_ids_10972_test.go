// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactParserFileRepositoryIDs10972 pins the repository scope
// of the parser-file path across the #10972 hoist: the same sorted IDs with
// or without the per-dependency key rebuild, blank repository dependencies
// skipped, nil when no npm affected package feeds the scope, and identical
// output across repeated calls despite Go map iteration order inside the
// match builder.
func TestSupplyChainImpactParserFileRepositoryIDs10972(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	advisory := func(id, cve, name string) facts.Envelope {
		return vulnerabilityAffectedPackageFact(id, cve, "pkg:npm/"+name, "npm", name, "1.0.0", "2.0.0")
	}
	dep := func(repo, name string) facts.Envelope {
		return packageManifestDependencyFactWithMetadata(
			repo, "api", "package-lock.json", name, "npm", "1.0.0",
			observedAt, map[string]any{"lockfile": true},
		)
	}

	envelopes := []facts.Envelope{
		advisory("affected-bench-a", "CVE-2026-0001", "alpha"),
		advisory("affected-bench-b", "CVE-2026-0002", "beta"),
		dep("repository:file-b", "beta"),
		dep("repository:file-a", "alpha"),
		dep("", "alpha"),
		dep("repository:file-c", "unrelated"),
	}

	first := supplyChainImpactParserFileRepositoryIDs(envelopes)
	if want := []string{"repository:file-a", "repository:file-b"}; !slices.Equal(first, want) {
		t.Fatalf("repository IDs = %v, want %v (sorted, blank/unmatched skipped)", first, want)
	}
	for range 50 {
		if again := supplyChainImpactParserFileRepositoryIDs(envelopes); !slices.Equal(again, first) {
			t.Fatalf("repeated call = %v, want %v (deterministic)", again, first)
		}
	}

	if got := supplyChainImpactParserFileRepositoryIDs([]facts.Envelope{dep("repository:file-a", "alpha")}); got != nil {
		t.Fatalf("no-affected scope = %v, want nil", got)
	}
}
