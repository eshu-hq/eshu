// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

func kmsRelationship(boundary awscloud.Boundary, parameter Parameter) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(parameter.KeyID)
	if targetID == "" {
		return nil
	}
	sourceID := parameterResourceID(parameter)
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipSSMParameterUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(parameter.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       "aws_kms_key",
		SourceRecordID:   sourceID + "->" + catalog.RelationshipSSMParameterUsesKMSKey + ":" + targetID,
	}
	return &relationship
}

func parameterResourceID(parameter Parameter) string {
	return firstNonEmpty(parameter.ARN, parameter.Name)
}
