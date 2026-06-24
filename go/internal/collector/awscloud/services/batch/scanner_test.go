// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package batch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/batch"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceBatch,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
	}
}

func newTestKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

// fakeClient is a metadata-only Batch read surface for scanner tests.
type fakeClient struct {
	computeEnvironments []batch.ComputeEnvironment
	jobQueues           []batch.JobQueue
	jobDefinitions      []batch.JobDefinition
	schedulingPolicies  []batch.SchedulingPolicy
	jobsByQueue         map[string][]batch.Job
	err                 error
}

func (c fakeClient) ListComputeEnvironments(context.Context) ([]batch.ComputeEnvironment, error) {
	return c.computeEnvironments, c.err
}

func (c fakeClient) ListJobQueues(context.Context) ([]batch.JobQueue, error) {
	return c.jobQueues, c.err
}

func (c fakeClient) ListJobDefinitions(context.Context) ([]batch.JobDefinition, error) {
	return c.jobDefinitions, c.err
}

func (c fakeClient) ListSchedulingPolicies(context.Context) ([]batch.SchedulingPolicy, error) {
	return c.schedulingPolicies, c.err
}

func (c fakeClient) ListRecentJobs(_ context.Context, queue batch.JobQueue) ([]batch.Job, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.jobsByQueue[queue.ARN], nil
}

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			out = append(out, envelope.Payload)
		}
	}
	return out
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == relationshipType {
			out = append(out, envelope.Payload)
		}
	}
	return out
}

func TestScannerRequiresClient(t *testing.T) {
	scanner := batch.Scanner{RedactionKey: newTestKey(t)}
	if _, err := scanner.Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	scanner := batch.Scanner{Client: fakeClient{}}
	_, err := scanner.Scan(context.Background(), testBoundary())
	if err == nil || !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scan() error = %v, want redaction key required", err)
	}
}

func TestScannerRejectsForeignServiceKind(t *testing.T) {
	scanner := batch.Scanner{Client: fakeClient{}, RedactionKey: newTestKey(t)}
	boundary := testBoundary()
	boundary.ServiceKind = "ecs"
	if _, err := scanner.Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerSurfacesClientError(t *testing.T) {
	wrapped := errors.New("boom")
	scanner := batch.Scanner{Client: fakeClient{err: wrapped}, RedactionKey: newTestKey(t)}
	_, err := scanner.Scan(context.Background(), testBoundary())
	if !errors.Is(err, wrapped) {
		t.Fatalf("Scan() error = %v, want wrapped client error", err)
	}
}

func sampleClient() fakeClient {
	return fakeClient{
		computeEnvironments: []batch.ComputeEnvironment{{
			ARN:               "arn:aws:batch:us-east-1:123456789012:compute-environment/ec2-ce",
			Name:              "ec2-ce",
			Type:              "MANAGED",
			State:             "ENABLED",
			Status:            "VALID",
			OrchestrationType: "ECS",
			ServiceRoleARN:    "arn:aws:iam::123456789012:role/batch-service",
			InstanceRoleARN:   "arn:aws:iam::123456789012:instance-profile/ecsInstanceRole",
			ComputeResource: batch.ComputeResource{
				ResourceType:     "EC2",
				SubnetIDs:        []string{"subnet-aaa", "subnet-bbb"},
				SecurityGroupIDs: []string{"sg-111"},
				LaunchTemplateID: "lt-0abc123",
			},
		}},
		jobQueues: []batch.JobQueue{{
			ARN:      "arn:aws:batch:us-east-1:123456789012:job-queue/prod-queue",
			Name:     "prod-queue",
			State:    "ENABLED",
			Status:   "VALID",
			Priority: 10,
			ComputeEnvironmentOrder: []batch.ComputeEnvironmentOrderEntry{{
				Order:              1,
				ComputeEnvironment: "arn:aws:batch:us-east-1:123456789012:compute-environment/ec2-ce",
			}},
		}},
		jobDefinitions: []batch.JobDefinition{{
			ARN:      "arn:aws:batch:us-east-1:123456789012:job-definition/etl:3",
			Name:     "etl",
			Revision: 3,
			Type:     "container",
			Status:   "ACTIVE",
			Container: &batch.Container{
				Image:            "123456789012.dkr.ecr.us-east-1.amazonaws.com/etl:prod",
				JobRoleARN:       "arn:aws:iam::123456789012:role/etl-job",
				ExecutionRoleARN: "arn:aws:iam::123456789012:role/etl-exec",
				Environment: []batch.EnvironmentVariable{{
					Name:  "DATABASE_URL",
					Value: "postgres://user:password@db.internal/app",
				}},
				Secrets: []batch.SecretReference{{
					Name:      "API_TOKEN",
					ValueFrom: "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token",
				}},
			},
		}},
		schedulingPolicies: []batch.SchedulingPolicy{{
			ARN:  "arn:aws:batch:us-east-1:123456789012:scheduling-policy/fair",
			Name: "fair",
		}},
		jobsByQueue: map[string][]batch.Job{
			"arn:aws:batch:us-east-1:123456789012:job-queue/prod-queue": {{
				ID:            "job-1",
				ARN:           "arn:aws:batch:us-east-1:123456789012:job/job-1",
				Name:          "etl-run",
				Status:        "RUNNING",
				JobQueueARN:   "arn:aws:batch:us-east-1:123456789012:job-queue/prod-queue",
				JobDefinition: "arn:aws:batch:us-east-1:123456789012:job-definition/etl:3",
			}},
		},
	}
}

func TestScannerEmitsAllResourceKinds(t *testing.T) {
	scanner := batch.Scanner{Client: sampleClient(), RedactionKey: newTestKey(t)}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, resourceType := range []string{
		awscloud.ResourceTypeBatchComputeEnvironment,
		awscloud.ResourceTypeBatchJobQueue,
		awscloud.ResourceTypeBatchJobDefinition,
		awscloud.ResourceTypeBatchSchedulingPolicy,
		awscloud.ResourceTypeBatchJob,
	} {
		if got := resourcesByType(t, envelopes, resourceType); len(got) != 1 {
			t.Fatalf("resource %q count = %d, want 1", resourceType, len(got))
		}
	}
}

func TestComputeEnvironmentRelationshipsHaveTargetTypeAndJoinKeys(t *testing.T) {
	scanner := batch.Scanner{Client: sampleClient(), RedactionKey: newTestKey(t)}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	cases := []struct {
		relationshipType string
		targetType       string
		targetResourceID string
	}{
		{
			relationshipType: awscloud.RelationshipBatchJobQueueUsesComputeEnvironment,
			targetType:       awscloud.ResourceTypeBatchComputeEnvironment,
			targetResourceID: "arn:aws:batch:us-east-1:123456789012:compute-environment/ec2-ce",
		},
		{
			relationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesSubnet,
			targetType:       awscloud.ResourceTypeEC2Subnet,
			targetResourceID: "subnet-aaa",
		},
		{
			relationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesLaunchTemplate,
			targetType:       awscloud.ResourceTypeEC2LaunchTemplate,
			targetResourceID: "lt-0abc123",
		},
		{
			relationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesIAMRole,
			targetType:       awscloud.ResourceTypeIAMRole,
			targetResourceID: "arn:aws:iam::123456789012:role/batch-service",
		},
		{
			relationshipType: awscloud.RelationshipBatchJobDefinitionUsesIAMRole,
			targetType:       awscloud.ResourceTypeIAMRole,
			targetResourceID: "arn:aws:iam::123456789012:role/etl-job",
		},
		{
			relationshipType: awscloud.RelationshipBatchJobDefinitionUsesImage,
			targetType:       "container_image",
			targetResourceID: "123456789012.dkr.ecr.us-east-1.amazonaws.com/etl:prod",
		},
		{
			relationshipType: awscloud.RelationshipBatchJobDefinitionReferencesSecret,
			targetType:       awscloud.ResourceTypeSecretsManagerSecret,
			targetResourceID: "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token",
		},
	}
	for _, tc := range cases {
		relationships := relationshipsByType(t, envelopes, tc.relationshipType)
		if len(relationships) == 0 {
			t.Fatalf("relationship %q not emitted", tc.relationshipType)
		}
		match := false
		for _, relationship := range relationships {
			if relationship["target_type"] == "" {
				t.Fatalf("relationship %q has empty target_type", tc.relationshipType)
			}
			if relationship["target_resource_id"] == tc.targetResourceID {
				if relationship["target_type"] != tc.targetType {
					t.Fatalf("relationship %q target_type = %q, want %q", tc.relationshipType, relationship["target_type"], tc.targetType)
				}
				match = true
			}
		}
		if !match {
			t.Fatalf("relationship %q missing target_resource_id %q", tc.relationshipType, tc.targetResourceID)
		}
	}
}

func TestComputeEnvironmentUsesIAMRoleCoversInstanceProfile(t *testing.T) {
	scanner := batch.Scanner{Client: sampleClient(), RedactionKey: newTestKey(t)}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	relationships := relationshipsByType(t, envelopes, awscloud.RelationshipBatchComputeEnvironmentUsesIAMRole)
	var sawInstanceProfile bool
	for _, relationship := range relationships {
		if relationship["target_resource_id"] == "arn:aws:iam::123456789012:instance-profile/ecsInstanceRole" {
			if relationship["target_type"] != awscloud.ResourceTypeIAMInstanceProfile {
				t.Fatalf("instance-profile target_type = %q, want %q", relationship["target_type"], awscloud.ResourceTypeIAMInstanceProfile)
			}
			sawInstanceProfile = true
		}
	}
	if !sawInstanceProfile {
		t.Fatalf("compute environment instance-profile relationship not emitted")
	}
}

// TestJobDefinitionRedactsEnvironmentValuesAndOmitsCommand is the security
// acceptance gate from the issue. The scanner must never persist a Batch
// container environment value in clear text and never carry a container command
// list or job parameters. Environment names survive; values are HMAC markers.
func TestJobDefinitionRedactsEnvironmentValuesAndOmitsCommand(t *testing.T) {
	scanner := batch.Scanner{Client: sampleClient(), RedactionKey: newTestKey(t)}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	jobDefinitions := resourcesByType(t, envelopes, awscloud.ResourceTypeBatchJobDefinition)
	if len(jobDefinitions) != 1 {
		t.Fatalf("job definition count = %d, want 1", len(jobDefinitions))
	}
	payload := jobDefinitions[0]
	secretValue := "postgres://user:password@db.internal/app"
	assertNoSubstring(t, payload, secretValue)
	attributes, ok := payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("job definition attributes payload missing")
	}
	if _, ok := attributes["command"]; ok {
		t.Fatalf("job definition attributes must not carry a command list")
	}
	if _, ok := attributes["parameters"]; ok {
		t.Fatalf("job definition attributes must not carry job parameters")
	}
	container, ok := attributes["container"].(map[string]any)
	if !ok {
		t.Fatalf("job definition container payload missing")
	}
	if _, ok := container["command"]; ok {
		t.Fatalf("container payload must not carry a command list")
	}
	environment, ok := container["environment"].([]map[string]any)
	if !ok || len(environment) != 1 {
		t.Fatalf("container environment = %#v, want one redacted entry", container["environment"])
	}
	if environment[0]["name"] != "DATABASE_URL" {
		t.Fatalf("environment name = %v, want DATABASE_URL", environment[0]["name"])
	}
	value, ok := environment[0]["value"].(map[string]any)
	if !ok {
		t.Fatalf("environment value = %#v, want redaction marker map", environment[0]["value"])
	}
	marker, _ := value["marker"].(string)
	if !strings.HasPrefix(marker, "redacted:") {
		t.Fatalf("environment value marker = %q, want redacted prefix", marker)
	}
}

// TestSchedulingPolicyNeverCarriesFairShareState confirms the scheduling policy
// fact carries name and ARN only, never fair-share weight state.
func TestSchedulingPolicyNeverCarriesFairShareState(t *testing.T) {
	scanner := batch.Scanner{Client: sampleClient(), RedactionKey: newTestKey(t)}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	policies := resourcesByType(t, envelopes, awscloud.ResourceTypeBatchSchedulingPolicy)
	if len(policies) != 1 {
		t.Fatalf("scheduling policy count = %d, want 1", len(policies))
	}
	payload := policies[0]
	for _, forbidden := range []string{"fairshare_policy", "fairshare", "share_distribution", "weight_factor", "compute_reservation"} {
		if attrs, ok := payload["attributes"].(map[string]any); ok {
			if _, present := attrs[forbidden]; present {
				t.Fatalf("scheduling policy attribute %q must not be present", forbidden)
			}
		}
	}
	if payload["name"] != "fair" {
		t.Fatalf("scheduling policy name = %v, want fair", payload["name"])
	}
}

// TestRecentJobOmitsParametersAndOverrides confirms job facts carry identity,
// status, and definition reference only.
func TestRecentJobOmitsParametersAndOverrides(t *testing.T) {
	scanner := batch.Scanner{Client: sampleClient(), RedactionKey: newTestKey(t)}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	jobs := resourcesByType(t, envelopes, awscloud.ResourceTypeBatchJob)
	if len(jobs) != 1 {
		t.Fatalf("job count = %d, want 1", len(jobs))
	}
	payload := jobs[0]
	if payload["state"] != "RUNNING" {
		t.Fatalf("job state = %v, want RUNNING", payload["state"])
	}
	attrs, _ := payload["attributes"].(map[string]any)
	for _, forbidden := range []string{"parameters", "container_overrides", "node_overrides", "container"} {
		if _, present := attrs[forbidden]; present {
			t.Fatalf("job attribute %q must not be present", forbidden)
		}
	}
	if attrs["job_definition"] != "arn:aws:batch:us-east-1:123456789012:job-definition/etl:3" {
		t.Fatalf("job_definition attribute = %v", attrs["job_definition"])
	}
}

func assertNoSubstring(t *testing.T, payload map[string]any, needle string) {
	t.Helper()
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			if strings.Contains(typed, needle) {
				t.Fatalf("found forbidden substring %q in payload", needle)
			}
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]string:
			for _, child := range typed {
				walk(child)
			}
		case []map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case []string:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(payload)
}
