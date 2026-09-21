// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package msk

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
	for _, subnetID := range clusterSubnetIDs(cluster) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMSKClusterUsesSubnet,
			SourceResourceID: clusterID,
			SourceARN:        strings.TrimSpace(cluster.ARN),
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   clusterID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range clusterSecurityGroupIDs(cluster) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMSKClusterUsesSecurityGroup,
			SourceResourceID: clusterID,
			SourceARN:        strings.TrimSpace(cluster.ARN),
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   clusterID + "#security-group#" + groupID,
		})
	}
	if cluster.Provisioned != nil {
		if kmsARN := strings.TrimSpace(cluster.Provisioned.EncryptionAtRestKMSKey); isARN(kmsARN) {
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipMSKClusterUsesKMSKey,
				SourceResourceID: clusterID,
				SourceARN:        strings.TrimSpace(cluster.ARN),
				TargetResourceID: kmsARN,
				TargetARN:        kmsARN,
				TargetType:       "aws_kms_key",
				SourceRecordID:   clusterID + "#kms-key#" + kmsARN,
			})
		}
		if cluster.Provisioned.CurrentConfiguration != nil {
			configARN := strings.TrimSpace(cluster.Provisioned.CurrentConfiguration.ARN)
			if isARN(configARN) {
				observations = append(observations, awscloud.RelationshipObservation{
					Boundary:         boundary,
					RelationshipType: awscloud.RelationshipMSKClusterUsesConfiguration,
					SourceResourceID: clusterID,
					SourceARN:        strings.TrimSpace(cluster.ARN),
					TargetResourceID: configARN,
					TargetARN:        configARN,
					TargetType:       awscloud.ResourceTypeMSKConfiguration,
					Attributes: map[string]any{
						"revision": cluster.Provisioned.CurrentConfiguration.Revision,
					},
					SourceRecordID: clusterID + "#configuration#" + configARN,
				})
			}
		}
	}
	return observations
}

func clusterSubnetIDs(cluster Cluster) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(values []string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	if cluster.Provisioned != nil {
		add(cluster.Provisioned.BrokerNodeGroup.ClientSubnets)
	}
	if cluster.Serverless != nil {
		for _, vpc := range cluster.Serverless.VPCConfigs {
			add(vpc.SubnetIDs)
		}
	}
	return out
}

func clusterSecurityGroupIDs(cluster Cluster) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(values []string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	if cluster.Provisioned != nil {
		add(cluster.Provisioned.BrokerNodeGroup.SecurityGroupIDs)
	}
	if cluster.Serverless != nil {
		for _, vpc := range cluster.Serverless.VPCConfigs {
			add(vpc.SecurityGroupIDs)
		}
	}
	return out
}

func replicatorRelationships(boundary awscloud.Boundary, replicator Replicator) []awscloud.RelationshipObservation {
	replicatorID := firstNonEmpty(replicator.ARN, replicator.Name)
	if replicatorID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	roleARN := strings.TrimSpace(replicator.ServiceExecutionRoleARN)
	if isARN(roleARN) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMSKReplicatorUsesIAMRole,
			SourceResourceID: replicatorID,
			SourceARN:        strings.TrimSpace(replicator.ARN),
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   replicatorID + "#role#" + roleARN,
		})
	}
	return observations
}
