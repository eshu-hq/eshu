// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeguru

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// providerCodeCommit is the CodeGuru Reviewer ProviderType value for an AWS
// CodeCommit-backed repository association. Only this provider produces a
// graph edge; every other provider's backing reference stays a resource
// attribute so no edge dangles to an unscanned third-party endpoint.
const providerCodeCommit = "CodeCommit"

// associationCodeCommitRelationship records a CodeGuru Reviewer repository
// association whose provider is AWS CodeCommit. The target is the partition-aware
// CodeCommit repository ARN synthesized from the boundary region and the reported
// owner account, which matches how the CodeCommit scanner publishes its
// repository resource_id, so the edge joins the existing repository node. It
// returns nil for non-CodeCommit providers and when the synthesized target ARN
// or the source identity is missing, so the edge never dangles.
func associationCodeCommitRelationship(
	boundary awscloud.Boundary,
	association RepositoryAssociation,
) *awscloud.RelationshipObservation {
	if !strings.EqualFold(strings.TrimSpace(association.ProviderType), providerCodeCommit) {
		return nil
	}
	sourceID := associationResourceID(association)
	if sourceID == "" {
		return nil
	}
	targetARN := codeCommitRepositoryARN(boundary, association.Owner, association.Name)
	if targetARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeGuruAssociationReviewsCodeCommitRepository,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(association.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeCodeCommitRepository,
		Attributes: map[string]any{
			"repository_name": strings.TrimSpace(association.Name),
		},
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipCodeGuruAssociationReviewsCodeCommitRepository + ":" + targetARN,
	}
}
