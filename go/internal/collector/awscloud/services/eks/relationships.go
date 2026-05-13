package eks

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func clusterRelationships(boundary awscloud.Boundary, cluster Cluster) []awscloud.RelationshipObservation {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	if clusterID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if roleARN := strings.TrimSpace(cluster.RoleARN); roleARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipEKSClusterUsesIAMRole,
			SourceResourceID: clusterID,
			SourceARN:        strings.TrimSpace(cluster.ARN),
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   clusterID + "#role#" + roleARN,
		})
	}
	for _, subnetID := range cluster.VPCConfig.SubnetIDs {
		if subnetID = strings.TrimSpace(subnetID); subnetID != "" {
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipEKSClusterUsesSubnet,
				SourceResourceID: clusterID,
				SourceARN:        strings.TrimSpace(cluster.ARN),
				TargetResourceID: subnetID,
				TargetType:       awscloud.ResourceTypeEC2Subnet,
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipEKSClusterUsesSecurityGroup,
			SourceResourceID: clusterID,
			SourceARN:        strings.TrimSpace(cluster.ARN),
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"vpc_id": strings.TrimSpace(cluster.VPCConfig.VPCID)},
			SourceRecordID:   clusterID + "#security-group#" + groupID,
		})
	}
	return observations
}

func clusterOIDCProviderRelationship(
	boundary awscloud.Boundary,
	cluster Cluster,
	provider OIDCProvider,
) (awscloud.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	providerID := firstNonEmpty(provider.ARN, provider.IssuerURL)
	if clusterID == "" || providerID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEKSClusterHasOIDCProvider,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: providerID,
		TargetARN:        strings.TrimSpace(provider.ARN),
		TargetType:       awscloud.ResourceTypeEKSOIDCProvider,
		Attributes:       map[string]any{"issuer_url": strings.TrimSpace(provider.IssuerURL)},
		SourceRecordID:   clusterID + "#oidc-provider#" + providerID,
	}, true
}

func clusterNodegroupRelationship(
	boundary awscloud.Boundary,
	cluster Cluster,
	nodegroup Nodegroup,
) (awscloud.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	if clusterID == "" || nodegroupID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEKSClusterHasNodegroup,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: nodegroupID,
		TargetARN:        strings.TrimSpace(nodegroup.ARN),
		TargetType:       awscloud.ResourceTypeEKSNodegroup,
		SourceRecordID:   clusterID + "#nodegroup#" + nodegroupID,
	}, true
}

func nodegroupRoleRelationship(
	boundary awscloud.Boundary,
	nodegroup Nodegroup,
) (awscloud.RelationshipObservation, bool) {
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	roleARN := strings.TrimSpace(nodegroup.NodeRoleARN)
	if nodegroupID == "" || roleARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEKSNodegroupUsesIAMRole,
		SourceResourceID: nodegroupID,
		SourceARN:        strings.TrimSpace(nodegroup.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   nodegroupID + "#role#" + roleARN,
	}, true
}

func nodegroupSubnetRelationships(boundary awscloud.Boundary, nodegroup Nodegroup) []awscloud.RelationshipObservation {
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	if nodegroupID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, subnetID := range nodegroup.Subnets {
		if subnetID = strings.TrimSpace(subnetID); subnetID != "" {
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipEKSNodegroupUsesSubnet,
				SourceResourceID: nodegroupID,
				SourceARN:        strings.TrimSpace(nodegroup.ARN),
				TargetResourceID: subnetID,
				TargetType:       awscloud.ResourceTypeEC2Subnet,
				SourceRecordID:   nodegroupID + "#subnet#" + subnetID,
			})
		}
	}
	return observations
}
