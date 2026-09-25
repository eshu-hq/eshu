// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dynamodb

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func kmsRelationship(boundary aws.Boundary, table Table) *aws.RelationshipObservation {
	targetID := strings.TrimSpace(table.SSE.KMSMasterKeyARN)
	if targetID == "" {
		return nil
	}
	sourceID := firstNonEmpty(table.ARN, table.ID, table.Name)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDynamoDBTableUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(table.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + aws.RelationshipDynamoDBTableUsesKMSKey + ":" + targetID,
	}
	return &relationship
}
