package secretsmanager

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func kmsRelationship(boundary awscloud.Boundary, secret Secret) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(secret.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := secretResourceID(secret)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSecretsManagerSecretUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(secret.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipSecretsManagerSecretUsesKMSKey + ":" + targetID,
	}
	return &relationship
}

func rotationLambdaRelationship(boundary awscloud.Boundary, secret Secret) *awscloud.RelationshipObservation {
	targetARN := strings.TrimSpace(secret.RotationLambdaARN)
	if targetARN == "" {
		return nil
	}
	sourceID := secretResourceID(secret)
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipSecretsManagerSecretUsesRotationLambda,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(secret.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeLambdaFunction,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipSecretsManagerSecretUsesRotationLambda + ":" + targetARN,
	}
	return &relationship
}

func secretResourceID(secret Secret) string {
	return firstNonEmpty(secret.ARN, secret.Name)
}
