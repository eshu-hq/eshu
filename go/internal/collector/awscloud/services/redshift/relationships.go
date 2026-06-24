// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redshift

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func clusterRelationships(
	boundary awscloud.Boundary,
	cluster Cluster,
	parameterGroupIDs map[string]string,
	subnetGroupIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.Identifier)
	if sourceID == "" {
		return nil
	}
	clusterARN := strings.TrimSpace(cluster.ARN)
	var relationships []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(cluster.VPCID); vpcID != "" {
		relationships = append(relationships, namedRelationship(
			boundary,
			awscloud.RelationshipRedshiftClusterInVPC,
			sourceID,
			clusterARN,
			vpcID,
			awscloud.ResourceTypeEC2VPC,
			nil,
		))
	}
	if targetID := subnetGroupIDs[strings.TrimSpace(cluster.ClusterSubnetGroupName)]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftClusterInSubnetGroup,
			sourceID,
			clusterARN,
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRedshiftClusterSubnetGroup,
			map[string]any{"cluster_subnet_group_name": strings.TrimSpace(cluster.ClusterSubnetGroupName)},
		))
	}
	if targetID := parameterGroupIDs[strings.TrimSpace(cluster.ClusterParameterGroup)]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftClusterUsesParameterGroup,
			sourceID,
			clusterARN,
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRedshiftClusterParameterGroup,
			map[string]any{"cluster_parameter_group_name": strings.TrimSpace(cluster.ClusterParameterGroup)},
		))
	}
	for _, groupID := range cloneStrings(cluster.VPCSecurityGroupIDs) {
		targetARN := securityGroupARN(boundary, groupID)
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftClusterUsesSecurityGroup,
			sourceID,
			clusterARN,
			targetARN,
			targetARN,
			awscloud.ResourceTypeEC2SecurityGroup,
			map[string]any{"security_group_id": groupID},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRedshiftClusterUsesKMSKey,
		sourceID,
		clusterARN,
		strings.TrimSpace(cluster.KMSKeyID),
		"aws_kms_key",
		nil,
	)...)
	for _, roleARN := range cloneStrings(cluster.IAMRoleARNs) {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftClusterUsesIAMRole,
			sourceID,
			clusterARN,
			roleARN,
			roleARN,
			awscloud.ResourceTypeIAMRole,
			nil,
		))
	}
	return relationships
}

func subnetGroupVPCRelationship(
	boundary awscloud.Boundary,
	group ClusterSubnetGroup,
) (awscloud.RelationshipObservation, bool) {
	sourceID := firstNonEmpty(group.ARN, group.Name)
	vpcID := strings.TrimSpace(group.VPCID)
	if sourceID == "" || vpcID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return namedRelationship(
		boundary,
		awscloud.RelationshipRedshiftClusterSubnetGroupInVPC,
		sourceID,
		strings.TrimSpace(group.ARN),
		vpcID,
		awscloud.ResourceTypeEC2VPC,
		nil,
	), true
}

func snapshotRelationships(
	boundary awscloud.Boundary,
	snapshot ClusterSnapshot,
	clusterIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(snapshot.ARN, snapshot.Identifier)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	clusterIdentifier := strings.TrimSpace(snapshot.ClusterIdentifier)
	if targetID := clusterIDs[clusterIdentifier]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftClusterSnapshotOfCluster,
			sourceID,
			strings.TrimSpace(snapshot.ARN),
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRedshiftCluster,
			map[string]any{"cluster_identifier": clusterIdentifier},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRedshiftClusterSnapshotUsesKMSKey,
		sourceID,
		strings.TrimSpace(snapshot.ARN),
		strings.TrimSpace(snapshot.KMSKeyID),
		"aws_kms_key",
		nil,
	)...)
	return relationships
}

func scheduledActionRelationships(
	boundary awscloud.Boundary,
	action ScheduledAction,
	clusterIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(action.Name)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	clusterIdentifier := strings.TrimSpace(action.TargetClusterIdentifier)
	if targetID := clusterIDs[clusterIdentifier]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftScheduledActionTargetsCluster,
			sourceID,
			"",
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRedshiftCluster,
			map[string]any{
				"target_action_name": strings.TrimSpace(action.TargetActionName),
				"cluster_identifier": clusterIdentifier,
			},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRedshiftScheduledActionUsesIAMRole,
		sourceID,
		"",
		strings.TrimSpace(action.IAMRoleARN),
		awscloud.ResourceTypeIAMRole,
		nil,
	)...)
	return relationships
}

func serverlessNamespaceRelationships(
	boundary awscloud.Boundary,
	namespace ServerlessNamespace,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(namespace.ARN, namespace.Name)
	if sourceID == "" {
		return nil
	}
	namespaceARN := strings.TrimSpace(namespace.ARN)
	var relationships []awscloud.RelationshipObservation
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRedshiftServerlessNamespaceUsesKMSKey,
		sourceID,
		namespaceARN,
		strings.TrimSpace(namespace.KMSKeyID),
		"aws_kms_key",
		nil,
	)...)
	for _, roleARN := range cloneStrings(namespace.IAMRoleARNs) {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftServerlessNamespaceUsesIAMRole,
			sourceID,
			namespaceARN,
			roleARN,
			roleARN,
			awscloud.ResourceTypeIAMRole,
			nil,
		))
	}
	return relationships
}

func serverlessWorkgroupRelationships(
	boundary awscloud.Boundary,
	workgroup ServerlessWorkgroup,
	namespaceIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(workgroup.ARN, workgroup.Name)
	if sourceID == "" {
		return nil
	}
	workgroupARN := strings.TrimSpace(workgroup.ARN)
	var relationships []awscloud.RelationshipObservation
	namespaceName := strings.TrimSpace(workgroup.NamespaceName)
	if targetID := namespaceIDs[namespaceName]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftServerlessWorkgroupInNamespace,
			sourceID,
			workgroupARN,
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRedshiftServerlessNamespace,
			map[string]any{"namespace_name": namespaceName},
		))
	}
	for _, subnetID := range cloneStrings(workgroup.SubnetIDs) {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftServerlessWorkgroupUsesSubnet,
			sourceID,
			workgroupARN,
			subnetID,
			"",
			awscloud.ResourceTypeEC2Subnet,
			map[string]any{"subnet_id": subnetID},
		))
	}
	for _, groupID := range cloneStrings(workgroup.SecurityGroupIDs) {
		targetARN := securityGroupARN(boundary, groupID)
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup,
			sourceID,
			workgroupARN,
			targetARN,
			targetARN,
			awscloud.ResourceTypeEC2SecurityGroup,
			map[string]any{"security_group_id": groupID},
		))
	}
	return relationships
}

func optionalTargetRelationship(
	boundary awscloud.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetType string,
	attributes map[string]any,
) []awscloud.RelationshipObservation {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil
	}
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	return []awscloud.RelationshipObservation{relationship(
		boundary,
		relationshipType,
		sourceID,
		sourceARN,
		targetID,
		targetARN,
		targetType,
		attributes,
	)}
}

func namedRelationship(
	boundary awscloud.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetType string,
	attributes map[string]any,
) awscloud.RelationshipObservation {
	return relationship(boundary, relationshipType, sourceID, sourceARN, targetID, "", targetType, attributes)
}

func relationship(
	boundary awscloud.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetARN string,
	targetType string,
	attributes map[string]any,
) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + targetID,
	}
}

func securityGroupARN(boundary awscloud.Boundary, groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || strings.HasPrefix(groupID, "arn:") {
		return groupID
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":ec2:" + boundary.Region + ":" + boundary.AccountID + ":security-group/" + groupID
}

func targetARNFor(targetID string) string {
	targetID = strings.TrimSpace(targetID)
	if strings.HasPrefix(targetID, "arn:") {
		return targetID
	}
	return ""
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
