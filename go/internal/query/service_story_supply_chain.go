// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

const (
	serviceStorySupplyChainReadLimit  = 3
	serviceStorySupplyChainProbeLimit = serviceStorySupplyChainReadLimit + 1
)

// enrichServiceStorySupplyChainEvidence attaches reducer-owned image and SBOM
// evidence to the service story context using deployment image references as
// target-scoped anchors.
func (h *EntityHandler) enrichServiceStorySupplyChainEvidence(ctx context.Context, workloadContext map[string]any) error {
	if h == nil || h.ContainerImageIdentities == nil || h.SBOMAttachments == nil || len(workloadContext) == 0 {
		return nil
	}

	allImageRefs := serviceStoryDeploymentImageRefs(workloadContext)
	imageRefs := allImageRefs
	truncated := false
	if len(imageRefs) > serviceStoryItemLimit {
		imageRefs = imageRefs[:serviceStoryItemLimit]
		truncated = true
	}
	evidence := make([]map[string]any, 0, len(imageRefs))
	missing := make([]string, 0)
	missingDetails := make([]map[string]any, 0)
	if len(imageRefs) == 0 {
		missing = append(missing, "deployment_image_reference_missing")
	}

	for _, imageRef := range imageRefs {
		if detail, ok := serviceStoryRepoOnlyImageCandidateDetail(imageRef); ok {
			missing = append(missing, StringVal(detail, "reason"))
			missingDetails = append(missingDetails, detail)
			continue
		}
		identities, err := h.ContainerImageIdentities.ListContainerImageIdentities(ctx, ContainerImageIdentityFilter{
			ImageRef: imageRef,
			Limit:    serviceStorySupplyChainProbeLimit,
		})
		if err != nil {
			return fmt.Errorf("load service story image identity: %w", err)
		}
		identity, reason := serviceStoryAdmissibleImageIdentity(identities)
		if reason != "" {
			detail, replacementReason, err := serviceStoryImageCandidateMissingExplanation(
				ctx,
				h.ContainerImageIdentities,
				imageRef,
				reason,
			)
			if err != nil {
				return err
			}
			if replacementReason != "" {
				reason = replacementReason
			}
			missing = append(missing, reason)
			if len(detail) > 0 {
				missingDetails = append(missingDetails, detail)
			}
			continue
		}

		attachments, err := h.SBOMAttachments.ListSBOMAttestationAttachments(ctx, SBOMAttestationAttachmentFilter{
			SubjectDigest: identity.Digest,
			Limit:         serviceStorySupplyChainProbeLimit,
		})
		if err != nil {
			return fmt.Errorf("load service story SBOM attachment: %w", err)
		}
		missing = append(missing, attachments.MissingEvidence...)
		for _, reason := range attachments.MissingEvidence {
			missingDetails = append(missingDetails, serviceStorySBOMMissingExplanation(imageRef, identity, reason))
		}
		sboms, reason := serviceStoryAdmissibleSBOMAttachments(identity.Digest, attachments.Attachments)
		if reason != "" {
			missing = append(missing, reason)
			missingDetails = append(missingDetails, serviceStorySBOMMissingExplanation(imageRef, identity, reason))
			continue
		}
		for _, sbom := range sboms {
			evidence = append(evidence, serviceStoryImagePackageEvidence(imageRef, identity, sbom))
		}
	}

	sort.Slice(evidence, func(i, j int) bool {
		if left, right := StringVal(evidence[i], "digest"), StringVal(evidence[j], "digest"); left != right {
			return left < right
		}
		return StringVal(evidence[i], "sbom_attachment_id") < StringVal(evidence[j], "sbom_attachment_id")
	})
	serviceStorySetSupplyChainImagePackage(workloadContext, map[string]any{
		"evidence":                  evidence,
		"missing_evidence":          uniqueSortedStrings(missing),
		"missing_evidence_details":  serviceStoryUniqueMissingDetails(missingDetails),
		"candidate_image_ref_count": len(allImageRefs),
		"candidate_image_refs":      imageRefs,
		"image_refs_truncated":      truncated,
	})
	return nil
}

func serviceStoryDeploymentImageRefs(workloadContext map[string]any) []string {
	refs := make([]string, 0)
	refs = append(refs, StringSliceVal(workloadContext, "image_refs")...)
	for _, key := range []string{"artifacts", "delivery_paths", "delivery_workflows"} {
		for _, row := range mapSliceValue(mapValue(workloadContext, "deployment_evidence"), key) {
			refs = append(refs, serviceStoryImageRefsFromDeploymentRow(row)...)
		}
	}
	return uniqueSortedStrings(refs)
}

func serviceStoryImageRefsFromDeploymentRow(row map[string]any) []string {
	refs := make([]string, 0)
	for _, key := range []string{"image_ref", "container_image"} {
		if value := strings.TrimSpace(StringVal(row, key)); value != "" {
			refs = append(refs, value)
		}
	}
	for _, key := range []string{"image_refs", "container_images"} {
		for _, value := range StringSliceVal(row, key) {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				refs = append(refs, trimmed)
			}
		}
	}
	if value := serviceStoryMatchedImageRef(row); value != "" {
		refs = append(refs, value)
	}
	return refs
}

func serviceStoryMatchedImageRef(row map[string]any) string {
	value := strings.TrimSpace(StringVal(row, "matched_value"))
	if value == "" {
		return ""
	}
	kind := strings.ToLower(strings.Join([]string{
		StringVal(row, "evidence_kind"),
		StringVal(row, "artifact_family"),
		StringVal(row, "extractor"),
	}, " "))
	if strings.Contains(kind, "image") || strings.Contains(kind, "oci") {
		return value
	}
	if ref := serviceStoryExplicitImageRef(value); ref != "" {
		return ref
	}
	if strings.Contains(kind, "helm") {
		return serviceStoryRegistryImageRepository(value)
	}
	return ""
}

func serviceStoryExplicitImageRef(raw string) string {
	ref := doctruth.NormalizeContainerImageRefClaim(raw)
	if ref == "" {
		return ""
	}
	repository := ref
	if digestIndex := strings.Index(repository, "@sha256:"); digestIndex >= 0 {
		repository = repository[:digestIndex]
	} else if tagIndex := strings.LastIndex(repository, ":"); tagIndex >= 0 {
		repository = repository[:tagIndex]
	}
	if !strings.Contains(repository, "/") && !strings.Contains(repository, ".") {
		return ""
	}
	return ref
}

func serviceStoryRegistryImageRepository(raw string) string {
	repository := strings.Trim(strings.TrimSpace(raw), `"'`)
	if repository == "" ||
		strings.ContainsAny(repository, " \t\n\r${}") ||
		strings.Contains(repository, "://") ||
		strings.Contains(repository, "@") {
		return ""
	}
	if tagIndex := strings.LastIndex(repository, ":"); tagIndex >= 0 {
		if tagIndex > strings.LastIndex(repository, "/") {
			return ""
		}
	}
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return ""
	}
	registry := parts[0]
	if !strings.Contains(registry, ".") && !strings.Contains(registry, ":") && !strings.EqualFold(registry, "localhost") {
		return ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".yaml") || strings.HasSuffix(part, ".yml") {
			return ""
		}
	}
	return repository
}

func serviceStoryAdmissibleImageIdentity(rows []ContainerImageIdentityRow) (ContainerImageIdentityRow, string) {
	if len(rows) == 0 {
		return ContainerImageIdentityRow{}, "container_image_identity_missing"
	}
	if len(rows) > serviceStorySupplyChainReadLimit {
		return ContainerImageIdentityRow{}, "container_image_identity_ambiguous"
	}
	admissible := make([]ContainerImageIdentityRow, 0, len(rows))
	stale := false
	for _, row := range rows {
		if !serviceStorySourceFresh(row.SourceFreshness) {
			stale = true
			continue
		}
		if !serviceStoryImageIdentityCanonical(row) {
			continue
		}
		admissible = append(admissible, row)
	}
	if len(admissible) == 0 {
		if stale {
			return ContainerImageIdentityRow{}, "container_image_identity_stale"
		}
		return ContainerImageIdentityRow{}, "container_image_identity_not_admissible"
	}
	byDigest := map[string]ContainerImageIdentityRow{}
	for _, row := range admissible {
		byDigest[row.Digest] = row
	}
	if len(byDigest) != 1 {
		return ContainerImageIdentityRow{}, "container_image_identity_ambiguous"
	}
	for _, row := range byDigest {
		return row, ""
	}
	return ContainerImageIdentityRow{}, "container_image_identity_missing"
}

func serviceStoryImageIdentityCanonical(row ContainerImageIdentityRow) bool {
	if row.CanonicalWrites <= 0 || strings.TrimSpace(row.Digest) == "" {
		return false
	}
	switch row.Outcome {
	case "exact_digest", "tag_resolved":
		return true
	default:
		return false
	}
}

func serviceStoryAdmissibleSBOMAttachments(
	subjectDigest string,
	rows []SBOMAttestationAttachmentRow,
) ([]SBOMAttestationAttachmentRow, string) {
	if len(rows) == 0 {
		return nil, "sbom_attachment_missing"
	}
	if len(rows) > serviceStorySupplyChainReadLimit {
		return nil, "sbom_attachment_ambiguous"
	}
	out := make([]SBOMAttestationAttachmentRow, 0, len(rows))
	stale := false
	for _, row := range rows {
		if !serviceStorySourceFresh(row.SourceFreshness) {
			stale = true
			continue
		}
		if serviceStorySBOMAttachmentCanonical(subjectDigest, row) {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		if stale {
			return nil, "sbom_attachment_stale"
		}
		return nil, "sbom_attachment_not_admissible"
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AttachmentID < out[j].AttachmentID
	})
	return out, ""
}

func serviceStorySBOMAttachmentCanonical(subjectDigest string, row SBOMAttestationAttachmentRow) bool {
	if row.CanonicalWrites <= 0 || row.SubjectDigest != subjectDigest {
		return false
	}
	switch row.AttachmentStatus {
	case "attached_verified", "attached_unverified", "attached_parse_only":
		return true
	default:
		return false
	}
}

func serviceStorySourceFresh(freshness string) bool {
	freshness = strings.TrimSpace(freshness)
	return freshness == "" || freshness == "active"
}

func serviceStoryImagePackageEvidence(
	anchorImageRef string,
	identity ContainerImageIdentityRow,
	sbom SBOMAttestationAttachmentRow,
) map[string]any {
	evidenceFactIDs := append([]string{}, identity.EvidenceFactIDs...)
	evidenceFactIDs = append(evidenceFactIDs, sbom.EvidenceFactIDs...)
	return map[string]any{
		"source":                     "supply_chain_read_model",
		"image_ref":                  firstNonEmptyString(identity.ImageRef, anchorImageRef),
		"digest":                     identity.Digest,
		"repository_id":              identity.RepositoryID,
		"identity_id":                identity.IdentityID,
		"identity_outcome":           identity.Outcome,
		"identity_strength":          identity.IdentityStrength,
		"identity_evidence_fact_ids": identity.EvidenceFactIDs,
		"sbom_attachment_id":         sbom.AttachmentID,
		"sbom_document_id":           sbom.DocumentID,
		"sbom_document_digest":       sbom.DocumentDigest,
		"sbom_attachment_status":     sbom.AttachmentStatus,
		"sbom_artifact_kind":         sbom.ArtifactKind,
		"sbom_format":                sbom.Format,
		"sbom_evidence_fact_ids":     sbom.EvidenceFactIDs,
		"evidence_fact_ids":          uniqueSortedStrings(evidenceFactIDs),
	}
}

func serviceStorySetSupplyChainImagePackage(workloadContext map[string]any, imagePackage map[string]any) {
	supplyChain := mapValue(workloadContext, "supply_chain_evidence")
	if supplyChain == nil {
		supplyChain = map[string]any{}
	}
	supplyChain["image_package"] = imagePackage
	workloadContext["supply_chain_evidence"] = supplyChain
}

func serviceStorySupplyChainImagePackage(workloadContext map[string]any) map[string]any {
	return mapValue(mapValue(workloadContext, "supply_chain_evidence"), "image_package")
}
