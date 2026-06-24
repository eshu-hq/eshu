// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	runtimeSubjectDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	runtimeOtherDigest   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestRuntimeSBOMFactsAttachToOCIReferrerSubjectTruth(t *testing.T) {
	t.Parallel()

	runtimeFacts := runtimeSBOMFacts(t)
	doc := firstFactKind(t, runtimeFacts, facts.SBOMDocumentFactKind)
	documentDigest := payloadString(doc.Payload, "document_digest")

	decisions := reducer.BuildSBOMAttestationAttachmentDecisions(append(
		runtimeFacts,
		ociImageReferrerFact("referrer-runtime", runtimeSubjectDigest, documentDigest, "application/vnd.cyclonedx+json"),
	))
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1: %#v", len(decisions), decisions)
	}
	assertRuntimeAttachmentDecision(t, decisions[0], reducer.SBOMAttachmentAttachedParseOnly, 1)
	if got, want := decisions[0].VerificationStatus, "not_configured"; got != want {
		t.Fatalf("VerificationStatus = %q, want %q", got, want)
	}
	if decisions[0].ComponentCount == 0 {
		t.Fatal("ComponentCount = 0, want runtime SBOM component evidence attached")
	}
}

func TestRuntimeSBOMFactsPreserveSubjectMismatchEvidence(t *testing.T) {
	t.Parallel()

	runtimeFacts := runtimeSBOMFacts(t)
	doc := firstFactKind(t, runtimeFacts, facts.SBOMDocumentFactKind)
	documentDigest := payloadString(doc.Payload, "document_digest")

	decisions := reducer.BuildSBOMAttestationAttachmentDecisions(append(
		runtimeFacts,
		ociImageReferrerFact("referrer-runtime-mismatch", runtimeOtherDigest, documentDigest, "application/vnd.cyclonedx+json"),
	))
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1: %#v", len(decisions), decisions)
	}
	assertRuntimeAttachmentDecision(t, decisions[0], reducer.SBOMAttachmentSubjectMismatch, 0)
}

func runtimeSBOMFacts(t *testing.T) []facts.Envelope {
	t.Helper()

	raw, err := os.ReadFile("../collector/sbomdocument/testdata/cyclonedx_image_subject.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	provider := runtimeAttachmentProvider{doc: sbomruntime.Document{
		Body:           raw,
		SourceURI:      "https://sbom.example.com/image.cdx.json",
		SourceRecordID: "referrer-runtime",
		ObservedAt:     time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	}}
	source, err := sbomruntime.NewClaimedSource(sbomruntime.SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets: []sbomruntime.TargetConfig{{
			ScopeID:        "sbom://runtime/attachment",
			SourceType:     sbomruntime.SourceTypeConfigured,
			ArtifactKind:   sbomruntime.ArtifactKindSBOM,
			DocumentFormat: sbomruntime.DocumentFormatCycloneDX,
			DocumentURL:    "https://sbom.example.com/image.cdx.json",
		}},
		Provider: provider,
		Now:      func() time.Time { return time.Date(2026, 5, 15, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorSBOMAttestation,
		CollectorInstanceID: "sbom-attestation-test",
		SourceSystem:        string(scope.CollectorSBOMAttestation),
		ScopeID:             "sbom://runtime/attachment",
		GenerationID:        "sbom-attestation:runtime",
		SourceRunID:         "sbom-attestation:runtime",
		CurrentFencingToken: 9,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	var out []facts.Envelope
	for envelope := range collected.Facts {
		out = append(out, envelope)
	}
	return out
}

func firstFactKind(t *testing.T, envs []facts.Envelope, kind string) facts.Envelope {
	t.Helper()

	for _, envelope := range envs {
		if envelope.FactKind == kind {
			return envelope
		}
	}
	t.Fatalf("fact kind %q not emitted", kind)
	return facts.Envelope{}
}

func assertRuntimeAttachmentDecision(
	t *testing.T,
	decision reducer.SBOMAttestationAttachmentDecision,
	status reducer.SBOMAttachmentStatus,
	canonicalWrites int,
) {
	t.Helper()

	if decision.AttachmentStatus != status {
		t.Fatalf("AttachmentStatus = %q, want %q for %#v", decision.AttachmentStatus, status, decision)
	}
	if decision.CanonicalWrites != canonicalWrites {
		t.Fatalf("CanonicalWrites = %d, want %d for %#v", decision.CanonicalWrites, canonicalWrites, decision)
	}
}

func ociImageReferrerFact(
	factID string,
	subjectDigest string,
	referrerDigest string,
	artifactType string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.OCIImageReferrerFactKind,
		Payload: map[string]any{
			"subject_digest":      subjectDigest,
			"referrer_digest":     referrerDigest,
			"referrer_media_type": artifactType,
			"artifact_type":       artifactType,
		},
	}
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

type runtimeAttachmentProvider struct {
	doc sbomruntime.Document
}

func (p runtimeAttachmentProvider) FetchDocument(context.Context, sbomruntime.TargetConfig) (sbomruntime.Document, error) {
	return p.doc, nil
}
