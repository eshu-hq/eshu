// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomattestation

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "sbom://remote-e2e/team-api"
	testGenerationID = "generation-sbom"
)

func attachmentEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      factKind,
		SchemaVersion: facts.SBOMAttestationSchemaVersionV1,
		CollectorKind: collectorKind,
		SourceRef:     facts.Ref{SourceSystem: sourceSystem},
	}
}

// TestBuildSBOMAttestationAttachmentReducerIntent proves the builder anchors
// to the earliest subject-anchor fact in original input order across the
// three candidate kinds, that each candidate kind triggers on its own, that
// the source-system label prefers SourceRef.SourceSystem and falls back to
// CollectorKind, and that component-only or anchor-free generations enqueue
// nothing.
func TestBuildSBOMAttestationAttachmentReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues once from the earliest subject anchor across kinds", func(t *testing.T) {
		t.Parallel()
		// The attestation statement is placed before the SBOM document on
		// purpose: input order picks the anchor, not a per-kind priority.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			{FactID: "decoy-1", FactKind: facts.SBOMComponentFactKind},
			attachmentEnvelope("fact-attestation-statement", facts.AttestationStatementFactKind, "sbom_attestation", ""),
			attachmentEnvelope("fact-sbom-doc", facts.SBOMDocumentFactKind, "sbom_attestation", ""),
		})
		got, ok := BuildSBOMAttestationAttachmentReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainSBOMAttestationAttachment,
			EntityKey: "sbom_attestation_attachment:" + testScopeID,
			Reason:    "sbom or attestation subject evidence observed",
			FactID:    "fact-attestation-statement", SourceSystem: "sbom_attestation",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("every candidate kind triggers on its own", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []string{
			facts.SBOMDocumentFactKind,
			facts.AttestationStatementFactKind,
			facts.OCIImageReferrerFactKind,
		} {
			lookup := projectorintent.NewFactLookup([]facts.Envelope{
				{FactID: "decoy-1", FactKind: facts.SBOMComponentFactKind},
				attachmentEnvelope("anchor-"+kind, kind, "sbom_attestation", ""),
			})
			got, ok := BuildSBOMAttestationAttachmentReducerIntent(testScopeID, testGenerationID, lookup)
			if !ok {
				t.Fatalf("kind %q: ok = false, want true", kind)
			}
			if got.FactID != "anchor-"+kind {
				t.Fatalf("kind %q: FactID = %q, want %q", kind, got.FactID, "anchor-"+kind)
			}
		}
	})

	t.Run("falls back to CollectorKind when SourceRef is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			attachmentEnvelope("fact-attestation-statement", facts.AttestationStatementFactKind, "  ", "sbom_attestation"),
		})
		got, ok := BuildSBOMAttestationAttachmentReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "sbom_attestation" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "sbom_attestation")
		}
	})

	t.Run("does not queue for component-only evidence", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			attachmentEnvelope("component-only", facts.SBOMComponentFactKind, "sbom_attestation", ""),
		})
		got, ok := BuildSBOMAttestationAttachmentReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) for component-only evidence, want zero intent and false", got, ok)
		}
	})

	t.Run("does not queue without a subject anchor", func(t *testing.T) {
		t.Parallel()
		got, ok := BuildSBOMAttestationAttachmentReducerIntent(testScopeID, testGenerationID,
			projectorintent.NewFactLookup(nil))
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t) without a subject anchor, want zero intent and false", got, ok)
		}
	})
}
