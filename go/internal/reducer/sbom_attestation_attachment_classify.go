// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

func classifySBOMAttachmentDocument(
	doc sbomAttachmentDocument,
	index sbomAttachmentIndex,
) SBOMAttestationAttachmentDecision {
	verification := doc.verificationStatus
	policy := doc.verificationPolicy
	evidence := []string{doc.factID}
	if verifyFact, ok := index.verifications[doc.documentID]; ok {
		verification = normalizedVerificationStatus(firstNonBlank(
			verifyFact.verificationResult,
			verifyFact.verificationStatus,
		))
		policy = firstNonBlank(verifyFact.verificationPolicy, policy)
		evidence = append(evidence, verifyFact.factID)
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
	dependencyRows, dependencyCount := dependencyRelationshipEvidenceRows(index.dependencies[doc.documentID])
	externalReferenceRows, externalReferenceCount := externalReferenceEvidenceRows(index.externalReferences[doc.documentID])
	warnings := warningSummaryRollup(index.warnings[doc.documentID])
	anchors := sbomAttachmentAnchorsForDocument(doc, index)
	scope, missing := sbomAttachmentScope(status, hasImageReferrer, anchors.hasUsableAnchor())
	reason = sbomAttachmentAnchoredReason(status, reason, hasImageReferrer, anchors.hasUsableAnchor())
	slsaProvenance, hasSLSAProvenance := index.slsaProvenance[doc.documentID]
	if hasSLSAProvenance {
		evidence = append(evidence, slsaProvenance.factID)
	}
	slsaMaterialRows, slsaMaterialCount := slsaMaterialEvidenceRows(slsaProvenance.materials)
	slsaConfigSourceURI, slsaConfigSourceEntryPoint, slsaConfigSourceDigest := slsaConfigSourceFields(slsaProvenance.configSource)
	return SBOMAttestationAttachmentDecision{
		DocumentID:                           doc.documentID,
		DocumentDigest:                       doc.documentDigest,
		SubjectDigest:                        doc.subjectDigest,
		AttachmentStatus:                     status,
		ParseStatus:                          doc.parseStatus,
		VerificationStatus:                   defaultStatus(verificationStatus, "not_configured"),
		VerificationPolicy:                   verificationPolicy,
		ArtifactKind:                         doc.artifactKind,
		Format:                               doc.format,
		SpecVersion:                          doc.specVersion,
		Reason:                               reason,
		AttachmentScope:                      scope,
		CanonicalWrites:                      sbomAttachmentCanonicalWriteCount(status, hasImageReferrer),
		ComponentCount:                       len(components),
		ComponentEvidence:                    components,
		DependencyRelationshipCount:          dependencyCount,
		DependencyRelationshipEvidence:       dependencyRows,
		ExternalReferenceCount:               externalReferenceCount,
		ExternalReferenceEvidence:            externalReferenceRows,
		SLSAProvenancePredicateType:          slsaProvenance.predicateType,
		SLSAProvenanceBuilderID:              slsaProvenance.builderID,
		SLSAProvenanceMaterials:              slsaMaterialRows,
		SLSAProvenanceMaterialCount:          slsaMaterialCount,
		SLSAProvenanceMaterialsTruncated:     slsaMaterialCount > len(slsaMaterialRows),
		SLSAProvenanceConfigSourceURI:        slsaConfigSourceURI,
		SLSAProvenanceConfigSourceEntryPoint: slsaConfigSourceEntryPoint,
		SLSAProvenanceConfigSourceDigest:     slsaConfigSourceDigest,
		RepositoryIDs:                        anchors.repositories,
		WorkloadIDs:                          anchors.workloads,
		ServiceIDs:                           anchors.services,
		WarningSummaries:                     warnings.summaries,
		WarningSummaryCount:                  warnings.count,
		EvidenceFactIDs:                      uniqueSortedStrings(append(evidence, anchors.evidenceFactIDs...)),
		MissingEvidence:                      uniqueSortedStrings(append(missing, anchors.missingEvidence...)),
		SourceLayerKinds:                     sbomAttachmentSourceLayerKinds(hasImageReferrer, anchors.hasUsableAnchor()),
	}
}

// slsaConfigSourceFields flattens a decoded SLSA provenance config source
// (#5456) into the three scalar fields the decision struct carries, treating
// a nil configSource identically to a well-formed-but-empty one.
func slsaConfigSourceFields(configSource *sbomv1.SLSAConfigSource) (uri string, entryPoint string, digest map[string]string) {
	if configSource == nil {
		return "", "", nil
	}
	return derefString(configSource.URI), derefString(configSource.EntryPoint), configSource.Digest
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

func componentEvidenceRows(components []sbomAttachmentComponentEvidence) []map[string]string {
	out := make([]map[string]string, 0, len(components))
	for _, component := range components {
		out = append(out, map[string]string{
			"component_id":      component.componentID,
			"name":              component.name,
			"version":           component.version,
			"purl":              component.purl,
			"cpe":               component.cpe,
			"ecosystem":         component.ecosystem,
			"lockfile_path":     component.lockfilePath,
			"dependency_scope":  component.dependencyScope,
			"dependency_type":   component.dependencyType,
			"extraction_reason": component.extractionReason,
			"fact_id":           component.factID,
		})
	}
	return out
}

type warningSummarySet struct {
	summaries []string
	count     int
}

// warningSummaryRollup aggregates the pre-decoded sbom.warning evidence
// (indexed by buildSBOMAttachmentIndex) for one document/statement into a
// deduplicated summary list and a total occurrence count.
func warningSummaryRollup(warnings []sbomAttachmentWarningEvidence) warningSummarySet {
	out := make([]string, 0, len(warnings))
	count := 0
	for _, warning := range warnings {
		out = append(out, warning.summary)
		count += warning.occurrenceCount
	}
	return warningSummarySet{
		summaries: uniqueSortedStrings(out),
		count:     count,
	}
}

// warningOccurrenceCount normalizes a decoded sbom.warning fact's optional
// OccurrenceCount pointer to at least 1: an absent value, or a non-positive
// observed value, defaults to one occurrence, matching the pre-typing
// behavior that tolerated a missing/malformed "occurrence_count" payload key.
func warningOccurrenceCount(occurrenceCount *int) int {
	if occurrenceCount == nil || *occurrenceCount <= 0 {
		return 1
	}
	return *occurrenceCount
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
