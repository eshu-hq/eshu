// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsmanager

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func kmsRelationship(boundary aws.Boundary, secret Secret) *aws.RelationshipObservation {
	targetID := strings.TrimSpace(secret.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := secretResourceID(secret)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecretsManagerSecretUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(secret.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecretsManagerSecretUsesKMSKey + ":" + targetID,
	}
	return &relationship
}

func rotationLambdaRelationship(boundary aws.Boundary, secret Secret) *aws.RelationshipObservation {
	targetARN := strings.TrimSpace(secret.RotationLambdaARN)
	if targetARN == "" {
		return nil
	}
	sourceID := secretResourceID(secret)
	relationship := aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSecretsManagerSecretUsesRotationLambda,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(secret.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeLambdaFunction,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSecretsManagerSecretUsesRotationLambda + ":" + targetARN,
	}
	return &relationship
}

func secretResourceID(secret Secret) string {
	return firstNonEmpty(secret.ARN, secret.Name)
}
