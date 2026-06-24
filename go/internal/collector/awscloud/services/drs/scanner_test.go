// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drs

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testSourceServerID  = "s-1234567890abcdef0"
	testSourceServerARN = "arn:aws:drs:us-east-1:123456789012:source-server/s-1234567890abcdef0"
	testRecoveryInstID2 = "i-0fedcba9876543210"
	testRecoveryInstARN = "arn:aws:drs:us-east-1:123456789012:recovery-instance/i-0fedcba9876543210"
	testEC2InstanceID   = "i-0123456789abcdef0"
	testTemplateID      = "rct-0123456789abcdef0"
	testTemplateARN     = "arn:aws:drs:us-east-1:123456789012:replication-configuration-template/rct-0123456789abcdef0"
)

func TestScannerEmitsDRSMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		SourceServers: []SourceServer{{
			SourceServerID:          testSourceServerID,
			ARN:                     testSourceServerARN,
			Hostname:                "web-01",
			FQDN:                    "web-01.example.com",
			OperatingSystem:         "Ubuntu 22.04",
			RecoveryInstanceID:      testRecoveryInstID2,
			DataReplicationState:    "CONTINUOUS",
			ReplicationDirection:    "FAILOVER",
			LastLaunchResult:        "SUCCEEDED",
			RecommendedInstanceType: "m5.large",
			OriginAccountID:         "123456789012",
			OriginRegion:            "us-west-2",
			Tags:                    map[string]string{"Environment": "prod"},
		}},
		RecoveryInstances: []RecoveryInstance{{
			RecoveryInstanceID: testRecoveryInstID2,
			ARN:                testRecoveryInstARN,
			EC2InstanceID:      testEC2InstanceID,
			EC2InstanceState:   "RUNNING",
			SourceServerID:     testSourceServerID,
			IsDrill:            true,
			OriginEnvironment:  "ON_PREMISES",
			Tags:               map[string]string{"Team": "dr"},
		}},
		ReplicationConfigurationTemplates: []ReplicationConfigurationTemplate{{
			TemplateID:                    testTemplateID,
			ARN:                           testTemplateARN,
			EBSEncryption:                 "DEFAULT",
			StagingAreaSubnetID:           "subnet-0abc1234",
			ReplicationServerInstanceType: "t3.small",
			UseDedicatedReplicationServer: false,
			AssociateDefaultSecurityGroup: true,
			Tags:                          map[string]string{"Owner": "platform"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Source server resource node.
	server := resourceByType(t, envelopes, awscloud.ResourceTypeDRSSourceServer)
	if got, want := server.Payload["resource_id"], testSourceServerID; got != want {
		t.Fatalf("source server resource_id = %#v, want %q", got, want)
	}
	if got, want := server.Payload["arn"], testSourceServerARN; got != want {
		t.Fatalf("source server arn = %#v, want %q", got, want)
	}
	serverAttrs := attributesOf(t, server)
	assertAttribute(t, serverAttrs, "hostname", "web-01")
	assertAttribute(t, serverAttrs, "operating_system", "Ubuntu 22.04")
	assertAttribute(t, serverAttrs, "recovery_instance_id", testRecoveryInstID2)
	assertAttribute(t, serverAttrs, "recommended_instance_type", "m5.large")

	// Recovery instance resource node.
	instance := resourceByType(t, envelopes, awscloud.ResourceTypeDRSRecoveryInstance)
	if got, want := instance.Payload["resource_id"], testRecoveryInstID2; got != want {
		t.Fatalf("recovery instance resource_id = %#v, want %q", got, want)
	}
	instAttrs := attributesOf(t, instance)
	assertAttribute(t, instAttrs, "ec2_instance_id", testEC2InstanceID)
	assertAttribute(t, instAttrs, "is_drill", true)
	assertAttribute(t, instAttrs, "source_server_id", testSourceServerID)

	// Replication configuration template resource node.
	template := resourceByType(t, envelopes, awscloud.ResourceTypeDRSReplicationConfigurationTemplate)
	if got, want := template.Payload["resource_id"], testTemplateID; got != want {
		t.Fatalf("template resource_id = %#v, want %q", got, want)
	}
	templateAttrs := attributesOf(t, template)
	assertAttribute(t, templateAttrs, "ebs_encryption", "DEFAULT")
	assertAttribute(t, templateAttrs, "staging_area_subnet_id", "subnet-0abc1234")
	assertAttribute(t, templateAttrs, "associate_default_security_group", true)

	// source server -> recovery instance edge, keyed by the recovery instance id
	// the recovery instance node publishes.
	recovers := relationshipByType(t, envelopes, awscloud.RelationshipDRSSourceServerRecoversToInstance)
	assertEdgeTarget(t, recovers, awscloud.ResourceTypeDRSRecoveryInstance, testRecoveryInstID2)
	if got, want := recovers.Payload["source_resource_id"], testSourceServerID; got != want {
		t.Fatalf("source->recovery source_resource_id = %#v, want %q", got, want)
	}

	// recovery instance -> EC2 instance edge, keyed by the bare i- id, target_arn empty.
	runsOn := relationshipByType(t, envelopes, awscloud.RelationshipDRSRecoveryInstanceRunsOnEC2Instance)
	assertEdgeTarget(t, runsOn, "aws_ec2_instance", testEC2InstanceID)
	if got := runsOn.Payload["target_arn"]; got != "" {
		t.Fatalf("recovery->ec2 target_arn = %#v, want empty (forward reference)", got)
	}
	if got, want := runsOn.Payload["source_resource_id"], testRecoveryInstID2; got != want {
		t.Fatalf("recovery->ec2 source_resource_id = %#v, want %q", got, want)
	}

	// No agent secret / replicated data / snapshot leakage anywhere in payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"agent_secret", "secret", "password", "token", "private_key",
			"replicated_disks", "disk_data", "snapshot_data", "snapshots",
			"replicated_storage_bytes", "credentials",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; DRS scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		SourceServers: []SourceServer{{
			SourceServerID: testSourceServerID,
			ARN:            testSourceServerARN,
			// No recovery instance id: no source->recovery edge.
		}},
		RecoveryInstances: []RecoveryInstance{{
			RecoveryInstanceID: testRecoveryInstID2,
			ARN:                testRecoveryInstARN,
			// No EC2 instance id: no recovery->ec2 edge.
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	server := SourceServer{
		SourceServerID:     testSourceServerID,
		ARN:                testSourceServerARN,
		RecoveryInstanceID: testRecoveryInstID2,
	}
	instance := RecoveryInstance{
		RecoveryInstanceID: testRecoveryInstID2,
		ARN:                testRecoveryInstARN,
		EC2InstanceID:      testEC2InstanceID,
	}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		sourceServerRecoversToInstanceRelationship(boundary, server),
		recoveryInstanceRunsOnEC2InstanceRelationship(boundary, instance),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerHandlesEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() on empty account returned %d envelopes, want 0", len(envelopes))
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		SourceServers: []SourceServer{{SourceServerID: testSourceServerID, ARN: testSourceServerARN}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "DRS DescribeRecoveryInstances throttled after SDK retries; recovery instance metadata omitted for this scan",
			SourceRecordID: "drs_recovery_instances_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceDRS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:drs:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
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

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
