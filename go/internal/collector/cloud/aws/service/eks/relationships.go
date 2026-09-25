// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eks

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func clusterRelationships(boundary aws.Boundary, cluster Cluster) []aws.RelationshipObservation {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	if clusterID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if roleARN := strings.TrimSpace(cluster.RoleARN); roleARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEKSClusterUsesIAMRole,
			SourceResourceID: clusterID,
			SourceARN:        strings.TrimSpace(cluster.ARN),
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   clusterID + "#role#" + roleARN,
		})
	}
	for _, subnetID := range cluster.VPCConfig.SubnetIDs {
		if subnetID = strings.TrimSpace(subnetID); subnetID != "" {
			observations = append(observations, aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipEKSClusterUsesSubnet,
				SourceResourceID: clusterID,
				SourceARN:        strings.TrimSpace(cluster.ARN),
				TargetResourceID: subnetID,
				TargetType:       aws.ResourceTypeEC2Subnet,
				Attributes:       map[string]any{"vpc_id": strings.TrimSpace(cluster.VPCConfig.VPCID)},
				SourceRecordID:   clusterID + "#subnet#" + subnetID,
			})
		}
	}
	groupIDs := append(cloneStrings(cluster.VPCConfig.SecurityGroupIDs), strings.TrimSpace(cluster.VPCConfig.ClusterSecurityGroupID))
	seenGroupIDs := make(map[string]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		if _, ok := seenGroupIDs[groupID]; ok {
			continue
		}
		seenGroupIDs[groupID] = struct{}{}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEKSClusterUsesSecurityGroup,
			SourceResourceID: clusterID,
			SourceARN:        strings.TrimSpace(cluster.ARN),
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"vpc_id": strings.TrimSpace(cluster.VPCConfig.VPCID)},
			SourceRecordID:   clusterID + "#security-group#" + groupID,
		})
	}
	return observations
}

func clusterOIDCProviderRelationship(
	boundary aws.Boundary,
	cluster Cluster,
	provider OIDCProvider,
) (aws.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	providerID := firstNonEmpty(provider.ARN, provider.IssuerURL)
	if clusterID == "" || providerID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEKSClusterHasOIDCProvider,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: providerID,
		TargetARN:        strings.TrimSpace(provider.ARN),
		TargetType:       aws.ResourceTypeEKSOIDCProvider,
		Attributes:       map[string]any{"issuer_url": strings.TrimSpace(provider.IssuerURL)},
		SourceRecordID:   clusterID + "#oidc-provider#" + providerID,
	}, true
}

func clusterNodegroupRelationship(
	boundary aws.Boundary,
	cluster Cluster,
	nodegroup Nodegroup,
) (aws.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	if clusterID == "" || nodegroupID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEKSClusterHasNodegroup,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: nodegroupID,
		TargetARN:        strings.TrimSpace(nodegroup.ARN),
		TargetType:       aws.ResourceTypeEKSNodegroup,
		SourceRecordID:   clusterID + "#nodegroup#" + nodegroupID,
	}, true
}

func nodegroupRoleRelationship(
	boundary aws.Boundary,
	nodegroup Nodegroup,
) (aws.RelationshipObservation, bool) {
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	roleARN := strings.TrimSpace(nodegroup.NodeRoleARN)
	if nodegroupID == "" || roleARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEKSNodegroupUsesIAMRole,
		SourceResourceID: nodegroupID,
		SourceARN:        strings.TrimSpace(nodegroup.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   nodegroupID + "#role#" + roleARN,
	}, true
}

func nodegroupSubnetRelationships(boundary aws.Boundary, nodegroup Nodegroup) []aws.RelationshipObservation {
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	if nodegroupID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	for _, subnetID := range nodegroup.Subnets {
		if subnetID = strings.TrimSpace(subnetID); subnetID != "" {
			observations = append(observations, aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipEKSNodegroupUsesSubnet,
				SourceResourceID: nodegroupID,
				SourceARN:        strings.TrimSpace(nodegroup.ARN),
				TargetResourceID: subnetID,
				TargetType:       aws.ResourceTypeEC2Subnet,
				SourceRecordID:   nodegroupID + "#subnet#" + subnetID,
			})
		}
	}
	return observations
}
