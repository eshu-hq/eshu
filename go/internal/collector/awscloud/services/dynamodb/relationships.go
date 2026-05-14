package dynamodb

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func kmsRelationship(boundary awscloud.Boundary, table Table) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(table.SSE.KMSMasterKeyARN)
	if targetID == "" {
		return nil
	}
	sourceID := firstNonEmpty(table.ARN, table.ID, table.Name)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDynamoDBTableUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(table.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDynamoDBTableUsesKMSKey + ":" + targetID,
	}
	return &relationship
}
