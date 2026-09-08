// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

type containerImageCandidateExplainer interface {
	ExplainContainerImageCandidate(context.Context, string) (map[string]any, error)
}

type ServiceStoryImageCandidateParts struct {
	ImageRef     string
	Repository   string
	RepositoryID string
	Tag          string
	Digest       string
}

func ServiceStoryRepoOnlyImageCandidateDetail(imageRef string) (map[string]any, bool) {
	parts, ok := ServiceStoryParseImageCandidate(imageRef)
	if !ok || parts.Tag != "" || parts.Digest != "" {
		return nil, false
	}
	return ServiceStoryBaseImageCandidateDetail(parts, "deployment_image_reference_repo_only", map[string]any{
		"collector_scope": "candidate_only",
		"operator_action": "add a tag or digest to the deployment image reference before expecting OCI identity or SBOM evidence",
	}), true
}

func ServiceStoryImageCandidateMissingExplanation(
	ctx context.Context,
	store supplychain.ContainerImageIdentityStore,
	imageRef string,
	fallbackReason string,
) (map[string]any, string, error) {
	if fallbackReason != "container_image_identity_missing" {
		return ServiceStoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	explainer, ok := store.(containerImageCandidateExplainer)
	if !ok {
		return ServiceStoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	detail, err := explainer.ExplainContainerImageCandidate(ctx, imageRef)
	if err != nil {
		return nil, "", fmt.Errorf("explain service story image candidate: %w", err)
	}
	if len(detail) == 0 {
		return ServiceStoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	reason := strings.TrimSpace(querycontract.StringVal(detail, "reason"))
	if reason == "" {
		reason = fallbackReason
		detail["reason"] = reason
	}
	return detail, reason, nil
}

func ServiceStoryGenericImageCandidateMissingDetail(imageRef string, reason string) map[string]any {
	parts, ok := ServiceStoryParseImageCandidate(imageRef)
	if !ok {
		return map[string]any{
			"candidate_image_ref": strings.TrimSpace(imageRef),
			"reason":              strings.TrimSpace(reason),
			"operator_action":     "verify OCI registry collector coverage and reducer image identity facts for this deployment image reference",
		}
	}
	return ServiceStoryBaseImageCandidateDetail(parts, reason, map[string]any{
		"operator_action": "verify OCI registry collector coverage and reducer image identity facts for this deployment image reference",
	})
}

func ServiceStorySBOMMissingExplanation(
	imageRef string,
	identity supplychain.ContainerImageIdentityRow,
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

func ServiceStoryParseImageCandidate(raw string) (ServiceStoryImageCandidateParts, bool) {
	imageRef := strings.TrimSpace(raw)
	if imageRef == "" {
		return ServiceStoryImageCandidateParts{}, false
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
		return ServiceStoryImageCandidateParts{}, false
	}
	repository = strings.ToLower(strings.Trim(repository, "/"))
	return ServiceStoryImageCandidateParts{
		ImageRef:     imageRef,
		Repository:   repository,
		RepositoryID: "oci-registry://" + repository,
		Tag:          strings.TrimSpace(tag),
		Digest:       strings.TrimSpace(digest),
	}, true
}

func ServiceStoryBaseImageCandidateDetail(
	parts ServiceStoryImageCandidateParts,
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

func ServiceStoryUniqueMissingDetails(rows []map[string]any) []map[string]any {
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
			querycontract.StringVal(row, "candidate_image_ref"),
			querycontract.StringVal(row, "reason"),
			querycontract.StringVal(row, "collector_scope"),
			querycontract.StringVal(row, "operator_action"),
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, copyMap(row))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if left, right := querycontract.StringVal(out[i], "candidate_image_ref"), querycontract.StringVal(out[j], "candidate_image_ref"); left != right {
			return left < right
		}
		return querycontract.StringVal(out[i], "reason") < querycontract.StringVal(out[j], "reason")
	})
	return out
}
