// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strconv"
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
	ambiguousSubject   bool
}

type sbomAttachmentReferrer struct {
	factID         string
	subjectDigest  string
	referrerDigest string
}

type sbomAttachmentImageAnchor struct {
	factID       string
	digest       string
	outcome      string
	writes       int
	repositories []string
	workloads    []string
	services     []string
}

type sbomAttachmentAnchorContext struct {
	repositories    []string
	workloads       []string
	services        []string
	evidenceFactIDs []string
	missingEvidence []string
}

type sbomAttachmentIndex struct {
	documents     map[string]sbomAttachmentDocument
	components    map[string][]facts.Envelope
	referrers     map[string][]sbomAttachmentReferrer
	images        map[string][]sbomAttachmentImageAnchor
	verifications map[string]facts.Envelope
	warnings      map[string][]facts.Envelope
}

func buildSBOMAttachmentIndex(envelopes []facts.Envelope) sbomAttachmentIndex {
	index := sbomAttachmentIndex{
		documents:     map[string]sbomAttachmentDocument{},
		components:    map[string][]facts.Envelope{},
		referrers:     map[string][]sbomAttachmentReferrer{},
		images:        map[string][]sbomAttachmentImageAnchor{},
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
		case containerImageIdentityFactKind:
			image := sbomAttachmentImageAnchorFromEnvelope(envelope)
			if image.digest != "" {
				index.images[image.digest] = append(index.images[image.digest], image)
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
	subjectDigests := payloadStrings(envelope.Payload, "subject_digest", "subject_digests")
	ambiguousSubject := len(subjectDigests) > 1
	subjectDigest := ""
	if len(subjectDigests) == 1 {
		subjectDigest = subjectDigests[0]
	}
	return sbomAttachmentDocument{
		factID:             envelope.FactID,
		documentID:         statementID,
		documentDigest:     firstNonBlank(payloadString(envelope.Payload, "statement_digest"), payloadString(envelope.Payload, "payload_digest")),
		subjectDigest:      subjectDigest,
		parseStatus:        defaultStatus(payloadString(envelope.Payload, "parse_status"), "parsed"),
		verificationStatus: normalizedVerificationStatus(payloadString(envelope.Payload, "verification_status")),
		verificationPolicy: payloadString(envelope.Payload, "verification_policy"),
		artifactKind:       "attestation",
		format:             firstNonBlank(payloadString(envelope.Payload, "attestation_format"), "in-toto"),
		specVersion:        firstNonBlank(payloadString(envelope.Payload, "attestation_version"), payloadString(envelope.Payload, "predicate_type")),
		ambiguousSubject:   ambiguousSubject,
	}
}

func sbomReferrerFromEnvelope(envelope facts.Envelope) sbomAttachmentReferrer {
	return sbomAttachmentReferrer{
		factID:         envelope.FactID,
		subjectDigest:  payloadString(envelope.Payload, "subject_digest"),
		referrerDigest: payloadString(envelope.Payload, "referrer_digest"),
	}
}

func sbomAttachmentImageAnchorFromEnvelope(envelope facts.Envelope) sbomAttachmentImageAnchor {
	return sbomAttachmentImageAnchor{
		factID:       envelope.FactID,
		digest:       payloadString(envelope.Payload, "digest"),
		outcome:      payloadString(envelope.Payload, "outcome"),
		writes:       supplyChainInt(envelope.Payload, "canonical_writes"),
		repositories: payloadStrings(envelope.Payload, "source_repository_id", "source_repository_ids"),
		workloads:    payloadStrings(envelope.Payload, "workload_id", "workload_ids"),
		services:     payloadStrings(envelope.Payload, "service_id", "service_ids"),
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
	hasImageReferrer := false
	for _, referrer := range index.referrers[doc.documentDigest] {
		evidence = append(evidence, referrer.factID)
		if doc.subjectDigest != "" && referrer.subjectDigest != "" && doc.subjectDigest != referrer.subjectDigest {
			return sbomAttachmentDecision(doc, SBOMAttachmentSubjectMismatch, verification, policy, false, "document subject does not match OCI referrer subject", evidence, index)
		}
		if doc.subjectDigest != "" && doc.subjectDigest == referrer.subjectDigest {
			hasImageReferrer = true
		}
	}
	if isUnparseableStatus(doc.parseStatus) {
		return sbomAttachmentDecision(doc, SBOMAttachmentUnparseable, verification, policy, false, "source document could not be parsed into stable facts", evidence, index)
	}
	if doc.ambiguousSubject {
		return sbomAttachmentDecision(doc, SBOMAttachmentAmbiguousSubject, verification, policy, false, "document reports multiple distinct subject digests", evidence, index)
	}
	if doc.subjectDigest == "" {
		return sbomAttachmentDecision(doc, SBOMAttachmentUnknownSubject, verification, policy, false, "document parsed but has no artifact subject digest", evidence, index)
	}
	switch verification {
	case "passed", "verified":
		return sbomAttachmentDecision(doc, SBOMAttachmentAttachedVerified, "passed", policy, hasImageReferrer, sbomAttachmentReason("subject digest matched and verification passed", hasImageReferrer), evidence, index)
	case "failed", "unverified":
		return sbomAttachmentDecision(doc, SBOMAttachmentAttachedUnverified, verification, policy, hasImageReferrer, sbomAttachmentReason("subject digest matched but verification did not pass", hasImageReferrer), evidence, index)
	default:
		return sbomAttachmentDecision(doc, SBOMAttachmentAttachedParseOnly, defaultStatus(verification, "not_configured"), policy, hasImageReferrer, sbomAttachmentReason("subject digest matched with parse-only evidence", hasImageReferrer), evidence, index)
	}
}

func sbomAttachmentDecision(
	doc sbomAttachmentDocument,
	status SBOMAttachmentStatus,
	verificationStatus string,
	verificationPolicy string,
	hasImageReferrer bool,
	reason string,
	evidence []string,
	index sbomAttachmentIndex,
) SBOMAttestationAttachmentDecision {
	components := componentEvidenceRows(index.components[doc.documentID])
	warnings := warningSummaryRollup(index.warnings[doc.documentID])
	anchors := sbomAttachmentAnchorsForDocument(doc, index)
	scope, missing := sbomAttachmentScope(status, hasImageReferrer, anchors.hasUsableAnchor())
	reason = sbomAttachmentAnchoredReason(status, reason, hasImageReferrer, anchors.hasUsableAnchor())
	return SBOMAttestationAttachmentDecision{
		DocumentID:          doc.documentID,
		DocumentDigest:      doc.documentDigest,
		SubjectDigest:       doc.subjectDigest,
		AttachmentStatus:    status,
		ParseStatus:         doc.parseStatus,
		VerificationStatus:  defaultStatus(verificationStatus, "not_configured"),
		VerificationPolicy:  verificationPolicy,
		ArtifactKind:        doc.artifactKind,
		Format:              doc.format,
		SpecVersion:         doc.specVersion,
		Reason:              reason,
		AttachmentScope:     scope,
		CanonicalWrites:     sbomAttachmentCanonicalWriteCount(status, hasImageReferrer),
		ComponentCount:      len(components),
		ComponentEvidence:   components,
		RepositoryIDs:       anchors.repositories,
		WorkloadIDs:         anchors.workloads,
		ServiceIDs:          anchors.services,
		WarningSummaries:    warnings.summaries,
		WarningSummaryCount: warnings.count,
		EvidenceFactIDs:     uniqueSortedStrings(append(evidence, anchors.evidenceFactIDs...)),
		MissingEvidence:     uniqueSortedStrings(append(missing, anchors.missingEvidence...)),
		SourceLayerKinds:    sbomAttachmentSourceLayerKinds(hasImageReferrer, anchors.hasUsableAnchor()),
	}
}

func sbomAttachmentReason(base string, hasImageReferrer bool) string {
	if hasImageReferrer {
		return base
	}
	return "subject digest reported without OCI referrer or repository attachment evidence"
}

func sbomAttachmentAnchoredReason(
	status SBOMAttachmentStatus,
	reason string,
	hasImageReferrer bool,
	hasUsableAnchor bool,
) string {
	if hasImageReferrer || !hasUsableAnchor {
		return reason
	}
	switch status {
	case SBOMAttachmentAttachedVerified:
		return "subject digest matched through reducer image identity anchor and verification passed"
	case SBOMAttachmentAttachedUnverified:
		return "subject digest matched through reducer image identity anchor but verification did not pass"
	case SBOMAttachmentAttachedParseOnly:
		return "subject digest matched through reducer image identity anchor with parse-only evidence"
	default:
		return reason
	}
}

func sbomAttachmentCanonicalWriteCount(status SBOMAttachmentStatus, hasImageReferrer bool) int {
	if !hasImageReferrer {
		return 0
	}
	switch status {
	case SBOMAttachmentAttachedVerified, SBOMAttachmentAttachedUnverified, SBOMAttachmentAttachedParseOnly:
		return 1
	default:
		return 0
	}
}

func sbomAttachmentScope(
	status SBOMAttachmentStatus,
	hasImageReferrer bool,
	hasUsableAnchor bool,
) (string, []string) {
	if hasImageReferrer {
		return "image_subject", nil
	}
	if hasUsableAnchor {
		return "subject_only_unanchored", nil
	}
	switch status {
	case SBOMAttachmentAttachedParseOnly:
		return "parse_only_unanchored", []string{"image_referrer_evidence", "repository_attachment_evidence"}
	case SBOMAttachmentAttachedVerified, SBOMAttachmentAttachedUnverified:
		return "subject_only_unanchored", []string{"image_referrer_evidence", "repository_attachment_evidence"}
	case SBOMAttachmentSubjectMismatch:
		return "unanchored", []string{"matching_image_referrer_evidence"}
	case SBOMAttachmentAmbiguousSubject:
		return "unanchored", []string{"single_subject_digest"}
	case SBOMAttachmentUnknownSubject:
		return "unanchored", []string{"subject_digest"}
	case SBOMAttachmentUnparseable:
		return "unanchored", []string{"parseable_document"}
	default:
		return "unanchored", []string{"image_referrer_evidence", "repository_attachment_evidence"}
	}
}

func sbomAttachmentSourceLayerKinds(hasImageReferrer bool, hasUsableAnchor bool) []string {
	if hasImageReferrer || hasUsableAnchor {
		return []string{"observed_resource", "reported"}
	}
	return []string{"reported"}
}

func sbomAttachmentAnchorsForDocument(
	doc sbomAttachmentDocument,
	index sbomAttachmentIndex,
) sbomAttachmentAnchorContext {
	if doc.subjectDigest == "" {
		return sbomAttachmentAnchorContext{}
	}
	var out sbomAttachmentAnchorContext
	usable := 0
	images := index.images[doc.subjectDigest]
	for _, image := range images {
		out.evidenceFactIDs = append(out.evidenceFactIDs, image.factID)
		if !sbomAttachmentImageAnchorUsable(image) {
			continue
		}
		usable++
		out.repositories = append(out.repositories, image.repositories...)
		out.workloads = append(out.workloads, image.workloads...)
		out.services = append(out.services, image.services...)
	}
	out.repositories = uniqueSortedStrings(out.repositories)
	out.workloads = uniqueSortedStrings(out.workloads)
	out.services = uniqueSortedStrings(out.services)
	out.evidenceFactIDs = uniqueSortedStrings(out.evidenceFactIDs)
	if len(images) > 0 && usable == 0 {
		out.missingEvidence = []string{"repository_to_image_evidence_missing"}
	}
	return out
}

func (c sbomAttachmentAnchorContext) hasUsableAnchor() bool {
	return len(c.repositories) > 0 || len(c.workloads) > 0 || len(c.services) > 0
}

func sbomAttachmentImageAnchorUsable(image sbomAttachmentImageAnchor) bool {
	if image.digest == "" || image.writes <= 0 {
		return false
	}
	switch image.outcome {
	case string(ContainerImageIdentityExactDigest), string(ContainerImageIdentityTagResolved):
		return true
	default:
		return false
	}
}

func componentEvidenceRows(components []facts.Envelope) []map[string]string {
	out := make([]map[string]string, 0, len(components))
	for _, component := range components {
		out = append(out, map[string]string{
			"component_id":      payloadString(component.Payload, "component_id"),
			"name":              payloadString(component.Payload, "name"),
			"version":           payloadString(component.Payload, "version"),
			"purl":              payloadString(component.Payload, "purl"),
			"cpe":               payloadString(component.Payload, "cpe"),
			"ecosystem":         payloadString(component.Payload, "ecosystem"),
			"lockfile_path":     payloadString(component.Payload, "lockfile_path"),
			"dependency_scope":  payloadString(component.Payload, "dependency_scope"),
			"dependency_type":   payloadString(component.Payload, "dependency_type"),
			"extraction_reason": payloadString(component.Payload, "extraction_reason"),
			"fact_id":           component.FactID,
		})
	}
	return out
}

type warningSummarySet struct {
	summaries []string
	count     int
}

func warningSummaryRollup(warnings []facts.Envelope) warningSummarySet {
	out := make([]string, 0, len(warnings))
	count := 0
	for _, warning := range warnings {
		if summary := firstNonBlank(payloadString(warning.Payload, "summary"), payloadString(warning.Payload, "reason")); summary != "" {
			out = append(out, summary)
			count += warningOccurrenceCount(warning.Payload)
		}
	}
	return warningSummarySet{
		summaries: uniqueSortedStrings(out),
		count:     count,
	}
}

func warningOccurrenceCount(payload map[string]any) int {
	raw := strings.TrimSpace(fmt.Sprint(payload["occurrence_count"]))
	if raw == "" || raw == "<nil>" {
		return 1
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count <= 0 {
		return 1
	}
	return count
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

func payloadStrings(payload map[string]any, scalarKey string, sliceKey string) []string {
	var values []string
	if value := payloadString(payload, scalarKey); value != "" {
		values = append(values, value)
	}
	raw, ok := payload[sliceKey]
	if !ok {
		return uniqueSortedStrings(values)
	}
	switch typed := raw.(type) {
	case []string:
		for _, value := range typed {
			if strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	case []any:
		for _, value := range typed {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				values = append(values, text)
			}
		}
	}
	return uniqueSortedStrings(values)
}
