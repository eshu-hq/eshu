// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package componentindex

import (
	"fmt"
	"strings"
)

const (
	// PublicationStatusDraft records local readiness without external
	// marketplace publication.
	PublicationStatusDraft = "draft"
	// PublicationStatusPublished records a package with reviewed publishable
	// metadata.
	PublicationStatusPublished = "published"
	// SignatureStatusPending records missing or deferred signature proof.
	SignatureStatusPending = "pending"
	// SignatureStatusSigned records reviewed signature evidence.
	SignatureStatusSigned = "signed"
	// ProvenanceStatusPending records missing or deferred provenance proof.
	ProvenanceStatusPending = "pending"
	// ProvenanceStatusVerified records reviewed provenance evidence.
	ProvenanceStatusVerified = "verified"
	// PolicyResultMissingProof records a badge that is not installable because
	// publication proof is incomplete.
	PolicyResultMissingProof = "missing_proof"
	// PolicyResultInstallable records a badge whose reviewed policy result
	// permits installation.
	PolicyResultInstallable = "installable"
)

func (v *indexVerifier) validatePublication(prefix, componentID string, entry Entry) {
	status := strings.TrimSpace(entry.Publication.Status)
	switch status {
	case "":
		v.add(IssueMissingMetadata, prefix+".publication.status", componentID, "publication status is required")
	case PublicationStatusDraft, PublicationStatusPublished:
	default:
		v.add(IssueUnsupportedPublicationStatus, prefix+".publication.status", componentID, fmt.Sprintf("unsupported publication status %q", entry.Publication.Status))
	}

	v.validateCompatibilityBadge(prefix, componentID, entry)
	if status == PublicationStatusPublished {
		v.validatePublishedReadiness(prefix, componentID, entry)
	}
}

func (v *indexVerifier) validateCompatibilityBadge(prefix, componentID string, entry Entry) {
	badge := entry.CompatibilityBadge
	fieldPrefix := prefix + ".compatibilityBadge"
	required := []struct {
		field string
		value string
	}{
		{field: "manifestApiVersion", value: badge.ManifestAPIVersion},
		{field: "manifestDigest", value: badge.ManifestDigest},
		{field: "compatibleCore", value: badge.CompatibleCore},
		{field: "artifactDigest", value: badge.ArtifactDigest},
		{field: "signatureStatus", value: badge.SignatureStatus},
		{field: "provenanceStatus", value: badge.ProvenanceStatus},
		{field: "runtimeProtocol", value: badge.RuntimeProtocol},
		{field: "adapter", value: badge.Adapter},
		{field: "conformanceProofUri", value: badge.ConformanceProofURI},
		{field: "conformanceStatus", value: badge.ConformanceStatus},
		{field: "policyResult", value: badge.PolicyResult},
	}
	for _, item := range required {
		if strings.TrimSpace(item.value) == "" {
			v.add(IssueMissingCompatibilityBadge, fieldPrefix+"."+item.field, componentID, item.field+" is required")
		}
	}
	v.validateBadgeDigest(fieldPrefix+".manifestDigest", componentID, badge.ManifestDigest)
	v.validateBadgeDigest(fieldPrefix+".artifactDigest", componentID, badge.ArtifactDigest)
	if strings.TrimSpace(badge.ManifestDigest) != "" && badge.ManifestDigest != entry.ManifestDigest {
		v.add(IssueMissingCompatibilityBadge, fieldPrefix+".manifestDigest", componentID, "badge manifest digest must match entry manifest digest")
	}
	if strings.TrimSpace(badge.ArtifactDigest) != "" && !artifactDigestMatches(entry.Artifacts, badge.ArtifactDigest) {
		v.add(IssueMissingCompatibilityBadge, fieldPrefix+".artifactDigest", componentID, "badge artifact digest must match a pinned entry artifact digest")
	}
	if strings.TrimSpace(badge.CompatibleCore) != "" && badge.CompatibleCore != entry.CompatibleCore {
		v.add(IssueMissingCompatibilityBadge, fieldPrefix+".compatibleCore", componentID, "badge compatible core must match entry compatible core")
	}
	if strings.TrimSpace(badge.ConformanceProofURI) != "" && badge.ConformanceProofURI != entry.Conformance.ProofURI {
		v.add(IssueMissingConformanceProof, fieldPrefix+".conformanceProofUri", componentID, "badge conformance proof URI must match entry proof URI")
	}
	if strings.TrimSpace(badge.ConformanceStatus) != "" && badge.ConformanceStatus != entry.Conformance.Status {
		v.add(IssueFailedConformanceProof, fieldPrefix+".conformanceStatus", componentID, "badge conformance status must match entry proof status")
	}
}

func (v *indexVerifier) validateBadgeDigest(field, componentID, digest string) {
	trimmed := strings.TrimSpace(digest)
	if trimmed == "" {
		return
	}
	if !sha256DigestPattern.MatchString(trimmed) {
		v.add(IssueMalformedDigest, field, componentID, "digest must use sha256 with 64 hex characters")
	}
}

func (v *indexVerifier) validatePublishedReadiness(prefix, componentID string, entry Entry) {
	if hasPlaceholderDigest(entry.ManifestDigest) {
		v.add(IssuePlaceholderPublicationMetadata, prefix+".manifestDigest", componentID, "published entries must not use placeholder manifest digests")
	}
	for i, artifact := range entry.Artifacts {
		if hasPlaceholderArtifactDigest(artifact.Image) {
			v.add(IssuePlaceholderPublicationMetadata, fmt.Sprintf("%s.artifacts[%d].image", prefix, i), componentID, "published entries must not use placeholder artifact digests")
		}
	}
	badge := entry.CompatibilityBadge
	if hasPlaceholderDigest(badge.ManifestDigest) {
		v.add(IssuePlaceholderPublicationMetadata, prefix+".compatibilityBadge.manifestDigest", componentID, "published badges must not use placeholder manifest digests")
	}
	if hasPlaceholderDigest(badge.ArtifactDigest) {
		v.add(IssuePlaceholderPublicationMetadata, prefix+".compatibilityBadge.artifactDigest", componentID, "published badges must not use placeholder artifact digests")
	}
	if isPendingSignature(entry.Provenance.Signature) ||
		badge.SignatureStatus != SignatureStatusSigned ||
		badge.ProvenanceStatus != ProvenanceStatusVerified {
		v.add(IssueMissingProvenanceSignature, prefix+".provenance.signature", componentID, "published entries require signed and verified provenance")
	}
	if badge.PolicyResult != PolicyResultInstallable {
		v.add(IssueMissingCompatibilityBadge, prefix+".compatibilityBadge.policyResult", componentID, "published badges must be installable")
	}
}

func hasPlaceholderArtifactDigest(image string) bool {
	marker := "@sha256:"
	index := strings.LastIndex(image, marker)
	if index < 0 {
		return false
	}
	return hasPlaceholderDigest("sha256:" + image[index+len(marker):])
}

func artifactDigestMatches(artifacts []ArtifactRef, digest string) bool {
	trimmed := strings.TrimSpace(digest)
	if trimmed == "" {
		return false
	}
	for _, artifact := range artifacts {
		if artifactDigest(artifact.Image) == trimmed {
			return true
		}
	}
	return false
}

func artifactDigest(image string) string {
	marker := "@sha256:"
	index := strings.LastIndex(image, marker)
	if index < 0 {
		return ""
	}
	return "sha256:" + image[index+len(marker):]
}

func hasPlaceholderDigest(digest string) bool {
	trimmed := strings.TrimSpace(digest)
	if !sha256DigestPattern.MatchString(trimmed) {
		return false
	}
	hex := strings.TrimPrefix(strings.ToLower(trimmed), "sha256:")
	for _, ch := range hex[1:] {
		if ch != rune(hex[0]) {
			return false
		}
	}
	return true
}

func isPendingSignature(signature string) bool {
	trimmed := strings.TrimSpace(signature)
	return trimmed == "" || strings.EqualFold(trimmed, "sigstore:pending")
}
