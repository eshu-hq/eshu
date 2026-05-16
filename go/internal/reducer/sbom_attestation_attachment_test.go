package reducer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testSBOMSubjectDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSBOMOtherDigest   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type stubSBOMAttestationAttachmentFactLoader struct {
	scopeFacts []facts.Envelope
	active     []facts.Envelope
	kindCalls  [][]string
	activeCall int
	digests    []string
}

func (s *stubSBOMAttestationAttachmentFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSBOMAttestationAttachmentFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSBOMAttestationAttachmentFactLoader) ListActiveSBOMAttestationAttachmentFacts(
	_ context.Context,
	digests []string,
) ([]facts.Envelope, error) {
	s.activeCall++
	s.digests = append([]string(nil), digests...)
	return append([]facts.Envelope(nil), s.active...), nil
}

type recordingSBOMAttestationAttachmentWriter struct {
	write SBOMAttestationAttachmentWrite
	calls int
}

func (w *recordingSBOMAttestationAttachmentWriter) WriteSBOMAttestationAttachments(
	_ context.Context,
	write SBOMAttestationAttachmentWrite,
) (SBOMAttestationAttachmentWriteResult, error) {
	w.calls++
	w.write = write
	return SBOMAttestationAttachmentWriteResult{
		CanonicalWrites: sbomAttestationAttachmentCanonicalWrites(write.Decisions),
		FactsWritten:    len(write.Decisions),
	}, nil
}

func TestBuildSBOMAttestationAttachmentDecisionsClassifiesSubjectsAndTrust(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		ociImageReferrerFact("referrer-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "application/vnd.in-toto+json"),
		sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		sbomComponentFact("component-verified", "doc-verified", "pkg:oci/api@1.0.0"),
		ociImageReferrerFact("referrer-unverified", testSBOMSubjectDigest, "sha256:2222222222222222222222222222222222222222222222222222222222222222", "application/vnd.cyclonedx+json"),
		sbomDocumentFact("doc-unverified", "doc-unverified", testSBOMSubjectDigest, "sha256:2222222222222222222222222222222222222222222222222222222222222222", "parsed", "failed"),
		sbomDocumentFact("doc-parse-only", "doc-parse-only", testSBOMSubjectDigest, "sha256:3333333333333333333333333333333333333333333333333333333333333333", "parsed", ""),
		sbomDocumentFact("doc-mismatch", "doc-mismatch", testSBOMOtherDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		sbomDocumentFact("doc-unknown", "doc-unknown", "", "sha256:4444444444444444444444444444444444444444444444444444444444444444", "parsed", "not_configured"),
		sbomDocumentFact("doc-unparseable", "doc-unparseable", testSBOMSubjectDigest, "sha256:5555555555555555555555555555555555555555555555555555555555555555", "unparseable", "not_configured"),
		attestationStatementFact("statement-verified", "stmt-verified", testSBOMSubjectDigest, "sha256:6666666666666666666666666666666666666666666666666666666666666666", "parsed", "verified"),
		attestationSignatureVerificationFact("verification-verified", "stmt-verified", "passed", "policy://prod"),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	assertSBOMAttachmentDecision(t, got["doc-verified"], SBOMAttachmentAttachedVerified, 1)
	assertSBOMAttachmentDecision(t, got["doc-unverified"], SBOMAttachmentAttachedUnverified, 1)
	assertSBOMAttachmentDecision(t, got["doc-parse-only"], SBOMAttachmentAttachedParseOnly, 1)
	assertSBOMAttachmentDecision(t, got["doc-mismatch"], SBOMAttachmentSubjectMismatch, 0)
	assertSBOMAttachmentDecision(t, got["doc-unknown"], SBOMAttachmentUnknownSubject, 0)
	assertSBOMAttachmentDecision(t, got["doc-unparseable"], SBOMAttachmentUnparseable, 0)
	assertSBOMAttachmentDecision(t, got["stmt-verified"], SBOMAttachmentAttachedVerified, 1)
	if got["doc-verified"].ComponentCount != 1 {
		t.Fatalf("ComponentCount = %d, want 1", got["doc-verified"].ComponentCount)
	}
	if got["doc-unverified"].VerificationStatus != "failed" {
		t.Fatalf("VerificationStatus = %q, want failed", got["doc-unverified"].VerificationStatus)
	}
	if got["doc-unparseable"].ParseStatus != "unparseable" {
		t.Fatalf("ParseStatus = %q, want unparseable", got["doc-unparseable"].ParseStatus)
	}
}

func TestSBOMAttestationAttachmentHandlerLoadsActiveSubjectEvidence(t *testing.T) {
	t.Parallel()

	loader := &stubSBOMAttestationAttachmentFactLoader{
		scopeFacts: []facts.Envelope{
			sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		},
		active: []facts.Envelope{
			ociImageReferrerFact("referrer-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "application/vnd.in-toto+json"),
		},
	}
	writer := &recordingSBOMAttestationAttachmentWriter{}
	handler := SBOMAttestationAttachmentHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sbom",
		ScopeID:      "sbom://oci/" + testSBOMSubjectDigest,
		GenerationID: "generation-sbom",
		SourceSystem: "sbom_attestation",
		Domain:       DomainSBOMAttestationAttachment,
		Cause:        "sbom attachment observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSBOMAttestationAttachments() calls = %d, want 1", writer.calls)
	}
	if loader.activeCall != 1 {
		t.Fatalf("ListActiveSBOMAttestationAttachmentFacts() calls = %d, want 1", loader.activeCall)
	}
	if got, want := strings.Join(loader.digests, ","), testSBOMSubjectDigest; got != want {
		t.Fatalf("active digests = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), strings.Join(sbomAttestationAttachmentFactKinds(), ","); got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if !strings.Contains(result.EvidenceSummary, "attached_verified=1") {
		t.Fatalf("EvidenceSummary = %q, want attached_verified count", result.EvidenceSummary)
	}
}

func TestPostgresSBOMAttestationAttachmentWriterPersistsAllStatuses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 15, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSBOMAttestationAttachmentWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteSBOMAttestationAttachments(context.Background(), SBOMAttestationAttachmentWrite{
		IntentID:     "intent-sbom",
		ScopeID:      "sbom://oci/" + testSBOMSubjectDigest,
		GenerationID: "generation-sbom",
		SourceSystem: "sbom_attestation",
		Cause:        "sbom attachment observed",
		Decisions: []SBOMAttestationAttachmentDecision{
			{
				DocumentID:         "doc-verified",
				DocumentDigest:     "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				SubjectDigest:      testSBOMSubjectDigest,
				AttachmentStatus:   SBOMAttachmentAttachedVerified,
				ParseStatus:        "parsed",
				VerificationStatus: "passed",
				VerificationPolicy: "policy://prod",
				ArtifactKind:       "sbom",
				Format:             "cyclonedx",
				SpecVersion:        "1.6",
				CanonicalWrites:    1,
				ComponentCount:     2,
				EvidenceFactIDs:    []string{"doc-fact", "referrer-fact"},
			},
			{
				DocumentID:         "doc-unparseable",
				DocumentDigest:     "sha256:5555555555555555555555555555555555555555555555555555555555555555",
				SubjectDigest:      testSBOMSubjectDigest,
				AttachmentStatus:   SBOMAttachmentUnparseable,
				ParseStatus:        "unparseable",
				VerificationStatus: "not_configured",
				ArtifactKind:       "sbom",
				EvidenceFactIDs:    []string{"doc-unparseable"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSBOMAttestationAttachments() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if result.FactsWritten != 2 {
		t.Fatalf("FactsWritten = %d, want 2", result.FactsWritten)
	}
	if len(db.execs) != 2 {
		t.Fatalf("execs = %d, want 2", len(db.execs))
	}
	if got, want := db.execs[0].args[3], sbomAttestationAttachmentFactKind; got != want {
		t.Fatalf("fact_kind = %#v, want %#v", got, want)
	}
	payload := unmarshalSBOMAttestationAttachmentPayload(t, db.execs[0].args[14])
	if got, want := payload["attachment_status"], string(SBOMAttachmentAttachedVerified); got != want {
		t.Fatalf("attachment_status = %#v, want %#v", got, want)
	}
	if got, want := payload["verification_status"], "passed"; got != want {
		t.Fatalf("verification_status = %#v, want %#v", got, want)
	}
	if _, exists := payload["vulnerability_priority"]; exists {
		t.Fatalf("payload must not emit vulnerability priority: %#v", payload)
	}
}

func sbomAttachmentDecisionsByDocument(
	decisions []SBOMAttestationAttachmentDecision,
) map[string]SBOMAttestationAttachmentDecision {
	out := make(map[string]SBOMAttestationAttachmentDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.DocumentID] = decision
	}
	return out
}

func assertSBOMAttachmentDecision(
	t *testing.T,
	decision SBOMAttestationAttachmentDecision,
	status SBOMAttachmentStatus,
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

func unmarshalSBOMAttestationAttachmentPayload(t *testing.T, raw any) map[string]any {
	t.Helper()

	var payload map[string]any
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal payload: %v", err)
	}
	return payload
}

func sbomDocumentFact(
	factID string,
	documentID string,
	subjectDigest string,
	documentDigest string,
	parseStatus string,
	verificationStatus string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMDocumentFactKind,
		Payload: map[string]any{
			"document_id":         documentID,
			"document_digest":     documentDigest,
			"subject_digest":      subjectDigest,
			"parse_status":        parseStatus,
			"verification_status": verificationStatus,
			"format":              "cyclonedx",
			"spec_version":        "1.6",
		},
	}
}

func sbomComponentFact(factID string, documentID string, purl string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMComponentFactKind,
		Payload: map[string]any{
			"document_id":  documentID,
			"component_id": purl,
			"purl":         purl,
		},
	}
}

func attestationStatementFact(
	factID string,
	statementID string,
	subjectDigest string,
	statementDigest string,
	parseStatus string,
	verificationStatus string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AttestationStatementFactKind,
		Payload: map[string]any{
			"statement_id":        statementID,
			"statement_digest":    statementDigest,
			"subject_digests":     []any{subjectDigest},
			"parse_status":        parseStatus,
			"verification_status": verificationStatus,
			"predicate_type":      "https://slsa.dev/provenance/v1",
			"attestation_format":  "in-toto",
			"attestation_version": "1.0",
		},
	}
}

func attestationSignatureVerificationFact(
	factID string,
	statementID string,
	result string,
	policy string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AttestationSignatureVerificationFactKind,
		Payload: map[string]any{
			"statement_id":        statementID,
			"verification_result": result,
			"verification_policy": policy,
			"verification_status": result,
		},
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
