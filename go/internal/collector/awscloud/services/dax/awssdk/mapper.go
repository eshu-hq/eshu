// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdaxtypes "github.com/aws/aws-sdk-go-v2/service/dax/types"

	daxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dax"
)

// mapCluster projects an AWS DAX cluster into scanner-owned metadata. It records
// the SSE status but never a KMS key (DAX does not report one), and treats the
// discovery endpoint address as plain connection metadata rather than a secret.
func mapCluster(raw awsdaxtypes.Cluster, tags map[string]string) daxservice.Cluster {
	cluster := daxservice.Cluster{
		ARN:                        strings.TrimSpace(aws.ToString(raw.ClusterArn)),
		Name:                       strings.TrimSpace(aws.ToString(raw.ClusterName)),
		Description:                strings.TrimSpace(aws.ToString(raw.Description)),
		Status:                     strings.TrimSpace(aws.ToString(raw.Status)),
		NodeType:                   strings.TrimSpace(aws.ToString(raw.NodeType)),
		ActiveNodes:                aws.ToInt32(raw.ActiveNodes),
		TotalNodes:                 aws.ToInt32(raw.TotalNodes),
		NetworkType:                string(raw.NetworkType),
		EndpointEncryptionType:     string(raw.ClusterEndpointEncryptionType),
		IAMRoleARN:                 strings.TrimSpace(aws.ToString(raw.IamRoleArn)),
		ParameterGroupName:         parameterGroupName(raw.ParameterGroup),
		SubnetGroupName:            strings.TrimSpace(aws.ToString(raw.SubnetGroup)),
		SecurityGroupIDs:           clusterSecurityGroupIDs(raw.SecurityGroups),
		SSEStatus:                  sseStatus(raw.SSEDescription),
		PreferredMaintenanceWindow: strings.TrimSpace(aws.ToString(raw.PreferredMaintenanceWindow)),
		Tags:                       tags,
	}
	if raw.ClusterDiscoveryEndpoint != nil {
		cluster.DiscoveryEndpointAddress = strings.TrimSpace(aws.ToString(raw.ClusterDiscoveryEndpoint.Address))
		cluster.DiscoveryEndpointPort = raw.ClusterDiscoveryEndpoint.Port
	}
	return cluster
}

func mapSubnetGroup(raw awsdaxtypes.SubnetGroup) daxservice.SubnetGroup {
	return daxservice.SubnetGroup{
		Name:        strings.TrimSpace(aws.ToString(raw.SubnetGroupName)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:   subnetIDs(raw.Subnets),
	}
}

func mapParameterGroup(raw awsdaxtypes.ParameterGroup) daxservice.ParameterGroup {
	return daxservice.ParameterGroup{
		Name:        strings.TrimSpace(aws.ToString(raw.ParameterGroupName)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
	}
}

func parameterGroupName(status *awsdaxtypes.ParameterGroupStatus) string {
	if status == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(status.ParameterGroupName))
}

// sseStatus returns the reported server-side-encryption state. DAX does not
// expose a KMS key ARN on the SSE description, so only the status is recorded.
func sseStatus(description *awsdaxtypes.SSEDescription) string {
	if description == nil {
		return ""
	}
	return string(description.Status)
}

func clusterSecurityGroupIDs(groups []awsdaxtypes.SecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.SecurityGroupIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func subnetIDs(subnets []awsdaxtypes.Subnet) []string {
	var ids []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.SubnetIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func mapTags(tags []awsdaxtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
