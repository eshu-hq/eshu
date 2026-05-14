package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	rdsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds"
)

func mapDBInstance(raw awsrdstypes.DBInstance, tags map[string]string) rdsservice.DBInstance {
	return rdsservice.DBInstance{
		ARN:                              strings.TrimSpace(aws.ToString(raw.DBInstanceArn)),
		Identifier:                       strings.TrimSpace(aws.ToString(raw.DBInstanceIdentifier)),
		ResourceID:                       strings.TrimSpace(aws.ToString(raw.DbiResourceId)),
		Class:                            strings.TrimSpace(aws.ToString(raw.DBInstanceClass)),
		Engine:                           strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:                    strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:                           strings.TrimSpace(aws.ToString(raw.DBInstanceStatus)),
		EndpointAddress:                  endpointAddress(raw.Endpoint),
		EndpointPort:                     endpointPort(raw.Endpoint),
		HostedZoneID:                     endpointHostedZone(raw.Endpoint),
		AvailabilityZone:                 strings.TrimSpace(aws.ToString(raw.AvailabilityZone)),
		SecondaryAvailabilityZone:        strings.TrimSpace(aws.ToString(raw.SecondaryAvailabilityZone)),
		MultiAZ:                          aws.ToBool(raw.MultiAZ),
		PubliclyAccessible:               aws.ToBool(raw.PubliclyAccessible),
		StorageEncrypted:                 aws.ToBool(raw.StorageEncrypted),
		KMSKeyID:                         strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		IAMDatabaseAuthenticationEnabled: aws.ToBool(raw.IAMDatabaseAuthenticationEnabled),
		DeletionProtection:               aws.ToBool(raw.DeletionProtection),
		BackupRetentionPeriod:            aws.ToInt32(raw.BackupRetentionPeriod),
		DBSubnetGroupName:                subnetGroupName(raw.DBSubnetGroup),
		VPCID:                            subnetGroupVPCID(raw.DBSubnetGroup),
		VPCSecurityGroupIDs:              securityGroupIDs(raw.VpcSecurityGroups),
		ClusterIdentifier:                strings.TrimSpace(aws.ToString(raw.DBClusterIdentifier)),
		ParameterGroups:                  parameterGroups(raw.DBParameterGroups),
		OptionGroups:                     optionGroups(raw.OptionGroupMemberships),
		MonitoringRoleARN:                strings.TrimSpace(aws.ToString(raw.MonitoringRoleArn)),
		PerformanceInsightsEnabled:       aws.ToBool(raw.PerformanceInsightsEnabled),
		PerformanceInsightsKMSKeyID:      strings.TrimSpace(aws.ToString(raw.PerformanceInsightsKMSKeyId)),
		Tags:                             tags,
	}
}

func mapDBCluster(raw awsrdstypes.DBCluster, tags map[string]string) rdsservice.DBCluster {
	return rdsservice.DBCluster{
		ARN:                              strings.TrimSpace(aws.ToString(raw.DBClusterArn)),
		Identifier:                       strings.TrimSpace(aws.ToString(raw.DBClusterIdentifier)),
		ResourceID:                       strings.TrimSpace(aws.ToString(raw.DbClusterResourceId)),
		Engine:                           strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:                    strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:                           strings.TrimSpace(aws.ToString(raw.Status)),
		EndpointAddress:                  strings.TrimSpace(aws.ToString(raw.Endpoint)),
		ReaderEndpointAddress:            strings.TrimSpace(aws.ToString(raw.ReaderEndpoint)),
		HostedZoneID:                     strings.TrimSpace(aws.ToString(raw.HostedZoneId)),
		Port:                             aws.ToInt32(raw.Port),
		MultiAZ:                          aws.ToBool(raw.MultiAZ),
		StorageEncrypted:                 aws.ToBool(raw.StorageEncrypted),
		KMSKeyID:                         strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		IAMDatabaseAuthenticationEnabled: aws.ToBool(raw.IAMDatabaseAuthenticationEnabled),
		DeletionProtection:               aws.ToBool(raw.DeletionProtection),
		BackupRetentionPeriod:            aws.ToInt32(raw.BackupRetentionPeriod),
		DBSubnetGroupName:                strings.TrimSpace(aws.ToString(raw.DBSubnetGroup)),
		VPCSecurityGroupIDs:              securityGroupIDs(raw.VpcSecurityGroups),
		Members:                          clusterMembers(raw.DBClusterMembers),
		ParameterGroup:                   strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroup)),
		AssociatedRoleARNs:               clusterRoleARNs(raw.AssociatedRoles),
		Tags:                             tags,
	}
}

func mapDBSubnetGroup(
	raw awsrdstypes.DBSubnetGroup,
	tags map[string]string,
) rdsservice.DBSubnetGroup {
	return rdsservice.DBSubnetGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.DBSubnetGroupArn)),
		Name:        strings.TrimSpace(aws.ToString(raw.DBSubnetGroupName)),
		Description: strings.TrimSpace(aws.ToString(raw.DBSubnetGroupDescription)),
		Status:      strings.TrimSpace(aws.ToString(raw.SubnetGroupStatus)),
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        tags,
	}
}

func endpointAddress(endpoint *awsrdstypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.Address))
}

func endpointPort(endpoint *awsrdstypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return aws.ToInt32(endpoint.Port)
}

func endpointHostedZone(endpoint *awsrdstypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.HostedZoneId))
}

func subnetGroupName(group *awsrdstypes.DBSubnetGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(group.DBSubnetGroupName))
}

func subnetGroupVPCID(group *awsrdstypes.DBSubnetGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(group.VpcId))
}

func securityGroupIDs(groups []awsrdstypes.VpcSecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.VpcSecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func parameterGroups(groups []awsrdstypes.DBParameterGroupStatus) []rdsservice.ParameterGroup {
	var output []rdsservice.ParameterGroup
	for _, group := range groups {
		name := strings.TrimSpace(aws.ToString(group.DBParameterGroupName))
		if name == "" {
			continue
		}
		output = append(output, rdsservice.ParameterGroup{
			Name:  name,
			State: strings.TrimSpace(aws.ToString(group.ParameterApplyStatus)),
		})
	}
	return output
}

func optionGroups(groups []awsrdstypes.OptionGroupMembership) []rdsservice.OptionGroup {
	var output []rdsservice.OptionGroup
	for _, group := range groups {
		name := strings.TrimSpace(aws.ToString(group.OptionGroupName))
		if name == "" {
			continue
		}
		output = append(output, rdsservice.OptionGroup{
			Name:  name,
			State: strings.TrimSpace(aws.ToString(group.Status)),
		})
	}
	return output
}

func clusterMembers(members []awsrdstypes.DBClusterMember) []rdsservice.ClusterMember {
	var output []rdsservice.ClusterMember
	for _, member := range members {
		identifier := strings.TrimSpace(aws.ToString(member.DBInstanceIdentifier))
		if identifier == "" {
			continue
		}
		output = append(output, rdsservice.ClusterMember{
			DBInstanceIdentifier: identifier,
			IsWriter:             aws.ToBool(member.IsClusterWriter),
		})
	}
	return output
}

func clusterRoleARNs(roles []awsrdstypes.DBClusterRole) []string {
	var output []string
	for _, role := range roles {
		if arn := strings.TrimSpace(aws.ToString(role.RoleArn)); arn != "" {
			output = append(output, arn)
		}
	}
	return output
}

func subnetIDs(subnets []awsrdstypes.Subnet) []string {
	var output []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.SubnetIdentifier)); id != "" {
			output = append(output, id)
		}
	}
	return output
}

func mapTags(tags []awsrdstypes.Tag) map[string]string {
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
