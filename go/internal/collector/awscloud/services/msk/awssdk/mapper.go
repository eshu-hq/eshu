// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskafka "github.com/aws/aws-sdk-go-v2/service/kafka"
	awskafkatypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"

	mskservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/msk"
)

func mapCluster(input awskafkatypes.Cluster) mskservice.Cluster {
	cluster := mskservice.Cluster{
		ARN:            strings.TrimSpace(aws.ToString(input.ClusterArn)),
		Name:           strings.TrimSpace(aws.ToString(input.ClusterName)),
		Type:           string(input.ClusterType),
		State:          string(input.State),
		CurrentVersion: strings.TrimSpace(aws.ToString(input.CurrentVersion)),
		CreationTime:   aws.ToTime(input.CreationTime),
		Tags:           cloneStringMap(input.Tags),
	}
	if input.Provisioned != nil {
		cluster.Provisioned = mapProvisioned(*input.Provisioned)
	}
	if input.Serverless != nil {
		cluster.Serverless = mapServerless(*input.Serverless)
	}
	return cluster
}

func mapProvisioned(input awskafkatypes.Provisioned) *mskservice.ProvisionedCluster {
	out := &mskservice.ProvisionedCluster{
		EnhancedMonitoring:  string(input.EnhancedMonitoring),
		NumberOfBrokerNodes: aws.ToInt32(input.NumberOfBrokerNodes),
		StorageMode:         string(input.StorageMode),
	}
	if input.CurrentBrokerSoftwareInfo != nil {
		out.KafkaVersion = strings.TrimSpace(aws.ToString(input.CurrentBrokerSoftwareInfo.KafkaVersion))
		configARN := strings.TrimSpace(aws.ToString(input.CurrentBrokerSoftwareInfo.ConfigurationArn))
		if configARN != "" {
			out.CurrentConfiguration = &mskservice.ConfigurationReference{
				ARN:      configARN,
				Revision: aws.ToInt64(input.CurrentBrokerSoftwareInfo.ConfigurationRevision),
			}
		}
	}
	if input.BrokerNodeGroupInfo != nil {
		group := mskservice.BrokerNodeGroup{
			InstanceType:     strings.TrimSpace(aws.ToString(input.BrokerNodeGroupInfo.InstanceType)),
			ClientSubnets:    cloneStrings(input.BrokerNodeGroupInfo.ClientSubnets),
			SecurityGroupIDs: cloneStrings(input.BrokerNodeGroupInfo.SecurityGroups),
		}
		if input.BrokerNodeGroupInfo.StorageInfo != nil && input.BrokerNodeGroupInfo.StorageInfo.EbsStorageInfo != nil {
			group.StorageGiB = aws.ToInt32(input.BrokerNodeGroupInfo.StorageInfo.EbsStorageInfo.VolumeSize)
		}
		out.BrokerNodeGroup = group
	}
	if input.EncryptionInfo != nil {
		if input.EncryptionInfo.EncryptionAtRest != nil {
			out.EncryptionAtRestKMSKey = strings.TrimSpace(aws.ToString(input.EncryptionInfo.EncryptionAtRest.DataVolumeKMSKeyId))
		}
		if input.EncryptionInfo.EncryptionInTransit != nil {
			out.EncryptionInTransit = mskservice.EncryptionInTransit{
				ClientBroker: string(input.EncryptionInfo.EncryptionInTransit.ClientBroker),
				InCluster:    aws.ToBool(input.EncryptionInfo.EncryptionInTransit.InCluster),
			}
		}
	}
	if input.ClientAuthentication != nil {
		out.ClientAuthentication = mapClientAuthentication(*input.ClientAuthentication)
	}
	return out
}

func mapClientAuthentication(input awskafkatypes.ClientAuthentication) mskservice.ClientAuthentication {
	out := mskservice.ClientAuthentication{}
	if input.Sasl != nil {
		if input.Sasl.Iam != nil {
			out.SASLIAMEnabled = aws.ToBool(input.Sasl.Iam.Enabled)
		}
		if input.Sasl.Scram != nil {
			out.SASLSCRAMEnabled = aws.ToBool(input.Sasl.Scram.Enabled)
		}
	}
	if input.Tls != nil {
		out.TLSEnabled = aws.ToBool(input.Tls.Enabled)
		out.TLSCertificateAuthorities = cloneStrings(input.Tls.CertificateAuthorityArnList)
	}
	if input.Unauthenticated != nil {
		out.UnauthenticatedEnabled = aws.ToBool(input.Unauthenticated.Enabled)
	}
	return out
}

func mapServerless(input awskafkatypes.Serverless) *mskservice.ServerlessCluster {
	out := &mskservice.ServerlessCluster{}
	for _, vpc := range input.VpcConfigs {
		out.VPCConfigs = append(out.VPCConfigs, mskservice.VPCConfig{
			SubnetIDs:        cloneStrings(vpc.SubnetIds),
			SecurityGroupIDs: cloneStrings(vpc.SecurityGroupIds),
		})
	}
	if input.ClientAuthentication != nil && input.ClientAuthentication.Sasl != nil &&
		input.ClientAuthentication.Sasl.Iam != nil {
		out.ClientAuthentication.SASLIAMEnabled = aws.ToBool(input.ClientAuthentication.Sasl.Iam.Enabled)
	}
	return out
}

func mapConfiguration(input awskafkatypes.Configuration) mskservice.Configuration {
	out := mskservice.Configuration{
		ARN:           strings.TrimSpace(aws.ToString(input.Arn)),
		Name:          strings.TrimSpace(aws.ToString(input.Name)),
		Description:   strings.TrimSpace(aws.ToString(input.Description)),
		State:         string(input.State),
		CreationTime:  aws.ToTime(input.CreationTime),
		KafkaVersions: cloneStrings(input.KafkaVersions),
	}
	if input.LatestRevision != nil {
		out.LatestRevision = mskservice.ConfigurationRevisionSummary{
			Revision:     aws.ToInt64(input.LatestRevision.Revision),
			CreationTime: aws.ToTime(input.LatestRevision.CreationTime),
			Description:  strings.TrimSpace(aws.ToString(input.LatestRevision.Description)),
		}
	}
	return out
}

func mapReplicatorDescription(input *awskafka.DescribeReplicatorOutput) mskservice.Replicator {
	if input == nil {
		return mskservice.Replicator{}
	}
	out := mskservice.Replicator{
		ARN:                     strings.TrimSpace(aws.ToString(input.ReplicatorArn)),
		Name:                    strings.TrimSpace(aws.ToString(input.ReplicatorName)),
		State:                   string(input.ReplicatorState),
		CurrentVersion:          strings.TrimSpace(aws.ToString(input.CurrentVersion)),
		CreationTime:            aws.ToTime(input.CreationTime),
		ServiceExecutionRoleARN: strings.TrimSpace(aws.ToString(input.ServiceExecutionRoleArn)),
		Tags:                    cloneStringMap(input.Tags),
	}
	for _, cluster := range input.KafkaClusters {
		entry := mskservice.ReplicatorKafkaCluster{
			Alias: strings.TrimSpace(aws.ToString(cluster.KafkaClusterAlias)),
		}
		if cluster.AmazonMskCluster != nil {
			entry.MSKClusterARN = strings.TrimSpace(aws.ToString(cluster.AmazonMskCluster.MskClusterArn))
		}
		if cluster.VpcConfig != nil {
			entry.VPCSubnetIDs = cloneStrings(cluster.VpcConfig.SubnetIds)
			entry.VPCSecurityGroupIDs = cloneStrings(cluster.VpcConfig.SecurityGroupIds)
		}
		out.KafkaClusters = append(out.KafkaClusters, entry)
	}
	for _, info := range input.ReplicationInfoList {
		summary := mskservice.ReplicationInfo{
			SourceAlias:       strings.TrimSpace(aws.ToString(info.SourceKafkaClusterAlias)),
			TargetAlias:       strings.TrimSpace(aws.ToString(info.TargetKafkaClusterAlias)),
			TargetCompression: string(info.TargetCompressionType),
		}
		summary.SourceClusterARN = clusterARNForAlias(out.KafkaClusters, summary.SourceAlias)
		summary.TargetClusterARN = clusterARNForAlias(out.KafkaClusters, summary.TargetAlias)
		if info.TopicReplication != nil {
			summary.TopicIncludePatternCount = len(info.TopicReplication.TopicsToReplicate)
			summary.TopicExcludePatternCount = len(info.TopicReplication.TopicsToExclude)
		}
		if info.ConsumerGroupReplication != nil {
			summary.ConsumerGroupIncludePatternCount = len(info.ConsumerGroupReplication.ConsumerGroupsToReplicate)
			summary.ConsumerGroupExcludePatternCount = len(info.ConsumerGroupReplication.ConsumerGroupsToExclude)
		}
		out.ReplicationInfo = append(out.ReplicationInfo, summary)
	}
	return out
}

func clusterARNForAlias(clusters []mskservice.ReplicatorKafkaCluster, alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	for _, cluster := range clusters {
		if cluster.Alias == alias {
			return cluster.MSKClusterARN
		}
	}
	return ""
}
