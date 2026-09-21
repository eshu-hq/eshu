// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatchlogs

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func kmsRelationship(boundary aws.Boundary, logGroup LogGroup) *aws.RelationshipObservation {
	targetID := strings.TrimSpace(logGroup.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := firstNonEmpty(logGroup.ARN, logGroup.Name)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCloudWatchLogsLogGroupUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(logGroup.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + aws.RelationshipCloudWatchLogsLogGroupUsesKMSKey + ":" + targetID,
	}
	return &relationship
}
