// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagesource

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "package-registry:npm:team-api"
	testGenerationID = "generation-1"
)

func sourceHintEnvelope(factID, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          testScopeID,
		GenerationID:     testGenerationID,
		FactKind:         facts.PackageRegistrySourceHintFactKind,
		SchemaVersion:    facts.PackageRegistrySourceHintSchemaVersion,
		CollectorKind:    collectorKind,
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef:        facts.Ref{SourceSystem: sourceSystem},
		Payload: map[string]any{
			"package_id":     "pkg:npm://registry.example/team-api",
			"hint_kind":      "repository",
			"normalized_url": "https://github.com/acme/team-api",
		},
	}
}

func packageIdentityEnvelope(factID, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          testScopeID,
		GenerationID:     testGenerationID,
		FactKind:         facts.PackageRegistryPackageFactKind,
		SchemaVersion:    facts.PackageRegistryPackageSchemaVersion,
		CollectorKind:    collectorKind,
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		SourceRef:        facts.Ref{SourceSystem: sourceSystem},
		Payload: map[string]any{
			"package_id":      "npm://registry.npmjs.org/vite",
			"ecosystem":       "npm",
			"raw_name":        "vite",
			"normalized_name": "vite",
		},
	}
}

// TestBuildPackageSourceCorrelationReducerIntent proves the builder anchors to
// the earliest package_registry.source_hint fact in original input order, that
// a source hint outranks an identity fact placed ahead of it (kind priority,
// not input position), that it falls back to the earliest
// package_registry.package fact when no hint exists, that it falls back to
// CollectorKind when SourceRef's SourceSystem is blank, and that a generation
// carrying neither kind enqueues nothing.
func TestBuildPackageSourceCorrelationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from the earliest source hint, outranking an earlier identity fact", func(t *testing.T) {
		t.Parallel()
		// The identity fact is placed first on purpose: the builder checks
		// source hints before identity, so the hint must still anchor.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			packageIdentityEnvelope("fact-package-1", "package_registry", "package_registry"),
			sourceHintEnvelope("fact-source-1", "package_registry", "package_registry"),
			sourceHintEnvelope("fact-source-2", "package_registry", "package_registry"),
		})
		got, ok := BuildPackageSourceCorrelationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID:      testScopeID,
			GenerationID: testGenerationID,
			Domain:       reducer.DomainPackageSourceCorrelation,
			EntityKey:    "package_source_correlation:" + testScopeID,
			Reason:       "package registry source hints observed",
			FactID:       "fact-source-1",
			SourceSystem: "package_registry",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("falls back to the earliest package identity fact when no hint exists", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "code_symbol_reference"},
			packageIdentityEnvelope("fact-package-1", "package_registry", "package_registry"),
			packageIdentityEnvelope("fact-package-2", "package_registry", "package_registry"),
		})
		got, ok := BuildPackageSourceCorrelationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID:      testScopeID,
			GenerationID: testGenerationID,
			Domain:       reducer.DomainPackageSourceCorrelation,
			EntityKey:    "package_source_correlation:" + testScopeID,
			Reason:       "package registry identity observed",
			FactID:       "fact-package-1",
			SourceSystem: "package_registry",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("falls back to CollectorKind when SourceSystem is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			sourceHintEnvelope("fact-source-1", "  ", "package_registry_collector"),
		})
		got, ok := BuildPackageSourceCorrelationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "package_registry_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "package_registry_collector")
		}
	})

	t.Run("does not queue without a source hint or package identity fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "code_symbol_reference"},
			{FactID: "decoy-2", FactKind: facts.PackageRegistryPackageVersionFactKind},
		})
		got, ok := BuildPackageSourceCorrelationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) without a trigger kind, want zero intent and false", got, ok)
		}
	})
}
