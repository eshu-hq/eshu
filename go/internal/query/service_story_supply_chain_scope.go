// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type containerImageCandidateExplainer interface {
	ExplainContainerImageCandidate(context.Context, string) (map[string]any, error)
}

type serviceStoryImageCandidateParts struct {
	ImageRef     string
	Repository   string
	RepositoryID string
	Tag          string
	Digest       string
}

func serviceStoryRepoOnlyImageCandidateDetail(imageRef string) (map[string]any, bool) {
	parts, ok := serviceStoryParseImageCandidate(imageRef)
	if !ok || parts.Tag != "" || parts.Digest != "" {
		return nil, false
	}
	return serviceStoryBaseImageCandidateDetail(parts, "deployment_image_reference_repo_only", map[string]any{
		"collector_scope": "candidate_only",
		"operator_action": "add a tag or digest to the deployment image reference before expecting OCI identity or SBOM evidence",
	}), true
}

func serviceStoryImageCandidateMissingExplanation(
	ctx context.Context,
	store ContainerImageIdentityStore,
	imageRef string,
	fallbackReason string,
) (map[string]any, string, error) {
	if fallbackReason != "container_image_identity_missing" {
		return serviceStoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	explainer, ok := store.(containerImageCandidateExplainer)
	if !ok {
		return serviceStoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	detail, err := explainer.ExplainContainerImageCandidate(ctx, imageRef)
	if err != nil {
		return nil, "", fmt.Errorf("explain service story image candidate: %w", err)
	}
	if len(detail) == 0 {
		return serviceStoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	reason := strings.TrimSpace(StringVal(detail, "reason"))
	if reason == "" {
		reason = fallbackReason
		detail["reason"] = reason
	}
	return detail, reason, nil
}

func serviceStoryGenericImageCandidateMissingDetail(imageRef string, reason string) map[string]any {
	parts, ok := serviceStoryParseImageCandidate(imageRef)
	if !ok {
		return map[string]any{
			"candidate_image_ref": strings.TrimSpace(imageRef),
			"reason":              strings.TrimSpace(reason),
			"operator_action":     "verify OCI registry collector coverage and reducer image identity facts for this deployment image reference",
		}
	}
	return serviceStoryBaseImageCandidateDetail(parts, reason, map[string]any{
		"operator_action": "verify OCI registry collector coverage and reducer image identity facts for this deployment image reference",
	})
}

func serviceStorySBOMMissingExplanation(
	imageRef string,
	identity ContainerImageIdentityRow,
	reason string,
) map[string]any {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	detail := map[string]any{
		"candidate_image_ref": strings.TrimSpace(imageRef),
		"reason":              strings.TrimSpace(reason),
		"identity_id":         strings.TrimSpace(identity.IdentityID),
		"repository_id":       strings.TrimSpace(identity.RepositoryID),
		"operator_action":     "verify SBOM attestation collection for the resolved image digest",
	}
	if strings.TrimSpace(identity.Digest) != "" {
		detail["identity_digest"] = strings.TrimSpace(identity.Digest)
	}
	return detail
}

func serviceStoryParseImageCandidate(raw string) (serviceStoryImageCandidateParts, bool) {
	imageRef := strings.TrimSpace(raw)
	if imageRef == "" {
		return serviceStoryImageCandidateParts{}, false
	}
	repository := imageRef
	digest := ""
	tag := ""
	if before, after, ok := strings.Cut(imageRef, "@"); ok {
		repository = before
		digest = after
	} else if tagIndex := strings.LastIndex(imageRef, ":"); tagIndex > strings.LastIndex(imageRef, "/") {
		repository = imageRef[:tagIndex]
		tag = imageRef[tagIndex+1:]
	}
	repository = serviceStoryRegistryImageRepository(repository)
	if repository == "" {
		return serviceStoryImageCandidateParts{}, false
	}
	repository = strings.ToLower(strings.Trim(repository, "/"))
	return serviceStoryImageCandidateParts{
		ImageRef:     imageRef,
		Repository:   repository,
		RepositoryID: "oci-registry://" + repository,
		Tag:          strings.TrimSpace(tag),
		Digest:       strings.TrimSpace(digest),
	}, true
}

func serviceStoryBaseImageCandidateDetail(
	parts serviceStoryImageCandidateParts,
	reason string,
	extra map[string]any,
) map[string]any {
	detail := map[string]any{
		"candidate_image_ref":     parts.ImageRef,
		"candidate_repository_id": parts.RepositoryID,
		"reason":                  strings.TrimSpace(reason),
	}
	for key, value := range extra {
		if value == nil || strings.TrimSpace(fmt.Sprintf("%v", value)) == "" {
			continue
		}
		detail[key] = value
	}
	return detail
}

func serviceStoryUniqueMissingDetails(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		key := strings.Join([]string{
			StringVal(row, "candidate_image_ref"),
			StringVal(row, "reason"),
			StringVal(row, "collector_scope"),
			StringVal(row, "operator_action"),
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, copyMap(row))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if left, right := StringVal(out[i], "candidate_image_ref"), StringVal(out[j], "candidate_image_ref"); left != right {
			return left < right
		}
		return StringVal(out[i], "reason") < StringVal(out[j], "reason")
	})
	return out
}
