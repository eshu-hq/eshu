package eks

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func nodegroupEnvelopes(
	boundary awscloud.Boundary,
	cluster Cluster,
	nodegroup Nodegroup,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(nodegroupObservation(boundary, nodegroup))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if observation, ok := clusterNodegroupRelationship(boundary, cluster, nodegroup); ok {
		envelopes, err = appendRelationship(envelopes, observation)
		if err != nil {
			return nil, err
		}
	}
	if observation, ok := nodegroupRoleRelationship(boundary, nodegroup); ok {
		envelopes, err = appendRelationship(envelopes, observation)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range nodegroupSubnetRelationships(boundary, nodegroup) {
		envelopes, err = appendRelationship(envelopes, observation)
		if err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendRelationship(
	envelopes []facts.Envelope,
	observation awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	envelope, err := awscloud.NewRelationshipEnvelope(observation)
	if err != nil {
		return nil, err
	}
	return append(envelopes, envelope), nil
}

func nodegroupObservation(boundary awscloud.Boundary, nodegroup Nodegroup) awscloud.ResourceObservation {
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(nodegroup.ARN),
		ResourceID:   nodegroupID,
		ResourceType: awscloud.ResourceTypeEKSNodegroup,
		Name:         strings.TrimSpace(nodegroup.Name),
		State:        strings.TrimSpace(nodegroup.Status),
		Tags:         nodegroup.Tags,
		Attributes: map[string]any{
			"ami_type":        strings.TrimSpace(nodegroup.AMIType),
			"capacity_type":   strings.TrimSpace(nodegroup.CapacityType),
			"cluster_name":    strings.TrimSpace(nodegroup.ClusterName),
			"instance_types":  cloneStrings(nodegroup.InstanceTypes),
			"node_role_arn":   strings.TrimSpace(nodegroup.NodeRoleARN),
			"release_version": strings.TrimSpace(nodegroup.ReleaseVersion),
			"scaling_config":  scalingConfigMap(nodegroup.ScalingConfig),
			"subnets":         cloneStrings(nodegroup.Subnets),
			"version":         strings.TrimSpace(nodegroup.Version),
		},
		CorrelationAnchors: []string{nodegroupID, strings.TrimSpace(nodegroup.Name), strings.TrimSpace(nodegroup.NodeRoleARN)},
		SourceRecordID:     nodegroupID,
	}
}

func addonEnvelopes(
	boundary awscloud.Boundary,
	cluster Cluster,
	addon Addon,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(addonObservation(boundary, addon))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if observation, ok := clusterAddonRelationship(boundary, cluster, addon); ok {
		envelopes, err = appendRelationship(envelopes, observation)
		if err != nil {
			return nil, err
		}
	}
	if observation, ok := addonRoleRelationship(boundary, addon); ok {
		envelopes, err = appendRelationship(envelopes, observation)
		if err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func addonObservation(boundary awscloud.Boundary, addon Addon) awscloud.ResourceObservation {
	addonID := firstNonEmpty(addon.ARN, addon.ClusterName+"/"+addon.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(addon.ARN),
		ResourceID:   addonID,
		ResourceType: awscloud.ResourceTypeEKSAddon,
		Name:         strings.TrimSpace(addon.Name),
		State:        strings.TrimSpace(addon.Status),
		Tags:         addon.Tags,
		Attributes: map[string]any{
			"addon_version":            strings.TrimSpace(addon.Version),
			"cluster_name":             strings.TrimSpace(addon.ClusterName),
			"created_at":               timeOrNil(addon.CreatedAt),
			"modified_at":              timeOrNil(addon.ModifiedAt),
			"service_account_role_arn": strings.TrimSpace(addon.ServiceAccountRoleARN),
		},
		CorrelationAnchors: []string{addonID, strings.TrimSpace(addon.Name), strings.TrimSpace(addon.ServiceAccountRoleARN)},
		SourceRecordID:     addonID,
	}
}

func clusterAddonRelationship(
	boundary awscloud.Boundary,
	cluster Cluster,
	addon Addon,
) (awscloud.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	addonID := firstNonEmpty(addon.ARN, addon.ClusterName+"/"+addon.Name)
	if clusterID == "" || addonID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEKSClusterHasAddon,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: addonID,
		TargetARN:        strings.TrimSpace(addon.ARN),
		TargetType:       awscloud.ResourceTypeEKSAddon,
		SourceRecordID:   clusterID + "#addon#" + addonID,
	}, true
}

func addonRoleRelationship(
	boundary awscloud.Boundary,
	addon Addon,
) (awscloud.RelationshipObservation, bool) {
	addonID := firstNonEmpty(addon.ARN, addon.ClusterName+"/"+addon.Name)
	roleARN := strings.TrimSpace(addon.ServiceAccountRoleARN)
	if addonID == "" || roleARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEKSAddonUsesIAMRole,
		SourceResourceID: addonID,
		SourceARN:        strings.TrimSpace(addon.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   addonID + "#role#" + roleARN,
	}, true
}
