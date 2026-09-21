// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mgn

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testApplicationARN  = "arn:aws:mgn:us-east-1:123456789012:application/app-1111111111111111a"
	testSourceServerARN = "arn:aws:mgn:us-east-1:123456789012:source-server/s-2222222222222222b"
	testJobARN          = "arn:aws:mgn:us-east-1:123456789012:job/mgnjob-3333333333333333c"
	testApplicationID   = "app-1111111111111111a"
	testSourceServerID  = "s-2222222222222222b"
	testJobID           = "mgnjob-3333333333333333c"
	testInstanceID      = "i-0abc123def4567890"
	testLaunchTemplate  = "lt-0fedcba9876543210"
)

func TestScannerEmitsMGNMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Applications: []Application{{
			ARN:                testApplicationARN,
			ApplicationID:      testApplicationID,
			Name:               "payments",
			Description:        "payments tier",
			WaveID:             "wave-1",
			HealthStatus:       "HEALTHY",
			ProgressStatus:     "IN_PROGRESS",
			TotalSourceServers: 1,
			CreationTime:       time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:               map[string]string{"Environment": "prod"},
		}},
		SourceServers: []SourceServer{{
			ARN:                     testSourceServerARN,
			SourceServerID:          testSourceServerID,
			ApplicationID:           testApplicationID,
			LifeCycleState:          "READY_FOR_CUTOVER",
			DataReplicationState:    "CONTINUOUS",
			ReplicationType:         "AGENT_BASED",
			RecommendedInstanceType: "m5.large",
			OS:                      "Ubuntu 22.04",
			Hostname:                "web01",
			FQDN:                    "web01.example.internal",
			LaunchedEC2InstanceID:   testInstanceID,
			Tags:                    map[string]string{"Team": "platform"},
			LaunchConfiguration: &LaunchConfiguration{
				SourceServerID:                      testSourceServerID,
				Name:                                "web01-launch",
				LaunchDisposition:                   "STARTED",
				BootMode:                            "LEGACY_BIOS",
				TargetInstanceTypeRightSizingMethod: "BASIC",
				EC2LaunchTemplateID:                 testLaunchTemplate,
				CopyTags:                            true,
			},
		}},
		Jobs: []Job{{
			ARN:                          testJobARN,
			JobID:                        testJobID,
			Type:                         "LAUNCH",
			Status:                       "COMPLETED",
			InitiatedBy:                  "START_CUTOVER",
			CreationTime:                 time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
			ParticipatingSourceServerIDs: []string{testSourceServerID, testSourceServerID},
			Tags:                         map[string]string{"Run": "cutover-1"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Application resource node.
	application := resourceByType(t, envelopes, awscloud.ResourceTypeMGNApplication)
	if got, want := application.Payload["resource_id"], testApplicationID; got != want {
		t.Fatalf("application resource_id = %#v, want %q", got, want)
	}
	if got, want := application.Payload["arn"], testApplicationARN; got != want {
		t.Fatalf("application arn = %#v, want %q", got, want)
	}
	appAttrs := attributesOf(t, application)
	assertAttribute(t, appAttrs, "wave_id", "wave-1")
	assertAttribute(t, appAttrs, "total_source_servers", int64(1))

	// Source server resource node.
	server := resourceByType(t, envelopes, awscloud.ResourceTypeMGNSourceServer)
	if got, want := server.Payload["resource_id"], testSourceServerID; got != want {
		t.Fatalf("source server resource_id = %#v, want %q", got, want)
	}
	if got, want := server.Payload["state"], "READY_FOR_CUTOVER"; got != want {
		t.Fatalf("source server state = %#v, want %q", got, want)
	}
	serverAttrs := attributesOf(t, server)
	assertAttribute(t, serverAttrs, "recommended_instance_type", "m5.large")
	assertAttribute(t, serverAttrs, "data_replication_state", "CONTINUOUS")
	assertAttribute(t, serverAttrs, "launched_ec2_instance_id", testInstanceID)

	// Launch configuration resource node.
	launchConfig := resourceByType(t, envelopes, awscloud.ResourceTypeMGNLaunchConfiguration)
	if got, want := launchConfig.Payload["resource_id"], testSourceServerID+"/launch-configuration"; got != want {
		t.Fatalf("launch config resource_id = %#v, want %q", got, want)
	}
	lcAttrs := attributesOf(t, launchConfig)
	assertAttribute(t, lcAttrs, "ec2_launch_template_id", testLaunchTemplate)
	assertAttribute(t, lcAttrs, "copy_tags", true)

	// Job resource node.
	job := resourceByType(t, envelopes, awscloud.ResourceTypeMGNJob)
	if got, want := job.Payload["resource_id"], testJobID; got != want {
		t.Fatalf("job resource_id = %#v, want %q", got, want)
	}
	if got, want := job.Payload["state"], "COMPLETED"; got != want {
		t.Fatalf("job state = %#v, want %q", got, want)
	}

	// application -> source server edge, keyed by the source server id the node publishes.
	appContains := relationshipByType(t, envelopes, awscloud.RelationshipMGNApplicationContainsSourceServer)
	assertEdgeTarget(t, appContains, awscloud.ResourceTypeMGNSourceServer, testSourceServerID)
	if got, want := appContains.Payload["source_resource_id"], testApplicationID; got != want {
		t.Fatalf("app->server source_resource_id = %#v, want %q", got, want)
	}
	if got := appContains.Payload["target_arn"]; got != "" {
		t.Fatalf("app->server target_arn = %#v, want empty (target keyed by bare source server id)", got)
	}

	// source server -> launched EC2 instance edge, keyed by the bare instance id.
	launched := relationshipByType(t, envelopes, awscloud.RelationshipMGNSourceServerLaunchedEC2Instance)
	assertEdgeTarget(t, launched, ec2InstanceTargetType, testInstanceID)
	if got, want := launched.Payload["source_resource_id"], testSourceServerID; got != want {
		t.Fatalf("server->instance source_resource_id = %#v, want %q", got, want)
	}
	if got := launched.Payload["target_arn"]; got != "" {
		t.Fatalf("server->instance target_arn = %#v, want empty for a bare instance id", got)
	}

	// launch config -> launch template edge, keyed by the bare launch template id.
	usesTemplate := relationshipByType(t, envelopes, awscloud.RelationshipMGNLaunchConfigurationUsesLaunchTemplate)
	assertEdgeTarget(t, usesTemplate, awscloud.ResourceTypeEC2LaunchTemplate, testLaunchTemplate)
	if got, want := usesTemplate.Payload["source_resource_id"], testSourceServerID+"/launch-configuration"; got != want {
		t.Fatalf("config->template source_resource_id = %#v, want %q", got, want)
	}
	if got := usesTemplate.Payload["target_arn"]; got != "" {
		t.Fatalf("config->template target_arn = %#v, want empty for a bare launch template id", got)
	}

	// job -> source server edge, deduplicated, keyed by the source server id the node publishes.
	jobTargets := relationshipByType(t, envelopes, awscloud.RelationshipMGNJobTargetsSourceServer)
	assertEdgeTarget(t, jobTargets, awscloud.ResourceTypeMGNSourceServer, testSourceServerID)
	if got, want := jobTargets.Payload["source_resource_id"], testJobID; got != want {
		t.Fatalf("job->server source_resource_id = %#v, want %q", got, want)
	}
	if count := countRelationships(envelopes, awscloud.RelationshipMGNJobTargetsSourceServer); count != 1 {
		t.Fatalf("job->server edge count = %d, want 1 (duplicate participating ids must dedupe)", count)
	}

	// No replication-agent secret / credential / replication payload leakage.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"replication_credentials", "agent_credentials", "access_key", "secret_access_key",
			"replicator_id", "replicated_disks", "snapshot_id", "private_key",
			"replication_configuration", "staging_credentials", "password",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; MGN scanner must stay metadata-only and never persist replication secrets", forbidden)
			}
		}
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		SourceServers: []SourceServer{{
			ARN:            testSourceServerARN,
			SourceServerID: testSourceServerID,
			LifeCycleState: "STOPPED",
			// No application id, no launched instance, no launch configuration.
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

func TestScannerSkipsLaunchedEdgeForNonInstanceID(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		SourceServers: []SourceServer{{
			ARN:                   testSourceServerARN,
			SourceServerID:        testSourceServerID,
			LaunchedEC2InstanceID: "not-an-instance",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if count := countRelationships(envelopes, awscloud.RelationshipMGNSourceServerLaunchedEC2Instance); count != 0 {
		t.Fatalf("launched edge count = %d, want 0 for a non-instance id", count)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	server := SourceServer{
		ARN:                   testSourceServerARN,
		SourceServerID:        testSourceServerID,
		ApplicationID:         testApplicationID,
		LaunchedEC2InstanceID: testInstanceID,
		LaunchConfiguration: &LaunchConfiguration{
			SourceServerID:      testSourceServerID,
			EC2LaunchTemplateID: testLaunchTemplate,
		},
	}
	job := Job{ARN: testJobARN, JobID: testJobID, ParticipatingSourceServerIDs: []string{testSourceServerID}}

	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		applicationContainsSourceServerRelationship(boundary, server),
		sourceServerLaunchedEC2Relationship(boundary, server),
		launchConfigurationUsesLaunchTemplateRelationship(boundary, server),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	observations = append(observations, jobTargetsSourceServerRelationships(boundary, job)...)
	relguard.AssertObservations(t, observations...)
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
		SourceServers: []SourceServer{{ARN: testSourceServerARN, SourceServerID: testSourceServerID}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "MGN DescribeJobs throttled after SDK retries; job metadata omitted for this scan",
			SourceRecordID: "mgn_jobs_throttled",
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceMGN,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:mgn:1",
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

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
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
