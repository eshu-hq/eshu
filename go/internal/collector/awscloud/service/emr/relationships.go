// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package emr

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

// clusterRelationships emits the EMR on EC2 cluster join evidence. The EMR
// cluster API does not report a VPC id directly, so the cluster-to-VPC join is
// derived from subnet membership downstream; this function emits the subnet,
// security group, IAM role, instance profile, security configuration, and KMS
// edges that AWS does report.
func clusterRelationships(boundary awscloud.Boundary, cluster Cluster) []awscloud.RelationshipObservation {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	if clusterID == "" {
		return nil
	}
	clusterARN := strings.TrimSpace(cluster.ARN)
	var observations []awscloud.RelationshipObservation

	for _, subnetID := range dedupe(append([]string{cluster.SubnetID}, cluster.RequestedSubnetIDs...)) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRClusterUsesSubnet,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: subnetID,
			TargetType:       catalog.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(clusterID, catalog.RelationshipEMRClusterUsesSubnet, subnetID),
		})
	}

	for _, groupID := range dedupe(cluster.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRClusterUsesSecurityGroup,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: groupID,
			TargetType:       catalog.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(clusterID, catalog.RelationshipEMRClusterUsesSecurityGroup, groupID),
		})
	}

	for _, role := range dedupe([]string{cluster.ServiceRole, cluster.AutoScalingRole}) {
		observations = append(observations, iamRoleRelationship(
			boundary,
			catalog.RelationshipEMRClusterUsesIAMRole,
			clusterID,
			clusterARN,
			role,
		))
	}

	if profile := strings.TrimSpace(cluster.InstanceProfile); profile != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRClusterUsesInstanceProfile,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: profile,
			TargetARN:        arnOrEmpty(profile),
			TargetType:       awscloud.ResourceTypeIAMInstanceProfile,
			SourceRecordID:   relationshipRecordID(clusterID, catalog.RelationshipEMRClusterUsesInstanceProfile, profile),
		})
	}

	if config := strings.TrimSpace(cluster.SecurityConfigName); config != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRClusterUsesSecurityConfiguration,
			SourceResourceID: clusterID,
			SourceARN:        clusterARN,
			TargetResourceID: config,
			TargetType:       catalog.ResourceTypeEMRSecurityConfiguration,
			SourceRecordID:   relationshipRecordID(clusterID, catalog.RelationshipEMRClusterUsesSecurityConfiguration, config),
		})
	}

	if kmsKey := strings.TrimSpace(cluster.LogEncryptionKMSKey); kmsKey != "" {
		observations = append(observations, kmsKeyRelationship(
			boundary,
			catalog.RelationshipEMRClusterUsesKMSKey,
			clusterID,
			clusterARN,
			kmsKey,
		))
	}

	return observations
}

func clusterInstanceGroupRelationship(
	boundary awscloud.Boundary,
	cluster Cluster,
	group InstanceGroup,
) (awscloud.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	groupID := scopedID(cluster, group.ID)
	if clusterID == "" || strings.TrimSpace(group.ID) == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipEMRClusterHasInstanceGroup,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: groupID,
		TargetType:       catalog.ResourceTypeEMRInstanceGroup,
		SourceRecordID:   relationshipRecordID(clusterID, catalog.RelationshipEMRClusterHasInstanceGroup, groupID),
	}, true
}

func clusterInstanceFleetRelationship(
	boundary awscloud.Boundary,
	cluster Cluster,
	fleet InstanceFleet,
) (awscloud.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.ID)
	fleetID := scopedID(cluster, fleet.ID)
	if clusterID == "" || strings.TrimSpace(fleet.ID) == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipEMRClusterHasInstanceFleet,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: fleetID,
		TargetType:       catalog.ResourceTypeEMRInstanceFleet,
		SourceRecordID:   relationshipRecordID(clusterID, catalog.RelationshipEMRClusterHasInstanceFleet, fleetID),
	}, true
}

// serverlessApplicationRelationships emits the EMR Serverless application join
// evidence. As with EMR on EC2 clusters, the EMR Serverless API does not report
// a VPC id directly, so the application-to-VPC join is derived from subnet
// membership downstream.
func serverlessApplicationRelationships(
	boundary awscloud.Boundary,
	application ServerlessApplication,
) []awscloud.RelationshipObservation {
	applicationID := firstNonEmpty(application.ARN, application.ID)
	if applicationID == "" {
		return nil
	}
	applicationARN := strings.TrimSpace(application.ARN)
	var observations []awscloud.RelationshipObservation

	for _, subnetID := range dedupe(application.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRServerlessApplicationUsesSubnet,
			SourceResourceID: applicationID,
			SourceARN:        applicationARN,
			TargetResourceID: subnetID,
			TargetType:       catalog.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(applicationID, catalog.RelationshipEMRServerlessApplicationUsesSubnet, subnetID),
		})
	}

	for _, groupID := range dedupe(application.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRServerlessApplicationUsesSecurityGroup,
			SourceResourceID: applicationID,
			SourceARN:        applicationARN,
			TargetResourceID: groupID,
			TargetType:       catalog.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(applicationID, catalog.RelationshipEMRServerlessApplicationUsesSecurityGroup, groupID),
		})
	}

	if kmsKey := strings.TrimSpace(application.DiskEncryptKMS); kmsKey != "" {
		observations = append(observations, kmsKeyRelationship(
			boundary,
			catalog.RelationshipEMRServerlessApplicationUsesKMSKey,
			applicationID,
			applicationARN,
			kmsKey,
		))
	}

	return observations
}

func studioRelationships(boundary awscloud.Boundary, studio Studio) []awscloud.RelationshipObservation {
	studioID := firstNonEmpty(studio.ARN, studio.ID)
	if studioID == "" {
		return nil
	}
	studioARN := strings.TrimSpace(studio.ARN)
	var observations []awscloud.RelationshipObservation

	if vpcID := strings.TrimSpace(studio.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRStudioInVPC,
			SourceResourceID: studioID,
			SourceARN:        studioARN,
			TargetResourceID: vpcID,
			TargetType:       catalog.ResourceTypeEC2VPC,
			SourceRecordID:   relationshipRecordID(studioID, catalog.RelationshipEMRStudioInVPC, vpcID),
		})
	}

	for _, subnetID := range dedupe(studio.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRStudioUsesSubnet,
			SourceResourceID: studioID,
			SourceARN:        studioARN,
			TargetResourceID: subnetID,
			TargetType:       catalog.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(studioID, catalog.RelationshipEMRStudioUsesSubnet, subnetID),
		})
	}

	for _, groupID := range dedupe([]string{studio.EngineSecGroupID, studio.WorkspaceSecGroup}) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipEMRStudioUsesSecurityGroup,
			SourceResourceID: studioID,
			SourceARN:        studioARN,
			TargetResourceID: groupID,
			TargetType:       catalog.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(studioID, catalog.RelationshipEMRStudioUsesSecurityGroup, groupID),
		})
	}

	for _, role := range dedupe([]string{studio.ServiceRole, studio.UserRole}) {
		observations = append(observations, iamRoleRelationship(
			boundary,
			catalog.RelationshipEMRStudioUsesIAMRole,
			studioID,
			studioARN,
			role,
		))
	}

	if kmsKey := strings.TrimSpace(studio.EncryptionKeyARN); kmsKey != "" {
		observations = append(observations, kmsKeyRelationship(
			boundary,
			catalog.RelationshipEMRStudioUsesKMSKey,
			studioID,
			studioARN,
			kmsKey,
		))
	}

	return observations
}

func studioSessionMappingRelationship(
	boundary awscloud.Boundary,
	studio Studio,
	mapping StudioSessionMapping,
) (awscloud.RelationshipObservation, bool) {
	studioID := firstNonEmpty(studio.ARN, studio.ID)
	mappingID := sessionMappingID(studio, mapping)
	if studioID == "" || mappingID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipEMRStudioHasSessionMapping,
		SourceResourceID: studioID,
		SourceARN:        strings.TrimSpace(studio.ARN),
		TargetResourceID: mappingID,
		TargetType:       catalog.ResourceTypeEMRStudioSessionMapping,
		SourceRecordID:   relationshipRecordID(studioID, catalog.RelationshipEMRStudioHasSessionMapping, mappingID),
	}, true
}

func iamRoleRelationship(
	boundary awscloud.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	role string,
) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: role,
		TargetARN:        arnOrEmpty(role),
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   relationshipRecordID(sourceID, relationshipType, role),
	}
}

func kmsKeyRelationship(
	boundary awscloud.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	kmsKey string,
) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: kmsKey,
		TargetARN:        arnOrEmpty(kmsKey),
		TargetType:       awscloud.ResourceTypeKMSKey,
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

// timeOrNil mirrors the awscloud envelope helper so attribute maps carry a
// nil for zero times instead of the Go zero instant.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
