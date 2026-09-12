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

// StoryImageCandidateParts is a deployment image reference split into the
// pieces StoryParseImageCandidate could resolve. Tag and Digest are
// mutually exclusive and both may be empty (a bare repository reference).
type StoryImageCandidateParts struct {
	// ImageRef is the original, trimmed reference as supplied by the caller.
	ImageRef string
	// Repository is the lowercased, slash-trimmed registry image repository
	// StoryParseImageCandidate extracted from ImageRef.
	Repository string
	// RepositoryID is the synthetic "oci-registry://" + Repository id this
	// package uses to key OCI registry evidence for the candidate.
	RepositoryID string
	// Tag is the parsed `:tag` suffix, or "" when the reference carries a
	// digest or no qualifier at all.
	Tag string
	// Digest is the parsed `@digest` suffix, or "" when the reference
	// carries a tag or no qualifier at all.
	Digest string
}

// StoryRepoOnlyImageCandidateDetail builds the missing-evidence detail for
// a deployment image reference that names a repository but no tag or
// digest -- a candidate that can never resolve to OCI identity or SBOM
// evidence until the caller adds one. It returns (nil, false) when imageRef
// does not parse, or does carry a tag or digest, since this helper only
// covers the repo-only case.
func StoryRepoOnlyImageCandidateDetail(imageRef string) (map[string]any, bool) {
	parts, ok := StoryParseImageCandidate(imageRef)
	if !ok || parts.Tag != "" || parts.Digest != "" {
		return nil, false
	}
	return StoryBaseImageCandidateDetail(parts, "deployment_image_reference_repo_only", map[string]any{
		"collector_scope": "candidate_only",
		"operator_action": "add a tag or digest to the deployment image reference before expecting OCI identity or SBOM evidence",
	}), true
}

// StoryImageCandidateMissingExplanation explains why imageRef has no
// resolved container image identity, preferring store's collector-specific
// explanation over the generic one. It only asks store to explain when
// fallbackReason is exactly "container_image_identity_missing" (any other
// reason gets the generic detail directly, since a different reason means
// identity resolution was not the actual blocker); when store does not
// implement the optional explainer interface, or its explanation is empty,
// it falls back to StoryGenericImageCandidateMissingDetail. It returns the
// detail map, the reason actually used (which may differ from
// fallbackReason when store supplied its own), and an error only when the
// explainer itself fails.
func StoryImageCandidateMissingExplanation(
	ctx context.Context,
	store supplychain.ContainerImageIdentityStore,
	imageRef string,
	fallbackReason string,
) (map[string]any, string, error) {
	if fallbackReason != "container_image_identity_missing" {
		return StoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	explainer, ok := store.(containerImageCandidateExplainer)
	if !ok {
		return StoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	detail, err := explainer.ExplainContainerImageCandidate(ctx, imageRef)
	if err != nil {
		return nil, "", fmt.Errorf("explain service story image candidate: %w", err)
	}
	if len(detail) == 0 {
		return StoryGenericImageCandidateMissingDetail(imageRef, fallbackReason), "", nil
	}
	reason := strings.TrimSpace(querycontract.StringVal(detail, "reason"))
	if reason == "" {
		reason = fallbackReason
		detail["reason"] = reason
	}
	return detail, reason, nil
}

// StoryGenericImageCandidateMissingDetail builds the missing-evidence
// detail for imageRef when no collector-specific explanation is available.
// It parses imageRef via StoryParseImageCandidate; on a parse failure it
// falls back to a minimal detail carrying only the trimmed raw reference
// and reason, since there are no resolved parts to report.
func StoryGenericImageCandidateMissingDetail(imageRef string, reason string) map[string]any {
	parts, ok := StoryParseImageCandidate(imageRef)
	if !ok {
		return map[string]any{
			"candidate_image_ref": strings.TrimSpace(imageRef),
			"reason":              strings.TrimSpace(reason),
			"operator_action":     "verify OCI registry collector coverage and reducer image identity facts for this deployment image reference",
		}
	}
	return StoryBaseImageCandidateDetail(parts, reason, map[string]any{
		"operator_action": "verify OCI registry collector coverage and reducer image identity facts for this deployment image reference",
	})
}

// StorySBOMMissingExplanation builds the missing-evidence detail for a
// resolved container image identity that has no SBOM attestation. It
// returns nil when reason is blank, treating that as "SBOM evidence is
// actually present, nothing to explain" rather than emitting an empty
// detail. identity.Digest is included only when non-blank, since a resolved
// identity is not guaranteed to carry one.
func StorySBOMMissingExplanation(
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

// StoryParseImageCandidate splits a deployment image reference into its
// repository/tag/digest parts. It tries a digest split (`repo@digest`)
// before a tag split (`repo:tag`), and only treats a trailing `:segment` as
// a tag when the colon comes after the last `/` (so a registry host's port,
// e.g. "host:5000/repo", is not mistaken for a tag). It returns
// (StoryImageCandidateParts{}, false) when raw is blank or its repository
// segment resolves to empty after normalization.
func StoryParseImageCandidate(raw string) (StoryImageCandidateParts, bool) {
	imageRef := strings.TrimSpace(raw)
	if imageRef == "" {
		return StoryImageCandidateParts{}, false
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
		return StoryImageCandidateParts{}, false
	}
	repository = strings.ToLower(strings.Trim(repository, "/"))
	return StoryImageCandidateParts{
		ImageRef:     imageRef,
		Repository:   repository,
		RepositoryID: "oci-registry://" + repository,
		Tag:          strings.TrimSpace(tag),
		Digest:       strings.TrimSpace(digest),
	}, true
}

// StoryBaseImageCandidateDetail builds the common shape every
// missing-evidence detail for an image candidate shares
// (candidate_image_ref, candidate_repository_id, reason), then merges in
// extra. An extra entry whose value is nil, or whose string form is blank
// after trimming, is dropped rather than written as an empty field.
func StoryBaseImageCandidateDetail(
	parts StoryImageCandidateParts,
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

// StoryUniqueMissingDetails deduplicates missing-evidence detail rows by
// their (candidate_image_ref, reason, collector_scope, operator_action)
// tuple, keeping the first occurrence, then sorts the result by
// (candidate_image_ref, reason) for a stable response order. A nil or empty
// input row is dropped rather than kept as an empty entry.
func StoryUniqueMissingDetails(rows []map[string]any) []map[string]any {
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
