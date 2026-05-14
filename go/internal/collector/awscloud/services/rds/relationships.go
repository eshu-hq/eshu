package rds

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func instanceRelationships(
	boundary awscloud.Boundary,
	instance DBInstance,
	clusterIDs map[string]string,
	clusterMemberships map[string]clusterMembership,
	subnetGroupIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(instance.ARN, instance.ResourceID, instance.Identifier)
	var relationships []awscloud.RelationshipObservation
	clusterIdentifier := strings.TrimSpace(instance.ClusterIdentifier)
	targetID := clusterIDs[clusterIdentifier]
	membership := clusterMemberships[strings.TrimSpace(instance.Identifier)]
	if targetID == "" {
		targetID = membership.clusterID
		clusterIdentifier = membership.clusterIdentifier
	}
	if targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRDSDBInstanceMemberOfCluster,
			sourceID,
			strings.TrimSpace(instance.ARN),
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRDSDBCluster,
			map[string]any{
				"cluster_identifier": clusterIdentifier,
				"is_writer":          membership.isWriter,
			},
		))
	}
	if targetID := subnetGroupIDs[strings.TrimSpace(instance.DBSubnetGroupName)]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRDSDBInstanceInSubnetGroup,
			sourceID,
			strings.TrimSpace(instance.ARN),
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRDSDBSubnetGroup,
			map[string]any{"db_subnet_group_name": strings.TrimSpace(instance.DBSubnetGroupName)},
		))
	}
	for _, groupID := range cloneStrings(instance.VPCSecurityGroupIDs) {
		targetARN := securityGroupARN(boundary, groupID)
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRDSDBInstanceUsesSecurityGroup,
			sourceID,
			strings.TrimSpace(instance.ARN),
			targetARN,
			targetARN,
			awscloud.ResourceTypeEC2SecurityGroup,
			map[string]any{"security_group_id": groupID},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRDSDBInstanceUsesKMSKey,
		sourceID,
		strings.TrimSpace(instance.ARN),
		strings.TrimSpace(instance.KMSKeyID),
		"aws_kms_key",
	)...)
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRDSDBInstanceUsesMonitoringRole,
		sourceID,
		strings.TrimSpace(instance.ARN),
		strings.TrimSpace(instance.MonitoringRoleARN),
		awscloud.ResourceTypeIAMRole,
	)...)
	for _, group := range instance.ParameterGroups {
		if name := strings.TrimSpace(group.Name); name != "" {
			relationships = append(relationships, namedRelationship(
				boundary,
				awscloud.RelationshipRDSDBInstanceUsesParameterGroup,
				sourceID,
				strings.TrimSpace(instance.ARN),
				name,
				"aws_rds_db_parameter_group",
				map[string]any{"apply_status": strings.TrimSpace(group.State)},
			))
		}
	}
	for _, group := range instance.OptionGroups {
		if name := strings.TrimSpace(group.Name); name != "" {
			relationships = append(relationships, namedRelationship(
				boundary,
				awscloud.RelationshipRDSDBInstanceUsesOptionGroup,
				sourceID,
				strings.TrimSpace(instance.ARN),
				name,
				"aws_rds_option_group",
				map[string]any{"status": strings.TrimSpace(group.State)},
			))
		}
	}
	return relationships
}

func clusterRelationships(
	boundary awscloud.Boundary,
	cluster DBCluster,
	subnetGroupIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.ResourceID, cluster.Identifier)
	var relationships []awscloud.RelationshipObservation
	if targetID := subnetGroupIDs[strings.TrimSpace(cluster.DBSubnetGroupName)]; targetID != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRDSDBClusterInSubnetGroup,
			sourceID,
			strings.TrimSpace(cluster.ARN),
			targetID,
			targetARNFor(targetID),
			awscloud.ResourceTypeRDSDBSubnetGroup,
			map[string]any{"db_subnet_group_name": strings.TrimSpace(cluster.DBSubnetGroupName)},
		))
	}
	for _, groupID := range cloneStrings(cluster.VPCSecurityGroupIDs) {
		targetARN := securityGroupARN(boundary, groupID)
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRDSDBClusterUsesSecurityGroup,
			sourceID,
			strings.TrimSpace(cluster.ARN),
			targetARN,
			targetARN,
			awscloud.ResourceTypeEC2SecurityGroup,
			map[string]any{"security_group_id": groupID},
		))
	}
	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipRDSDBClusterUsesKMSKey,
		sourceID,
		strings.TrimSpace(cluster.ARN),
		strings.TrimSpace(cluster.KMSKeyID),
		"aws_kms_key",
	)...)
	for _, roleARN := range cloneStrings(cluster.AssociatedRoleARNs) {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipRDSDBClusterUsesIAMRole,
			sourceID,
			strings.TrimSpace(cluster.ARN),
			roleARN,
			roleARN,
			awscloud.ResourceTypeIAMRole,
			nil,
		))
	}
	if name := strings.TrimSpace(cluster.ParameterGroup); name != "" {
		relationships = append(relationships, namedRelationship(
			boundary,
			awscloud.RelationshipRDSDBClusterUsesParameterGroup,
			sourceID,
			strings.TrimSpace(cluster.ARN),
			name,
			"aws_rds_db_cluster_parameter_group",
			nil,
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
) []awscloud.RelationshipObservation {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil
	}
	var targetARN string
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
		nil,
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
	return "arn:aws:ec2:" + boundary.Region + ":" + boundary.AccountID + ":security-group/" + groupID
}

func targetARNFor(targetID string) string {
	targetID = strings.TrimSpace(targetID)
	if strings.HasPrefix(targetID, "arn:") {
		return targetID
	}
	return ""
}

func clusterIdentityMap(clusters []DBCluster) map[string]string {
	identities := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		identifier := strings.TrimSpace(cluster.Identifier)
		id := firstNonEmpty(cluster.ARN, cluster.ResourceID, identifier)
		if identifier != "" && id != "" {
			identities[identifier] = id
		}
	}
	return identities
}

type clusterMembership struct {
	clusterID         string
	clusterIdentifier string
	isWriter          bool
}

func clusterMembershipMap(clusters []DBCluster) map[string]clusterMembership {
	memberships := map[string]clusterMembership{}
	for _, cluster := range clusters {
		clusterIdentifier := strings.TrimSpace(cluster.Identifier)
		clusterID := firstNonEmpty(cluster.ARN, cluster.ResourceID, clusterIdentifier)
		if clusterID == "" {
			continue
		}
		for _, member := range cluster.Members {
			instanceIdentifier := strings.TrimSpace(member.DBInstanceIdentifier)
			if instanceIdentifier == "" {
				continue
			}
			memberships[instanceIdentifier] = clusterMembership{
				clusterID:         clusterID,
				clusterIdentifier: clusterIdentifier,
				isWriter:          member.IsWriter,
			}
		}
	}
	return memberships
}

func subnetGroupIdentityMap(subnetGroups []DBSubnetGroup) map[string]string {
	identities := make(map[string]string, len(subnetGroups))
	for _, subnetGroup := range subnetGroups {
		name := strings.TrimSpace(subnetGroup.Name)
		id := firstNonEmpty(subnetGroup.ARN, name)
		if name != "" && id != "" {
			identities[name] = id
		}
	}
	return identities
}
