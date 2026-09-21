// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eks

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func nodegroupEnvelopes(
	boundary aws.Boundary,
	cluster Cluster,
	nodegroup Nodegroup,
) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(nodegroupObservation(boundary, nodegroup))
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
	observation aws.RelationshipObservation,
) ([]facts.Envelope, error) {
	envelope, err := aws.NewRelationshipEnvelope(observation)
	if err != nil {
		return nil, err
	}
	return append(envelopes, envelope), nil
}

func nodegroupObservation(boundary aws.Boundary, nodegroup Nodegroup) aws.ResourceObservation {
	nodegroupID := firstNonEmpty(nodegroup.ARN, nodegroup.ClusterName+"/"+nodegroup.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(nodegroup.ARN),
		ResourceID:   nodegroupID,
		ResourceType: aws.ResourceTypeEKSNodegroup,
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
	boundary aws.Boundary,
	cluster Cluster,
	addon Addon,
) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(addonObservation(boundary, addon))
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

func addonObservation(boundary aws.Boundary, addon Addon) aws.ResourceObservation {
	addonID := firstNonEmpty(addon.ARN, addon.ClusterName+"/"+addon.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(addon.ARN),
		ResourceID:   addonID,
		ResourceType: aws.ResourceTypeEKSAddon,
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
	boundary aws.Boundary,
	cluster Cluster,
	addon Addon,
) (aws.RelationshipObservation, bool) {
	clusterID := firstNonEmpty(cluster.ARN, cluster.Name)
	addonID := firstNonEmpty(addon.ARN, addon.ClusterName+"/"+addon.Name)
	if clusterID == "" || addonID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEKSClusterHasAddon,
		SourceResourceID: clusterID,
		SourceARN:        strings.TrimSpace(cluster.ARN),
		TargetResourceID: addonID,
		TargetARN:        strings.TrimSpace(addon.ARN),
		TargetType:       aws.ResourceTypeEKSAddon,
		SourceRecordID:   clusterID + "#addon#" + addonID,
	}, true
}

func addonRoleRelationship(
	boundary aws.Boundary,
	addon Addon,
) (aws.RelationshipObservation, bool) {
	addonID := firstNonEmpty(addon.ARN, addon.ClusterName+"/"+addon.Name)
	roleARN := strings.TrimSpace(addon.ServiceAccountRoleARN)
	if addonID == "" || roleARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEKSAddonUsesIAMRole,
		SourceResourceID: addonID,
		SourceARN:        strings.TrimSpace(addon.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   addonID + "#role#" + roleARN,
	}, true
}
