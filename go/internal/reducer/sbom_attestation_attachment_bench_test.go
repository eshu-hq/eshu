// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkSBOMAttachmentCorpus builds a synthetic corpus of documentCount
// sbom.document facts, each with one component and one warning, plus a
// verified attestation statement/signature-verification pair per document —
// a realistic per-image SBOM+attestation shape for
// BenchmarkBuildSBOMAttestationAttachmentDecisions.
func benchmarkSBOMAttachmentCorpus(documentCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, documentCount*5)
	for i := 0; i < documentCount; i++ {
		docID := fmt.Sprintf("doc-%d", i)
		subjectDigest := fmt.Sprintf("sha256:%064d", i)
		envelopes = append(envelopes,
			facts.Envelope{
				FactID:   docID,
				FactKind: facts.SBOMDocumentFactKind,
				Payload: map[string]any{
					"document_id":         docID,
					"document_digest":     fmt.Sprintf("sha256:%064x", i),
					"subject_digest":      subjectDigest,
					"parse_status":        "parsed",
					"verification_status": "verified",
					"format":              "cyclonedx",
					"spec_version":        "1.6",
				},
			},
			facts.Envelope{
				FactID:   docID + "-component",
				FactKind: facts.SBOMComponentFactKind,
				Payload: map[string]any{
					"document_id":  docID,
					"component_id": docID + "-component",
					"purl":         fmt.Sprintf("pkg:npm/example-lib-%d@1.0.0", i),
					"name":         fmt.Sprintf("example-lib-%d", i),
					"version":      "1.0.0",
				},
			},
			facts.Envelope{
				FactID:   docID + "-warning",
				FactKind: facts.SBOMWarningFactKind,
				Payload: map[string]any{
					"document_id":      docID,
					"reason":           "missing_purl_identity",
					"summary":          "1 component missing purl identity",
					"occurrence_count": 1,
				},
			},
			facts.Envelope{
				FactID:   docID + "-referrer",
				FactKind: facts.OCIImageReferrerFactKind,
				Payload: map[string]any{
					"subject_digest":      subjectDigest,
					"referrer_digest":     fmt.Sprintf("sha256:%064x", i+1),
					"referrer_media_type": "application/vnd.cyclonedx+json",
					"artifact_type":       "application/vnd.cyclonedx+json",
				},
			},
			facts.Envelope{
				FactID:   docID + "-verification",
				FactKind: facts.AttestationSignatureVerificationFactKind,
				Payload: map[string]any{
					"statement_id":         "",
					"document_id":          docID,
					"verification_result":  "passed",
					"verification_status":  "passed",
					"verification_policy":  "policy://prod",
					"verification_subject": subjectDigest,
				},
			},
		)
	}
	return envelopes
}

// BenchmarkBuildSBOMAttestationAttachmentDecisions is the No-Regression
// Evidence benchmark for the sbom_attestation family's typed-decode migration
// (Contract System v1, Wave 4c): it measures the cost of classifying a
// realistic 5,000-document corpus (document + component + warning + OCI
// referrer + verification per document — 25,000 facts total) into attachment
// decisions, before versus after the sbom.document/sbom.component/
// sbom.warning/attestation.signature_verification decode sites moved from raw
// payloadString lookups to the sdk/go/factschema seam.
func BenchmarkBuildSBOMAttestationAttachmentDecisions(b *testing.B) {
	const documentCount = 5000
	envelopes := benchmarkSBOMAttachmentCorpus(documentCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decisions := BuildSBOMAttestationAttachmentDecisions(envelopes)
		if len(decisions) != documentCount {
			b.Fatalf("len(decisions) = %d, want %d", len(decisions), documentCount)
		}
	}
}
