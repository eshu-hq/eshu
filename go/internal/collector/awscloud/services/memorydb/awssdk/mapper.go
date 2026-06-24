// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmemorydbtypes "github.com/aws/aws-sdk-go-v2/service/memorydb/types"

	memorydbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/memorydb"
)

func mapCluster(
	raw awsmemorydbtypes.Cluster,
	tags map[string]string,
) memorydbservice.Cluster {
	return memorydbservice.Cluster{
		ARN:                      strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:                     strings.TrimSpace(aws.ToString(raw.Name)),
		Description:              strings.TrimSpace(aws.ToString(raw.Description)),
		Status:                   strings.TrimSpace(aws.ToString(raw.Status)),
		Engine:                   strings.TrimSpace(aws.ToString(raw.Engine)),
		EngineVersion:            strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		NodeType:                 strings.TrimSpace(aws.ToString(raw.NodeType)),
		NumberOfShards:           aws.ToInt32(raw.NumberOfShards),
		NumberOfReplicasPerShard: replicasPerShard(raw.Shards),
		ACLName:                  strings.TrimSpace(aws.ToString(raw.ACLName)),
		ParameterGroupName:       strings.TrimSpace(aws.ToString(raw.ParameterGroupName)),
		SubnetGroupName:          strings.TrimSpace(aws.ToString(raw.SubnetGroupName)),
		SecurityGroupIDs:         clusterSecurityGroupIDs(raw.SecurityGroups),
		KMSKeyID:                 strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		SNSTopicARN:              strings.TrimSpace(aws.ToString(raw.SnsTopicArn)),
		TLSEnabled:               aws.ToBool(raw.TLSEnabled),
		DataTiering:              string(raw.DataTiering),
		AutoMinorVersionUpgrade:  aws.ToBool(raw.AutoMinorVersionUpgrade),
		SnapshotRetentionLimit:   aws.ToInt32(raw.SnapshotRetentionLimit),
		SnapshotWindow:           strings.TrimSpace(aws.ToString(raw.SnapshotWindow)),
		MaintenanceWindow:        strings.TrimSpace(aws.ToString(raw.MaintenanceWindow)),
		AvailabilityMode:         string(raw.AvailabilityMode),
		NetworkType:              string(raw.NetworkType),
		IPDiscovery:              string(raw.IpDiscovery),
		Tags:                     tags,
	}
}

func mapSubnetGroup(
	raw awsmemorydbtypes.SubnetGroup,
	tags map[string]string,
) memorydbservice.SubnetGroup {
	return memorydbservice.SubnetGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:        strings.TrimSpace(aws.ToString(raw.Name)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        tags,
	}
}

func mapParameterGroup(
	raw awsmemorydbtypes.ParameterGroup,
	tags map[string]string,
) memorydbservice.ParameterGroup {
	return memorydbservice.ParameterGroup{
		ARN:         strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:        strings.TrimSpace(aws.ToString(raw.Name)),
		Family:      strings.TrimSpace(aws.ToString(raw.Family)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		Tags:        tags,
	}
}

// mapUser intentionally drops the AWS-returned AccessString (the raw ACL grant
// string). Persisting it would leak ACL grant detail into facts and logs; the
// scanner records only a non-secret presence signal. Password material is not
// present in the describe response and is never persisted.
func mapUser(
	raw awsmemorydbtypes.User,
	tags map[string]string,
) memorydbservice.User {
	user := memorydbservice.User{
		ARN:                  strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:                 strings.TrimSpace(aws.ToString(raw.Name)),
		Status:               strings.TrimSpace(aws.ToString(raw.Status)),
		AccessStringPresent:  strings.TrimSpace(aws.ToString(raw.AccessString)) != "",
		MinimumEngineVersion: strings.TrimSpace(aws.ToString(raw.MinimumEngineVersion)),
		ACLNames:             trimmedStrings(raw.ACLNames),
		Tags:                 tags,
	}
	if raw.Authentication != nil {
		user.AuthenticationType = string(raw.Authentication.Type)
		user.PasswordCount = aws.ToInt32(raw.Authentication.PasswordCount)
	}
	return user
}

func mapACL(
	raw awsmemorydbtypes.ACL,
	tags map[string]string,
) memorydbservice.ACL {
	return memorydbservice.ACL{
		ARN:                  strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:                 strings.TrimSpace(aws.ToString(raw.Name)),
		Status:               strings.TrimSpace(aws.ToString(raw.Status)),
		MinimumEngineVersion: strings.TrimSpace(aws.ToString(raw.MinimumEngineVersion)),
		UserNames:            trimmedStrings(raw.UserNames),
		ClusterNames:         trimmedStrings(raw.Clusters),
		Tags:                 tags,
	}
}

// mapSnapshot persists name, source cluster identity, source, and status only.
// Cluster configuration, shard sizes, engine version, KMS key, and any backup
// payload detail are intentionally not projected into scanner-owned types.
func mapSnapshot(
	raw awsmemorydbtypes.Snapshot,
	tags map[string]string,
) memorydbservice.SnapshotMetadata {
	return memorydbservice.SnapshotMetadata{
		ARN:               strings.TrimSpace(aws.ToString(raw.ARN)),
		Name:              strings.TrimSpace(aws.ToString(raw.Name)),
		Status:            strings.TrimSpace(aws.ToString(raw.Status)),
		Source:            strings.TrimSpace(aws.ToString(raw.Source)),
		SourceClusterName: snapshotClusterName(raw.ClusterConfiguration),
		Tags:              tags,
	}
}

// replicasPerShard derives the per-shard read replica count from the reported
// shard node counts. MemoryDB does not expose a replica-count field on the
// Cluster response, so the adapter computes it as the shard node count minus
// the single primary. It returns the maximum across shards so an unbalanced
// cluster reports the configured replica count rather than zero. The result is
// clamped at zero for shards that report no node detail.
func replicasPerShard(shards []awsmemorydbtypes.Shard) int32 {
	var maxReplicas int32
	for _, shard := range shards {
		nodes := aws.ToInt32(shard.NumberOfNodes)
		if nodes <= 1 {
			continue
		}
		replicas := nodes - 1
		if replicas > maxReplicas {
			maxReplicas = replicas
		}
	}
	return maxReplicas
}

func snapshotClusterName(config *awsmemorydbtypes.ClusterConfiguration) string {
	if config == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(config.Name))
}

func clusterSecurityGroupIDs(groups []awsmemorydbtypes.SecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.SecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func subnetIDs(subnets []awsmemorydbtypes.Subnet) []string {
	var ids []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.Identifier)); id != "" {
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
