// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codepipeline

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodePipeline,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC),
	}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("codepipeline-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

type fakeClient struct {
	pipelines            []Pipeline
	executionsByPipeline map[string][]Execution
	webhooks             []Webhook
	actionTypes          []ActionType
}

func (f fakeClient) ListPipelines(context.Context) ([]Pipeline, error) { return f.pipelines, nil }

func (f fakeClient) ListRecentExecutions(_ context.Context, name string) ([]Execution, error) {
	return f.executionsByPipeline[name], nil
}

func (f fakeClient) ListWebhooks(context.Context) ([]Webhook, error) { return f.webhooks, nil }

func (f fakeClient) ListCustomActionTypes(context.Context) ([]ActionType, error) {
	return f.actionTypes, nil
}

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if env.Payload["resource_type"] == resourceType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if env.Payload["relationship_type"] == relType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func sampleClient() fakeClient {
	return fakeClient{
		pipelines: []Pipeline{{
			Name:          "checkout",
			RoleARN:       "arn:aws:iam::123456789012:role/CodePipelineServiceRole",
			PipelineType:  "V2",
			ExecutionMode: "SUPERSEDED",
			Version:       3,
			ArtifactStore: ArtifactStoreSummary{
				Type:       "S3",
				S3Bucket:   "checkout-artifacts",
				KMSKeyID:   "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
				KMSKeyType: "KMS",
			},
			Stages: []Stage{
				{
					Name: "Source",
					Actions: []Action{{
						Name:              "Source",
						Category:          "Source",
						Owner:             "AWS",
						Provider:          "CodeCommit",
						Version:           "1",
						ConfigurationKeys: []string{"BranchName", "RepositoryName"},
						SourceProvider:    "CodeCommit",
					}},
				},
				{
					Name: "Build",
					Actions: []Action{{
						Name:               "Build",
						Category:           "Build",
						Owner:              "AWS",
						Provider:           "CodeBuild",
						Version:            "1",
						ConfigurationKeys:  []string{"ProjectName"},
						TargetProvider:     "CodeBuild",
						TargetResourceName: "checkout-build",
					}},
				},
				{
					Name: "Deploy",
					Actions: []Action{
						{
							Name:               "DeployEcs",
							Category:           "Deploy",
							Owner:              "AWS",
							Provider:           "ECS",
							Version:            "1",
							ConfigurationKeys:  []string{"ClusterName", "ServiceName"},
							TargetProvider:     "ECS",
							TargetResourceName: "checkout-cluster/checkout-svc",
						},
						{
							Name:               "DeployCfn",
							Category:           "Deploy",
							Owner:              "AWS",
							Provider:           "CloudFormation",
							Version:            "1",
							ConfigurationKeys:  []string{"StackName"},
							TargetProvider:     "CloudFormation",
							TargetResourceName: "checkout-stack",
						},
						{
							Name:               "InvokeLambda",
							Category:           "Invoke",
							Owner:              "AWS",
							Provider:           "Lambda",
							Version:            "1",
							ConfigurationKeys:  []string{"FunctionName"},
							TargetProvider:     "Lambda",
							TargetResourceName: "checkout-postdeploy",
						},
					},
				},
			},
		}},
		executionsByPipeline: map[string][]Execution{
			"checkout": {{
				PipelineName: "checkout",
				ID:           "exec-1",
				Status:       "Succeeded",
				StartTime:    time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
				SourceRevisions: []SourceRevision{{
					ActionName: "Source",
					RevisionID: "abc123",
					HasSummary: true,
					SummaryMarker: map[string]any{
						"marker": "redacted",
						"reason": "codepipeline_source_revision_summary",
					},
				}},
			}},
		},
		webhooks: []Webhook{{
			Name:               "checkout-hook",
			ARN:                "arn:aws:codepipeline:us-east-1:123456789012:webhook:checkout-hook",
			TargetPipeline:     "checkout",
			TargetAction:       "Source",
			AuthenticationType: "GITHUB_HMAC",
		}},
		actionTypes: []ActionType{{
			Category: "Build",
			Owner:    "Custom",
			Provider: "AcmeRunner",
			Version:  "2",
		}},
	}
}

func TestScannerEmitsPipelineExecutionWebhookActionTypeResources(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	pipelines := resourcesByType(t, envelopes, awscloud.ResourceTypeCodePipelinePipeline)
	if len(pipelines) != 1 {
		t.Fatalf("pipeline resources = %d, want 1", len(pipelines))
	}
	attrs := pipelines[0]["attributes"].(map[string]any)
	if got := attrs["stage_count"]; got != 3 {
		t.Fatalf("stage_count = %v, want 3", got)
	}
	if got := attrs["action_count"]; got != 5 {
		t.Fatalf("action_count = %v, want 5", got)
	}
	store := attrs["artifact_store"].(map[string]any)
	if store["s3_bucket"] != "checkout-artifacts" {
		t.Fatalf("artifact_store.s3_bucket = %v", store["s3_bucket"])
	}

	executions := resourcesByType(t, envelopes, awscloud.ResourceTypeCodePipelineExecution)
	if len(executions) != 1 {
		t.Fatalf("execution resources = %d, want 1", len(executions))
	}
	if executions[0]["state"] != "Succeeded" {
		t.Fatalf("execution state = %v, want Succeeded", executions[0]["state"])
	}

	webhooks := resourcesByType(t, envelopes, awscloud.ResourceTypeCodePipelineWebhook)
	if len(webhooks) != 1 {
		t.Fatalf("webhook resources = %d, want 1", len(webhooks))
	}
	webhookAttrs := webhooks[0]["attributes"].(map[string]any)
	if webhookAttrs["authentication_type"] != "GITHUB_HMAC" {
		t.Fatalf("webhook authentication_type = %v", webhookAttrs["authentication_type"])
	}

	actionTypes := resourcesByType(t, envelopes, awscloud.ResourceTypeCodePipelineActionType)
	if len(actionTypes) != 1 {
		t.Fatalf("action-type resources = %d, want 1", len(actionTypes))
	}
}

func TestScannerEmitsPipelineRelationshipsWithJoinKeys(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	role := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelinePipelineUsesIAMRole)
	if len(role) != 1 || role[0]["target_type"] != awscloud.ResourceTypeIAMRole {
		t.Fatalf("pipeline->IAM-role = %#v", role)
	}
	if role[0]["target_arn"] != "arn:aws:iam::123456789012:role/CodePipelineServiceRole" {
		t.Fatalf("pipeline->IAM-role target_arn = %v", role[0]["target_arn"])
	}

	bucket := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelinePipelineStoresArtifactsInS3Bucket)
	if len(bucket) != 1 || bucket[0]["target_type"] != awscloud.ResourceTypeS3Bucket {
		t.Fatalf("pipeline->S3 = %#v", bucket)
	}
	if bucket[0]["target_resource_id"] != "arn:aws:s3:::checkout-artifacts" {
		t.Fatalf("pipeline->S3 target_resource_id = %v, want bucket ARN", bucket[0]["target_resource_id"])
	}

	key := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelinePipelineEncryptsArtifactsWithKMSKey)
	if len(key) != 1 || key[0]["target_type"] != awscloud.ResourceTypeKMSKey {
		t.Fatalf("pipeline->KMS = %#v", key)
	}

	stageActions := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineStageContainsAction)
	if len(stageActions) != 5 {
		t.Fatalf("stage->action relationships = %d, want 5", len(stageActions))
	}
	for _, rel := range stageActions {
		if rel["target_type"] == "" {
			t.Fatalf("stage->action relationship has empty target_type: %#v", rel)
		}
	}

	source := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineActionUsesSourceProvider)
	if len(source) != 1 || source[0]["target_resource_id"] != "CodeCommit" {
		t.Fatalf("action->source-provider = %#v", source)
	}
	if source[0]["target_type"] != ResourceTypeCodePipelineSourceProvider {
		t.Fatalf("action->source-provider target_type = %v", source[0]["target_type"])
	}

	build := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineActionTargetsCodeBuildProject)
	if len(build) != 1 || build[0]["target_type"] != awscloud.ResourceTypeCodeBuildProject {
		t.Fatalf("action->CodeBuild = %#v", build)
	}
	if build[0]["target_arn"] != "arn:aws:codebuild:us-east-1:123456789012:project/checkout-build" {
		t.Fatalf("action->CodeBuild target_arn = %v", build[0]["target_arn"])
	}

	const wantECSARN = "arn:aws:ecs:us-east-1:123456789012:service/checkout-cluster/checkout-svc"
	ecs := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineActionTargetsECSService)
	if len(ecs) != 1 || ecs[0]["target_type"] != awscloud.ResourceTypeECSService {
		t.Fatalf("action->ECS = %#v", ecs)
	}
	if ecs[0]["target_resource_id"] != wantECSARN || ecs[0]["target_arn"] != wantECSARN {
		t.Fatalf("action->ECS target = %v / %v, want %v", ecs[0]["target_resource_id"], ecs[0]["target_arn"], wantECSARN)
	}

	cfn := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineActionTargetsCloudFormationStack)
	if len(cfn) != 1 || cfn[0]["target_type"] != awscloud.ResourceTypeCloudFormationStack {
		t.Fatalf("action->CloudFormation = %#v", cfn)
	}
	if cfn[0]["target_resource_id"] != "checkout-stack" {
		t.Fatalf("action->CloudFormation target_resource_id = %v, want stack name", cfn[0]["target_resource_id"])
	}

	lambda := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineActionTargetsLambdaFunction)
	if len(lambda) != 1 || lambda[0]["target_type"] != awscloud.ResourceTypeLambdaFunction {
		t.Fatalf("action->Lambda = %#v", lambda)
	}
	if lambda[0]["target_arn"] != "arn:aws:lambda:us-east-1:123456789012:function:checkout-postdeploy" {
		t.Fatalf("action->Lambda target_arn = %v", lambda[0]["target_arn"])
	}

	webhook := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineWebhookTriggersPipeline)
	if len(webhook) != 1 || webhook[0]["target_type"] != awscloud.ResourceTypeCodePipelinePipeline {
		t.Fatalf("webhook->pipeline = %#v", webhook)
	}
	if webhook[0]["attributes"].(map[string]any)["target_action"] != "Source" {
		t.Fatalf("webhook->pipeline target_action attribute missing")
	}
}

// TestArtifactKeyRelationshipTargetsAliasVersusKey proves the artifact-key edge
// targets aws_kms_alias when CodePipeline reports an alias ARN and aws_kms_key
// when it reports a key ARN or a bare key id. KMS keys never carry an alias ARN
// in their correlation anchors (aliases are separate aws_kms_alias resources),
// so an alias-ARN reference mislabeled as a key target would dangle.
func TestArtifactKeyRelationshipTargetsAliasVersusKey(t *testing.T) {
	boundary := testBoundary()
	cases := []struct {
		name           string
		keyID          string
		wantTargetType string
		wantTargetID   string
		wantTargetARN  string
	}{
		{
			name:           "alias ARN targets the alias node",
			keyID:          "arn:aws:kms:us-east-1:123456789012:alias/checkout-artifacts",
			wantTargetType: awscloud.ResourceTypeKMSAlias,
			wantTargetID:   "arn:aws:kms:us-east-1:123456789012:alias/checkout-artifacts",
			wantTargetARN:  "arn:aws:kms:us-east-1:123456789012:alias/checkout-artifacts",
		},
		{
			name:           "key ARN targets the key node",
			keyID:          "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
			wantTargetType: awscloud.ResourceTypeKMSKey,
			wantTargetID:   "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
			wantTargetARN:  "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		},
		{
			name:           "bare key id targets the key node",
			keyID:          "abcd-1234-ef56",
			wantTargetType: awscloud.ResourceTypeKMSKey,
			wantTargetID:   "abcd-1234-ef56",
			wantTargetARN:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pipeline := Pipeline{
				Name:          "checkout",
				ARN:           "arn:aws:codepipeline:us-east-1:123456789012:checkout",
				ArtifactStore: ArtifactStoreSummary{KMSKeyID: tc.keyID},
			}
			rel, ok := artifactKeyRelationship(boundary, pipeline, pipeline.ARN, pipeline.ARN)
			if !ok {
				t.Fatalf("artifactKeyRelationship returned ok=false, want an edge")
			}
			if rel.TargetType != tc.wantTargetType {
				t.Fatalf("target_type = %q, want %q", rel.TargetType, tc.wantTargetType)
			}
			if rel.TargetResourceID != tc.wantTargetID {
				t.Fatalf("target_resource_id = %q, want %q", rel.TargetResourceID, tc.wantTargetID)
			}
			if rel.TargetARN != tc.wantTargetARN {
				t.Fatalf("target_arn = %q, want %q", rel.TargetARN, tc.wantTargetARN)
			}
		})
	}
}

func TestScannerUsesBoundaryPartitionForSynthesizedARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	build := relationshipsByType(t, envelopes, awscloud.RelationshipCodePipelineActionTargetsCodeBuildProject)
	if len(build) != 1 {
		t.Fatalf("action->CodeBuild = %d, want 1", len(build))
	}
	if got := build[0]["target_arn"]; got != "arn:aws-us-gov:codebuild:us-gov-west-1:123456789012:project/checkout-build" {
		t.Fatalf("GovCloud CodeBuild target_arn = %v, want aws-us-gov partition", got)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{RedactionKey: testKey(t)}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want redaction-key-required error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service-kind mismatch error")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, env := range envelopes {
		if env.Payload["service_kind"] != awscloud.ServiceCodePipeline {
			t.Fatalf("service_kind = %v, want codepipeline", env.Payload["service_kind"])
		}
	}
}
