// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package msk

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsMSKClusterConfigurationReplicatorMetadataAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:kafka:us-east-1:123456789012:cluster/orders/11111111-2222-3333-4444-555555555555-1"
	configurationARN := "arn:aws:kafka:us-east-1:123456789012:configuration/orders-config/66666666-7777-8888-9999-aaaaaaaaaaaa-2"
	replicatorARN := "arn:aws:kafka:us-east-1:123456789012:replicator/cross-region/bbbbbbbb-cccc-dddd-eeee-ffffffffffff-3"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/22222222-3333-4444-5555-666666666666"
	roleARN := "arn:aws:iam::123456789012:role/msk-replicator"

	client := fakeClient{
		clusters: []Cluster{{
			ARN:            clusterARN,
			Name:           "orders",
			Type:           "PROVISIONED",
			State:          "ACTIVE",
			CurrentVersion: "K3J9NPHZ4YL2T1",
			CreationTime:   time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC),
			Tags:           map[string]string{"Environment": "prod"},
			Provisioned: &ProvisionedCluster{
				KafkaVersion:           "3.6.0",
				EnhancedMonitoring:     "PER_TOPIC_PER_PARTITION",
				NumberOfBrokerNodes:    3,
				StorageMode:            "LOCAL",
				EncryptionAtRestKMSKey: kmsKeyARN,
				EncryptionInTransit:    EncryptionInTransit{ClientBroker: "TLS", InCluster: true},
				ClientAuthentication: ClientAuthentication{
					SASLIAMEnabled: true,
					TLSEnabled:     true,
				},
				BrokerNodeGroup: BrokerNodeGroup{
					InstanceType:     "kafka.m7g.large",
					ClientSubnets:    []string{"subnet-aaa", "subnet-bbb"},
					SecurityGroupIDs: []string{"sg-msk"},
					StorageGiB:       1000,
				},
				CurrentConfiguration: &ConfigurationReference{
					ARN:      configurationARN,
					Revision: 2,
				},
			},
		}},
		configurations: []Configuration{{
			ARN:           configurationARN,
			Name:          "orders-config",
			Description:   "orders broker configuration",
			State:         "ACTIVE",
			CreationTime:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
			KafkaVersions: []string{"3.6.0"},
			LatestRevision: ConfigurationRevisionSummary{
				Revision:     2,
				CreationTime: time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC),
				Description:  "tighten retention",
			},
		}},
		replicators: []Replicator{{
			ARN:                     replicatorARN,
			Name:                    "cross-region",
			State:                   "RUNNING",
			CurrentVersion:          "REPLICATOR-1",
			CreationTime:            time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC),
			ServiceExecutionRoleARN: roleARN,
			Tags:                    map[string]string{"Owner": "platform"},
			KafkaClusters: []ReplicatorKafkaCluster{{
				Alias:               "source",
				MSKClusterARN:       clusterARN,
				VPCSubnetIDs:        []string{"subnet-aaa", "subnet-bbb"},
				VPCSecurityGroupIDs: []string{"sg-msk"},
			}},
			ReplicationInfo: []ReplicationInfo{{
				SourceClusterARN:                 clusterARN,
				TargetClusterARN:                 clusterARN,
				SourceAlias:                      "source",
				TargetAlias:                      "target",
				TargetCompression:                "GZIP",
				TopicIncludePatternCount:         2,
				TopicExcludePatternCount:         1,
				ConsumerGroupIncludePatternCount: 1,
				ConsumerGroupExcludePatternCount: 0,
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeMSKCluster)
	clusterAttributes := attributesOf(t, cluster)
	if got, want := clusterAttributes["kafka_version"], "3.6.0"; got != want {
		t.Fatalf("kafka_version = %#v, want %q", got, want)
	}
	if got, want := clusterAttributes["cluster_type"], "PROVISIONED"; got != want {
		t.Fatalf("cluster_type = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"server_properties", "broker_logs", "bootstrap_brokers", "scram_secret_arns"} {
		if _, exists := clusterAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; MSK scanner must not store broker payload, log, or secret material", forbidden)
		}
	}

	configuration := resourceByType(t, envelopes, awscloud.ResourceTypeMSKConfiguration)
	configurationAttributes := attributesOf(t, configuration)
	latest, ok := configurationAttributes["latest_revision"].(map[string]any)
	if !ok {
		t.Fatalf("latest_revision = %#v, want map", configurationAttributes["latest_revision"])
	}
	if got, want := latest["revision"], int64(2); got != want {
		t.Fatalf("latest_revision.revision = %#v, want %d", got, want)
	}
	for _, forbidden := range []string{"server_properties", "revision_body", "properties"} {
		if _, exists := configurationAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; MSK scanner must not store configuration revision bodies", forbidden)
		}
	}

	replicator := resourceByType(t, envelopes, awscloud.ResourceTypeMSKReplicator)
	replicatorAttributes := attributesOf(t, replicator)
	infos, ok := replicatorAttributes["replication_info"].([]map[string]any)
	if !ok {
		t.Fatalf("replication_info = %#v, want []map[string]any", replicatorAttributes["replication_info"])
	}
	if len(infos) != 1 {
		t.Fatalf("len(replication_info) = %d, want 1", len(infos))
	}
	for _, forbidden := range []string{"topics_to_replicate", "topics_to_exclude", "consumer_groups_to_replicate", "consumer_groups_to_exclude", "bootstrap_brokers"} {
		if _, exists := infos[0][forbidden]; exists {
			t.Fatalf("replication_info[0].%s persisted; MSK scanner must summarize patterns and never store raw bootstrap brokers", forbidden)
		}
	}

	assertRelationship(t, envelopes, awscloud.RelationshipMSKClusterUsesSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipMSKClusterUsesSecurityGroup)
	assertRelationship(t, envelopes, awscloud.RelationshipMSKClusterUsesKMSKey)
	assertRelationship(t, envelopes, awscloud.RelationshipMSKClusterUsesConfiguration)
	assertRelationship(t, envelopes, awscloud.RelationshipMSKReplicatorUsesIAMRole)
}

func TestScannerSkipsNonARNKMSAndConfigurationRelationships(t *testing.T) {
	client := fakeClient{
		clusters: []Cluster{{
			ARN:   "arn:aws:kafka:us-east-1:123456789012:cluster/serverless-only/1",
			Name:  "serverless-only",
			Type:  "SERVERLESS",
			State: "ACTIVE",
			Serverless: &ServerlessCluster{
				VPCConfigs: []VPCConfig{{
					SubnetIDs:        []string{"subnet-private-1"},
					SecurityGroupIDs: []string{"sg-private"},
				}},
				ClientAuthentication: ClientAuthentication{SASLIAMEnabled: true},
			},
			Provisioned: &ProvisionedCluster{
				EncryptionAtRestKMSKey: "alias/aws/kafka",
				CurrentConfiguration: &ConfigurationReference{
					ARN:      "not-an-arn",
					Revision: 1,
				},
			},
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipMSKClusterUsesKMSKey); got != 0 {
		t.Fatalf("kms relationship count = %d, want 0 when KMS identity is not an ARN", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipMSKClusterUsesConfiguration); got != 0 {
		t.Fatalf("configuration relationship count = %d, want 0 when configuration identity is not an ARN", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipMSKClusterUsesSubnet); got != 1 {
		t.Fatalf("serverless subnet relationship count = %d, want 1", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipMSKClusterUsesSecurityGroup); got != 1 {
		t.Fatalf("serverless security group relationship count = %d, want 1", got)
	}
}

func TestScannerSkipsReplicatorIAMRoleWhenNotARN(t *testing.T) {
	client := fakeClient{
		replicators: []Replicator{{
			ARN:                     "arn:aws:kafka:us-east-1:123456789012:replicator/x/9",
			Name:                    "x",
			ServiceExecutionRoleARN: "msk-replicator-role",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipMSKReplicatorUsesIAMRole); got != 0 {
		t.Fatalf("replicator iam role relationship count = %d, want 0 when role identity is not an ARN", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceMSK,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:msk:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 14, 17, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	clusters       []Cluster
	configurations []Configuration
	replicators    []Replicator
}

func (c fakeClient) ListClusters(context.Context) ([]Cluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListConfigurations(context.Context) ([]Configuration, error) {
	return c.configurations, nil
}

func (c fakeClient) ListReplicators(context.Context) ([]Replicator, error) {
	return c.replicators, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if countRelationships(envelopes, relationshipType) == 0 {
		t.Fatalf("missing relationship_type %q", relationshipType)
	}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}
