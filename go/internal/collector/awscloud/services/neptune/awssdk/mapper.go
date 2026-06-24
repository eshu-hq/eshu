// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsneptunetypes "github.com/aws/aws-sdk-go-v2/service/neptune/types"
	awsneptunegraph "github.com/aws/aws-sdk-go-v2/service/neptunegraph"
	awsneptunegraphtypes "github.com/aws/aws-sdk-go-v2/service/neptunegraph/types"

	neptuneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/neptune"
)

func mapDBCluster(raw awsneptunetypes.DBCluster, tags map[string]string) neptuneservice.DBCluster {
	return neptuneservice.DBCluster{
		ARN:                          strings.TrimSpace(aws.ToString(raw.DBClusterArn)),
		Identifier:                   strings.TrimSpace(aws.ToString(raw.DBClusterIdentifier)),
		ResourceID:                   strings.TrimSpace(aws.ToString(raw.DbClusterResourceId)),
		Engine:                       strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:                strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:                       strings.TrimSpace(aws.ToString(raw.Status)),
		EndpointAddress:              strings.TrimSpace(aws.ToString(raw.Endpoint)),
		ReaderEndpointAddress:        strings.TrimSpace(aws.ToString(raw.ReaderEndpoint)),
		HostedZoneID:                 strings.TrimSpace(aws.ToString(raw.HostedZoneId)),
		Port:                         aws.ToInt32(raw.Port),
		MultiAZ:                      aws.ToBool(raw.MultiAZ),
		StorageEncrypted:             aws.ToBool(raw.StorageEncrypted),
		KMSKeyID:                     strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		DeletionProtection:           aws.ToBool(raw.DeletionProtection),
		BackupRetentionPeriod:        aws.ToInt32(raw.BackupRetentionPeriod),
		DBSubnetGroupName:            strings.TrimSpace(aws.ToString(raw.DBSubnetGroup)),
		VPCSecurityGroupIDs:          securityGroupIDs(raw.VpcSecurityGroups),
		Members:                      clusterMembers(raw.DBClusterMembers),
		ParameterGroup:               strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroup)),
		EnabledCloudwatchLogsExports: trimmedStrings(raw.EnabledCloudwatchLogsExports),
		AssociatedRoleARNs:           clusterRoleARNs(raw.AssociatedRoles),
		Tags:                         tags,
	}
}

func mapClusterInstance(raw awsneptunetypes.DBInstance, tags map[string]string) neptuneservice.ClusterInstance {
	return neptuneservice.ClusterInstance{
		ARN:               strings.TrimSpace(aws.ToString(raw.DBInstanceArn)),
		Identifier:        strings.TrimSpace(aws.ToString(raw.DBInstanceIdentifier)),
		ResourceID:        strings.TrimSpace(aws.ToString(raw.DbiResourceId)),
		Class:             strings.TrimSpace(aws.ToString(raw.DBInstanceClass)),
		Engine:            strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:     strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:            strings.TrimSpace(aws.ToString(raw.DBInstanceStatus)),
		EndpointAddress:   endpointAddress(raw.Endpoint),
		EndpointPort:      endpointPort(raw.Endpoint),
		HostedZoneID:      endpointHostedZone(raw.Endpoint),
		AvailabilityZone:  strings.TrimSpace(aws.ToString(raw.AvailabilityZone)),
		StorageEncrypted:  aws.ToBool(raw.StorageEncrypted),
		KMSKeyID:          strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		ClusterIdentifier: strings.TrimSpace(aws.ToString(raw.DBClusterIdentifier)),
		PromotionTier:     aws.ToInt32(raw.PromotionTier),
		Tags:              tags,
	}
}

func mapClusterParameterGroup(
	raw awsneptunetypes.DBClusterParameterGroup,
	tags map[string]string,
) neptuneservice.ClusterParameterGroup {
	return neptuneservice.ClusterParameterGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroupArn)),
		Name:        strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroupName)),
		Family:      strings.TrimSpace(aws.ToString(raw.DBParameterGroupFamily)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		Tags:        tags,
	}
}

func mapClusterSnapshot(
	raw awsneptunetypes.DBClusterSnapshot,
	tags map[string]string,
) neptuneservice.ClusterSnapshot {
	return neptuneservice.ClusterSnapshot{
		ARN:               strings.TrimSpace(aws.ToString(raw.DBClusterSnapshotArn)),
		Identifier:        strings.TrimSpace(aws.ToString(raw.DBClusterSnapshotIdentifier)),
		ClusterIdentifier: strings.TrimSpace(aws.ToString(raw.DBClusterIdentifier)),
		Engine:            strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:     strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:            strings.TrimSpace(aws.ToString(raw.Status)),
		SnapshotType:      strings.TrimSpace(aws.ToString(raw.SnapshotType)),
		StorageEncrypted:  aws.ToBool(raw.StorageEncrypted),
		KMSKeyID:          strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		VPCID:             strings.TrimSpace(aws.ToString(raw.VpcId)),
		Tags:              tags,
	}
}

func mapSubnetGroup(
	raw awsneptunetypes.DBSubnetGroup,
	tags map[string]string,
) neptuneservice.SubnetGroup {
	return neptuneservice.SubnetGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.DBSubnetGroupArn)),
		Name:        strings.TrimSpace(aws.ToString(raw.DBSubnetGroupName)),
		Description: strings.TrimSpace(aws.ToString(raw.DBSubnetGroupDescription)),
		Status:      strings.TrimSpace(aws.ToString(raw.SubnetGroupStatus)),
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        tags,
	}
}

func mapGlobalCluster(raw awsneptunetypes.GlobalCluster) neptuneservice.GlobalCluster {
	return neptuneservice.GlobalCluster{
		ARN:                strings.TrimSpace(aws.ToString(raw.GlobalClusterArn)),
		Identifier:         strings.TrimSpace(aws.ToString(raw.GlobalClusterIdentifier)),
		ResourceID:         strings.TrimSpace(aws.ToString(raw.GlobalClusterResourceId)),
		Engine:             strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:      strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:             strings.TrimSpace(aws.ToString(raw.Status)),
		StorageEncrypted:   aws.ToBool(raw.StorageEncrypted),
		DeletionProtection: aws.ToBool(raw.DeletionProtection),
		Members:            globalClusterMembers(raw.GlobalClusterMembers),
		Tags:               mapNeptuneTags(raw.TagList),
	}
}

// mapGraph maps a Neptune Analytics GetGraph detail response to scanner-owned
// graph metadata. The vector-search embedding dimension is carried only on the
// detail response; ListGraphs does not return it. Graph vertex and edge
// contents and query results are never read.
func mapGraph(raw *awsneptunegraph.GetGraphOutput) neptuneservice.Graph {
	if raw == nil {
		return neptuneservice.Graph{}
	}
	graph := neptuneservice.Graph{
		ARN:                strings.TrimSpace(aws.ToString(raw.Arn)),
		ID:                 strings.TrimSpace(aws.ToString(raw.Id)),
		Name:               strings.TrimSpace(aws.ToString(raw.Name)),
		Status:             string(raw.Status),
		KMSKeyID:           strings.TrimSpace(aws.ToString(raw.KmsKeyIdentifier)),
		ProvisionedMemory:  aws.ToInt32(raw.ProvisionedMemory),
		ReplicaCount:       aws.ToInt32(raw.ReplicaCount),
		PublicConnectivity: aws.ToBool(raw.PublicConnectivity),
		DeletionProtection: aws.ToBool(raw.DeletionProtection),
		EndpointAddress:    strings.TrimSpace(aws.ToString(raw.Endpoint)),
	}
	if raw.VectorSearchConfiguration != nil && raw.VectorSearchConfiguration.Dimension != nil {
		dimension := aws.ToInt32(raw.VectorSearchConfiguration.Dimension)
		graph.VectorSearchDimension = &dimension
	}
	return graph
}

func mapGraphSnapshot(
	raw awsneptunegraphtypes.GraphSnapshotSummary,
	tags map[string]string,
) neptuneservice.GraphSnapshot {
	return neptuneservice.GraphSnapshot{
		ARN:           strings.TrimSpace(aws.ToString(raw.Arn)),
		ID:            strings.TrimSpace(aws.ToString(raw.Id)),
		Name:          strings.TrimSpace(aws.ToString(raw.Name)),
		Status:        string(raw.Status),
		KMSKeyID:      strings.TrimSpace(aws.ToString(raw.KmsKeyIdentifier)),
		SourceGraphID: strings.TrimSpace(aws.ToString(raw.SourceGraphId)),
		Tags:          tags,
	}
}

func endpointAddress(endpoint *awsneptunetypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.Address))
}

func endpointPort(endpoint *awsneptunetypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return aws.ToInt32(endpoint.Port)
}

func endpointHostedZone(endpoint *awsneptunetypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.HostedZoneId))
}

func securityGroupIDs(groups []awsneptunetypes.VpcSecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.VpcSecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func clusterMembers(members []awsneptunetypes.DBClusterMember) []neptuneservice.ClusterMember {
	var output []neptuneservice.ClusterMember
	for _, member := range members {
		identifier := strings.TrimSpace(aws.ToString(member.DBInstanceIdentifier))
		if identifier == "" {
			continue
		}
		output = append(output, neptuneservice.ClusterMember{
			DBInstanceIdentifier: identifier,
			IsWriter:             aws.ToBool(member.IsClusterWriter),
		})
	}
	return output
}

func globalClusterMembers(members []awsneptunetypes.GlobalClusterMember) []neptuneservice.GlobalClusterMember {
	var output []neptuneservice.GlobalClusterMember
	for _, member := range members {
		arn := strings.TrimSpace(aws.ToString(member.DBClusterArn))
		if arn == "" {
			continue
		}
		output = append(output, neptuneservice.GlobalClusterMember{
			DBClusterARN: arn,
			IsWriter:     aws.ToBool(member.IsWriter),
		})
	}
	return output
}

func clusterRoleARNs(roles []awsneptunetypes.DBClusterRole) []string {
	var output []string
	for _, role := range roles {
		if arn := strings.TrimSpace(aws.ToString(role.RoleArn)); arn != "" {
			output = append(output, arn)
		}
	}
	return output
}

func subnetIDs(subnets []awsneptunetypes.Subnet) []string {
	var output []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.SubnetIdentifier)); id != "" {
			output = append(output, id)
		}
	}
	return output
}

func trimmedStrings(values []string) []string {
	var output []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

func mapNeptuneTags(tags []awsneptunetypes.Tag) map[string]string {
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

func cloneTagMap(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for key, value := range tags {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
