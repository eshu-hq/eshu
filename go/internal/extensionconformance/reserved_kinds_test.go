// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestTrustedCoreFactsKeepsExactGrantTuples proves authorization cannot be
// assembled from a grant for one collector and a different grant's version.
func TestTrustedCoreFactsKeepsExactGrantTuples(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	manifest := component.Manifest{}
	manifest.Metadata.ID = "dev.example.collector.docs"
	manifest.Metadata.Version = "0.1.0"
	manifest.Spec.CollectorKinds = []string{"docs", "other"}
	manifest.Spec.EmittedFacts = []component.FactFamily{{Kind: "documentation_section", SchemaVersions: []string{"1.0.0", "1.1.0"}}}
	grants := []component.ProducerGrant{
		{ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version, Kind: "documentation_section", Scope: "docs", SchemaVersions: []string{"1.0.0"}, ExpiresAt: now.Add(time.Hour)},
		{ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version, Kind: "documentation_section", Scope: "other", SchemaVersions: []string{"1.1.0"}, ExpiresAt: now.Add(time.Hour)},
	}
	context := trustedCoreFacts(manifest, grants, now)
	if context == nil || len(context.Facts) != 1 {
		t.Fatalf("trustedCoreFacts() = %#v, want one authorization", context)
	}
	emissions := context.Facts[0].Emissions
	if len(emissions) != 2 || emissions[0].CollectorKind != "docs" || !slices.Equal(emissions[0].SchemaVersions, []string{"1.0.0"}) ||
		emissions[1].CollectorKind != "other" || !slices.Equal(emissions[1].SchemaVersions, []string{"1.1.0"}) {
		t.Fatalf("emissions = %#v, want exact independent tuples", emissions)
	}
	if !slices.Contains(facts.CoreFactKinds(), "documentation_section") {
		t.Fatal("documentation_section missing from full reserved inventory")
	}
}
