// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mq

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsBrokerMetadataOnlyWithRelationships(t *testing.T) {
	brokerARN := "arn:aws:mq:us-east-1:123456789012:broker:orders:b-1111"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/22222222-3333-4444-5555-666666666666"
	configARN := "arn:aws:mq:us-east-1:123456789012:configuration:c-2222"
	client := fakeClient{brokers: []Broker{{
		ARN:                     brokerARN,
		ID:                      "b-1111",
		Name:                    "orders",
		EngineType:              "ACTIVEMQ",
		EngineVersion:           "5.18.4",
		DeploymentMode:          "ACTIVE_STANDBY_MULTI_AZ",
		HostInstanceType:        "mq.m5.large",
		State:                   "RUNNING",
		StorageType:             "EBS",
		AuthStrategy:            "SIMPLE",
		PubliclyAccessible:      false,
		AutoMinorVersionUpgrade: true,
		Created:                 time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC),
		Tags:                    map[string]string{"Environment": "prod"},
		SubnetIDs:               []string{"subnet-aaa", "subnet-bbb"},
		SecurityGroupIDs:        []string{"sg-mq"},
		Encryption:              Encryption{UseAWSOwnedKey: false, KMSKeyID: kmsARN},
		Configuration:           &ConfigurationReference{ID: "c-2222", Revision: 3},
		Logs: Logs{
			GeneralEnabled:  true,
			GeneralLogGroup: "/aws/amazonmq/broker/b-1111/general",
			AuditEnabled:    true,
			AuditLogGroup:   "/aws/amazonmq/broker/b-1111/audit",
		},
		Usernames: []string{"admin", "publisher"},
	}}, configurations: []Configuration{{
		ARN:  configARN,
		ID:   "c-2222",
		Name: "orders-config",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	broker := resourceByType(t, envelopes, awscloud.ResourceTypeMQBroker)
	attributes := attributesOf(t, broker)
	if got, want := attributes["engine_type"], "ACTIVEMQ"; got != want {
		t.Fatalf("engine_type = %#v, want %q", got, want)
	}
	if got, want := attributes["engine_version"], "5.18.4"; got != want {
		t.Fatalf("engine_version = %#v, want %q", got, want)
	}
	if got, want := attributes["deployment_mode"], "ACTIVE_STANDBY_MULTI_AZ"; got != want {
		t.Fatalf("deployment_mode = %#v, want %q", got, want)
	}
	if got, want := attributes["host_instance_type"], "mq.m5.large"; got != want {
		t.Fatalf("host_instance_type = %#v, want %q", got, want)
	}
	if got, want := broker.Payload["state"], "RUNNING"; got != want {
		t.Fatalf("state = %#v, want %q", got, want)
	}
	encryption, ok := attributes["encryption"].(map[string]any)
	if !ok {
		t.Fatalf("encryption = %#v, want map", attributes["encryption"])
	}
	if got, want := encryption["kms_key_id"], kmsARN; got != want {
		t.Fatalf("encryption.kms_key_id = %#v, want %q", got, want)
	}
	if got, want := encryption["use_aws_owned_key"], false; got != want {
		t.Fatalf("encryption.use_aws_owned_key = %#v, want %v", got, want)
	}
	if got, want := attributes["usernames"], []string{"admin", "publisher"}; !equalStringSlices(got, want) {
		t.Fatalf("usernames = %#v, want %#v", got, want)
	}
	assertNoPasswordMaterial(t, broker.Payload)

	assertRelationship(t, envelopes, awscloud.RelationshipMQBrokerUsesSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipMQBrokerUsesSecurityGroup)
	assertRelationship(t, envelopes, awscloud.RelationshipMQBrokerUsesKMSKey)
	assertRelationship(t, envelopes, awscloud.RelationshipMQBrokerUsesConfiguration)
	assertRelationship(t, envelopes, awscloud.RelationshipMQBrokerLogsToCloudWatchLogGroup)

	// The configuration and log group edges must target the ARN form the
	// owning scanners use as their resource ResourceID, or the edges will not
	// join in the reducer.
	assertRelationshipTargetARN(t, envelopes, awscloud.RelationshipMQBrokerUsesConfiguration, configARN)
	assertRelationshipTargetARN(t, envelopes, awscloud.RelationshipMQBrokerLogsToCloudWatchLogGroup,
		"arn:aws:logs:us-east-1:123456789012:log-group:/aws/amazonmq/broker/b-1111/general")
}

func TestScannerEmitsConfigurationIdentityNotBody(t *testing.T) {
	configARN := "arn:aws:mq:us-east-1:123456789012:configuration:c-2222"
	client := fakeClient{configurations: []Configuration{{
		ARN:           configARN,
		ID:            "c-2222",
		Name:          "orders-config",
		Description:   "orders broker configuration",
		EngineType:    "ACTIVEMQ",
		EngineVersion: "5.18.4",
		AuthStrategy:  "SIMPLE",
		Created:       time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Tags:          map[string]string{"Owner": "platform"},
		LatestRevision: ConfigurationRevisionSummary{
			Revision:    3,
			Created:     time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC),
			Description: "tighten ACLs",
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	configuration := resourceByType(t, envelopes, awscloud.ResourceTypeMQConfiguration)
	attributes := attributesOf(t, configuration)
	revision, ok := attributes["latest_revision"].(map[string]any)
	if !ok {
		t.Fatalf("latest_revision = %#v, want map", attributes["latest_revision"])
	}
	if got, want := revision["revision"], int32(3); got != want {
		t.Fatalf("latest_revision.revision = %#v, want %v", got, want)
	}
	if got, want := attributes["engine_type"], "ACTIVEMQ"; got != want {
		t.Fatalf("engine_type = %#v, want %q", got, want)
	}
	assertNoConfigurationBody(t, configuration.Payload)
}

func TestScannerEmitsRabbitMQBrokerWithoutKMSKeyRelationshipForAWSOwnedKey(t *testing.T) {
	brokerARN := "arn:aws:mq:us-east-1:123456789012:broker:events:b-3333"
	client := fakeClient{brokers: []Broker{{
		ARN:            brokerARN,
		ID:             "b-3333",
		Name:           "events",
		EngineType:     "RABBITMQ",
		EngineVersion:  "3.13",
		DeploymentMode: "SINGLE_INSTANCE",
		State:          "RUNNING",
		Encryption:     Encryption{UseAWSOwnedKey: true},
		SubnetIDs:      []string{"subnet-private-1"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	broker := resourceByType(t, envelopes, awscloud.ResourceTypeMQBroker)
	if got, want := attributesOf(t, broker)["engine_type"], "RABBITMQ"; got != want {
		t.Fatalf("engine_type = %#v, want %q", got, want)
	}
	assertRelationship(t, envelopes, awscloud.RelationshipMQBrokerUsesSubnet)
	if relationshipPresent(envelopes, awscloud.RelationshipMQBrokerUsesKMSKey) {
		t.Fatalf("AWS-owned-key broker emitted a KMS key relationship; only customer-managed keys should produce one")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceMSK

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceMQ,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:mq:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	brokers        []Broker
	configurations []Configuration
}

func (c fakeClient) ListBrokers(context.Context) ([]Broker, error) {
	return c.brokers, nil
}

func (c fakeClient) ListConfigurations(context.Context) ([]Configuration, error) {
	return c.configurations, nil
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if !relationshipPresent(envelopes, relationshipType) {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
}

func relationshipPresent(envelopes []facts.Envelope, relationshipType string) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return true
		}
	}
	return false
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// assertNoPasswordMaterial fails if any persisted broker field carries a
// password key. Amazon MQ broker User resources have a Password field; the
// scanner records usernames only and must never carry password material.
func assertNoPasswordMaterial(t *testing.T, payload map[string]any) {
	t.Helper()
	attributes, _ := payload["attributes"].(map[string]any)
	for _, key := range []string{"password", "passwords", "user_password", "user_passwords"} {
		if _, exists := attributes[key]; exists {
			t.Fatalf("attribute %q persisted; MQ scanner must never store broker user passwords", key)
		}
		if _, exists := payload[key]; exists {
			t.Fatalf("payload field %q persisted; MQ scanner must never store broker user passwords", key)
		}
	}
}

// assertNoConfigurationBody fails if a persisted configuration field carries the
// configuration XML body. The body can contain inline credentials and ACL
// rules, so the scanner persists identity and revision metadata only.
func assertNoConfigurationBody(t *testing.T, payload map[string]any) {
	t.Helper()
	attributes, _ := payload["attributes"].(map[string]any)
	for _, key := range []string{"data", "body", "xml", "configuration_body", "configuration_xml"} {
		if _, exists := attributes[key]; exists {
			t.Fatalf("attribute %q persisted; MQ scanner must never store configuration XML body", key)
		}
		if _, exists := payload[key]; exists {
			t.Fatalf("payload field %q persisted; MQ scanner must never store configuration XML body", key)
		}
	}
}

func equalStringSlices(got any, want []string) bool {
	values, ok := got.([]string)
	if !ok || len(values) != len(want) {
		return false
	}
	for i := range values {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}
