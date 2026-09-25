// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// subnetGroupIdentity carries the resolved subnet group identity and its
// reported VPC so a cluster can emit both subnet-group and VPC placement edges
// without re-reading the subnet group.
type subnetGroupIdentity struct {
	id    string
	vpcID string
}

func clusterRelationships(
	boundary aws.Boundary,
	cluster DBCluster,
	subnets map[string]subnetGroupIdentity,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.ResourceID, cluster.Identifier)
	sourceARN := strings.TrimSpace(cluster.ARN)
	var relationships []aws.RelationshipObservation

	subnetGroupName := strings.TrimSpace(cluster.DBSubnetGroupName)
	if subnet, ok := subnets[subnetGroupName]; ok && subnet.id != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipNeptuneClusterInSubnetGroup,
			sourceID,
			sourceARN,
			subnet.id,
			arnIfARN(subnet.id),
			aws.ResourceTypeNeptuneSubnetGroup,
			map[string]any{"db_subnet_group_name": subnetGroupName},
		))
		if vpcID := strings.TrimSpace(subnet.vpcID); vpcID != "" {
			relationships = append(relationships, relationship(
				boundary,
				aws.RelationshipNeptuneClusterInVPC,
				sourceID,
				sourceARN,
				vpcID,
				"",
				aws.ResourceTypeEC2VPC,
				map[string]any{"db_subnet_group_name": subnetGroupName},
			))
		}
	}

	relationships = append(relationships, optionalTargetRelationship(
		boundary,
		aws.RelationshipNeptuneClusterUsesKMSKey,
		sourceID,
		sourceARN,
		strings.TrimSpace(cluster.KMSKeyID),
		aws.ResourceTypeKMSKey,
	)...)

	for _, roleARN := range cloneStrings(cluster.AssociatedRoleARNs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipNeptuneClusterUsesIAMRole,
			sourceID,
			sourceARN,
			roleARN,
			roleARN,
			aws.ResourceTypeIAMRole,
			nil,
		))
	}
	return relationships
}

func instanceRelationships(
	boundary aws.Boundary,
	instance ClusterInstance,
	clusterIDs map[string]string,
	memberships map[string]clusterMembership,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(instance.ARN, instance.ResourceID, instance.Identifier)
	sourceARN := strings.TrimSpace(instance.ARN)
	clusterIdentifier := strings.TrimSpace(instance.ClusterIdentifier)
	targetID := clusterIDs[clusterIdentifier]
	if targetID == "" {
		return nil
	}
	membership := memberships[strings.TrimSpace(instance.Identifier)]
	return []aws.RelationshipObservation{relationship(
		boundary,
		aws.RelationshipNeptuneInstanceMemberOfCluster,
		sourceID,
		sourceARN,
		targetID,
		arnIfARN(targetID),
		aws.ResourceTypeNeptuneCluster,
		map[string]any{
			"cluster_identifier": clusterIdentifier,
			"is_writer":          membership.isWriter,
		},
	)}
}

func globalClusterRelationships(
	boundary aws.Boundary,
	globalCluster GlobalCluster,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(globalCluster.ARN, globalCluster.ResourceID, globalCluster.Identifier)
	sourceARN := strings.TrimSpace(globalCluster.ARN)
	var relationships []aws.RelationshipObservation
	for _, member := range globalCluster.Members {
		targetARN := strings.TrimSpace(member.DBClusterARN)
		if targetARN == "" {
			continue
		}
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipNeptuneGlobalClusterHasCluster,
			sourceID,
			sourceARN,
			targetARN,
			targetARN,
			aws.ResourceTypeNeptuneCluster,
			map[string]any{"is_writer": member.IsWriter},
		))
	}
	return relationships
}

func graphRelationships(
	boundary aws.Boundary,
	graph Graph,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(graph.ARN, graph.ID, graph.Name)
	sourceARN := strings.TrimSpace(graph.ARN)
	return optionalTargetRelationship(
		boundary,
		aws.RelationshipNeptuneGraphUsesKMSKey,
		sourceID,
		sourceARN,
		strings.TrimSpace(graph.KMSKeyID),
		aws.ResourceTypeKMSKey,
	)
}

func optionalTargetRelationship(
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetType string,
) []aws.RelationshipObservation {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil
	}
	return []aws.RelationshipObservation{relationship(
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

func arnIfARN(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "arn:") {
		return value
	}
	return ""
}

// clusterMembership records an instance's resolved cluster-writer role as
// reported by the owning cluster's member list. Neptune clusters report one
// writer (primary) and N readers (replicas), so the membership edge must carry
// each instance's true role rather than assuming every member is a writer.
type clusterMembership struct {
	isWriter bool
}

// clusterMembershipMap maps an instance identifier to its writer role using the
// cluster-reported member list. Instances absent from any member list resolve
// to the zero value (is_writer=false), which is the safe default for a reader
// or not-yet-promoted instance.
func clusterMembershipMap(clusters []DBCluster) map[string]clusterMembership {
	memberships := make(map[string]clusterMembership)
	for _, cluster := range clusters {
		for _, member := range cluster.Members {
			identifier := strings.TrimSpace(member.DBInstanceIdentifier)
			if identifier == "" {
				continue
			}
			memberships[identifier] = clusterMembership{isWriter: member.IsWriter}
		}
	}
	return memberships
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
