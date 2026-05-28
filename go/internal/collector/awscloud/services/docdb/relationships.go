package docdb

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// subnetGroupIdentity carries the resolved subnet group identity and its
// reported VPC so a cluster can emit both subnet-group and VPC placement edges
// without re-reading the subnet group.
type subnetGroupIdentity struct {
	id    string
	vpcID string
}

func clusterRelationships(
	boundary awscloud.Boundary,
	cluster DBCluster,
	subnets map[string]subnetGroupIdentity,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.ResourceID, cluster.Identifier)
	sourceARN := strings.TrimSpace(cluster.ARN)
	var relationships []awscloud.RelationshipObservation

	subnetGroupName := strings.TrimSpace(cluster.DBSubnetGroupName)
	if subnet, ok := subnets[subnetGroupName]; ok && subnet.id != "" {
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipDocDBClusterInSubnetGroup,
			sourceID,
			sourceARN,
			subnet.id,
			arnIfARN(subnet.id),
			awscloud.ResourceTypeDocDBSubnetGroup,
			map[string]any{"db_subnet_group_name": subnetGroupName},
		))
		if vpcID := strings.TrimSpace(subnet.vpcID); vpcID != "" {
			relationships = append(relationships, relationship(
				boundary,
				awscloud.RelationshipDocDBClusterInVPC,
				sourceID,
				sourceARN,
				vpcID,
				"",
				"aws_vpc",
				map[string]any{"db_subnet_group_name": subnetGroupName},
			))
		}
	}

	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		awscloud.RelationshipDocDBClusterUsesKMSKey,
		sourceID,
		sourceARN,
		strings.TrimSpace(cluster.KMSKeyID),
		"aws_kms_key",
	)...)
	return relationships
}

func instanceRelationships(
	boundary awscloud.Boundary,
	instance ClusterInstance,
	clusterIDs map[string]string,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(instance.ARN, instance.ResourceID, instance.Identifier)
	sourceARN := strings.TrimSpace(instance.ARN)
	clusterIdentifier := strings.TrimSpace(instance.ClusterIdentifier)
	targetID := clusterIDs[clusterIdentifier]
	if targetID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{relationship(
		boundary,
		awscloud.RelationshipDocDBInstanceMemberOfCluster,
		sourceID,
		sourceARN,
		targetID,
		arnIfARN(targetID),
		awscloud.ResourceTypeDocDBCluster,
		map[string]any{
			"cluster_identifier": clusterIdentifier,
			"is_writer":          true,
		},
	)}
}

func globalClusterRelationships(
	boundary awscloud.Boundary,
	globalCluster GlobalCluster,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(globalCluster.ARN, globalCluster.ResourceID, globalCluster.Identifier)
	sourceARN := strings.TrimSpace(globalCluster.ARN)
	var relationships []awscloud.RelationshipObservation
	for _, member := range globalCluster.Members {
		targetARN := strings.TrimSpace(member.DBClusterARN)
		if targetARN == "" {
			continue
		}
		relationships = append(relationships, relationship(
			boundary,
			awscloud.RelationshipDocDBGlobalClusterHasCluster,
			sourceID,
			sourceARN,
			targetARN,
			targetARN,
			awscloud.ResourceTypeDocDBCluster,
			map[string]any{"is_writer": member.IsWriter},
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
	return []awscloud.RelationshipObservation{relationship(
		boundary,
		relationshipType,
		sourceID,
		sourceARN,
		targetID,
		arnIfARN(targetID),
		targetType,
		nil,
	)}
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

func arnIfARN(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "arn:") {
		return value
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

func subnetGroupIdentityMap(subnetGroups []SubnetGroup) map[string]subnetGroupIdentity {
	identities := make(map[string]subnetGroupIdentity, len(subnetGroups))
	for _, subnetGroup := range subnetGroups {
		name := strings.TrimSpace(subnetGroup.Name)
		id := firstNonEmpty(subnetGroup.ARN, name)
		if name == "" || id == "" {
			continue
		}
		identities[name] = subnetGroupIdentity{
			id:    id,
			vpcID: strings.TrimSpace(subnetGroup.VPCID),
		}
	}
	return identities
}
