// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmq "github.com/aws/aws-sdk-go-v2/service/mq"
	awsmqtypes "github.com/aws/aws-sdk-go-v2/service/mq/types"

	mqservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mq"
)

func mapBrokerDescription(input *awsmq.DescribeBrokerOutput) mqservice.Broker {
	if input == nil {
		return mqservice.Broker{}
	}
	broker := mqservice.Broker{
		ARN:                     strings.TrimSpace(aws.ToString(input.BrokerArn)),
		ID:                      strings.TrimSpace(aws.ToString(input.BrokerId)),
		Name:                    strings.TrimSpace(aws.ToString(input.BrokerName)),
		EngineType:              string(input.EngineType),
		EngineVersion:           strings.TrimSpace(aws.ToString(input.EngineVersion)),
		DeploymentMode:          string(input.DeploymentMode),
		HostInstanceType:        strings.TrimSpace(aws.ToString(input.HostInstanceType)),
		State:                   string(input.BrokerState),
		StorageType:             string(input.StorageType),
		AuthStrategy:            string(input.AuthenticationStrategy),
		PubliclyAccessible:      aws.ToBool(input.PubliclyAccessible),
		AutoMinorVersionUpgrade: aws.ToBool(input.AutoMinorVersionUpgrade),
		Created:                 aws.ToTime(input.Created),
		Tags:                    cloneStringMap(input.Tags),
		SubnetIDs:               cloneStrings(input.SubnetIds),
		SecurityGroupIDs:        cloneStrings(input.SecurityGroups),
		Encryption:              mapEncryption(input.EncryptionOptions),
		Logs:                    mapLogs(input.Logs),
		Usernames:               brokerUsernames(input.Users),
	}
	if input.Configurations != nil && input.Configurations.Current != nil {
		broker.Configuration = &mqservice.ConfigurationReference{
			ID:       strings.TrimSpace(aws.ToString(input.Configurations.Current.Id)),
			Revision: aws.ToInt32(input.Configurations.Current.Revision),
		}
	}
	return broker
}

func mapEncryption(input *awsmqtypes.EncryptionOptions) mqservice.Encryption {
	if input == nil {
		return mqservice.Encryption{}
	}
	return mqservice.Encryption{
		UseAWSOwnedKey: aws.ToBool(input.UseAwsOwnedKey),
		KMSKeyID:       strings.TrimSpace(aws.ToString(input.KmsKeyId)),
	}
}

func mapLogs(input *awsmqtypes.LogsSummary) mqservice.Logs {
	if input == nil {
		return mqservice.Logs{}
	}
	return mqservice.Logs{
		GeneralEnabled:  aws.ToBool(input.General),
		GeneralLogGroup: strings.TrimSpace(aws.ToString(input.GeneralLogGroup)),
		AuditEnabled:    aws.ToBool(input.Audit),
		AuditLogGroup:   strings.TrimSpace(aws.ToString(input.AuditLogGroup)),
	}
}

// brokerUsernames extracts the broker usernames reported by DescribeBroker.
// UserSummary carries only the username and a pending-change marker; the
// password lives on the User resource returned by DescribeUser, which the
// adapter never calls. The scanner therefore records usernames only.
func brokerUsernames(input []awsmqtypes.UserSummary) []string {
	if len(input) == 0 {
		return nil
	}
	usernames := make([]string, 0, len(input))
	for _, user := range input {
		if username := strings.TrimSpace(aws.ToString(user.Username)); username != "" {
			usernames = append(usernames, username)
		}
	}
	if len(usernames) == 0 {
		return nil
	}
	return usernames
}

func mapConfiguration(input awsmqtypes.Configuration) mqservice.Configuration {
	out := mqservice.Configuration{
		ARN:           strings.TrimSpace(aws.ToString(input.Arn)),
		ID:            strings.TrimSpace(aws.ToString(input.Id)),
		Name:          strings.TrimSpace(aws.ToString(input.Name)),
		Description:   strings.TrimSpace(aws.ToString(input.Description)),
		EngineType:    string(input.EngineType),
		EngineVersion: strings.TrimSpace(aws.ToString(input.EngineVersion)),
		AuthStrategy:  string(input.AuthenticationStrategy),
		Created:       aws.ToTime(input.Created),
		Tags:          cloneStringMap(input.Tags),
	}
	if input.LatestRevision != nil {
		out.LatestRevision = mqservice.ConfigurationRevisionSummary{
			Revision:    aws.ToInt32(input.LatestRevision.Revision),
			Created:     aws.ToTime(input.LatestRevision.Created),
			Description: strings.TrimSpace(aws.ToString(input.LatestRevision.Description)),
		}
	}
	return out
}
