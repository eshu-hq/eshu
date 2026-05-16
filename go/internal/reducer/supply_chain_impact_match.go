package reducer

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func supplyChainCVEFromEnvelope(envelope facts.Envelope) supplyChainImpactCVE {
	return supplyChainImpactCVE{
		factID:     envelope.FactID,
		cveID:      supplyChainCVEID(envelope.Payload),
		advisoryID: payloadStr(envelope.Payload, "advisory_id"),
		cvssScore:  supplyChainFloat(envelope.Payload, "cvss_score"),
	}
}

func supplyChainAffectedPackageFromEnvelope(envelope facts.Envelope) supplyChainAffectedPackage {
	return supplyChainAffectedPackage{
		factID:           envelope.FactID,
		cveID:            supplyChainCVEID(envelope.Payload),
		packageID:        payloadStr(envelope.Payload, "package_id"),
		ecosystem:        strings.ToLower(payloadStr(envelope.Payload, "ecosystem")),
		name:             payloadStr(envelope.Payload, "package_name"),
		purl:             payloadStr(envelope.Payload, "purl"),
		affectedVersions: payloadStrings(envelope.Payload, "affected_version", "affected_versions"),
		fixedVersions:    payloadStrings(envelope.Payload, "fixed_version", "fixed_versions"),
	}
}

func supplyChainPackageVersionFromEnvelope(envelope facts.Envelope) supplyChainPackageVersion {
	return supplyChainPackageVersion{
		factID:    envelope.FactID,
		packageID: payloadStr(envelope.Payload, "package_id"),
		purl:      payloadStr(envelope.Payload, "purl"),
		version:   payloadStr(envelope.Payload, "version"),
	}
}

func supplyChainConsumptionFromEnvelope(envelope facts.Envelope) supplyChainPackageConsumption {
	return supplyChainPackageConsumption{
		factID:       envelope.FactID,
		packageID:    payloadStr(envelope.Payload, "package_id"),
		repositoryID: payloadStr(envelope.Payload, "repository_id"),
	}
}

func supplyChainSBOMComponentFromEnvelope(envelope facts.Envelope) supplyChainSBOMComponent {
	purl := payloadStr(envelope.Payload, "purl")
	return supplyChainSBOMComponent{
		factID:     envelope.FactID,
		documentID: payloadStr(envelope.Payload, "document_id"),
		purl:       purl,
		packageID:  packageIDFromPURL(purl),
		version:    firstNonBlank(payloadStr(envelope.Payload, "version"), versionFromPURL(purl)),
	}
}

func supplyChainAttachmentFromEnvelope(envelope facts.Envelope) supplyChainAttachment {
	return supplyChainAttachment{
		factID:        envelope.FactID,
		documentID:    payloadStr(envelope.Payload, "document_id"),
		subjectDigest: payloadStr(envelope.Payload, "subject_digest"),
		status:        payloadStr(envelope.Payload, "attachment_status"),
	}
}

func supplyChainImageIdentityFromEnvelope(envelope facts.Envelope) supplyChainImageIdentity {
	return supplyChainImageIdentity{
		factID:       envelope.FactID,
		digest:       payloadStr(envelope.Payload, "digest"),
		repositoryID: payloadStr(envelope.Payload, "repository_id"),
	}
}

func firstPackageVersion(
	pkg supplyChainAffectedPackage,
	versions []supplyChainPackageVersion,
) (supplyChainPackageVersion, bool) {
	for _, version := range versions {
		if pkg.purl != "" && version.purl != "" && pkg.purl != version.purl {
			continue
		}
		return version, true
	}
	return supplyChainPackageVersion{}, false
}

func firstConsumption(
	packageID string,
	consumption map[string][]supplyChainPackageConsumption,
) supplyChainPackageConsumption {
	for _, row := range consumption[packageID] {
		if row.repositoryID != "" {
			return row
		}
	}
	return supplyChainPackageConsumption{}
}

func firstSBOMImpactPath(
	pkg supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) (supplyChainSBOMComponent, supplyChainAttachment, supplyChainImageIdentity, bool) {
	for _, component := range index.components {
		if !componentMatchesAffectedPackage(component, pkg) {
			continue
		}
		attachment := index.attachments[component.documentID]
		if attachment.subjectDigest == "" || attachment.status == "subject_mismatch" || attachment.status == "unknown_subject" {
			continue
		}
		image := index.images[attachment.subjectDigest]
		if image.digest == "" {
			continue
		}
		return component, attachment, image, true
	}
	return supplyChainSBOMComponent{}, supplyChainAttachment{}, supplyChainImageIdentity{}, false
}

func componentMatchesAffectedPackage(component supplyChainSBOMComponent, pkg supplyChainAffectedPackage) bool {
	if pkg.purl != "" && component.purl == pkg.purl {
		return true
	}
	if pkg.packageID != "" && component.packageID == pkg.packageID {
		return true
	}
	return false
}

func versionAffected(observed string, affected []string) bool {
	observed = strings.TrimSpace(observed)
	if observed == "" {
		return false
	}
	for _, candidate := range affected {
		if strings.TrimSpace(candidate) == observed {
			return true
		}
	}
	return len(affected) == 0
}

func isKnownFixed(observed string, fixed string) bool {
	observedParts, ok := numericVersionParts(observed)
	if !ok {
		return false
	}
	fixedParts, ok := numericVersionParts(fixed)
	if !ok {
		return false
	}
	maxLen := len(observedParts)
	if len(fixedParts) > maxLen {
		maxLen = len(fixedParts)
	}
	for i := 0; i < maxLen; i++ {
		left := versionPart(observedParts, i)
		right := versionPart(fixedParts, i)
		if left > right {
			return true
		}
		if left < right {
			return false
		}
	}
	return true
}

func numericVersionParts(version string) ([]int, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return nil, false
	}
	pieces := strings.Split(version, ".")
	parts := make([]int, 0, len(pieces))
	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			return nil, false
		}
		value, err := strconv.Atoi(piece)
		if err != nil {
			return nil, false
		}
		parts = append(parts, value)
	}
	return parts, true
}

func versionPart(parts []int, index int) int {
	if index >= len(parts) {
		return 0
	}
	return parts[index]
}

func packageIDFromPURL(purl string) string {
	purl = strings.TrimSpace(purl)
	if before, _, ok := strings.Cut(purl, "@"); ok {
		return before
	}
	return purl
}

func versionFromPURL(purl string) string {
	_, after, ok := strings.Cut(strings.TrimSpace(purl), "@")
	if !ok {
		return ""
	}
	if version, _, ok := strings.Cut(after, "?"); ok {
		return version
	}
	return after
}

func missingImpactEvidence(finding SupplyChainImpactFinding) []string {
	var missing []string
	if finding.PackageID == "" || finding.ObservedVersion == "" {
		missing = append(missing, "package version evidence missing")
	}
	if finding.RepositoryID == "" {
		missing = append(missing, "repository or workload evidence missing")
	}
	if finding.SubjectDigest == "" && finding.RuntimeReachability != "package_manifest" {
		missing = append(missing, "image or SBOM attachment evidence missing")
	}
	if finding.RuntimeReachability != "known_fixed" {
		missing = append(missing, "deployment exposure evidence missing")
	}
	return uniqueSortedStrings(missing)
}
