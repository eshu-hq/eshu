// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdbelastic

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// clusterRelationships builds every resolvable outgoing edge for one cluster:
// one edge per VPC subnet, one per security group, the KMS-key edge, and the
// admin-secret edge. Each builder returns nil when its target identity is
// missing so the edge is skipped rather than dangled.
func clusterRelationships(boundary awscloud.Boundary, cluster Cluster) []awscloud.RelationshipObservation {
	var relationships []awscloud.RelationshipObservation
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
	boundary awscloud.Boundary,
	cluster Cluster,
	subnetID string,
) *awscloud.RelationshipObservation {
	subnetID = strings.TrimSpace(subnetID)
	sourceID := clusterResourceID(cluster)
	if subnetID == "" || sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDocDBElasticClusterInSubnet,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: subnetID,
		TargetType:       awscloud.ResourceTypeEC2Subnet,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDocDBElasticClusterInSubnet + ":" + subnetID,
	}
}

// clusterSecurityGroupRelationship records a DocumentDB Elastic cluster's
// attachment to one VPC security group. DocumentDB Elastic reports a bare
// security-group id (sg-...), which matches how the EC2 scanner publishes its
// security-group resource_id, so the edge is keyed by that bare id with no
// synthesized ARN. It returns nil when either endpoint identity is missing.
func clusterSecurityGroupRelationship(
	boundary awscloud.Boundary,
	cluster Cluster,
	groupID string,
) *awscloud.RelationshipObservation {
	groupID = strings.TrimSpace(groupID)
	sourceID := clusterResourceID(cluster)
	if groupID == "" || sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDocDBElasticClusterUsesSecurityGroup,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: groupID,
		TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDocDBElasticClusterUsesSecurityGroup + ":" + groupID,
	}
}

// clusterKMSRelationship records a DocumentDB Elastic cluster's reported KMS
// encryption key dependency. AWS reports a key id or key ARN, which matches how
// the KMS scanner publishes its key resource_id (bare id or ARN). target_arn is
// set only for an ARN-shaped identifier. It returns nil when no key is reported.
func clusterKMSRelationship(boundary awscloud.Boundary, cluster Cluster) *awscloud.RelationshipObservation {
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
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDocDBElasticClusterUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDocDBElasticClusterUsesKMSKey + ":" + targetID,
	}
}

// clusterAdminSecretRelationship records a DocumentDB Elastic cluster's
// reference to the Secrets Manager secret holding its admin credentials. It is
// emitted only when the cluster uses SECRET_ARN auth and reports a secret ARN,
// which matches how the Secrets Manager scanner publishes its secret
// resource_id (the ARN). The secret value is never read. It returns nil when no
// secret ARN is reported.
func clusterAdminSecretRelationship(boundary awscloud.Boundary, cluster Cluster) *awscloud.RelationshipObservation {
	secretARN := strings.TrimSpace(cluster.AdminSecretARN)
	if secretARN == "" || !isARN(secretARN) {
		return nil
	}
	sourceID := clusterResourceID(cluster)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDocDBElasticClusterUsesAdminSecret,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: secretARN,
		TargetARN:        secretARN,
		TargetType:       awscloud.ResourceTypeSecretsManagerSecret,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDocDBElasticClusterUsesAdminSecret + ":" + secretARN,
	}
}
