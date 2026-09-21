// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"reflect"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awskafka "github.com/aws/aws-sdk-go-v2/service/kafka"
	awskafkatypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// TestAPIClientNeverIncludesForbiddenMethods asserts the narrow apiClient
// interface excludes every MSK API that would expose broker payload, log,
// secret, or configuration-revision body material. Adding any of these
// methods to apiClient (e.g., to enable a hidden read path) fails this test
// even before any code can call them.
func TestAPIClientNeverIncludesForbiddenMethods(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		"DescribeConfigurationRevision",
		"GetBootstrapBrokers",
		"ListScramSecrets",
		"BatchAssociateScramSecret",
		"BatchDisassociateScramSecret",
		"GetClusterPolicy",
		"PutClusterPolicy",
		"DeleteClusterPolicy",
		"CreateClusterV2",
		"UpdateClusterKafkaVersion",
		"DeleteCluster",
		"RebootBroker",
		"UpdateBrokerCount",
		"UpdateBrokerStorage",
		"UpdateBrokerType",
		"UpdateConfiguration",
		"CreateConfiguration",
		"DeleteConfiguration",
		"CreateReplicator",
		"DeleteReplicator",
		"CreateTopic",
		"DeleteTopic",
		"TagResource",
		"UntagResource",
	}
	for _, method := range forbidden {
		if _, ok := apiClientType.MethodByName(method); ok {
			t.Fatalf("apiClient declares forbidden MSK API %q; the adapter must not gain access to that method", method)
		}
	}
}

func TestClientListClustersMapsProvisionedAndServerlessMetadataSafely(t *testing.T) {
	clusterARN := "arn:aws:kafka:us-east-1:123456789012:cluster/orders/abcd-1"
	configARN := "arn:aws:kafka:us-east-1:123456789012:configuration/orders-config/efgh-2"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/22222222-3333-4444-5555-666666666666"
	provisioned := awskafkatypes.Cluster{
		ClusterArn:     awsv2.String(clusterARN),
		ClusterName:    awsv2.String("orders"),
		ClusterType:    awskafkatypes.ClusterTypeProvisioned,
		State:          awskafkatypes.ClusterStateActive,
		CurrentVersion: awsv2.String("K3J9NPHZ4YL2T1"),
		CreationTime:   awsv2.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
		Tags:           map[string]string{"Environment": "prod"},
		Provisioned: &awskafkatypes.Provisioned{
			NumberOfBrokerNodes: awsv2.Int32(3),
			StorageMode:         awskafkatypes.StorageModeLocal,
			EnhancedMonitoring:  awskafkatypes.EnhancedMonitoringPerTopicPerPartition,
			CurrentBrokerSoftwareInfo: &awskafkatypes.BrokerSoftwareInfo{
				KafkaVersion:          awsv2.String("3.6.0"),
				ConfigurationArn:      awsv2.String(configARN),
				ConfigurationRevision: awsv2.Int64(2),
			},
			BrokerNodeGroupInfo: &awskafkatypes.BrokerNodeGroupInfo{
				InstanceType:   awsv2.String("kafka.m7g.large"),
				ClientSubnets:  []string{"subnet-aaa", "subnet-bbb"},
				SecurityGroups: []string{"sg-msk"},
				StorageInfo: &awskafkatypes.StorageInfo{
					EbsStorageInfo: &awskafkatypes.EBSStorageInfo{VolumeSize: awsv2.Int32(1000)},
				},
			},
			EncryptionInfo: &awskafkatypes.EncryptionInfo{
				EncryptionAtRest: &awskafkatypes.EncryptionAtRest{
					DataVolumeKMSKeyId: awsv2.String(kmsARN),
				},
				EncryptionInTransit: &awskafkatypes.EncryptionInTransit{
					ClientBroker: awskafkatypes.ClientBrokerTls,
					InCluster:    awsv2.Bool(true),
				},
			},
			ClientAuthentication: &awskafkatypes.ClientAuthentication{
				Sasl: &awskafkatypes.Sasl{
					Iam:   &awskafkatypes.Iam{Enabled: awsv2.Bool(true)},
					Scram: &awskafkatypes.Scram{Enabled: awsv2.Bool(false)},
				},
				Tls: &awskafkatypes.Tls{
					Enabled: awsv2.Bool(true),
					CertificateAuthorityArnList: []string{
						"arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/aaa",
					},
				},
			},
		},
	}
	serverless := awskafkatypes.Cluster{
		ClusterArn:  awsv2.String("arn:aws:kafka:us-east-1:123456789012:cluster/orders-serverless/zzzz-3"),
		ClusterName: awsv2.String("orders-serverless"),
		ClusterType: awskafkatypes.ClusterTypeServerless,
		State:       awskafkatypes.ClusterStateActive,
		Serverless: &awskafkatypes.Serverless{
			VpcConfigs: []awskafkatypes.VpcConfig{{
				SubnetIds:        []string{"subnet-private-1"},
				SecurityGroupIds: []string{"sg-private"},
			}},
			ClientAuthentication: &awskafkatypes.ServerlessClientAuthentication{
				Sasl: &awskafkatypes.ServerlessSasl{
					Iam: &awskafkatypes.Iam{Enabled: awsv2.Bool(true)},
				},
			},
		},
	}

	api := &fakeKafkaAPI{
		clusterPages: []*awskafka.ListClustersV2Output{{
			ClusterInfoList: []awskafkatypes.Cluster{provisioned, serverless},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceMSK},
	}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if got, want := len(clusters), 2; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	got := clusters[0]
	if got.ARN != clusterARN {
		t.Fatalf("cluster[0].ARN = %q, want %q", got.ARN, clusterARN)
	}
	if got.Provisioned == nil {
		t.Fatalf("cluster[0].Provisioned = nil, want populated")
	}
	if got.Provisioned.KafkaVersion != "3.6.0" {
		t.Fatalf("cluster[0].Provisioned.KafkaVersion = %q", got.Provisioned.KafkaVersion)
	}
	if got.Provisioned.CurrentConfiguration == nil || got.Provisioned.CurrentConfiguration.ARN != configARN {
		t.Fatalf("CurrentConfiguration = %#v, want ARN %q", got.Provisioned.CurrentConfiguration, configARN)
	}
	if got.Provisioned.EncryptionAtRestKMSKey != kmsARN {
		t.Fatalf("EncryptionAtRestKMSKey = %q, want %q", got.Provisioned.EncryptionAtRestKMSKey, kmsARN)
	}
	if !got.Provisioned.ClientAuthentication.SASLIAMEnabled {
		t.Fatalf("ClientAuthentication.SASLIAMEnabled = false, want true")
	}
	if !got.Provisioned.ClientAuthentication.TLSEnabled {
		t.Fatalf("ClientAuthentication.TLSEnabled = false, want true")
	}
	if got.Provisioned.BrokerNodeGroup.StorageGiB != 1000 {
		t.Fatalf("StorageGiB = %d, want 1000", got.Provisioned.BrokerNodeGroup.StorageGiB)
	}
	gotServerless := clusters[1]
	if gotServerless.Serverless == nil {
		t.Fatalf("cluster[1].Serverless = nil, want populated")
	}
	if len(gotServerless.Serverless.VPCConfigs) != 1 {
		t.Fatalf("Serverless.VPCConfigs len = %d, want 1", len(gotServerless.Serverless.VPCConfigs))
	}
	if !gotServerless.Serverless.ClientAuthentication.SASLIAMEnabled {
		t.Fatalf("Serverless SASL IAM enabled = false, want true")
	}
}

func TestClientListConfigurationsCapturesIdentityNotBody(t *testing.T) {
	configARN := "arn:aws:kafka:us-east-1:123456789012:configuration/orders-config/efgh-2"
	api := &fakeKafkaAPI{
		configurationPages: []*awskafka.ListConfigurationsOutput{{
			Configurations: []awskafkatypes.Configuration{{
				Arn:           awsv2.String(configARN),
				Name:          awsv2.String("orders-config"),
				Description:   awsv2.String("orders broker configuration"),
				State:         awskafkatypes.ConfigurationStateActive,
				CreationTime:  awsv2.Time(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)),
				KafkaVersions: []string{"3.6.0"},
				LatestRevision: &awskafkatypes.ConfigurationRevision{
					Revision:     awsv2.Int64(2),
					CreationTime: awsv2.Time(time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC)),
					Description:  awsv2.String("tighten retention"),
				},
			}},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceMSK},
	}

	configurations, err := adapter.ListConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListConfigurations() error = %v, want nil", err)
	}
	if got, want := len(configurations), 1; got != want {
		t.Fatalf("len(configurations) = %d, want %d", got, want)
	}
	got := configurations[0]
	if got.ARN != configARN {
		t.Fatalf("Configuration.ARN = %q, want %q", got.ARN, configARN)
	}
	if got.LatestRevision.Revision != 2 {
		t.Fatalf("LatestRevision.Revision = %d, want 2", got.LatestRevision.Revision)
	}
}

func TestClientListReplicatorsDescribesEachForRoleClustersAndPatternCounts(t *testing.T) {
	replicatorARN := "arn:aws:kafka:us-east-1:123456789012:replicator/cross-region/bbbb-3"
	sourceClusterARN := "arn:aws:kafka:us-east-1:123456789012:cluster/orders/abcd-1"
	roleARN := "arn:aws:iam::123456789012:role/msk-replicator"
	api := &fakeKafkaAPI{
		replicatorPages: []*awskafka.ListReplicatorsOutput{{
			Replicators: []awskafkatypes.ReplicatorSummary{{
				ReplicatorArn:   awsv2.String(replicatorARN),
				ReplicatorName:  awsv2.String("cross-region"),
				ReplicatorState: awskafkatypes.ReplicatorStateRunning,
				CreationTime:    awsv2.Time(time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC)),
				CurrentVersion:  awsv2.String("REPLICATOR-1"),
			}},
		}},
		describeReplicatorOutput: &awskafka.DescribeReplicatorOutput{
			ReplicatorArn:           awsv2.String(replicatorARN),
			ReplicatorName:          awsv2.String("cross-region"),
			ReplicatorState:         awskafkatypes.ReplicatorStateRunning,
			ServiceExecutionRoleArn: awsv2.String(roleARN),
			CreationTime:            awsv2.Time(time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC)),
			CurrentVersion:          awsv2.String("REPLICATOR-1"),
			Tags:                    map[string]string{"Owner": "platform"},
			KafkaClusters: []awskafkatypes.KafkaClusterDescription{{
				KafkaClusterAlias: awsv2.String("source"),
				AmazonMskCluster:  &awskafkatypes.AmazonMskCluster{MskClusterArn: awsv2.String(sourceClusterARN)},
				VpcConfig: &awskafkatypes.KafkaClusterClientVpcConfig{
					SubnetIds:        []string{"subnet-aaa", "subnet-bbb"},
					SecurityGroupIds: []string{"sg-msk"},
				},
			}},
			ReplicationInfoList: []awskafkatypes.ReplicationInfoDescription{{
				SourceKafkaClusterAlias: awsv2.String("source"),
				TargetKafkaClusterAlias: awsv2.String("target"),
				TargetCompressionType:   awskafkatypes.TargetCompressionTypeGzip,
				TopicReplication: &awskafkatypes.TopicReplication{
					TopicsToReplicate: []string{"orders.*", "payments.*"},
					TopicsToExclude:   []string{"orders\\.internal"},
				},
				ConsumerGroupReplication: &awskafkatypes.ConsumerGroupReplication{
					ConsumerGroupsToReplicate: []string{"shipping-.*"},
				},
			}},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceMSK},
	}

	replicators, err := adapter.ListReplicators(context.Background())
	if err != nil {
		t.Fatalf("ListReplicators() error = %v, want nil", err)
	}
	if got, want := len(replicators), 1; got != want {
		t.Fatalf("len(replicators) = %d, want %d", got, want)
	}
	got := replicators[0]
	if got.ServiceExecutionRoleARN != roleARN {
		t.Fatalf("ServiceExecutionRoleARN = %q, want %q", got.ServiceExecutionRoleARN, roleARN)
	}
	if got.Tags["Owner"] != "platform" {
		t.Fatalf("Tags = %#v, want Owner=platform", got.Tags)
	}
	if len(got.KafkaClusters) != 1 || got.KafkaClusters[0].MSKClusterARN != sourceClusterARN {
		t.Fatalf("KafkaClusters = %#v, want one with MSKClusterARN %q", got.KafkaClusters, sourceClusterARN)
	}
	if len(got.ReplicationInfo) != 1 {
		t.Fatalf("ReplicationInfo len = %d, want 1", len(got.ReplicationInfo))
	}
	info := got.ReplicationInfo[0]
	if info.TopicIncludePatternCount != 2 || info.TopicExcludePatternCount != 1 {
		t.Fatalf("topic pattern counts = (%d,%d), want (2,1)", info.TopicIncludePatternCount, info.TopicExcludePatternCount)
	}
	if info.ConsumerGroupIncludePatternCount != 1 {
		t.Fatalf("consumer group include pattern count = %d, want 1", info.ConsumerGroupIncludePatternCount)
	}
}

type fakeKafkaAPI struct {
	clusterPages             []*awskafka.ListClustersV2Output
	clusterCalls             int
	configurationPages       []*awskafka.ListConfigurationsOutput
	configurationCalls       int
	replicatorPages          []*awskafka.ListReplicatorsOutput
	replicatorCalls          int
	describeReplicatorOutput *awskafka.DescribeReplicatorOutput
}

func (f *fakeKafkaAPI) ListClustersV2(
	_ context.Context,
	_ *awskafka.ListClustersV2Input,
	_ ...func(*awskafka.Options),
) (*awskafka.ListClustersV2Output, error) {
	if f.clusterCalls >= len(f.clusterPages) {
		return &awskafka.ListClustersV2Output{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeKafkaAPI) ListConfigurations(
	_ context.Context,
	_ *awskafka.ListConfigurationsInput,
	_ ...func(*awskafka.Options),
) (*awskafka.ListConfigurationsOutput, error) {
	if f.configurationCalls >= len(f.configurationPages) {
		return &awskafka.ListConfigurationsOutput{}, nil
	}
	page := f.configurationPages[f.configurationCalls]
	f.configurationCalls++
	return page, nil
}

func (f *fakeKafkaAPI) ListReplicators(
	_ context.Context,
	_ *awskafka.ListReplicatorsInput,
	_ ...func(*awskafka.Options),
) (*awskafka.ListReplicatorsOutput, error) {
	if f.replicatorCalls >= len(f.replicatorPages) {
		return &awskafka.ListReplicatorsOutput{}, nil
	}
	page := f.replicatorPages[f.replicatorCalls]
	f.replicatorCalls++
	return page, nil
}

func (f *fakeKafkaAPI) DescribeReplicator(
	_ context.Context,
	_ *awskafka.DescribeReplicatorInput,
	_ ...func(*awskafka.Options),
) (*awskafka.DescribeReplicatorOutput, error) {
	if f.describeReplicatorOutput == nil {
		return &awskafka.DescribeReplicatorOutput{}, nil
	}
	return f.describeReplicatorOutput, nil
}

var _ apiClient = (*fakeKafkaAPI)(nil)
