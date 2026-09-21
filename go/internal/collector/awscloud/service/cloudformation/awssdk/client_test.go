// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfn "github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesTemplateAndMutationAPIs is the adapter side of
// the CloudFormation security contract. It proves the AWS SDK surface this
// adapter accepts never lists a template-body extraction API (GetTemplate,
// GetTemplateSummary) or any mutation API. Combined with the scanner-side
// guard, this gives two reflective layers so a maintainer cannot quietly widen
// the contract to reach template bodies, parameter values, or change-set
// bodies.
func TestAPIClientInterfaceExcludesTemplateAndMutationAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Template body extraction.
		"GetTemplate",
		"GetTemplateSummary",
		// Change set body extraction.
		"DescribeChangeSet",
		"DescribeChangeSetHooks",
		// Stack policy body extraction.
		"GetStackPolicy",
		// Stack mutation.
		"CreateStack",
		"UpdateStack",
		"DeleteStack",
		"RollbackStack",
		"ContinueUpdateRollback",
		"CancelUpdateStack",
		"SignalResource",
		"SetStackPolicy",
		"UpdateTerminationProtection",
		// Stack set mutation.
		"CreateStackSet",
		"UpdateStackSet",
		"DeleteStackSet",
		// Change set creation and execution.
		"CreateChangeSet",
		"ExecuteChangeSet",
		"DeleteChangeSet",
		// Stack instance mutation.
		"CreateStackInstances",
		"UpdateStackInstances",
		"DeleteStackInstances",
		"ImportStacksToStackSet",
		// Drift detection mutation.
		"DetectStackDrift",
		"DetectStackResourceDrift",
		"DetectStackSetDrift",
		// Type mutation.
		"RegisterType",
		"DeregisterType",
		"ActivateType",
		"DeactivateType",
		"PublishType",
		"SetTypeConfiguration",
		"SetTypeDefaultVersion",
		"TestType",
		// Generated template / resource scan data plane.
		"CreateGeneratedTemplate",
		"GetGeneratedTemplate",
		"StartResourceScan",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("apiClient exposes forbidden method %q; CloudFormation SDK adapter must stay metadata-only and never read template bodies", name)
		}
	}
}

func TestClientListStacksDropsParameterValuesAndNeverReadsTemplate(t *testing.T) {
	stackID := "arn:aws:cloudformation:us-east-1:123456789012:stack/prod/abc"
	api := &fakeCFNAPI{
		describeStacksPages: []*awscfn.DescribeStacksOutput{{
			Stacks: []cfntypes.Stack{{
				StackId:      aws.String(stackID),
				StackName:    aws.String("prod"),
				StackStatus:  cfntypes.StackStatusCreateComplete,
				CreationTime: aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
				RoleARN:      aws.String("arn:aws:iam::123456789012:role/cfn"),
				Capabilities: []cfntypes.Capability{cfntypes.CapabilityCapabilityNamedIam},
				DriftInformation: &cfntypes.StackDriftInformation{
					StackDriftStatus: cfntypes.StackDriftStatusInSync,
				},
				Parameters: []cfntypes.Parameter{
					{ParameterKey: aws.String("DBPassword"), ParameterValue: aws.String("super-secret-value")},
					{ParameterKey: aws.String("InstanceType"), ParameterValue: aws.String("m5.large")},
				},
				Outputs: []cfntypes.Output{
					{OutputKey: aws.String("ServiceEndpoint"), OutputValue: aws.String("https://api.example.com")},
					{OutputKey: aws.String("DatabasePassword"), OutputValue: aws.String("hunter2-leaked")},
				},
				Tags: []cfntypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
			}},
		}},
	}
	adapter := newTestClient(api)

	stacks, err := adapter.ListStacks(context.Background())
	if err != nil {
		t.Fatalf("ListStacks() error = %v, want nil", err)
	}
	if got, want := len(stacks), 1; got != want {
		t.Fatalf("len(stacks) = %d, want %d", got, want)
	}
	stack := stacks[0]

	// Parameter keys are inventory metadata; the SDK adapter must carry keys
	// only and never the parameter values.
	if got, want := len(stack.ParameterKeys), 2; got != want {
		t.Fatalf("ParameterKeys = %#v, want 2", stack.ParameterKeys)
	}
	for _, key := range stack.ParameterKeys {
		if key == "super-secret-value" || key == "m5.large" {
			t.Fatalf("ParameterKeys carried a parameter value: %q", key)
		}
	}
	// Output redaction is the scanner's job; the adapter carries raw key+value
	// so the scanner can classify by key. The adapter must surface both
	// outputs unchanged here.
	if got, want := len(stack.Outputs), 2; got != want {
		t.Fatalf("Outputs = %#v, want 2", stack.Outputs)
	}
	if stack.RoleARN != "arn:aws:iam::123456789012:role/cfn" {
		t.Fatalf("RoleARN = %q, want the stack role", stack.RoleARN)
	}
	if stack.DriftStatus != string(cfntypes.StackDriftStatusInSync) {
		t.Fatalf("DriftStatus = %q, want IN_SYNC", stack.DriftStatus)
	}

	// No template-body or parameter-value API was reached.
	for _, forbidden := range []string{"GetTemplate", "GetTemplateSummary", "DescribeChangeSet", "GetStackPolicy"} {
		if contains(api.calls, forbidden) {
			t.Fatalf("forbidden CloudFormation call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

func TestClientListStackSetsNeverCarriesTemplateBody(t *testing.T) {
	api := &fakeCFNAPI{
		listStackSetsPages: []*awscfn.ListStackSetsOutput{{
			Summaries: []cfntypes.StackSetSummary{{
				StackSetId:   aws.String("ss-1"),
				StackSetName: aws.String("baseline"),
				Status:       cfntypes.StackSetStatusActive,
			}},
		}},
		describeStackSet: map[string]*awscfn.DescribeStackSetOutput{
			"baseline": {
				StackSet: &cfntypes.StackSet{
					StackSetId:            aws.String("ss-1"),
					StackSetName:          aws.String("baseline"),
					StackSetARN:           aws.String("arn:aws:cloudformation:us-east-1:123456789012:stackset/baseline:ss-1"),
					Status:                cfntypes.StackSetStatusActive,
					PermissionModel:       cfntypes.PermissionModelsServiceManaged,
					AdministrationRoleARN: aws.String("arn:aws:iam::123456789012:role/admin"),
					TemplateBody:          aws.String(`{"Resources":{"Secret":{"Type":"AWS::SecretsManager::Secret"}}}`),
					Parameters: []cfntypes.Parameter{
						{ParameterKey: aws.String("AdminPassword"), ParameterValue: aws.String("leak-me")},
					},
				},
			},
		},
	}
	adapter := newTestClient(api)

	stackSets, err := adapter.ListStackSets(context.Background())
	if err != nil {
		t.Fatalf("ListStackSets() error = %v, want nil", err)
	}
	if got, want := len(stackSets), 1; got != want {
		t.Fatalf("len(stackSets) = %d, want %d", got, want)
	}
	stackSet := stackSets[0]
	if stackSet.Name != "baseline" {
		t.Fatalf("Name = %q, want baseline", stackSet.Name)
	}
	if got, want := len(stackSet.ParameterKeys), 1; got != want || stackSet.ParameterKeys[0] != "AdminPassword" {
		t.Fatalf("ParameterKeys = %#v, want [AdminPassword]", stackSet.ParameterKeys)
	}
	// StackSet has no TemplateBody field by construction; this asserts the
	// scanner type cannot leak it regardless of SDK contents.
	reflectStackSet := reflect.TypeOf(stackSet)
	if _, ok := reflectStackSet.FieldByName("TemplateBody"); ok {
		t.Fatalf("StackSet scanner type exposes TemplateBody; stack-set template bodies must never be persisted")
	}
}

func TestClientListStackResourceDriftsSummarizesCountsOnly(t *testing.T) {
	api := &fakeCFNAPI{
		driftsPages: []*awscfn.DescribeStackResourceDriftsOutput{{
			StackResourceDrifts: []cfntypes.StackResourceDrift{
				{
					StackResourceDriftStatus: cfntypes.StackResourceDriftStatusModified,
					ActualProperties:         aws.String(`{"BucketName":"actual"}`),
					ExpectedProperties:       aws.String(`{"BucketName":"expected"}`),
				},
				{StackResourceDriftStatus: cfntypes.StackResourceDriftStatusInSync},
				{StackResourceDriftStatus: cfntypes.StackResourceDriftStatusDeleted},
			},
		}},
	}
	adapter := newTestClient(api)

	drift, err := adapter.ListStackResourceDrifts(context.Background(), "stack-1")
	if err != nil {
		t.Fatalf("ListStackResourceDrifts() error = %v, want nil", err)
	}
	if drift.TotalChecked != 3 {
		t.Fatalf("TotalChecked = %d, want 3", drift.TotalChecked)
	}
	// Modified and Deleted resources both count as drifted.
	if drift.ModifiedCount != 1 || drift.DriftedCount != 2 {
		t.Fatalf("ModifiedCount=%d DriftedCount=%d, want 1/2", drift.ModifiedCount, drift.DriftedCount)
	}
	if drift.InSyncCount != 1 || drift.DeletedCount != 1 {
		t.Fatalf("InSyncCount=%d DeletedCount=%d, want 1/1", drift.InSyncCount, drift.DeletedCount)
	}
	// The drift summary type carries counts only.
	reflectDrift := reflect.TypeOf(drift)
	for _, banned := range []string{"ActualProperties", "ExpectedProperties", "PropertyDifferences"} {
		if _, ok := reflectDrift.FieldByName(banned); ok {
			t.Fatalf("StackDriftResult exposes %q; drift property bodies must never be persisted", banned)
		}
	}
}

func newTestClient(api apiClient) *Client {
	return &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceCloudFormation},
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
