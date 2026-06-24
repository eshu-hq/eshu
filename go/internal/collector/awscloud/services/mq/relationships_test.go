// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mq

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBrokerLogsRelationshipTargetsLogGroupARN proves the broker→CloudWatch Logs
// edge joins the cloudwatchlogs scanner's emitted resource, whose ResourceID is
// the non-wildcard log group ARN. The edge must therefore set both
// TargetResourceID and TargetARN to the synthesized ARN, not the bare name.
func TestBrokerLogsRelationshipTargetsLogGroupARN(t *testing.T) {
	boundary := testBoundary()
	broker := Broker{
		ARN:  "arn:aws:mq:us-east-1:123456789012:broker:orders:b-1111",
		ID:   "b-1111",
		Name: "orders",
		Logs: Logs{
			GeneralEnabled:  true,
			GeneralLogGroup: "/aws/amazonmq/broker/b-1111/general",
			AuditEnabled:    true,
			AuditLogGroup:   "/aws/amazonmq/broker/b-1111/audit",
		},
	}

	wantGeneral := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/amazonmq/broker/b-1111/general"
	wantAudit := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/amazonmq/broker/b-1111/audit"

	observations := brokerRelationships(boundary, broker, nil)
	general := logRelationshipByKind(t, observations, "general")
	if got := general.TargetResourceID; got != wantGeneral {
		t.Fatalf("general TargetResourceID = %q, want %q", got, wantGeneral)
	}
	if got := general.TargetARN; got != wantGeneral {
		t.Fatalf("general TargetARN = %q, want %q", got, wantGeneral)
	}
	audit := logRelationshipByKind(t, observations, "audit")
	if got := audit.TargetResourceID; got != wantAudit {
		t.Fatalf("audit TargetResourceID = %q, want %q", got, wantAudit)
	}
	if got := audit.TargetARN; got != wantAudit {
		t.Fatalf("audit TargetARN = %q, want %q", got, wantAudit)
	}
}

// TestBrokerConfigurationRelationshipTargetsConfigurationARN proves the
// broker→configuration edge joins the emitted aws_mq_configuration resource,
// whose ResourceID is the configuration ARN. The edge must resolve the broker's
// configuration ID to its ARN via the ID→ARN map built from ListConfigurations.
func TestBrokerConfigurationRelationshipTargetsConfigurationARN(t *testing.T) {
	boundary := testBoundary()
	configARN := "arn:aws:mq:us-east-1:123456789012:configuration:c-2222"
	broker := Broker{
		ARN:           "arn:aws:mq:us-east-1:123456789012:broker:orders:b-1111",
		ID:            "b-1111",
		Name:          "orders",
		Configuration: &ConfigurationReference{ID: "c-2222", Revision: 3},
	}

	observations := brokerRelationships(boundary, broker, map[string]string{"c-2222": configARN})
	config := relationshipByTypeObs(t, observations, awscloud.RelationshipMQBrokerUsesConfiguration)
	if got := config.TargetResourceID; got != configARN {
		t.Fatalf("configuration TargetResourceID = %q, want %q", got, configARN)
	}
	if got := config.TargetARN; got != configARN {
		t.Fatalf("configuration TargetARN = %q, want %q", got, configARN)
	}
}

// TestBrokerConfigurationRelationshipFallsBackToIDWhenARNUnknown proves an edge
// is still emitted (targeting the broker-reported configuration ID, which the
// configuration resource carries as a correlation anchor) when ListConfigurations
// did not return the referenced configuration, e.g. a shared or cross-account
// configuration. The edge must not be dropped.
func TestBrokerConfigurationRelationshipFallsBackToIDWhenARNUnknown(t *testing.T) {
	boundary := testBoundary()
	broker := Broker{
		ARN:           "arn:aws:mq:us-east-1:123456789012:broker:orders:b-1111",
		ID:            "b-1111",
		Name:          "orders",
		Configuration: &ConfigurationReference{ID: "c-9999", Revision: 1},
	}

	observations := brokerRelationships(boundary, broker, map[string]string{"c-2222": "arn:aws:mq:us-east-1:123456789012:configuration:c-2222"})
	config := relationshipByTypeObs(t, observations, awscloud.RelationshipMQBrokerUsesConfiguration)
	if got, want := config.TargetResourceID, "c-9999"; got != want {
		t.Fatalf("configuration TargetResourceID = %q, want %q", got, want)
	}
	if got := config.TargetARN; got != "" {
		t.Fatalf("configuration TargetARN = %q, want empty when ARN unknown", got)
	}
}

// TestBrokerLogsRelationshipTargetsLogGroupARNInNonAWSPartition proves the
// synthesized CloudWatch Logs ARN carries the broker's own partition. The
// cloudwatchlogs scanner emits the log group ResourceID using the real ARN
// (for example arn:aws-us-gov:logs:... in GovCloud), so hard-coding arn:aws:
// would make the broker logging edge never join the log group resource in the
// aws-us-gov and aws-cn partitions.
func TestBrokerLogsRelationshipTargetsLogGroupARNInNonAWSPartition(t *testing.T) {
	boundary := testBoundary()
	broker := Broker{
		ARN:  "arn:aws-us-gov:mq:us-gov-west-1:123456789012:broker:orders:b-1111",
		ID:   "b-1111",
		Name: "orders",
		Logs: Logs{
			GeneralEnabled:  true,
			GeneralLogGroup: "/aws/amazonmq/broker/b-1111/general",
		},
	}

	want := "arn:aws-us-gov:logs:us-east-1:123456789012:log-group:/aws/amazonmq/broker/b-1111/general"

	observations := brokerRelationships(boundary, broker, nil)
	general := logRelationshipByKind(t, observations, "general")
	if got := general.TargetResourceID; got != want {
		t.Fatalf("general TargetResourceID = %q, want %q", got, want)
	}
	if got := general.TargetARN; got != want {
		t.Fatalf("general TargetARN = %q, want %q", got, want)
	}
}

func logRelationshipByKind(t *testing.T, observations []awscloud.RelationshipObservation, kind string) awscloud.RelationshipObservation {
	t.Helper()
	for _, observation := range observations {
		if observation.RelationshipType != awscloud.RelationshipMQBrokerLogsToCloudWatchLogGroup {
			continue
		}
		if got, _ := observation.Attributes["log_kind"].(string); got == kind {
			return observation
		}
	}
	t.Fatalf("missing %q log group relationship in %#v", kind, observations)
	return awscloud.RelationshipObservation{}
}

func relationshipByTypeObs(t *testing.T, observations []awscloud.RelationshipObservation, relationshipType string) awscloud.RelationshipObservation {
	t.Helper()
	for _, observation := range observations {
		if observation.RelationshipType == relationshipType {
			return observation
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, observations)
	return awscloud.RelationshipObservation{}
}

// assertRelationshipTargetARN locates the relationship of relationshipType in the
// emitted envelopes and asserts both target_resource_id and target_arn equal want.
func assertRelationshipTargetARN(t *testing.T, envelopes []facts.Envelope, relationshipType, want string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got != want {
			t.Fatalf("%s target_resource_id = %q, want %q", relationshipType, got, want)
		}
		if got, _ := envelope.Payload["target_arn"].(string); got != want {
			t.Fatalf("%s target_arn = %q, want %q", relationshipType, got, want)
		}
		return
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}
