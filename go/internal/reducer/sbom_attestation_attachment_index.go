package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type sbomAttachmentDocument struct {
	factID             string
	documentID         string
	documentDigest     string
	subjectDigest      string
	parseStatus        string
	verificationStatus string
	verificationPolicy string
	artifactKind       string
	format             string
	specVersion        string
}

type sbomAttachmentReferrer struct {
	factID         string
	subjectDigest  string
	referrerDigest string
}

type sbomAttachmentIndex struct {
	documents     map[string]sbomAttachmentDocument
	components    map[string][]facts.Envelope
	referrers     map[string][]sbomAttachmentReferrer
	verifications map[string]facts.Envelope
	warnings      map[string][]facts.Envelope
}

func buildSBOMAttachmentIndex(envelopes []facts.Envelope) sbomAttachmentIndex {
	index := sbomAttachmentIndex{
		documents:     map[string]sbomAttachmentDocument{},
		components:    map[string][]facts.Envelope{},
		referrers:     map[string][]sbomAttachmentReferrer{},
		verifications: map[string]facts.Envelope{},
		warnings:      map[string][]facts.Envelope{},
	}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.SBOMDocumentFactKind:
			doc := sbomDocumentFromEnvelope(envelope)
			if doc.documentID != "" {
				index.documents[doc.documentID] = doc
			}
		case facts.AttestationStatementFactKind:
			doc := attestationDocumentFromEnvelope(envelope)
			if doc.documentID != "" {
				index.documents[doc.documentID] = doc
			}
		case facts.SBOMComponentFactKind:
			documentID := payloadString(envelope.Payload, "document_id")
			if documentID != "" {
				index.components[documentID] = append(index.components[documentID], envelope)
			}
		case facts.OCIImageReferrerFactKind:
			referrer := sbomReferrerFromEnvelope(envelope)
			if referrer.referrerDigest != "" {
				index.referrers[referrer.referrerDigest] = append(index.referrers[referrer.referrerDigest], referrer)
			}
		case facts.AttestationSignatureVerificationFactKind:
			key := firstNonBlank(payloadString(envelope.Payload, "statement_id"), payloadString(envelope.Payload, "document_id"))
			if key != "" {
				index.verifications[key] = envelope
			}
		case facts.SBOMWarningFactKind:
			key := firstNonBlank(payloadString(envelope.Payload, "document_id"), payloadString(envelope.Payload, "statement_id"))
			if key != "" {
				index.warnings[key] = append(index.warnings[key], envelope)
			}
		}
	}
	return index
}

func sbomDocumentFromEnvelope(envelope facts.Envelope) sbomAttachmentDocument {
	documentID := firstNonBlank(payloadString(envelope.Payload, "document_id"), envelope.FactID)
	return sbomAttachmentDocument{
		factID:             envelope.FactID,
		documentID:         documentID,
		documentDigest:     payloadString(envelope.Payload, "document_digest"),
		subjectDigest:      payloadString(envelope.Payload, "subject_digest"),
		parseStatus:        defaultStatus(payloadString(envelope.Payload, "parse_status"), "parsed"),
		verificationStatus: normalizedVerificationStatus(payloadString(envelope.Payload, "verification_status")),
		verificationPolicy: payloadString(envelope.Payload, "verification_policy"),
		artifactKind:       "sbom",
		format:             payloadString(envelope.Payload, "format"),
		specVersion:        payloadString(envelope.Payload, "spec_version"),
	}
}

func attestationDocumentFromEnvelope(envelope facts.Envelope) sbomAttachmentDocument {
	statementID := firstNonBlank(payloadString(envelope.Payload, "statement_id"), envelope.FactID)
	return sbomAttachmentDocument{
		factID:             envelope.FactID,
		documentID:         statementID,
		documentDigest:     firstNonBlank(payloadString(envelope.Payload, "statement_digest"), payloadString(envelope.Payload, "payload_digest")),
		subjectDigest:      firstPayloadString(envelope.Payload, "subject_digest", "subject_digests"),
		parseStatus:        defaultStatus(payloadString(envelope.Payload, "parse_status"), "parsed"),
		verificationStatus: normalizedVerificationStatus(payloadString(envelope.Payload, "verification_status")),
		verificationPolicy: payloadString(envelope.Payload, "verification_policy"),
		artifactKind:       "attestation",
		format:             firstNonBlank(payloadString(envelope.Payload, "attestation_format"), "in-toto"),
		specVersion:        firstNonBlank(payloadString(envelope.Payload, "attestation_version"), payloadString(envelope.Payload, "predicate_type")),
	}
}

func sbomReferrerFromEnvelope(envelope facts.Envelope) sbomAttachmentReferrer {
	return sbomAttachmentReferrer{
		factID:         envelope.FactID,
		subjectDigest:  payloadString(envelope.Payload, "subject_digest"),
		referrerDigest: payloadString(envelope.Payload, "referrer_digest"),
	}
}

func classifySBOMAttachmentDocument(
	doc sbomAttachmentDocument,
	index sbomAttachmentIndex,
) SBOMAttestationAttachmentDecision {
	verification := doc.verificationStatus
	policy := doc.verificationPolicy
	evidence := []string{doc.factID}
	if verifyFact, ok := index.verifications[doc.documentID]; ok {
		verification = normalizedVerificationStatus(firstNonBlank(
			payloadString(verifyFact.Payload, "verification_result"),
			payloadString(verifyFact.Payload, "verification_status"),
		))
		policy = firstNonBlank(payloadString(verifyFact.Payload, "verification_policy"), policy)
		evidence = append(evidence, verifyFact.FactID)
	}
	for _, referrer := range index.referrers[doc.documentDigest] {
		evidence = append(evidence, referrer.factID)
		if doc.subjectDigest != "" && referrer.subjectDigest != "" && doc.subjectDigest != referrer.subjectDigest {
			return sbomAttachmentDecision(doc, SBOMAttachmentSubjectMismatch, verification, policy, 0, "document subject does not match OCI referrer subject", evidence, index)
		}
	}
	if isUnparseableStatus(doc.parseStatus) {
		return sbomAttachmentDecision(doc, SBOMAttachmentUnparseable, verification, policy, 0, "source document could not be parsed into stable facts", evidence, index)
	}
	if doc.subjectDigest == "" {
		return sbomAttachmentDecision(doc, SBOMAttachmentUnknownSubject, verification, policy, 0, "document parsed but has no artifact subject digest", evidence, index)
	}
	switch verification {
	case "passed", "verified":
		return sbomAttachmentDecision(doc, SBOMAttachmentAttachedVerified, "passed", policy, 1, "subject digest matched and verification passed", evidence, index)
	case "failed", "unverified":
		return sbomAttachmentDecision(doc, SBOMAttachmentAttachedUnverified, verification, policy, 1, "subject digest matched but verification did not pass", evidence, index)
	default:
		return sbomAttachmentDecision(doc, SBOMAttachmentAttachedParseOnly, defaultStatus(verification, "not_configured"), policy, 1, "subject digest matched with parse-only evidence", evidence, index)
	}
}

func sbomAttachmentDecision(
	doc sbomAttachmentDocument,
	status SBOMAttachmentStatus,
	verificationStatus string,
	verificationPolicy string,
	canonicalWrites int,
	reason string,
	evidence []string,
	index sbomAttachmentIndex,
) SBOMAttestationAttachmentDecision {
	components := componentEvidenceRows(index.components[doc.documentID])
	return SBOMAttestationAttachmentDecision{
		DocumentID:         doc.documentID,
		DocumentDigest:     doc.documentDigest,
		SubjectDigest:      doc.subjectDigest,
		AttachmentStatus:   status,
		ParseStatus:        doc.parseStatus,
		VerificationStatus: defaultStatus(verificationStatus, "not_configured"),
		VerificationPolicy: verificationPolicy,
		ArtifactKind:       doc.artifactKind,
		Format:             doc.format,
		SpecVersion:        doc.specVersion,
		Reason:             reason,
		CanonicalWrites:    canonicalWrites,
		ComponentCount:     len(components),
		ComponentEvidence:  components,
		WarningSummaries:   warningSummaries(index.warnings[doc.documentID]),
		EvidenceFactIDs:    uniqueSortedStrings(evidence),
		SourceLayerKinds:   []string{"reported", "observed_resource"},
	}
}

func componentEvidenceRows(components []facts.Envelope) []map[string]string {
	out := make([]map[string]string, 0, len(components))
	for _, component := range components {
		out = append(out, map[string]string{
			"component_id": payloadString(component.Payload, "component_id"),
			"name":         payloadString(component.Payload, "name"),
			"version":      payloadString(component.Payload, "version"),
			"purl":         payloadString(component.Payload, "purl"),
			"cpe":          payloadString(component.Payload, "cpe"),
			"fact_id":      component.FactID,
		})
	}
	return out
}

func warningSummaries(warnings []facts.Envelope) []string {
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if summary := firstNonBlank(payloadString(warning.Payload, "summary"), payloadString(warning.Payload, "reason")); summary != "" {
			out = append(out, summary)
		}
	}
	return uniqueSortedStrings(out)
}

func isUnparseableStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "unparseable", "parse_failed", "malformed", "invalid":
		return true
	default:
		return false
	}
}

func defaultStatus(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func firstPayloadString(payload map[string]any, scalarKey string, sliceKey string) string {
	if value := payloadString(payload, scalarKey); value != "" {
		return value
	}
	raw, ok := payload[sliceKey]
	if !ok {
		return ""
	}
	switch typed := raw.(type) {
	case []string:
		for _, value := range typed {
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	case []any:
		for _, value := range typed {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				return text
			}
		}
	}
	return ""
}
