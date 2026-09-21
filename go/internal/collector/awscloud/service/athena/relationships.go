// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package athena

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func workGroupResultBucketRelationship(
	boundary awscloud.Boundary,
	workGroup WorkGroup,
) *awscloud.RelationshipObservation {
	bucketARN := outputBucketARN(awscloud.PartitionForBoundary(boundary), workGroup.OutputLocation)
	if bucketARN == "" {
		return nil
	}
	sourceID := strings.TrimSpace(workGroup.Name)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAthenaWorkGroupUsesResultBucket,
		SourceResourceID: sourceID,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		// Attributes intentionally omits the workgroup OutputLocation URI to
		// keep the relationship payload bucket-only; including the raw URI
		// would leak the result-object prefix and violate the package
		// invariant in README.md / AGENTS.md.
		SourceRecordID: sourceID + "->" + awscloud.RelationshipAthenaWorkGroupUsesResultBucket + ":" + bucketARN,
	}
}

func workGroupKMSRelationship(
	boundary awscloud.Boundary,
	workGroup WorkGroup,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(workGroup.KMSKey)
	if targetID == "" {
		return nil
	}
	sourceID := strings.TrimSpace(workGroup.Name)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAthenaWorkGroupUsesKMSKey,
		SourceResourceID: sourceID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		Attributes: map[string]any{
			"encryption_option": strings.TrimSpace(workGroup.EncryptionOption),
		},
		SourceRecordID: sourceID + "->" + awscloud.RelationshipAthenaWorkGroupUsesKMSKey + ":" + targetID,
	}
}

func preparedStatementWorkGroupRelationship(
	boundary awscloud.Boundary,
	statement PreparedStatement,
) *awscloud.RelationshipObservation {
	sourceID := preparedStatementResourceID(statement)
	target := strings.TrimSpace(statement.WorkGroupName)
	if sourceID == "" || target == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAthenaPreparedStatementInWorkGroup,
		SourceResourceID: sourceID,
		TargetResourceID: target,
		TargetType:       awscloud.ResourceTypeAthenaWorkGroup,
		Attributes: map[string]any{
			"statement_name": strings.TrimSpace(statement.StatementName),
		},
		SourceRecordID: sourceID + "->" + target,
	}
}

func namedQueryWorkGroupRelationship(
	boundary awscloud.Boundary,
	query NamedQuery,
) *awscloud.RelationshipObservation {
	sourceID := namedQueryResourceID(query)
	target := strings.TrimSpace(query.WorkGroupName)
	if sourceID == "" || target == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAthenaNamedQueryInWorkGroup,
		SourceResourceID: sourceID,
		TargetResourceID: target,
		TargetType:       awscloud.ResourceTypeAthenaWorkGroup,
		Attributes: map[string]any{
			"named_query_id": strings.TrimSpace(query.NamedQueryID),
			"query_name":     strings.TrimSpace(query.Name),
		},
		SourceRecordID: sourceID + "->" + target,
	}
}

func preparedStatementResourceID(statement PreparedStatement) string {
	workGroup := strings.TrimSpace(statement.WorkGroupName)
	name := strings.TrimSpace(statement.StatementName)
	if workGroup == "" || name == "" {
		return firstNonEmpty(name, workGroup)
	}
	return workGroup + "/" + name
}

func namedQueryResourceID(query NamedQuery) string {
	id := strings.TrimSpace(query.NamedQueryID)
	if id != "" {
		return id
	}
	workGroup := strings.TrimSpace(query.WorkGroupName)
	name := strings.TrimSpace(query.Name)
	if workGroup == "" {
		return name
	}
	return workGroup + "/" + name
}
