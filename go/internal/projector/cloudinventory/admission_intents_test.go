// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudinventory

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "gcp:acct:demo"
	testGenerationID = "gcp-generation-1"
)

func admissionEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      factKind,
		CollectorKind: collectorKind,
		SourceRef:     facts.Ref{SourceSystem: sourceSystem},
	}
}

// TestBuildCloudInventoryAdmissionReducerIntent proves the builder anchors to
// the earliest provider cloud-inventory source fact in original input order
// across the three candidate kinds, that each provider kind triggers on its
// own, that the source-system label prefers SourceRef.SourceSystem and falls
// back to CollectorKind, and that a generation without a provider
// cloud-inventory source fact enqueues nothing.
func TestBuildCloudInventoryAdmissionReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues once from the earliest source fact across provider kinds", func(t *testing.T) {
		t.Parallel()
		// The Azure fact is placed before the GCP fact on purpose: input
		// order picks the anchor, not a per-kind priority.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "file"},
			admissionEnvelope("fact-azure-1", facts.AzureCloudResourceFactKind, "azure", ""),
			admissionEnvelope("fact-gcp-1", facts.GCPCloudResourceFactKind, "gcp", ""),
		})
		got, ok := BuildCloudInventoryAdmissionReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainCloudInventoryAdmission,
			EntityKey: "cloud_inventory_admission:" + testScopeID,
			Reason:    "provider cloud-inventory source facts observed",
			FactID:    "fact-azure-1", SourceSystem: "azure",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("every provider source kind triggers on its own", func(t *testing.T) {
		t.Parallel()
		for kind, provider := range map[string]string{
			facts.AWSResourceFactKind:        "aws",
			facts.GCPCloudResourceFactKind:   "gcp",
			facts.AzureCloudResourceFactKind: "azure",
		} {
			lookup := projectorintent.NewFactLookup([]facts.Envelope{
				{FactID: "decoy-1", FactKind: "file"},
				admissionEnvelope("anchor-"+kind, kind, provider, ""),
			})
			got, ok := BuildCloudInventoryAdmissionReducerIntent(testScopeID, testGenerationID, lookup)
			if !ok {
				t.Fatalf("kind %q: ok = false, want true", kind)
			}
			if got.FactID != "anchor-"+kind {
				t.Fatalf("kind %q: FactID = %q, want %q", kind, got.FactID, "anchor-"+kind)
			}
			if got.SourceSystem != provider {
				t.Fatalf("kind %q: SourceSystem = %q, want %q", kind, got.SourceSystem, provider)
			}
		}
	})

	t.Run("falls back to CollectorKind when SourceRef is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			admissionEnvelope("fact-gcp-1", facts.GCPCloudResourceFactKind, "  ", "gcp"),
		})
		got, ok := BuildCloudInventoryAdmissionReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "gcp" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "gcp")
		}
	})

	t.Run("does not queue for non-inventory evidence", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: "file"},
		})
		got, ok := BuildCloudInventoryAdmissionReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) for non-inventory evidence, want zero intent and false", got, ok)
		}
	})

	t.Run("does not queue for an empty generation", func(t *testing.T) {
		t.Parallel()
		got, ok := BuildCloudInventoryAdmissionReducerIntent(testScopeID, testGenerationID,
			projectorintent.NewFactLookup(nil))
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) for an empty generation, want zero intent and false", got, ok)
		}
	})
}
