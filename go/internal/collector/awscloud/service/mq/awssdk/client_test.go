// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmq "github.com/aws/aws-sdk-go-v2/service/mq"
	awsmqtypes "github.com/aws/aws-sdk-go-v2/service/mq/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientNeverIncludesForbiddenMethods asserts the narrow apiClient
// interface excludes every Amazon MQ API that mutates brokers, configurations,
// or users, reboots brokers, or exposes broker user password material and
// configuration XML bodies. Adding any of these methods to apiClient (for
// example, to enable a hidden read or write path) fails this test before any
// code can call them.
func TestAPIClientNeverIncludesForbiddenMethods(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Broker mutation and reboot.
		"CreateBroker",
		"UpdateBroker",
		"DeleteBroker",
		"RebootBroker",
		// Configuration mutation.
		"CreateConfiguration",
		"UpdateConfiguration",
		"DeleteConfiguration",
		// Broker-local user resource mutation and password-bearing reads.
		"CreateUser",
		"UpdateUser",
		"DeleteUser",
		"DescribeUser",
		// Configuration body and tag mutation.
		"DescribeConfigurationRevision",
		"CreateTag",
		"DeleteTag",
	}
	for _, method := range forbidden {
		if _, ok := apiClientType.MethodByName(method); ok {
			t.Fatalf("apiClient declares forbidden MQ API %q; the adapter must not gain access to that method", method)
		}
	}
}

func TestClientListBrokersDescribesEachForMetadataAndUsernames(t *testing.T) {
	brokerARN := "arn:aws:mq:us-east-1:123456789012:broker:orders:b-1111"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/22222222-3333-4444-5555-666666666666"
	api := &fakeMQAPI{
		listBrokersPages: []*awsmq.ListBrokersOutput{{
			BrokerSummaries: []awsmqtypes.BrokerSummary{{
				BrokerArn:      aws.String(brokerARN),
				BrokerId:       aws.String("b-1111"),
				BrokerName:     aws.String("orders"),
				BrokerState:    awsmqtypes.BrokerStateRunning,
				EngineType:     awsmqtypes.EngineTypeActivemq,
				DeploymentMode: awsmqtypes.DeploymentModeActiveStandbyMultiAz,
			}},
		}},
		describeBroker: &awsmq.DescribeBrokerOutput{
			BrokerArn:               aws.String(brokerARN),
			BrokerId:                aws.String("b-1111"),
			BrokerName:              aws.String("orders"),
			BrokerState:             awsmqtypes.BrokerStateRunning,
			EngineType:              awsmqtypes.EngineTypeActivemq,
			EngineVersion:           aws.String("5.18.4"),
			DeploymentMode:          awsmqtypes.DeploymentModeActiveStandbyMultiAz,
			HostInstanceType:        aws.String("mq.m5.large"),
			StorageType:             awsmqtypes.BrokerStorageTypeEbs,
			AuthenticationStrategy:  awsmqtypes.AuthenticationStrategySimple,
			PubliclyAccessible:      aws.Bool(false),
			AutoMinorVersionUpgrade: aws.Bool(true),
			Created:                 aws.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
			Tags:                    map[string]string{"Environment": "prod"},
			SubnetIds:               []string{"subnet-aaa", "subnet-bbb"},
			SecurityGroups:          []string{"sg-mq"},
			EncryptionOptions: &awsmqtypes.EncryptionOptions{
				UseAwsOwnedKey: aws.Bool(false),
				KmsKeyId:       aws.String(kmsARN),
			},
			Configurations: &awsmqtypes.Configurations{
				Current: &awsmqtypes.ConfigurationId{
					Id:       aws.String("c-2222"),
					Revision: aws.Int32(3),
				},
			},
			Logs: &awsmqtypes.LogsSummary{
				General:         aws.Bool(true),
				GeneralLogGroup: aws.String("/aws/amazonmq/broker/b-1111/general"),
				Audit:           aws.Bool(true),
				AuditLogGroup:   aws.String("/aws/amazonmq/broker/b-1111/audit"),
			},
			Users: []awsmqtypes.UserSummary{
				{Username: aws.String("admin")},
				{Username: aws.String("publisher")},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMQ},
	}

	brokers, err := adapter.ListBrokers(context.Background())
	if err != nil {
		t.Fatalf("ListBrokers() error = %v, want nil", err)
	}
	if got, want := len(brokers), 1; got != want {
		t.Fatalf("len(brokers) = %d, want %d", got, want)
	}
	got := brokers[0]
	if got.ARN != brokerARN {
		t.Fatalf("broker.ARN = %q, want %q", got.ARN, brokerARN)
	}
	if got.EngineType != "ACTIVEMQ" || got.EngineVersion != "5.18.4" {
		t.Fatalf("engine = (%q,%q), want (ACTIVEMQ,5.18.4)", got.EngineType, got.EngineVersion)
	}
	if got.DeploymentMode != "ACTIVE_STANDBY_MULTI_AZ" {
		t.Fatalf("DeploymentMode = %q, want ACTIVE_STANDBY_MULTI_AZ", got.DeploymentMode)
	}
	if got.HostInstanceType != "mq.m5.large" {
		t.Fatalf("HostInstanceType = %q, want mq.m5.large", got.HostInstanceType)
	}
	if got.State != "RUNNING" {
		t.Fatalf("State = %q, want RUNNING", got.State)
	}
	if got.Encryption.UseAWSOwnedKey || got.Encryption.KMSKeyID != kmsARN {
		t.Fatalf("Encryption = %#v, want customer-managed key %q", got.Encryption, kmsARN)
	}
	if got.Configuration == nil || got.Configuration.ID != "c-2222" || got.Configuration.Revision != 3 {
		t.Fatalf("Configuration = %#v, want id c-2222 revision 3", got.Configuration)
	}
	if got.Logs.GeneralLogGroup != "/aws/amazonmq/broker/b-1111/general" {
		t.Fatalf("Logs.GeneralLogGroup = %q", got.Logs.GeneralLogGroup)
	}
	if len(got.Usernames) != 2 || got.Usernames[0] != "admin" || got.Usernames[1] != "publisher" {
		t.Fatalf("Usernames = %#v, want [admin publisher]", got.Usernames)
	}
	if got.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", got.Tags)
	}
}

func TestClientListConfigurationsCapturesIdentityNotBody(t *testing.T) {
	configARN := "arn:aws:mq:us-east-1:123456789012:configuration:c-2222"
	api := &fakeMQAPI{
		listConfigurationsPages: []*awsmq.ListConfigurationsOutput{{
			Configurations: []awsmqtypes.Configuration{{
				Arn:                    aws.String(configARN),
				Id:                     aws.String("c-2222"),
				Name:                   aws.String("orders-config"),
				Description:            aws.String("orders broker configuration"),
				EngineType:             awsmqtypes.EngineTypeActivemq,
				EngineVersion:          aws.String("5.18.4"),
				AuthenticationStrategy: awsmqtypes.AuthenticationStrategySimple,
				Created:                aws.Time(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)),
				Tags:                   map[string]string{"Owner": "platform"},
				LatestRevision: &awsmqtypes.ConfigurationRevision{
					Revision:    aws.Int32(3),
					Created:     aws.Time(time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC)),
					Description: aws.String("tighten ACLs"),
				},
			}},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMQ},
	}

	configurations, err := adapter.ListConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListConfigurations() error = %v, want nil", err)
	}
	if got, want := len(configurations), 1; got != want {
		t.Fatalf("len(configurations) = %d, want %d", got, want)
	}
	got := configurations[0]
	if got.ARN != configARN || got.ID != "c-2222" {
		t.Fatalf("Configuration identity = (%q,%q), want (%q,c-2222)", got.ARN, got.ID, configARN)
	}
	if got.EngineType != "ACTIVEMQ" {
		t.Fatalf("EngineType = %q, want ACTIVEMQ", got.EngineType)
	}
	if got.LatestRevision.Revision != 3 || got.LatestRevision.Description != "tighten ACLs" {
		t.Fatalf("LatestRevision = %#v, want revision 3", got.LatestRevision)
	}
}

func TestClientListBrokersPaginates(t *testing.T) {
	api := &fakeMQAPI{
		listBrokersPages: []*awsmq.ListBrokersOutput{
			{
				BrokerSummaries: []awsmqtypes.BrokerSummary{{
					BrokerArn: aws.String("arn:aws:mq:us-east-1:123456789012:broker:a:b-1"),
					BrokerId:  aws.String("b-1"),
				}},
				NextToken: aws.String("page-2"),
			},
			{
				BrokerSummaries: []awsmqtypes.BrokerSummary{{
					BrokerArn: aws.String("arn:aws:mq:us-east-1:123456789012:broker:b:b-2"),
					BrokerId:  aws.String("b-2"),
				}},
			},
		},
		describeBroker: &awsmq.DescribeBrokerOutput{
			BrokerArn: aws.String("arn:aws:mq:us-east-1:123456789012:broker:a:b-1"),
			BrokerId:  aws.String("b-1"),
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceMQ},
	}

	brokers, err := adapter.ListBrokers(context.Background())
	if err != nil {
		t.Fatalf("ListBrokers() error = %v, want nil", err)
	}
	if got, want := len(brokers), 2; got != want {
		t.Fatalf("len(brokers) = %d, want %d", got, want)
	}
	if api.describeBrokerCalls != 2 {
		t.Fatalf("DescribeBroker calls = %d, want 2", api.describeBrokerCalls)
	}
}

type fakeMQAPI struct {
	listBrokersPages        []*awsmq.ListBrokersOutput
	listBrokersCalls        int
	describeBroker          *awsmq.DescribeBrokerOutput
	describeBrokerCalls     int
	listConfigurationsPages []*awsmq.ListConfigurationsOutput
	listConfigurationsCalls int
}

func (f *fakeMQAPI) ListBrokers(
	_ context.Context,
	_ *awsmq.ListBrokersInput,
	_ ...func(*awsmq.Options),
) (*awsmq.ListBrokersOutput, error) {
	if f.listBrokersCalls >= len(f.listBrokersPages) {
		return &awsmq.ListBrokersOutput{}, nil
	}
	page := f.listBrokersPages[f.listBrokersCalls]
	f.listBrokersCalls++
	return page, nil
}

func (f *fakeMQAPI) DescribeBroker(
	_ context.Context,
	_ *awsmq.DescribeBrokerInput,
	_ ...func(*awsmq.Options),
) (*awsmq.DescribeBrokerOutput, error) {
	f.describeBrokerCalls++
	if f.describeBroker == nil {
		return &awsmq.DescribeBrokerOutput{}, nil
	}
	return f.describeBroker, nil
}

func (f *fakeMQAPI) ListConfigurations(
	_ context.Context,
	_ *awsmq.ListConfigurationsInput,
	_ ...func(*awsmq.Options),
) (*awsmq.ListConfigurationsOutput, error) {
	if f.listConfigurationsCalls >= len(f.listConfigurationsPages) {
		return &awsmq.ListConfigurationsOutput{}, nil
	}
	page := f.listConfigurationsPages[f.listConfigurationsCalls]
	f.listConfigurationsCalls++
	return page, nil
}

var _ apiClient = (*fakeMQAPI)(nil)
