// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lambda

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func functionRelationships(
	boundary aws.Boundary,
	function Function,
) []aws.RelationshipObservation {
	functionARN := strings.TrimSpace(function.ARN)
	if functionARN == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if roleARN := strings.TrimSpace(function.RoleARN); roleARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipLambdaFunctionUsesExecutionRole,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   functionARN + "#execution-role#" + roleARN,
		})
	}
	if imageURI := strings.TrimSpace(function.ImageURI); imageURI != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipLambdaFunctionUsesImage,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: imageURI,
			TargetType:       containerImageTargetType,
			Attributes: map[string]any{
				"package_type":       strings.TrimSpace(function.PackageType),
				"resolved_image_uri": strings.TrimSpace(function.ResolvedImageURI),
			},
			SourceRecordID: functionARN + "#container-image#" + imageURI,
		})
	}
	for _, subnetID := range function.VPCConfig.SubnetIDs {
		subnetID = strings.TrimSpace(subnetID)
		if subnetID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipLambdaFunctionUsesSubnet,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"vpc_id": strings.TrimSpace(function.VPCConfig.VPCID),
			},
			SourceRecordID: functionARN + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range function.VPCConfig.SecurityGroupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipLambdaFunctionUsesSecurityGroup,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			Attributes: map[string]any{
				"vpc_id": strings.TrimSpace(function.VPCConfig.VPCID),
			},
			SourceRecordID: functionARN + "#security-group#" + groupID,
		})
	}
	return observations
}

func aliasFunctionRelationship(
	boundary aws.Boundary,
	function Function,
	alias Alias,
) (aws.RelationshipObservation, bool) {
	aliasARN := strings.TrimSpace(alias.ARN)
	targetARN := firstNonEmpty(alias.FunctionARN, function.ARN)
	if aliasARN == "" || targetARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipLambdaAliasTargetsFunction,
		SourceResourceID: aliasARN,
		SourceARN:        aliasARN,
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeLambdaFunction,
		Attributes: map[string]any{
			"function_version": strings.TrimSpace(alias.FunctionVersion),
			"routing_weights":  cloneFloatMap(alias.RoutingWeights),
		},
		SourceRecordID: aliasARN + "#function#" + targetARN,
	}, true
}

func eventSourceMappingFunctionRelationship(
	boundary aws.Boundary,
	function Function,
	mapping EventSourceMapping,
) (aws.RelationshipObservation, bool) {
	mappingID := firstNonEmpty(mapping.ARN, mapping.UUID)
	targetARN := firstNonEmpty(mapping.FunctionARN, function.ARN)
	if mappingID == "" || targetARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipLambdaEventSourceMappingTargetsFunction,
		SourceResourceID: mappingID,
		SourceARN:        strings.TrimSpace(mapping.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeLambdaFunction,
		Attributes: map[string]any{
			"event_source_arn": strings.TrimSpace(mapping.EventSourceARN),
			"state":            strings.TrimSpace(mapping.State),
			"uuid":             strings.TrimSpace(mapping.UUID),
		},
		SourceRecordID: mappingID + "#function#" + targetARN,
	}, true
}
