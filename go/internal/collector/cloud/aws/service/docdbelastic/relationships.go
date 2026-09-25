// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdbelastic

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// clusterRelationships builds every resolvable outgoing edge for one cluster:
// one edge per VPC subnet, one per security group, the KMS-key edge, and the
// admin-secret edge. Each builder returns nil when its target identity is
// missing so the edge is skipped rather than dangled.
func clusterRelationships(boundary aws.Boundary, cluster Cluster) []aws.RelationshipObservation {
	var relationships []aws.RelationshipObservation
	for _, subnetID := range cluster.SubnetIDs {
		if rel := clusterSubnetRelationship(boundary, cluster, subnetID); rel != nil {
			relationships = append(relationships, *rel)
		}
	}
	for _, groupID := range cluster.SecurityGroupIDs {
		if rel := clusterSecurityGroupRelationship(boundary, cluster, groupID); rel != nil {
			relationships = append(relationships, *rel)
		}
	}
	if rel := clusterKMSRelationship(boundary, cluster); rel != nil {
		relationships = append(relationships, *rel)
	}
	if rel := clusterAdminSecretRelationship(boundary, cluster); rel != nil {
		relationships = append(relationships, *rel)
	}
	return relationships
}

// clusterSubnetRelationship records a DocumentDB Elastic cluster's placement in
// one VPC subnet. DocumentDB Elastic reports a bare subnet id (subnet-...),
// which matches how the EC2 scanner publishes its subnet resource_id, so the
// edge is keyed by that bare id with no synthesized ARN. It returns nil when
// either endpoint identity is missing.
func clusterSubnetRelationship(
	boundary aws.Boundary,
	cluster Cluster,
	subnetID string,
) *aws.RelationshipObservation {
	subnetID = strings.TrimSpace(subnetID)
	sourceID := clusterResourceID(cluster)
	if subnetID == "" || sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDocDBElasticClusterInSubnet,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: subnetID,
		TargetType:       aws.ResourceTypeEC2Subnet,
		SourceRecordID:   sourceID + "->" + aws.RelationshipDocDBElasticClusterInSubnet + ":" + subnetID,
	}
}

// clusterSecurityGroupRelationship records a DocumentDB Elastic cluster's
// attachment to one VPC security group. DocumentDB Elastic reports a bare
// security-group id (sg-...), which matches how the EC2 scanner publishes its
// security-group resource_id, so the edge is keyed by that bare id with no
// synthesized ARN. It returns nil when either endpoint identity is missing.
func clusterSecurityGroupRelationship(
	boundary aws.Boundary,
	cluster Cluster,
	groupID string,
) *aws.RelationshipObservation {
	groupID = strings.TrimSpace(groupID)
	sourceID := clusterResourceID(cluster)
	if groupID == "" || sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDocDBElasticClusterUsesSecurityGroup,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: groupID,
		TargetType:       aws.ResourceTypeEC2SecurityGroup,
		SourceRecordID:   sourceID + "->" + aws.RelationshipDocDBElasticClusterUsesSecurityGroup + ":" + groupID,
	}
}

// clusterKMSRelationship records a DocumentDB Elastic cluster's reported KMS
// encryption key dependency. AWS reports a key id or key ARN, which matches how
// the KMS scanner publishes its key resource_id (bare id or ARN). target_arn is
// set only for an ARN-shaped identifier. It returns nil when no key is reported.
func clusterKMSRelationship(boundary aws.Boundary, cluster Cluster) *aws.RelationshipObservation {
	targetID := strings.TrimSpace(cluster.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := clusterResourceID(cluster)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDocDBElasticClusterUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + aws.RelationshipDocDBElasticClusterUsesKMSKey + ":" + targetID,
	}
}

// clusterAdminSecretRelationship records a DocumentDB Elastic cluster's
// reference to the Secrets Manager secret holding its admin credentials. It is
// emitted only when the cluster uses SECRET_ARN auth and reports a secret ARN,
// which matches how the Secrets Manager scanner publishes its secret
// resource_id (the ARN). The secret value is never read. It returns nil when no
// secret ARN is reported.
func clusterAdminSecretRelationship(boundary aws.Boundary, cluster Cluster) *aws.RelationshipObservation {
	secretARN := strings.TrimSpace(cluster.AdminSecretARN)
	if secretARN == "" || !isARN(secretARN) {
		return nil
	}
	sourceID := clusterResourceID(cluster)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDocDBElasticClusterUsesAdminSecret,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: secretARN,
		TargetARN:        secretARN,
		TargetType:       aws.ResourceTypeSecretsManagerSecret,
		SourceRecordID:   sourceID + "->" + aws.RelationshipDocDBElasticClusterUsesAdminSecret + ":" + secretARN,
	}
}
