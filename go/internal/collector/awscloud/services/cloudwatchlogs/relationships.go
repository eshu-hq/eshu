package cloudwatchlogs

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func kmsRelationship(boundary awscloud.Boundary, logGroup LogGroup) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(logGroup.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := firstNonEmpty(logGroup.ARN, logGroup.Name)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudWatchLogsLogGroupUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(logGroup.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipCloudWatchLogsLogGroupUsesKMSKey + ":" + targetID,
	}
	return &relationship
}
