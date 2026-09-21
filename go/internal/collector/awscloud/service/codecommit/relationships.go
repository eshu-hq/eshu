// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codecommit

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// kmsKeyRelationship records the KMS key a CodeCommit repository is encrypted
// with. It returns nil when AWS reports no key (the default AWS-managed key
// case) so no edge is emitted for repositories without a customer key.
//
// The reported key is used directly: CodeCommit returns the key as either a
// bare key id or a full key ARN, both of which are partition-correct identities
// the scanner never synthesizes. When the value is ARN-shaped the edge is
// ARN-keyed (target_arn set); when it is a bare key id the edge keys on the
// bare id, matching how the KMS scanner publishes its key resource_id
// (firstNonEmpty(keyID, keyARN)).
func kmsKeyRelationship(boundary awscloud.Boundary, repository Repository) *awscloud.RelationshipObservation {
	repositoryID := repositoryResourceID(repository)
	keyID := strings.TrimSpace(repository.KMSKeyID)
	if repositoryID == "" || keyID == "" {
		return nil
	}
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeCommitRepositoryEncryptedWithKMSKey,
		SourceResourceID: repositoryID,
		SourceARN:        strings.TrimSpace(repository.ARN),
		TargetResourceID: keyID,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   repositoryID + "->" + awscloud.RelationshipCodeCommitRepositoryEncryptedWithKMSKey + ":" + keyID,
	}
	if isARN(keyID) {
		relationship.TargetARN = keyID
	}
	return &relationship
}

// triggerRelationships records the SNS topics a CodeCommit repository's triggers
// notify. CodeCommit trigger destinations are most commonly SNS topics; only
// SNS-topic destination ARNs produce a typed edge. Non-SNS destinations (for
// example a Lambda function) stay as resource attributes and are not promoted to
// a relationship here so the edge never dangles against the wrong target family.
// Duplicate destination topics across multiple triggers collapse to one edge.
func triggerRelationships(boundary awscloud.Boundary, repository Repository) []awscloud.RelationshipObservation {
	repositoryID := repositoryResourceID(repository)
	if repositoryID == "" || len(repository.Triggers) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(repository.Triggers))
	seen := make(map[string]struct{}, len(repository.Triggers))
	for _, trigger := range repository.Triggers {
		destination := strings.TrimSpace(trigger.DestinationARN)
		if !isSNSTopicARN(destination) {
			continue
		}
		if _, ok := seen[destination]; ok {
			continue
		}
		seen[destination] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeCommitRepositoryTriggersSNSTopic,
			SourceResourceID: repositoryID,
			SourceARN:        strings.TrimSpace(repository.ARN),
			TargetResourceID: destination,
			TargetARN:        destination,
			TargetType:       awscloud.ResourceTypeSNSTopic,
			Attributes: map[string]any{
				"trigger_name": strings.TrimSpace(trigger.Name),
				"events":       cloneStringSlice(trigger.Events),
			},
			SourceRecordID: repositoryID + "->" + awscloud.RelationshipCodeCommitRepositoryTriggersSNSTopic + ":" + destination,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// repositoryResourceID resolves the stable identity for a repository edge
// source. The repository ARN is preferred; the repository name is the fallback
// so an edge still keys on a stable value when an ARN is unexpectedly absent.
func repositoryResourceID(repository Repository) string {
	return firstNonEmpty(repository.ARN, repository.Name)
}
