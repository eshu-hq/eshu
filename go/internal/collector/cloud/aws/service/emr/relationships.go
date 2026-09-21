// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package emr

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// clusterRelationships emits the EMR on EC2 cluster join evidence. The EMR
// cluster API does not report a VPC id directly, so the cluster-to-VPC join is
// derived from subnet membership downstream; this function emits the subnet,
// security group, IAM role, instance profile, security configuration, and KMS
// edges that AWS does report.
func clusterRelationships(boundary aws.Boundary, cluster Cluster) []aws.RelationshipObservation {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	if clusterID == "" {
		return nil
	}
	clusterARN := strings.TrimSpace(cluster.ARN)
	var observations []aws.RelationshipObservation

	for _, subnetID := range dedupe(append([]string{cluster.SubnetID}, cluster.RequestedSubnetIDs...)) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRClusterUsesSubnet,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(clusterID, aws.RelationshipEMRClusterUsesSubnet, subnetID),
		})
	}

	for _, groupID := range dedupe(cluster.SecurityGroupIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRClusterUsesSecurityGroup,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(clusterID, aws.RelationshipEMRClusterUsesSecurityGroup, groupID),
		})
	}

	for _, role := range dedupe([]string{cluster.ServiceRole, cluster.AutoScalingRole}) {
		observations = append(observations, iamRoleRelationship(
			boundary,
			aws.RelationshipEMRClusterUsesIAMRole,
			clusterID,
			clusterARN,
			role,
		))
	}

	if profile := strings.TrimSpace(cluster.InstanceProfile); profile != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRClusterUsesInstanceProfile,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: profile,
			TargetARN:        arnOrEmpty(profile),
			TargetType:       aws.ResourceTypeIAMInstanceProfile,
			SourceRecordID:   relationshipRecordID(clusterID, aws.RelationshipEMRClusterUsesInstanceProfile, profile),
		})
	}

	if config := strings.TrimSpace(cluster.SecurityConfigName); config != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRClusterUsesSecurityConfiguration,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: config,
			TargetType:       aws.ResourceTypeEMRSecurityConfiguration,
			SourceRecordID:   relationshipRecordID(clusterID, aws.RelationshipEMRClusterUsesSecurityConfiguration, config),
		})
	}

	if kmsKey := strings.TrimSpace(cluster.LogEncryptionKMSKey); kmsKey != "" {
		observations = append(observations, kmsKeyRelationship(
			boundary,
			aws.RelationshipEMRClusterUsesKMSKey,
			clusterID,
			clusterARN,
			kmsKey,
		))
	}

	return observations
}

func clusterInstanceGroupRelationship(
	boundary aws.Boundary,
	cluster Cluster,
	group InstanceGroup,
) (aws.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	groupID := scopedID(cluster, group.ID)
	if clusterID == "" || strings.TrimSpace(group.ID) == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEMRClusterHasInstanceGroup,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: groupID,
		TargetType:       aws.ResourceTypeEMRInstanceGroup,
		SourceRecordID:   relationshipRecordID(clusterID, aws.RelationshipEMRClusterHasInstanceGroup, groupID),
	}, true
}

func clusterInstanceFleetRelationship(
	boundary aws.Boundary,
	cluster Cluster,
	fleet InstanceFleet,
) (aws.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	fleetID := scopedID(cluster, fleet.ID)
	if clusterID == "" || strings.TrimSpace(fleet.ID) == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEMRClusterHasInstanceFleet,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: fleetID,
		TargetType:       aws.ResourceTypeEMRInstanceFleet,
		SourceRecordID:   relationshipRecordID(clusterID, aws.RelationshipEMRClusterHasInstanceFleet, fleetID),
	}, true
}

// serverlessApplicationRelationships emits the EMR Serverless application join
// evidence. As with EMR on EC2 clusters, the EMR Serverless API does not report
// a VPC id directly, so the application-to-VPC join is derived from subnet
// membership downstream.
func serverlessApplicationRelationships(
	boundary aws.Boundary,
	application ServerlessApplication,
) []aws.RelationshipObservation {
	applicationID := firstNonEmpty(application.ARN, application.ID)
	if applicationID == "" {
		return nil
	}
	applicationARN := strings.TrimSpace(application.ARN)
	var observations []aws.RelationshipObservation

	for _, subnetID := range dedupe(application.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRServerlessApplicationUsesSubnet,
			SourceResourceID: applicationID,
			SourceARN:        applicationARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(applicationID, aws.RelationshipEMRServerlessApplicationUsesSubnet, subnetID),
		})
	}

	for _, groupID := range dedupe(application.SecurityGroupIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRServerlessApplicationUsesSecurityGroup,
			SourceResourceID: applicationID,
			SourceARN:        applicationARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(applicationID, aws.RelationshipEMRServerlessApplicationUsesSecurityGroup, groupID),
		})
	}

	if kmsKey := strings.TrimSpace(application.DiskEncryptKMS); kmsKey != "" {
		observations = append(observations, kmsKeyRelationship(
			boundary,
			aws.RelationshipEMRServerlessApplicationUsesKMSKey,
			applicationID,
			applicationARN,
			kmsKey,
		))
	}

	return observations
}

func studioRelationships(boundary aws.Boundary, studio Studio) []aws.RelationshipObservation {
	studioID := firstNonEmpty(studio.ARN, studio.ID)
	if studioID == "" {
		return nil
	}
	studioARN := strings.TrimSpace(studio.ARN)
	var observations []aws.RelationshipObservation

	if vpcID := strings.TrimSpace(studio.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRStudioInVPC,
			SourceResourceID: studioID,
			SourceARN:        studioARN,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   relationshipRecordID(studioID, aws.RelationshipEMRStudioInVPC, vpcID),
		})
	}

	for _, subnetID := range dedupe(studio.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRStudioUsesSubnet,
			SourceResourceID: studioID,
			SourceARN:        studioARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(studioID, aws.RelationshipEMRStudioUsesSubnet, subnetID),
		})
	}

	for _, groupID := range dedupe([]string{studio.EngineSecGroupID, studio.WorkspaceSecGroup}) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEMRStudioUsesSecurityGroup,
			SourceResourceID: studioID,
			SourceARN:        studioARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(studioID, aws.RelationshipEMRStudioUsesSecurityGroup, groupID),
		})
	}

	for _, role := range dedupe([]string{studio.ServiceRole, studio.UserRole}) {
		observations = append(observations, iamRoleRelationship(
			boundary,
			aws.RelationshipEMRStudioUsesIAMRole,
			studioID,
			studioARN,
			role,
		))
	}

	if kmsKey := strings.TrimSpace(studio.EncryptionKeyARN); kmsKey != "" {
		observations = append(observations, kmsKeyRelationship(
			boundary,
			aws.RelationshipEMRStudioUsesKMSKey,
			studioID,
			studioARN,
			kmsKey,
		))
	}

	return observations
}

func studioSessionMappingRelationship(
	boundary aws.Boundary,
	studio Studio,
	mapping StudioSessionMapping,
) (aws.RelationshipObservation, bool) {
	studioID := firstNonEmpty(studio.ARN, studio.ID)
	mappingID := sessionMappingID(studio, mapping)
	if studioID == "" || mappingID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEMRStudioHasSessionMapping,
		SourceResourceID: studioID,
		SourceARN:        strings.TrimSpace(studio.ARN),
		TargetResourceID: mappingID,
		TargetType:       aws.ResourceTypeEMRStudioSessionMapping,
		SourceRecordID:   relationshipRecordID(studioID, aws.RelationshipEMRStudioHasSessionMapping, mappingID),
	}, true
}

func iamRoleRelationship(
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	role string,
) aws.RelationshipObservation {
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: role,
		TargetARN:        arnOrEmpty(role),
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   relationshipRecordID(sourceID, relationshipType, role),
	}
}

func kmsKeyRelationship(
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	kmsKey string,
) aws.RelationshipObservation {
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: kmsKey,
		TargetARN:        arnOrEmpty(kmsKey),
		TargetType:       aws.ResourceTypeKMSKey,
		SourceRecordID:   relationshipRecordID(sourceID, relationshipType, kmsKey),
	}
}

// scopedID namespaces a child resource id under its cluster so instance group
// and instance fleet identities stay distinct across clusters that may reuse
// AWS-assigned ids in different generations.
func scopedID(cluster Cluster, childID string) string {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	childID = strings.TrimSpace(childID)
	if clusterID == "" {
		return childID
	}
	if childID == "" {
		return ""
	}
	return clusterID + "/" + childID
}

// sessionMappingID namespaces a session mapping under its studio with the
// identity type and id, which together uniquely key a mapping.
func sessionMappingID(studio Studio, mapping StudioSessionMapping) string {
	studioID := firstNonEmpty(studio.ARN, studio.ID)
	identity := firstNonEmpty(mapping.IdentityID, mapping.IdentityName)
	if studioID == "" || identity == "" {
		return ""
	}
	identityType := strings.TrimSpace(mapping.IdentityType)
	if identityType == "" {
		return studioID + "/session-mapping/" + identity
	}
	return studioID + "/session-mapping/" + strings.ToLower(identityType) + ":" + identity
}

// relationshipRecordID encodes the relationship type into the durable
// SourceRecordID alongside the source and target identity. Including the
// relationship type keeps each edge's source ref distinct when one source has
// multiple edges to the same target, mirroring the ElastiCache and RDS
// scanners.
func relationshipRecordID(sourceID, relationshipType, targetID string) string {
	return strings.TrimSpace(sourceID) + "->" + strings.TrimSpace(relationshipType) + ":" + strings.TrimSpace(targetID)
}

// arnOrEmpty returns value when it is ARN-shaped so the relationship can carry
// target_arn for ARN targets while EMR-reported bare names still join through
// correlation anchors. Partition is preserved from the source value; no ARN is
// synthesized.
func arnOrEmpty(value string) string {
	if isARN(value) {
		return strings.TrimSpace(value)
	}
	return ""
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// dedupe trims, drops empties, and removes duplicates while preserving order.
func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	return output
}

// timeOrNil mirrors the aws envelope helper so attribute maps carry a
// nil for zero times instead of the Go zero instant.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
