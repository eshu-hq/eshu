// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdocdbtypes "github.com/aws/aws-sdk-go-v2/service/docdb/types"

	docdbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/docdb"
)

func mapDBCluster(raw awsdocdbtypes.DBCluster, tags map[string]string) docdbservice.DBCluster {
	return docdbservice.DBCluster{
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

func mapClusterInstance(raw awsdocdbtypes.DBInstance, tags map[string]string) docdbservice.ClusterInstance {
	return docdbservice.ClusterInstance{
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
	raw awsdocdbtypes.DBClusterParameterGroup,
	parameterCount int,
	tags map[string]string,
) docdbservice.ClusterParameterGroup {
	return docdbservice.ClusterParameterGroup{
		ARN:            strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroupArn)),
		Name:           strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroupName)),
		Family:         strings.TrimSpace(aws.ToString(raw.DBParameterGroupFamily)),
		Description:    strings.TrimSpace(aws.ToString(raw.Description)),
		ParameterCount: parameterCount,
		Tags:           tags,
	}
}

func mapClusterSnapshot(
	raw awsdocdbtypes.DBClusterSnapshot,
	tags map[string]string,
) docdbservice.ClusterSnapshot {
	return docdbservice.ClusterSnapshot{
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
	raw awsdocdbtypes.DBSubnetGroup,
	tags map[string]string,
) docdbservice.SubnetGroup {
	return docdbservice.SubnetGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.DBSubnetGroupArn)),
		Name:        strings.TrimSpace(aws.ToString(raw.DBSubnetGroupName)),
		Description: strings.TrimSpace(aws.ToString(raw.DBSubnetGroupDescription)),
		Status:      strings.TrimSpace(aws.ToString(raw.SubnetGroupStatus)),
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        tags,
	}
}

func mapGlobalCluster(raw awsdocdbtypes.GlobalCluster) docdbservice.GlobalCluster {
	return docdbservice.GlobalCluster{
		ARN:                strings.TrimSpace(aws.ToString(raw.GlobalClusterArn)),
		Identifier:         strings.TrimSpace(aws.ToString(raw.GlobalClusterIdentifier)),
		ResourceID:         strings.TrimSpace(aws.ToString(raw.GlobalClusterResourceId)),
		Engine:             strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:      strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:             strings.TrimSpace(aws.ToString(raw.Status)),
		StorageEncrypted:   aws.ToBool(raw.StorageEncrypted),
		DeletionProtection: aws.ToBool(raw.DeletionProtection),
		Members:            globalClusterMembers(raw.GlobalClusterMembers),
		Tags:               mapTags(raw.TagList),
	}
}

func mapEventSubscription(
	raw awsdocdbtypes.EventSubscription,
	tags map[string]string,
) docdbservice.EventSubscription {
	return docdbservice.EventSubscription{
		ARN:             strings.TrimSpace(aws.ToString(raw.EventSubscriptionArn)),
		Name:            strings.TrimSpace(aws.ToString(raw.CustSubscriptionId)),
		CustomerAWSID:   strings.TrimSpace(aws.ToString(raw.CustomerAwsId)),
		Enabled:         aws.ToBool(raw.Enabled),
		Status:          strings.TrimSpace(aws.ToString(raw.Status)),
		SourceType:      strings.TrimSpace(aws.ToString(raw.SourceType)),
		SNSTopicARN:     strings.TrimSpace(aws.ToString(raw.SnsTopicArn)),
		SourceIDs:       trimmedStrings(raw.SourceIdsList),
		EventCategories: trimmedStrings(raw.EventCategoriesList),
		Tags:            tags,
	}
}

func endpointAddress(endpoint *awsdocdbtypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.Address))
}

func endpointPort(endpoint *awsdocdbtypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return aws.ToInt32(endpoint.Port)
}

func endpointHostedZone(endpoint *awsdocdbtypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.HostedZoneId))
}

func securityGroupIDs(groups []awsdocdbtypes.VpcSecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.VpcSecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func clusterMembers(members []awsdocdbtypes.DBClusterMember) []docdbservice.ClusterMember {
	var output []docdbservice.ClusterMember
	for _, member := range members {
		identifier := strings.TrimSpace(aws.ToString(member.DBInstanceIdentifier))
		if identifier == "" {
			continue
		}
		output = append(output, docdbservice.ClusterMember{
			DBInstanceIdentifier: identifier,
			IsWriter:             aws.ToBool(member.IsClusterWriter),
		})
	}
	return output
}

func globalClusterMembers(members []awsdocdbtypes.GlobalClusterMember) []docdbservice.GlobalClusterMember {
	var output []docdbservice.GlobalClusterMember
	for _, member := range members {
		arn := strings.TrimSpace(aws.ToString(member.DBClusterArn))
		if arn == "" {
			continue
		}
		output = append(output, docdbservice.GlobalClusterMember{
			DBClusterARN: arn,
			IsWriter:     aws.ToBool(member.IsWriter),
		})
	}
	return output
}

func clusterRoleARNs(roles []awsdocdbtypes.DBClusterRole) []string {
	var output []string
	for _, role := range roles {
		if arn := strings.TrimSpace(aws.ToString(role.RoleArn)); arn != "" {
			output = append(output, arn)
		}
	}
	return output
}

func subnetIDs(subnets []awsdocdbtypes.Subnet) []string {
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

func mapTags(tags []awsdocdbtypes.Tag) map[string]string {
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
