// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsDataSyncMetadataResourcesAndRelationships(t *testing.T) {
	taskARN := "arn:aws:datasync:us-east-1:123456789012:task/task-01234567890abcdef"
	sourceLocationARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-0source0000000000"
	destLocationARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-0dest00000000000000"
	agentARN := "arn:aws:datasync:us-east-1:123456789012:agent/agent-0123456789abcdef0"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/datasync"
	bucketAccessRole := "arn:aws:iam::123456789012:role/datasync-s3-access"
	efsAccessRole := "arn:aws:iam::123456789012:role/datasync-efs-access"

	client := fakeClient{
		tasks: []Task{{
			ARN:                    taskARN,
			Name:                   "nightly-archive",
			Status:                 "AVAILABLE",
			SourceLocationARN:      sourceLocationARN,
			DestinationLocationARN: destLocationARN,
			CloudWatchLogGroupARN:  logGroupARN + ":*",
			ScheduleExpression:     "cron(0 2 * * ? *)",
			ScheduleStatus:         "ENABLED",
			TaskMode:               "BASIC",
			CreationTime:           time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
		}},
		locations: []Location{
			{
				ARN:          sourceLocationARN,
				Type:         "S3",
				URI:          "s3://archive-source/incoming/",
				S3BucketName: "archive-source",
				IAMRoleARN:   bucketAccessRole,
				CreationTime: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
			},
			{
				ARN:             destLocationARN,
				Type:            "EFS",
				URI:             "efs://us-east-1.fs-0123456789abcdef0/backups/",
				EFSFileSystemID: "fs-0123456789abcdef0",
				IAMRoleARN:      efsAccessRole,
				CreationTime:    time.Date(2026, 4, 2, 8, 0, 0, 0, time.UTC),
			},
		},
		agents: []Agent{{
			ARN:             agentARN,
			Name:            "on-prem-agent",
			Status:          "ONLINE",
			EndpointType:    "PUBLIC",
			PlatformVersion: "1.2.3",
			CreationTime:    time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC),
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	task := resourceByType(t, envelopes, awscloud.ResourceTypeDataSyncTask)
	if got, want := task.Payload["resource_id"], taskARN; got != want {
		t.Fatalf("task resource_id = %#v, want %q", got, want)
	}
	if got, want := task.Payload["name"], "nightly-archive"; got != want {
		t.Fatalf("task name = %#v, want %q", got, want)
	}
	taskAttributes := attributesOf(t, task)
	if got, want := taskAttributes["schedule_expression"], "cron(0 2 * * ? *)"; got != want {
		t.Fatalf("task schedule_expression = %#v, want %q", got, want)
	}
	if got, want := taskAttributes["cloudwatch_log_group_arn"], logGroupARN; got != want {
		t.Fatalf("task cloudwatch_log_group_arn = %#v, want %q (trailing :* must be trimmed)", got, want)
	}

	source := resourceByID(t, envelopes, awscloud.ResourceTypeDataSyncLocation, sourceLocationARN)
	sourceAttributes := attributesOf(t, source)
	if got, want := sourceAttributes["location_type"], "S3"; got != want {
		t.Fatalf("source location_type = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"access_key", "secret_access_key", "server_certificate", "password"} {
		if _, exists := sourceAttributes[forbidden]; exists {
			t.Fatalf("location %s attribute persisted; metadata-only contract forbids storage credentials", forbidden)
		}
	}

	agent := resourceByType(t, envelopes, awscloud.ResourceTypeDataSyncAgent)
	if got, want := agent.Payload["resource_id"], agentARN; got != want {
		t.Fatalf("agent resource_id = %#v, want %q", got, want)
	}
	if got, want := agent.Payload["state"], "ONLINE"; got != want {
		t.Fatalf("agent state = %#v, want %q", got, want)
	}

	sourceEdge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncTaskSourceLocation)
	if got, want := sourceEdge.Payload["source_resource_id"], taskARN; got != want {
		t.Fatalf("task->source source_resource_id = %#v, want %q", got, want)
	}
	if got, want := sourceEdge.Payload["target_resource_id"], sourceLocationARN; got != want {
		t.Fatalf("task->source target_resource_id = %#v, want %q", got, want)
	}
	if got, want := sourceEdge.Payload["target_type"], awscloud.ResourceTypeDataSyncLocation; got != want {
		t.Fatalf("task->source target_type = %#v, want %q", got, want)
	}

	destEdge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncTaskDestinationLocation)
	if got, want := destEdge.Payload["target_resource_id"], destLocationARN; got != want {
		t.Fatalf("task->dest target_resource_id = %#v, want %q", got, want)
	}
	if got, want := destEdge.Payload["target_type"], awscloud.ResourceTypeDataSyncLocation; got != want {
		t.Fatalf("task->dest target_type = %#v, want %q", got, want)
	}

	logEdge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncTaskLogsToCloudWatch)
	if got, want := logEdge.Payload["target_resource_id"], logGroupARN; got != want {
		t.Fatalf("task->log target_resource_id = %#v, want %q (must match trimmed log-group ARN)", got, want)
	}
	if got, want := logEdge.Payload["target_type"], awscloud.ResourceTypeCloudWatchLogsLogGroup; got != want {
		t.Fatalf("task->log target_type = %#v, want %q", got, want)
	}

	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncLocationTargetsS3Bucket)
	if got, want := s3Edge.Payload["source_resource_id"], sourceLocationARN; got != want {
		t.Fatalf("location->s3 source_resource_id = %#v, want %q", got, want)
	}
	if got, want := s3Edge.Payload["target_resource_id"], "arn:aws:s3:::archive-source"; got != want {
		t.Fatalf("location->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := s3Edge.Payload["target_arn"], "arn:aws:s3:::archive-source"; got != want {
		t.Fatalf("location->s3 target_arn = %#v, want %q", got, want)
	}
	if got, want := s3Edge.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("location->s3 target_type = %#v, want %q", got, want)
	}

	efsEdge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncLocationTargetsEFSFileSystem)
	wantEFSARN := "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-0123456789abcdef0"
	if got, want := efsEdge.Payload["target_resource_id"], wantEFSARN; got != want {
		t.Fatalf("location->efs target_resource_id = %#v, want %q", got, want)
	}
	if got, want := efsEdge.Payload["target_type"], awscloud.ResourceTypeEFSFileSystem; got != want {
		t.Fatalf("location->efs target_type = %#v, want %q", got, want)
	}

	roleEdges := relationshipsByType(envelopes, awscloud.RelationshipDataSyncLocationUsesIAMRole)
	if len(roleEdges) != 2 {
		t.Fatalf("location->role relationship count = %d, want 2", len(roleEdges))
	}
	for _, edge := range roleEdges {
		if got, want := edge.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
			t.Fatalf("location->role target_type = %#v, want %q", got, want)
		}
		arn, _ := edge.Payload["target_arn"].(string)
		if arn != bucketAccessRole && arn != efsAccessRole {
			t.Fatalf("location->role target_arn = %q, want one of the access role ARNs", arn)
		}
	}
}

func TestScannerEmitsFSxFileSystemRelationshipFromReportedARN(t *testing.T) {
	locationARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-0fsx00000000000000"
	fsARN := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-0ontap0000000000000"
	client := fakeClient{locations: []Location{{
		ARN:              locationARN,
		Type:             "FSX_ONTAP",
		URI:              "fsxn://us-east-1.fs-0ontap0000000000000/vol1/",
		FSxFileSystemARN: fsARN,
		FSxFileSystemID:  "fs-0ontap0000000000000",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncLocationTargetsFSxFileSystem)
	if got, want := edge.Payload["target_resource_id"], fsARN; got != want {
		t.Fatalf("location->fsx target_resource_id = %#v, want %q (API ARN used directly)", got, want)
	}
	if got, want := edge.Payload["target_arn"], fsARN; got != want {
		t.Fatalf("location->fsx target_arn = %#v, want %q", got, want)
	}
	if got, want := edge.Payload["target_type"], awscloud.ResourceTypeFSxFileSystem; got != want {
		t.Fatalf("location->fsx target_type = %#v, want %q", got, want)
	}
}

func TestScannerSynthesizesFSxFileSystemARNFromIDWhenAPIOmitsARN(t *testing.T) {
	locationARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-0fsxl0000000000000"
	client := fakeClient{locations: []Location{{
		ARN:             locationARN,
		Type:            "FSX_LUSTRE",
		URI:             "fsxl://us-east-1.fs-0lustre000000000000/",
		FSxFileSystemID: "fs-0lustre000000000000",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipDataSyncLocationTargetsFSxFileSystem)
	want := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-0lustre000000000000"
	if got := edge.Payload["target_resource_id"]; got != want {
		t.Fatalf("location->fsx target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerOmitsLocationStorageEdgesWithoutBackingIdentity(t *testing.T) {
	locationARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-0nfs00000000000000"
	client := fakeClient{locations: []Location{{
		ARN:  locationARN,
		Type: "NFS",
		URI:  "nfs://198.51.100.10/exports/",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipDataSyncLocationTargetsS3Bucket,
		awscloud.RelationshipDataSyncLocationTargetsEFSFileSystem,
		awscloud.RelationshipDataSyncLocationTargetsFSxFileSystem,
		awscloud.RelationshipDataSyncLocationUsesIAMRole,
	} {
		if got := len(relationshipsByType(envelopes, relationshipType)); got != 0 {
			t.Fatalf("%s relationship count = %d, want 0 for an NFS location with no AWS backing identity", relationshipType, got)
		}
	}
}

func TestScannerOmitsTaskEdgesWhenARNsAreNotARN(t *testing.T) {
	client := fakeClient{tasks: []Task{{
		ARN:                    "arn:aws:datasync:us-east-1:123456789012:task/task-bare",
		SourceLocationARN:      "loc-not-an-arn",
		DestinationLocationARN: "",
		CloudWatchLogGroupARN:  "",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipDataSyncTaskSourceLocation,
		awscloud.RelationshipDataSyncTaskDestinationLocation,
		awscloud.RelationshipDataSyncTaskLogsToCloudWatch,
	} {
		if got := len(relationshipsByType(envelopes, relationshipType)); got != 0 {
			t.Fatalf("%s relationship count = %d, want 0 for non-ARN identities", relationshipType, got)
		}
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceDataSync,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:datasync:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	tasks     []Task
	locations []Location
	agents    []Agent
}

func (c fakeClient) ListTasks(context.Context) ([]Task, error)         { return c.tasks, nil }
func (c fakeClient) ListLocations(context.Context) ([]Location, error) { return c.locations, nil }
func (c fakeClient) ListAgents(context.Context) ([]Agent, error)       { return c.agents, nil }

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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceType, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		gotType, _ := envelope.Payload["resource_type"].(string)
		gotID, _ := envelope.Payload["resource_id"].(string)
		if gotType == resourceType && gotID == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q with resource_id %q in %d envelopes", resourceType, resourceID, len(envelopes))
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	edges := relationshipsByType(envelopes, relationshipType)
	if len(edges) == 0 {
		t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	}
	return edges[0]
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var matches []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			matches = append(matches, envelope)
		}
	}
	return matches
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
