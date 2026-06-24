// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	elasticacheservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticache"
)

func mapCacheCluster(
	raw awselasticachetypes.CacheCluster,
	tags map[string]string,
	subnetGroups map[string]elasticacheservice.SubnetGroup,
	replicationGroupKMS map[string]string,
) elasticacheservice.CacheCluster {
	subnetGroupName := strings.TrimSpace(aws.ToString(raw.CacheSubnetGroupName))
	replicationGroupID := strings.TrimSpace(aws.ToString(raw.ReplicationGroupId))
	cluster := elasticacheservice.CacheCluster{
		ARN:                       strings.TrimSpace(aws.ToString(raw.ARN)),
		ID:                        strings.TrimSpace(aws.ToString(raw.CacheClusterId)),
		Engine:                    strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:             strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Status:                    strings.TrimSpace(aws.ToString(raw.CacheClusterStatus)),
		NodeType:                  strings.TrimSpace(aws.ToString(raw.CacheNodeType)),
		NumCacheNodes:             aws.ToInt32(raw.NumCacheNodes),
		PreferredAvailabilityZone: strings.TrimSpace(aws.ToString(raw.PreferredAvailabilityZone)),
		SubnetGroupName:           subnetGroupName,
		ParameterGroupName:        parameterGroupName(raw.CacheParameterGroup),
		SecurityGroupIDs:          cacheClusterSecurityGroupIDs(raw.SecurityGroups),
		ReplicationGroupID:        replicationGroupID,
		TransitEncryptionEnabled:  aws.ToBool(raw.TransitEncryptionEnabled),
		AtRestEncryptionEnabled:   aws.ToBool(raw.AtRestEncryptionEnabled),
		AuthTokenEnabled:          aws.ToBool(raw.AuthTokenEnabled),
		SnapshotRetentionLimit:    aws.ToInt32(raw.SnapshotRetentionLimit),
		SnapshotWindow:            strings.TrimSpace(aws.ToString(raw.SnapshotWindow)),
		AutoMinorVersionUpgrade:   aws.ToBool(raw.AutoMinorVersionUpgrade),
		NotificationTopicARN:      notificationTopicARN(raw.NotificationConfiguration),
		NetworkType:               string(raw.NetworkType),
		IPDiscovery:               string(raw.IpDiscovery),
		Tags:                      tags,
	}
	if group, ok := subnetGroups[subnetGroupName]; ok {
		cluster.VPCID = group.VPCID
		cluster.SubnetIDs = append([]string(nil), group.SubnetIDs...)
	}
	if key, ok := replicationGroupKMS[replicationGroupID]; ok {
		cluster.KMSKeyID = key
	}
	return cluster
}

func mapReplicationGroup(
	raw awselasticachetypes.ReplicationGroup,
	tags map[string]string,
) elasticacheservice.ReplicationGroup {
	return elasticacheservice.ReplicationGroup{
		ARN:                      strings.TrimSpace(aws.ToString(raw.ARN)),
		ID:                       strings.TrimSpace(aws.ToString(raw.ReplicationGroupId)),
		Description:              strings.TrimSpace(aws.ToString(raw.Description)),
		Status:                   strings.TrimSpace(aws.ToString(raw.Status)),
		MemberClusters:           trimmedStrings(raw.MemberClusters),
		AutomaticFailover:        string(raw.AutomaticFailover),
		MultiAZ:                  string(raw.MultiAZ),
		ClusterEnabled:           aws.ToBool(raw.ClusterEnabled),
		NodeType:                 strings.TrimSpace(aws.ToString(raw.CacheNodeType)),
		KMSKeyID:                 strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		TransitEncryptionEnabled: aws.ToBool(raw.TransitEncryptionEnabled),
		AtRestEncryptionEnabled:  aws.ToBool(raw.AtRestEncryptionEnabled),
		AuthTokenEnabled:         aws.ToBool(raw.AuthTokenEnabled),
		SnapshotRetentionLimit:   aws.ToInt32(raw.SnapshotRetentionLimit),
		SnapshotWindow:           strings.TrimSpace(aws.ToString(raw.SnapshotWindow)),
		DataTiering:              string(raw.DataTiering),
		NetworkType:              string(raw.NetworkType),
		IPDiscovery:              string(raw.IpDiscovery),
		Tags:                     tags,
	}
}

func mapSubnetGroup(
	raw awselasticachetypes.CacheSubnetGroup,
	tags map[string]string,
) elasticacheservice.SubnetGroup {
	return elasticacheservice.SubnetGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:        strings.TrimSpace(aws.ToString(raw.CacheSubnetGroupName)),
		Description: strings.TrimSpace(aws.ToString(raw.CacheSubnetGroupDescription)),
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        tags,
	}
}

func mapParameterGroup(
	raw awselasticachetypes.CacheParameterGroup,
	tags map[string]string,
) elasticacheservice.ParameterGroup {
	return elasticacheservice.ParameterGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:        strings.TrimSpace(aws.ToString(raw.CacheParameterGroupName)),
		Family:      strings.TrimSpace(aws.ToString(raw.CacheParameterGroupFamily)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		IsGlobal:    aws.ToBool(raw.IsGlobal),
		Tags:        tags,
	}
}

// mapUser intentionally drops the AWS-returned Passwords and AccessString
// fields. Persisting either would leak ACL access strings or password material
// into facts and logs; per issue #713 they must never reach the scanner.
func mapUser(
	raw awselasticachetypes.User,
	tags map[string]string,
) elasticacheservice.User {
	user := elasticacheservice.User{
		ARN:                  strings.TrimSpace(aws.ToString(raw.ARN)),
		ID:                   strings.TrimSpace(aws.ToString(raw.UserId)),
		Name:                 strings.TrimSpace(aws.ToString(raw.UserName)),
		Engine:               strings.TrimSpace(aws.ToString(raw.Engine)),
		Status:               strings.TrimSpace(aws.ToString(raw.Status)),
		MinimumEngineVersion: strings.TrimSpace(aws.ToString(raw.MinimumEngineVersion)),
		UserGroupIDs:         trimmedStrings(raw.UserGroupIds),
		Tags:                 tags,
	}
	if raw.Authentication != nil {
		user.AuthenticationType = string(raw.Authentication.Type)
		user.PasswordCount = aws.ToInt32(raw.Authentication.PasswordCount)
	}
	return user
}

func mapUserGroup(
	raw awselasticachetypes.UserGroup,
	tags map[string]string,
) elasticacheservice.UserGroup {
	return elasticacheservice.UserGroup{
		ARN:     strings.TrimSpace(aws.ToString(raw.ARN)),
		ID:      strings.TrimSpace(aws.ToString(raw.UserGroupId)),
		Engine:  strings.TrimSpace(aws.ToString(raw.Engine)),
		Status:  strings.TrimSpace(aws.ToString(raw.Status)),
		UserIDs: trimmedStrings(raw.UserIds),
		Tags:    tags,
	}
}

// mapSnapshot persists name, source identity, and status only. Per issue #713
// node-snapshot detail, engine, engine version, KMS key, snapshot window,
// snapshot retention limit, and AUTH token state are intentionally not
// projected into scanner-owned types.
func mapSnapshot(
	raw awselasticachetypes.Snapshot,
	tags map[string]string,
) elasticacheservice.SnapshotMetadata {
	return elasticacheservice.SnapshotMetadata{
		ARN:                  strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:                 strings.TrimSpace(aws.ToString(raw.SnapshotName)),
		Status:               strings.TrimSpace(aws.ToString(raw.SnapshotStatus)),
		SnapshotSource:       strings.TrimSpace(aws.ToString(raw.SnapshotSource)),
		SourceCacheClusterID: strings.TrimSpace(aws.ToString(raw.CacheClusterId)),
		SourceReplicationGrp: strings.TrimSpace(aws.ToString(raw.ReplicationGroupId)),
		Tags:                 tags,
	}
}

func parameterGroupName(group *awselasticachetypes.CacheParameterGroupStatus) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(group.CacheParameterGroupName))
}

func notificationTopicARN(config *awselasticachetypes.NotificationConfiguration) string {
	if config == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(config.TopicArn))
}

func cacheClusterSecurityGroupIDs(groups []awselasticachetypes.SecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.SecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func subnetIDs(subnets []awselasticachetypes.Subnet) []string {
	var ids []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.SubnetIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func trimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
