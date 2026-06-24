// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	testSubjectDigest  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testReferrerDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestClaimedSourceParsesConfiguredCycloneDXSourceIntoStableRedactedFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "../sbomdocument/testdata/cyclonedx_image_subject.json")
	provider := &recordingProvider{
		doc: Document{
			Body:           raw,
			SourceURI:      "https://user:secret@sbom.example.com/private/sbom.json?token=secret#frag",
			SourceRecordID: "oci-referrer:" + testReferrerDigest,
			ObservedAt:     time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		},
	}
	target := TargetConfig{
		ScopeID:        "sbom://configured/example",
		SourceType:     SourceTypeConfigured,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		SubjectDigest:  testSubjectDigest,
		DocumentURL:    "https://user:secret@sbom.example.com/private/sbom.json?token=secret#frag",
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets:             []TargetConfig{target},
		Provider:            provider,
		Now:                 fixedNow,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	first := collectClaimed(t, source, target.ScopeID)
	second := collectClaimed(t, source, target.ScopeID)
	firstDoc := requireFactKind(t, first, facts.SBOMDocumentFactKind)
	secondDoc := requireFactKind(t, second, facts.SBOMDocumentFactKind)

	if got, want := first.Scope.ScopeKind, scope.KindSBOMAttestation; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	if got, want := first.Scope.CollectorKind, scope.CollectorSBOMAttestation; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := payloadString(firstDoc.Payload, "subject_digest"), testSubjectDigest; got != want {
		t.Fatalf("subject_digest = %q, want %q", got, want)
	}
	if got := payloadString(firstDoc.Payload, "verification_status"); got != "" {
		t.Fatalf("verification_status = %q, want parser-owned blank status", got)
	}
	if got, want := firstDoc.SourceRef.SourceURI, "https://sbom.example.com/private/sbom.json"; got != want {
		t.Fatalf("SourceRef.SourceURI = %q, want %q", got, want)
	}
	if strings.Contains(firstDoc.SourceRef.SourceURI, "secret") || strings.Contains(firstDoc.SourceRef.SourceURI, "token") {
		t.Fatalf("SourceRef.SourceURI leaked sensitive material: %q", firstDoc.SourceRef.SourceURI)
	}
	if got, want := payloadString(firstDoc.Payload, "document_id"), payloadString(secondDoc.Payload, "document_id"); got != want {
		t.Fatalf("document_id changed across identical observations: first=%q second=%q", got, want)
	}
	if firstDoc.StableFactKey != secondDoc.StableFactKey {
		t.Fatalf("StableFactKey changed across identical observations: first=%q second=%q", firstDoc.StableFactKey, secondDoc.StableFactKey)
	}
}

func TestClaimedSourceParsesMalformedSBOMAsWarningFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "../sbomdocument/testdata/cyclonedx_malformed.json")
	target := TargetConfig{
		ScopeID:        "sbom://configured/malformed",
		SourceType:     SourceTypeConfigured,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		DocumentURL:    "https://sbom.example.com/private/malformed.json",
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets:             []TargetConfig{target},
		Provider: &recordingProvider{doc: Document{
			Body:       raw,
			SourceURI:  "https://sbom.example.com/private/malformed.json",
			ObservedAt: time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		}},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected := collectClaimed(t, source, target.ScopeID)
	doc := requireFactKind(t, collected, facts.SBOMDocumentFactKind)
	warning := requireFactKind(t, collected, facts.SBOMWarningFactKind)

	if got, want := payloadString(doc.Payload, "parse_status"), "malformed"; got != want {
		t.Fatalf("parse_status = %q, want %q", got, want)
	}
	if got, want := payloadString(warning.Payload, "reason"), "malformed_document"; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}
}

func TestClaimedSourceUsesOCIReferrerTargetWithoutEmittingOCIFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "../sbomdocument/testdata/cyclonedx_image_subject.json")
	target := TargetConfig{
		ScopeID:        "sbom://oci/registry.example.com/library/example@sha256:1111",
		SourceType:     SourceTypeOCIReferrer,
		ArtifactKind:   ArtifactKindSBOM,
		DocumentFormat: DocumentFormatCycloneDX,
		Provider:       "oci",
		Registry:       "https://registry.example.com",
		Repository:     "library/example",
		SubjectDigest:  testSubjectDigest,
		ReferrerDigest: testReferrerDigest,
	}
	provider := &recordingProvider{doc: Document{Body: raw, ObservedAt: fixedNow()}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets:             []TargetConfig{target},
		Provider:            provider,
		Now:                 fixedNow,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected := collectClaimed(t, source, target.ScopeID)
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	if got, want := provider.calls[0].SourceType, SourceTypeOCIReferrer; got != want {
		t.Fatalf("provider SourceType = %q, want %q", got, want)
	}
	if got, want := provider.calls[0].ReferrerDigest, testReferrerDigest; got != want {
		t.Fatalf("provider ReferrerDigest = %q, want %q", got, want)
	}
	requireFactKind(t, collected, facts.SBOMDocumentFactKind)
	if fact := optionalFactKind(collected, facts.OCIImageReferrerFactKind); fact.FactID != "" {
		t.Fatalf("runtime emitted OCI referrer fact %q; OCI collector owns those facts", fact.FactID)
	}
	doc := requireFactKind(t, collected, facts.SBOMDocumentFactKind)
	if got, want := doc.SourceRef.SourceRecordID, testReferrerDigest; got != want {
		t.Fatalf("SourceRecordID = %q, want referrer digest %q", got, want)
	}
}

func TestClaimedSourceEmitsAttestationStatementAndSeparateVerificationFact(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": {"buildDefinition": {"buildType": "https://example.com/build"}}
	}`)
	target := TargetConfig{
		ScopeID:            "sbom://attestation/example",
		SourceType:         SourceTypeOCIReferrer,
		ArtifactKind:       ArtifactKindAttestation,
		DocumentFormat:     DocumentFormatInToto,
		Provider:           "oci",
		Registry:           "https://registry.example.com",
		Repository:         "library/example",
		SubjectDigest:      testSubjectDigest,
		ReferrerDigest:     testReferrerDigest,
		VerificationResult: "passed",
		VerificationPolicy: "cosign-keyless",
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets:             []TargetConfig{target},
		Provider:            &recordingProvider{doc: Document{Body: raw, ObservedAt: fixedNow()}},
		Now:                 fixedNow,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected := collectClaimed(t, source, target.ScopeID)
	statement := requireFactKind(t, collected, facts.AttestationStatementFactKind)
	verification := requireFactKind(t, collected, facts.AttestationSignatureVerificationFactKind)

	if got, want := payloadString(statement.Payload, "subject_digest"), testSubjectDigest; got != want {
		t.Fatalf("statement subject_digest = %q, want %q", got, want)
	}
	if got := payloadString(statement.Payload, "verification_status"); got != "" {
		t.Fatalf("statement verification_status = %q, want blank source statement status", got)
	}
	statementID := payloadString(statement.Payload, "statement_id")
	if statementID == "" {
		t.Fatal("statement_id is blank")
	}
	if got, want := payloadString(verification.Payload, "statement_id"), statementID; got != want {
		t.Fatalf("verification statement_id = %q, want %q", got, want)
	}
	if got, want := payloadString(verification.Payload, "verification_result"), "passed"; got != want {
		t.Fatalf("verification_result = %q, want %q", got, want)
	}
	if got, want := payloadString(verification.Payload, "verification_policy"), "cosign-keyless"; got != want {
		t.Fatalf("verification_policy = %q, want %q", got, want)
	}
}

func TestClaimedSourceRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets: []TargetConfig{{
			ScopeID:        "sbom://configured/example",
			SourceType:     SourceTypeConfigured,
			ArtifactKind:   ArtifactKindSBOM,
			DocumentFormat: DocumentFormatCycloneDX,
			DocumentURL:    "https://sbom.example.com/sbom.json",
		}},
		Provider: &recordingProvider{doc: Document{Body: []byte(`{}`)}},
		Now:      fixedNow,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: "sbom-attestation-test",
		ScopeID:             "sbom://configured/example",
		GenerationID:        "sbom-attestation:gen-1",
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want collector-kind rejection")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
}

type recordingProvider struct {
	doc   Document
	calls []TargetConfig
}

func (p *recordingProvider) FetchDocument(_ context.Context, target TargetConfig) (Document, error) {
	p.calls = append(p.calls, target)
	return p.doc, nil
}

type claimedResult struct {
	Scope scope.IngestionScope
	Facts []facts.Envelope
}

func collectClaimed(t *testing.T, source *ClaimedSource, scopeID string) claimedResult {
	t.Helper()

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorSBOMAttestation,
		CollectorInstanceID: "sbom-attestation-test",
		ScopeID:             scopeID,
		GenerationID:        "sbom-attestation:gen-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	return claimedResult{
		Scope: collected.Scope,
		Facts: drainFacts(collected.Facts),
	}
}

func requireFactKind(t *testing.T, collected claimedResult, kind string) facts.Envelope {
	t.Helper()

	fact := optionalFactKind(collected, kind)
	if fact.FactID == "" {
		t.Fatalf("fact kind %q not emitted", kind)
	}
	return fact
}

func optionalFactKind(collected claimedResult, kind string) facts.Envelope {
	for _, envelope := range collected.Facts {
		if envelope.FactKind == kind {
			return envelope
		}
	}
	return facts.Envelope{}
}

func drainFacts(ch <-chan facts.Envelope) []facts.Envelope {
	var out []facts.Envelope
	for envelope := range ch {
		out = append(out, envelope)
	}
	return out
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(value.(string))
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 15, 13, 0, 0, 0, time.UTC)
}
