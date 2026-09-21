// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redshift

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func clusterRelationships(
	boundary aws.Boundary,
	cluster Cluster,
	parameterGroupIDs map[string]string,
	subnetGroupIDs map[string]string,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.Identifier)
	if sourceID == "" {
		return nil
	}
	clusterARN := strings.TrimSpace(cluster.ARN)
	var relationships []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(cluster.VPCID); vpcID != "" {
		relationships = append(relationships, namedRelationship(
			boundary,
			aws.RelationshipRedshiftClusterInVPC,
			sourceID,
			clusterARN,
			vpcID,
			aws.ResourceTypeEC2VPC,
			nil,
		))
	}
	if targetID := subnetGroupIDs[strings.TrimSpace(cluster.ClusterSubnetGroupName)]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftClusterInSubnetGroup,
			sourceID,
			clusterARN,
			targetID,
			targetARNFor(targetID),
			aws.ResourceTypeRedshiftClusterSubnetGroup,
			map[string]any{"cluster_subnet_group_name": strings.TrimSpace(cluster.ClusterSubnetGroupName)},
		))
	}
	if targetID := parameterGroupIDs[strings.TrimSpace(cluster.ClusterParameterGroup)]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftClusterUsesParameterGroup,
			sourceID,
			clusterARN,
			targetID,
			targetARNFor(targetID),
			aws.ResourceTypeRedshiftClusterParameterGroup,
			map[string]any{"cluster_parameter_group_name": strings.TrimSpace(cluster.ClusterParameterGroup)},
		))
	}
	for _, groupID := range cloneStrings(cluster.VPCSecurityGroupIDs) {
		targetARN := securityGroupARN(boundary, groupID)
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftClusterUsesSecurityGroup,
			sourceID,
			clusterARN,
			targetARN,
			targetARN,
			aws.ResourceTypeEC2SecurityGroup,
			map[string]any{"security_group_id": groupID},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		aws.RelationshipRedshiftClusterUsesKMSKey,
		sourceID,
		clusterARN,
		strings.TrimSpace(cluster.KMSKeyID),
		"aws_kms_key",
		nil,
	)...)
	for _, roleARN := range cloneStrings(cluster.IAMRoleARNs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftClusterUsesIAMRole,
			sourceID,
			clusterARN,
			roleARN,
			roleARN,
			aws.ResourceTypeIAMRole,
			nil,
		))
	}
	return relationships
}

func subnetGroupVPCRelationship(
	boundary aws.Boundary,
	group ClusterSubnetGroup,
) (aws.RelationshipObservation, bool) {
	sourceID := firstNonEmpty(group.ARN, group.Name)
	vpcID := strings.TrimSpace(group.VPCID)
	if sourceID == "" || vpcID == "" {
		return aws.RelationshipObservation{}, false
	}
	return namedRelationship(
		boundary,
		aws.RelationshipRedshiftClusterSubnetGroupInVPC,
		sourceID,
		strings.TrimSpace(group.ARN),
		vpcID,
		aws.ResourceTypeEC2VPC,
		nil,
	), true
}

func snapshotRelationships(
	boundary aws.Boundary,
	snapshot ClusterSnapshot,
	clusterIDs map[string]string,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(snapshot.ARN, snapshot.Identifier)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	clusterIdentifier := strings.TrimSpace(snapshot.ClusterIdentifier)
	if targetID := clusterIDs[clusterIdentifier]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftClusterSnapshotOfCluster,
			sourceID,
			strings.TrimSpace(snapshot.ARN),
			targetID,
			targetARNFor(targetID),
			aws.ResourceTypeRedshiftCluster,
			map[string]any{"cluster_identifier": clusterIdentifier},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		aws.RelationshipRedshiftClusterSnapshotUsesKMSKey,
		sourceID,
		strings.TrimSpace(snapshot.ARN),
		strings.TrimSpace(snapshot.KMSKeyID),
		"aws_kms_key",
		nil,
	)...)
	return relationships
}

func scheduledActionRelationships(
	boundary aws.Boundary,
	action ScheduledAction,
	clusterIDs map[string]string,
) []aws.RelationshipObservation {
	sourceID := strings.TrimSpace(action.Name)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	clusterIdentifier := strings.TrimSpace(action.TargetClusterIdentifier)
	if targetID := clusterIDs[clusterIdentifier]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftScheduledActionTargetsCluster,
			sourceID,
			"",
			targetID,
			targetARNFor(targetID),
			aws.ResourceTypeRedshiftCluster,
			map[string]any{
				"target_action_name": strings.TrimSpace(action.TargetActionName),
				"cluster_identifier": clusterIdentifier,
			},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		aws.RelationshipRedshiftScheduledActionUsesIAMRole,
		sourceID,
		"",
		strings.TrimSpace(action.IAMRoleARN),
		aws.ResourceTypeIAMRole,
		nil,
	)...)
	return relationships
}

func serverlessNamespaceRelationships(
	boundary aws.Boundary,
	namespace ServerlessNamespace,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(namespace.ARN, namespace.Name)
	if sourceID == "" {
		return nil
	}
	namespaceARN := strings.TrimSpace(namespace.ARN)
	var relationships []aws.RelationshipObservation
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		aws.RelationshipRedshiftServerlessNamespaceUsesKMSKey,
		sourceID,
		namespaceARN,
		strings.TrimSpace(namespace.KMSKeyID),
		"aws_kms_key",
		nil,
	)...)
	for _, roleARN := range cloneStrings(namespace.IAMRoleARNs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftServerlessNamespaceUsesIAMRole,
			sourceID,
			namespaceARN,
			roleARN,
			roleARN,
			aws.ResourceTypeIAMRole,
			nil,
		))
	}
	return relationships
}

func serverlessWorkgroupRelationships(
	boundary aws.Boundary,
	workgroup ServerlessWorkgroup,
	namespaceIDs map[string]string,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(workgroup.ARN, workgroup.Name)
	if sourceID == "" {
		return nil
	}
	workgroupARN := strings.TrimSpace(workgroup.ARN)
	var relationships []aws.RelationshipObservation
	namespaceName := strings.TrimSpace(workgroup.NamespaceName)
	if targetID := namespaceIDs[namespaceName]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftServerlessWorkgroupInNamespace,
			sourceID,
			workgroupARN,
			targetID,
			targetARNFor(targetID),
			aws.ResourceTypeRedshiftServerlessNamespace,
			map[string]any{"namespace_name": namespaceName},
		))
	}
	for _, subnetID := range cloneStrings(workgroup.SubnetIDs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftServerlessWorkgroupUsesSubnet,
			sourceID,
			workgroupARN,
			subnetID,
			"",
			aws.ResourceTypeEC2Subnet,
			map[string]any{"subnet_id": subnetID},
		))
	}
	for _, groupID := range cloneStrings(workgroup.SecurityGroupIDs) {
		targetARN := securityGroupARN(boundary, groupID)
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup,
			sourceID,
			workgroupARN,
			targetARN,
			targetARN,
			aws.ResourceTypeEC2SecurityGroup,
			map[string]any{"security_group_id": groupID},
		))
	}
	return relationships
}

func optionalTargetRelationship(
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetType string,
	attributes map[string]any,
) []aws.RelationshipObservation {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil
	}
	targetARN := ""
	if strings.HasPrefix(targetID, "arn:") {
		targetARN = targetID
	}
	return []aws.RelationshipObservation{relationship(
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
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetType string,
	attributes map[string]any,
) aws.RelationshipObservation {
	return relationship(boundary, relationshipType, sourceID, sourceARN, targetID, "", targetType, attributes)
}

func relationship(
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetARN string,
	targetType string,
	attributes map[string]any,
) aws.RelationshipObservation {
	return aws.RelationshipObservation{
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

func securityGroupARN(boundary aws.Boundary, groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || strings.HasPrefix(groupID, "arn:") {
		return groupID
	}
	return "arn:" + aws.PartitionForBoundary(boundary) + ":ec2:" + boundary.Region + ":" + boundary.AccountID + ":security-group/" + groupID
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
