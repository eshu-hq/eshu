// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cptypes "github.com/aws/aws-sdk-go-v2/service/codepipeline/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cpservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codepipeline"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestAPIClientInterfaceExcludesMutationAndJobAPIs proves the AWS SDK surface
// this adapter accepts never lists a CodePipeline mutation, execution-control,
// webhook-management, custom-action-mutation, or job-worker API as a callable
// method. It is the reflective guard the issue requires first: a maintainer
// cannot widen the metadata-only contract to reach a forbidden API without
// failing this test.
func TestAPIClientInterfaceExcludesMutationAndJobAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Pipeline mutation.
		"CreatePipeline",
		"UpdatePipeline",
		"DeletePipeline",
		// Execution control.
		"StartPipelineExecution",
		"StopPipelineExecution",
		"RetryStageExecution",
		"RollbackStage",
		"OverrideStageCondition",
		"PutApprovalResult",
		"DisableStageTransition",
		"EnableStageTransition",
		// Webhook management.
		"PutWebhook",
		"DeleteWebhook",
		"RegisterWebhookWithThirdParty",
		"DeregisterWebhookWithThirdParty",
		// Custom action type mutation.
		"CreateCustomActionType",
		"DeleteCustomActionType",
		"UpdateActionType",
		// Job-worker plane (returns action configuration secret values).
		"PollForJobs",
		"PollForThirdPartyJobs",
		"GetJobDetails",
		"GetThirdPartyJobDetails",
		"AcknowledgeJob",
		"AcknowledgeThirdPartyJob",
		"PutJobSuccessResult",
		"PutJobFailureResult",
		"PutThirdPartyJobSuccessResult",
		"PutThirdPartyJobFailureResult",
		// Tag mutation.
		"TagResource",
		"UntagResource",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("apiClient exposes forbidden method %q; CodePipeline SDK adapter must stay metadata-only", name)
		}
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceCodePipeline,
	}
}

// TestGetPipelineDropsActionConfigurationValues is the value-absence guard the
// issue requires: it feeds an action configuration carrying an inline GitHub
// OAuth token and a webhook secret, then asserts the scanner-owned pipeline
// retains only configuration key names and never any configuration value.
func TestGetPipelineDropsActionConfigurationValues(t *testing.T) {
	const oauthToken = "ghp_supersecrettoken1234567890ABCDEF"
	const webhookSecret = "hmac-shared-secret-value-9f8e7d"
	api := &fakeCodePipelineAPI{
		pipelineNames: []string{"checkout"},
		pipelines: map[string]cptypes.PipelineDeclaration{
			"checkout": {
				Name:    aws.String("checkout"),
				RoleArn: aws.String("arn:aws:iam::123456789012:role/CodePipelineServiceRole"),
				ArtifactStore: &cptypes.ArtifactStore{
					Type:     cptypes.ArtifactStoreTypeS3,
					Location: aws.String("checkout-artifacts"),
				},
				Stages: []cptypes.StageDeclaration{{
					Name: aws.String("Source"),
					Actions: []cptypes.ActionDeclaration{{
						Name: aws.String("Source"),
						ActionTypeId: &cptypes.ActionTypeId{
							Category: cptypes.ActionCategorySource,
							Owner:    cptypes.ActionOwnerThirdParty,
							Provider: aws.String("GitHub"),
							Version:  aws.String("1"),
						},
						Configuration: map[string]string{
							"Owner":       "octocorp",
							"Repo":        "checkout",
							"Branch":      "main",
							"OAuthToken":  oauthToken,
							"SecretToken": webhookSecret,
						},
					}},
				}},
			},
		},
	}
	client := newTestClient(api, testKey(t))

	pipelines, err := client.ListPipelines(context.Background())
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if len(pipelines) != 1 {
		t.Fatalf("pipelines = %d, want 1", len(pipelines))
	}
	action := pipelines[0].Stages[0].Actions[0]
	// Configuration keys are retained; values are not.
	if len(action.ConfigurationKeys) == 0 {
		t.Fatalf("ConfigurationKeys empty, want key names retained")
	}
	wantKey := false
	for _, key := range action.ConfigurationKeys {
		if key == "OAuthToken" {
			wantKey = true
		}
	}
	if !wantKey {
		t.Fatalf("ConfigurationKeys = %v, want OAuthToken key name retained", action.ConfigurationKeys)
	}
	// No scanner field anywhere in the pipeline tree may hold a config value.
	if leaks := pipelineLeaksSecret(pipelines[0], oauthToken, webhookSecret); leaks != "" {
		t.Fatalf("pipeline tree leaked action configuration value: %q", leaks)
	}
}

// pipelineLeaksSecret walks every string field reachable from a scanner-owned
// Pipeline and returns the first that contains a forbidden secret substring.
func pipelineLeaksSecret(value any, needles ...string) string {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		for _, needle := range needles {
			if needle != "" && strings.Contains(v.String(), needle) {
				return v.String()
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if leak := pipelineLeaksSecret(v.Index(i).Interface(), needles...); leak != "" {
				return leak
			}
		}
	case reflect.Map:
		for _, mk := range v.MapKeys() {
			if leak := pipelineLeaksSecret(v.MapIndex(mk).Interface(), needles...); leak != "" {
				return leak
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Field(i).CanInterface() {
				continue
			}
			if leak := pipelineLeaksSecret(v.Field(i).Interface(), needles...); leak != "" {
				return leak
			}
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return pipelineLeaksSecret(v.Elem().Interface(), needles...)
		}
	}
	return ""
}

func TestListWebhooksDropsAuthenticationSecretToken(t *testing.T) {
	const secretToken = "webhook-hmac-secret-abc123XYZ"
	api := &fakeCodePipelineAPI{
		webhooks: []cptypes.ListWebhookItem{{
			Arn: aws.String("arn:aws:codepipeline:us-east-1:123456789012:webhook:checkout-hook"),
			Url: aws.String("https://webhooks.example.com/trigger/abc"),
			Definition: &cptypes.WebhookDefinition{
				Name:           aws.String("checkout-hook"),
				TargetPipeline: aws.String("checkout"),
				TargetAction:   aws.String("Source"),
				Authentication: cptypes.WebhookAuthenticationTypeGithubHmac,
				AuthenticationConfiguration: &cptypes.WebhookAuthConfiguration{
					SecretToken: aws.String(secretToken),
				},
			},
		}},
	}
	client := newTestClient(api, testKey(t))

	webhooks, err := client.ListWebhooks(context.Background())
	if err != nil {
		t.Fatalf("ListWebhooks() error = %v", err)
	}
	if len(webhooks) != 1 {
		t.Fatalf("webhooks = %d, want 1", len(webhooks))
	}
	if webhooks[0].TargetAction != "Source" {
		t.Fatalf("webhook TargetAction = %q, want Source", webhooks[0].TargetAction)
	}
	if leak := pipelineLeaksSecret(webhooks[0], secretToken); leak != "" {
		t.Fatalf("webhook leaked authentication secret token: %q", leak)
	}
	if webhooks[0].AuthenticationType != "GITHUB_HMAC" {
		t.Fatalf("webhook AuthenticationType = %q, want GITHUB_HMAC", webhooks[0].AuthenticationType)
	}
}

func TestListPipelinesPaginatesAndResolvesTargets(t *testing.T) {
	api := &fakeCodePipelineAPI{
		pipelinePages: [][]string{{"alpha"}, {"beta"}},
		pipelines: map[string]cptypes.PipelineDeclaration{
			"alpha": {
				Name:    aws.String("alpha"),
				RoleArn: aws.String("arn:aws:iam::123456789012:role/Alpha"),
				ArtifactStore: &cptypes.ArtifactStore{
					Type:     cptypes.ArtifactStoreTypeS3,
					Location: aws.String("alpha-bucket"),
					EncryptionKey: &cptypes.EncryptionKey{
						Id:   aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd-1234"),
						Type: cptypes.EncryptionKeyTypeKms,
					},
				},
				Stages: []cptypes.StageDeclaration{{
					Name: aws.String("Build"),
					Actions: []cptypes.ActionDeclaration{{
						Name: aws.String("Build"),
						ActionTypeId: &cptypes.ActionTypeId{
							Category: cptypes.ActionCategoryBuild,
							Owner:    cptypes.ActionOwnerAws,
							Provider: aws.String("CodeBuild"),
							Version:  aws.String("1"),
						},
						Configuration: map[string]string{"ProjectName": "alpha-build"},
					}},
				}},
			},
			"beta": {
				Name:    aws.String("beta"),
				RoleArn: aws.String("arn:aws:iam::123456789012:role/Beta"),
				Stages:  []cptypes.StageDeclaration{},
			},
		},
	}
	client := newTestClient(api, testKey(t))

	pipelines, err := client.ListPipelines(context.Background())
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if len(pipelines) != 2 {
		t.Fatalf("pipelines = %d, want 2 (pagination must follow nextToken)", len(pipelines))
	}
	if api.listPipelinesCalls != 2 {
		t.Fatalf("ListPipelines API calls = %d, want 2", api.listPipelinesCalls)
	}
	var alpha cpservice.Pipeline
	for _, pipeline := range pipelines {
		if pipeline.Name == "alpha" {
			alpha = pipeline
		}
	}
	if alpha.Name == "" {
		t.Fatalf("alpha pipeline missing")
	}
	build := alpha.Stages[0].Actions[0]
	if build.TargetProvider != "CodeBuild" {
		t.Fatalf("build action TargetProvider = %q, want CodeBuild", build.TargetProvider)
	}
	if build.TargetResourceName != "alpha-build" {
		t.Fatalf("build action TargetResourceName = %q, want alpha-build", build.TargetResourceName)
	}
	if alpha.ArtifactStore.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/abcd-1234" {
		t.Fatalf("artifact store KMS key id = %q", alpha.ArtifactStore.KMSKeyID)
	}
}

// TestListPipelinesPopulatesMetadataTimestamps proves the adapter maps the
// GetPipeline PipelineMetadata Created/Updated timestamps onto the scanner-owned
// Pipeline. Without this the pipeline observation always emits null created and
// updated, so the regression guards the metadata->scanner wiring.
func TestListPipelinesPopulatesMetadataTimestamps(t *testing.T) {
	created := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2024, time.March, 6, 7, 8, 9, 0, time.UTC)
	api := &fakeCodePipelineAPI{
		pipelineNames: []string{"checkout"},
		pipelines: map[string]cptypes.PipelineDeclaration{
			"checkout": {
				Name:    aws.String("checkout"),
				RoleArn: aws.String("arn:aws:iam::123456789012:role/CodePipelineServiceRole"),
			},
		},
		metadata: map[string]*cptypes.PipelineMetadata{
			"checkout": {
				PipelineArn: aws.String("arn:aws:codepipeline:us-east-1:123456789012:checkout"),
				Created:     aws.Time(created),
				Updated:     aws.Time(updated),
			},
		},
	}
	client := newTestClient(api, testKey(t))

	pipelines, err := client.ListPipelines(context.Background())
	if err != nil {
		t.Fatalf("ListPipelines() error = %v", err)
	}
	if len(pipelines) != 1 {
		t.Fatalf("pipelines = %d, want 1", len(pipelines))
	}
	pipeline := pipelines[0]
	if !pipeline.Created.Equal(created) {
		t.Fatalf("pipeline Created = %v, want %v", pipeline.Created, created)
	}
	if !pipeline.Updated.Equal(updated) {
		t.Fatalf("pipeline Updated = %v, want %v", pipeline.Updated, updated)
	}
	// Timestamps must be normalized to UTC like the execution timestamps.
	if loc := pipeline.Created.Location(); loc != time.UTC {
		t.Fatalf("pipeline Created location = %v, want UTC", loc)
	}
}

func TestListRecentExecutionsKeepsSafeRevisionRefs(t *testing.T) {
	api := &fakeCodePipelineAPI{
		pipelineNames: []string{"checkout"},
		pipelines: map[string]cptypes.PipelineDeclaration{
			"checkout": {Name: aws.String("checkout"), RoleArn: aws.String("arn:aws:iam::123456789012:role/R")},
		},
		executionsByPipeline: map[string][]cptypes.PipelineExecutionSummary{
			"checkout": {{
				PipelineExecutionId: aws.String("exec-1"),
				Status:              cptypes.PipelineExecutionStatusSucceeded,
				SourceRevisions: []cptypes.SourceRevision{{
					ActionName:      aws.String("Source"),
					RevisionId:      aws.String("abc123"),
					RevisionSummary: aws.String("fix: ship the thing"),
				}},
			}},
		},
	}
	client := newTestClient(api, testKey(t))

	executions, err := client.ListRecentExecutions(context.Background(), "checkout")
	if err != nil {
		t.Fatalf("ListRecentExecutions() error = %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("executions = %d, want 1", len(executions))
	}
	if executions[0].Status != "Succeeded" {
		t.Fatalf("execution status = %q, want Succeeded", executions[0].Status)
	}
	if len(executions[0].SourceRevisions) != 1 {
		t.Fatalf("source revisions = %d, want 1", len(executions[0].SourceRevisions))
	}
	rev := executions[0].SourceRevisions[0]
	if rev.RevisionID != "abc123" {
		t.Fatalf("revision id = %q, want abc123", rev.RevisionID)
	}
	// The commit-message summary is routed through the redaction library so a
	// pasted secret in a commit message cannot land raw in a durable fact.
	if rev.SummaryMarker == nil {
		t.Fatalf("revision summary marker missing, want redacted summary")
	}
	if leak := pipelineLeaksSecret(rev.SummaryMarker, "fix: ship the thing"); leak != "" {
		t.Fatalf("revision summary leaked raw commit text: %q", leak)
	}
}

func TestListActionTypesKeepsCustomTypesMetadata(t *testing.T) {
	api := &fakeCodePipelineAPI{
		actionTypes: []cptypes.ActionType{{
			Id: &cptypes.ActionTypeId{
				Category: cptypes.ActionCategoryBuild,
				Owner:    cptypes.ActionOwnerCustom,
				Provider: aws.String("AcmeRunner"),
				Version:  aws.String("2"),
			},
		}},
	}
	client := newTestClient(api, testKey(t))

	types, err := client.ListCustomActionTypes(context.Background())
	if err != nil {
		t.Fatalf("ListCustomActionTypes() error = %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("action types = %d, want 1", len(types))
	}
	if types[0].Provider != "AcmeRunner" || types[0].Owner != "Custom" {
		t.Fatalf("action type = %#v, want custom AcmeRunner", types[0])
	}
}

var (
	_ apiClient        = (*fakeCodePipelineAPI)(nil)
	_ cpservice.Client = (*Client)(nil)
)
