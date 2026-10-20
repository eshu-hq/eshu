// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appstream

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// fleetRelationships returns the reported dependency edges for one fleet: VPC
// subnets and security groups (bare ids, matching the EC2 scanner), the applied
// IAM role (ARN, matching the IAM scanner), and the source image (image ARN,
// matching this scanner's published image node). It returns nil when the fleet
// has no resolvable identity.
func fleetRelationships(boundary awscloud.Boundary, fleet Fleet) []awscloud.RelationshipObservation {
	fleetID := fleetResourceID(fleet)
	if fleetID == "" {
		return nil
	}
	fleetARN := strings.TrimSpace(fleet.ARN)
	var observations []awscloud.RelationshipObservation
	for _, subnetID := range cloneStrings(fleet.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamFleetUsesSubnet,
			SourceResourceID: fleetID,
			SourceARN:        fleetARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   fleetID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range cloneStrings(fleet.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamFleetUsesSecurityGroup,
			SourceResourceID: fleetID,
			SourceARN:        fleetARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   fleetID + "#security-group#" + groupID,
		})
	}
	if roleARN := strings.TrimSpace(fleet.IAMRoleARN); roleARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamFleetUsesIAMRole,
			SourceResourceID: fleetID,
			SourceARN:        fleetARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   fleetID + "#role#" + roleARN,
		})
	}
	if imageARN := strings.TrimSpace(fleet.ImageARN); imageARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamFleetUsesImage,
			SourceResourceID: fleetID,
			SourceARN:        fleetARN,
			TargetResourceID: imageARN,
			TargetARN:        imageARN,
			TargetType:       awscloud.ResourceTypeAppStreamImage,
			SourceRecordID:   fleetID + "#image#" + imageARN,
		})
	}
	return observations
}

// imageBuilderRelationships returns the reported dependency edges for one image
// builder: VPC subnets and security groups (bare ids), the applied IAM role
// (ARN), and the base image (image ARN). It returns nil when the builder has no
// resolvable identity.
func imageBuilderRelationships(boundary awscloud.Boundary, builder ImageBuilder) []awscloud.RelationshipObservation {
	builderID := imageBuilderResourceID(builder)
	if builderID == "" {
		return nil
	}
	builderARN := strings.TrimSpace(builder.ARN)
	var observations []awscloud.RelationshipObservation
	for _, subnetID := range cloneStrings(builder.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamImageBuilderUsesSubnet,
			SourceResourceID: builderID,
			SourceARN:        builderARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   builderID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range cloneStrings(builder.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamImageBuilderUsesSecurityGroup,
			SourceResourceID: builderID,
			SourceARN:        builderARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   builderID + "#security-group#" + groupID,
		})
	}
	if roleARN := strings.TrimSpace(builder.IAMRoleARN); roleARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamImageBuilderUsesIAMRole,
			SourceResourceID: builderID,
			SourceARN:        builderARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   builderID + "#role#" + roleARN,
		})
	}
	if imageARN := strings.TrimSpace(builder.ImageARN); imageARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamImageBuilderUsesImage,
			SourceResourceID: builderID,
			SourceARN:        builderARN,
			TargetResourceID: imageARN,
			TargetARN:        imageARN,
			TargetType:       awscloud.ResourceTypeAppStreamImage,
			SourceRecordID:   builderID + "#image#" + imageARN,
		})
	}
	return observations
}

// fleetStackRelationship records a fleet-to-stack association. AppStream's
// association API reports the stack by NAME, so the scanner resolves the stack
// name to the resource_id the stack node publishes (its ARN when known, else the
// name) via stackIDByName. It returns false when either endpoint is unresolved.
func fleetStackRelationship(
	boundary awscloud.Boundary,
	association FleetStackAssociation,
	fleetIDByName map[string]string,
	fleetARNByName map[string]string,
	stackIDByName map[string]string,
) (awscloud.RelationshipObservation, bool) {
	fleetName := strings.TrimSpace(association.FleetName)
	stackName := strings.TrimSpace(association.StackName)
	if fleetName == "" || stackName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	fleetID := firstNonEmpty(fleetIDByName[fleetName], fleetName)
	stackID := firstNonEmpty(stackIDByName[stackName], stackName)
	if fleetID == "" || stackID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetARN := ""
	if isARN(stackID) {
		targetARN = stackID
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppStreamFleetAssociatedWithStack,
		SourceResourceID: fleetID,
		SourceARN:        strings.TrimSpace(fleetARNByName[fleetName]),
		TargetResourceID: stackID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeAppStreamStack,
		Attributes: map[string]any{
			"fleet_name": fleetName,
			"stack_name": stackName,
		},
		SourceRecordID: fleetID + "#stack#" + stackID,
	}, true
}

// stackS3Relationships returns the S3 bucket edges a stack reports: persistent
// application-settings storage and home-folders storage connectors. AppStream
// reports bucket NAMES, so the scanner synthesizes the partition-aware bucket
// ARN to match the S3 scanner's published bucket resource_id. It returns nil
// when the stack has no resolvable identity or no reported buckets.
func stackS3Relationships(boundary awscloud.Boundary, stack Stack) []awscloud.RelationshipObservation {
	stackID := stackResourceID(stack)
	if stackID == "" {
		return nil
	}
	stackARN := strings.TrimSpace(stack.ARN)
	partition := awscloud.PartitionForBoundary(boundary)
	buckets := make([]string, 0, 1+len(stack.StorageConnectorBuckets))
	bucketUses := map[string]string{}
	if appSettings := strings.TrimSpace(stack.ApplicationSettingsS3Bucket); appSettings != "" {
		buckets = append(buckets, appSettings)
		bucketUses[appSettings] = "application_settings"
	}
	for _, connectorBucket := range cloneStrings(stack.StorageConnectorBuckets) {
		buckets = append(buckets, connectorBucket)
		if _, ok := bucketUses[connectorBucket]; !ok {
			bucketUses[connectorBucket] = "storage_connector"
		}
	}
	var observations []awscloud.RelationshipObservation
	emitted := map[string]struct{}{}
	for _, bucket := range buckets {
		bucketARN := arnForBucket(partition, bucket)
		if bucketARN == "" {
			continue
		}
		if _, ok := emitted[bucketARN]; ok {
			continue
		}
		emitted[bucketARN] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppStreamStackUsesS3Bucket,
			SourceResourceID: stackID,
			SourceARN:        stackARN,
			TargetResourceID: bucketARN,
			TargetARN:        bucketARN,
			TargetType:       awscloud.ResourceTypeS3Bucket,
			Attributes: map[string]any{
				"bucket": bucket,
				"usage":  bucketUses[bucket],
			},
			SourceRecordID: stackID + "#s3#" + bucketARN,
		})
	}
	return observations
}
