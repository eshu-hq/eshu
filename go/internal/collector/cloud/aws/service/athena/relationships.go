// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package athena

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func workGroupResultBucketRelationship(
	boundary aws.Boundary,
	workGroup WorkGroup,
) *aws.RelationshipObservation {
	bucketARN := outputBucketARN(aws.PartitionForBoundary(boundary), workGroup.OutputLocation)
	if bucketARN == "" {
		return nil
	}
	sourceID := strings.TrimSpace(workGroup.Name)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAthenaWorkGroupUsesResultBucket,
		SourceResourceID: sourceID,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		// Attributes intentionally omits the workgroup OutputLocation URI to
		// keep the relationship payload bucket-only; including the raw URI
		// would leak the result-object prefix and violate the package
		// invariant in README.md / AGENTS.md.
		SourceRecordID: sourceID + "->" + aws.RelationshipAthenaWorkGroupUsesResultBucket + ":" + bucketARN,
	}
}

func workGroupKMSRelationship(
	boundary aws.Boundary,
	workGroup WorkGroup,
) *aws.RelationshipObservation {
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
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAthenaWorkGroupUsesKMSKey,
		SourceResourceID: sourceID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		Attributes: map[string]any{
			"encryption_option": strings.TrimSpace(workGroup.EncryptionOption),
		},
		SourceRecordID: sourceID + "->" + aws.RelationshipAthenaWorkGroupUsesKMSKey + ":" + targetID,
	}
}

func preparedStatementWorkGroupRelationship(
	boundary aws.Boundary,
	statement PreparedStatement,
) *aws.RelationshipObservation {
	sourceID := preparedStatementResourceID(statement)
	target := strings.TrimSpace(statement.WorkGroupName)
	if sourceID == "" || target == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAthenaPreparedStatementInWorkGroup,
		SourceResourceID: sourceID,
		TargetResourceID: target,
		TargetType:       aws.ResourceTypeAthenaWorkGroup,
		Attributes: map[string]any{
			"statement_name": strings.TrimSpace(statement.StatementName),
		},
		SourceRecordID: sourceID + "->" + target,
	}
}

func namedQueryWorkGroupRelationship(
	boundary aws.Boundary,
	query NamedQuery,
) *aws.RelationshipObservation {
	sourceID := namedQueryResourceID(query)
	target := strings.TrimSpace(query.WorkGroupName)
	if sourceID == "" || target == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAthenaNamedQueryInWorkGroup,
		SourceResourceID: sourceID,
		TargetResourceID: target,
		TargetType:       aws.ResourceTypeAthenaWorkGroup,
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
