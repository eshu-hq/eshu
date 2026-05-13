package lambda

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func functionRelationships(
	boundary awscloud.Boundary,
	function Function,
) []awscloud.RelationshipObservation {
	functionARN := strings.TrimSpace(function.ARN)
	if functionARN == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if roleARN := strings.TrimSpace(function.RoleARN); roleARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipLambdaFunctionUsesExecutionRole,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   functionARN + "#execution-role#" + roleARN,
		})
	}
	if imageURI := strings.TrimSpace(function.ImageURI); imageURI != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipLambdaFunctionUsesImage,
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipLambdaFunctionUsesSubnet,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipLambdaFunctionUsesSecurityGroup,
			SourceResourceID: functionARN,
			SourceARN:        functionARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			Attributes: map[string]any{
				"vpc_id": strings.TrimSpace(function.VPCConfig.VPCID),
			},
			SourceRecordID: functionARN + "#security-group#" + groupID,
		})
	}
	return observations
}

func aliasFunctionRelationship(
	boundary awscloud.Boundary,
	function Function,
	alias Alias,
) (awscloud.RelationshipObservation, bool) {
	aliasARN := strings.TrimSpace(alias.ARN)
	targetARN := firstNonEmpty(alias.FunctionARN, function.ARN)
	if aliasARN == "" || targetARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLambdaAliasTargetsFunction,
		SourceResourceID: aliasARN,
		SourceARN:        aliasARN,
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeLambdaFunction,
		Attributes: map[string]any{
			"function_version": strings.TrimSpace(alias.FunctionVersion),
			"routing_weights":  cloneFloatMap(alias.RoutingWeights),
		},
		SourceRecordID: aliasARN + "#function#" + targetARN,
	}, true
}

func eventSourceMappingFunctionRelationship(
	boundary awscloud.Boundary,
	function Function,
	mapping EventSourceMapping,
) (awscloud.RelationshipObservation, bool) {
	mappingID := firstNonEmpty(mapping.ARN, mapping.UUID)
	targetARN := firstNonEmpty(mapping.FunctionARN, function.ARN)
	if mappingID == "" || targetARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLambdaEventSourceMappingTargetsFunction,
		SourceResourceID: mappingID,
		SourceARN:        strings.TrimSpace(mapping.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeLambdaFunction,
		Attributes: map[string]any{
			"event_source_arn": strings.TrimSpace(mapping.EventSourceARN),
			"state":            strings.TrimSpace(mapping.State),
			"uuid":             strings.TrimSpace(mapping.UUID),
		},
		SourceRecordID: mappingID + "#function#" + targetARN,
	}, true
}
