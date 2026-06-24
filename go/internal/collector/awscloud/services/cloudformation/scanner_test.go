// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("cloudformation-test-redaction-key-0123456789"))
	if err != nil {
		t.Fatalf("redact.NewKey() error = %v", err)
	}
	return key
}

// TestClientInterfaceExcludesMutationAndTemplateAPIs is the primary security
// guard for the CloudFormation scanner. CloudFormation carries the highest
// template-body redaction risk in the AWS collector, so the scanner-owned
// Client interface must never expose a template-body extraction API
// (GetTemplate, GetTemplateSummary) or any mutation API. The forbidden set is
// excluded by construction; a maintainer adding one of these methods to Client
// breaks the redaction contract for the entire scanner and this test fails.
func TestClientInterfaceExcludesMutationAndTemplateAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbidden := []string{
		// Template body extraction. These return the raw CloudFormation
		// template, which can contain inline IAM policy bodies, NoEcho
		// parameter values, and embedded secrets.
		"GetTemplate",
		"GetTemplateSummary",
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
		// Drift detection mutation (triggers detection, not read).
		"DetectStackDrift",
		"DetectStackResourceDrift",
		"DetectStackSetDrift",
		// Change set body extraction.
		"DescribeChangeSet",
		"DescribeChangeSetHooks",
		// Type mutation and configuration.
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
		// Stack policy body extraction.
		"GetStackPolicy",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("Client exposes forbidden method %q; CloudFormation scanner must never read template bodies or mutate resources", name)
		}
	}
}

func TestScanRejectsForeignServiceKind(t *testing.T) {
	scanner := Scanner{Client: &fakeClient{}, RedactionKey: testRedactionKey(t)}
	_, err := scanner.Scan(context.Background(), boundaryFor("ec2"))
	if err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScanRequiresClient(t *testing.T) {
	scanner := Scanner{}
	if _, err := scanner.Scan(context.Background(), boundaryFor(awscloud.ServiceCloudFormation)); err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScanRequiresRedactionKey(t *testing.T) {
	scanner := Scanner{Client: &fakeClient{}}
	if _, err := scanner.Scan(context.Background(), boundaryFor(awscloud.ServiceCloudFormation)); err == nil {
		t.Fatalf("Scan() error = nil, want redaction-key-required error")
	}
}

func TestScanEmitsStackResourceWithMetadataAndRelationships(t *testing.T) {
	stackID := "arn:aws:cloudformation:us-east-1:123456789012:stack/prod-app/abc-123"
	roleARN := "arn:aws:iam::123456789012:role/cfn-exec"
	client := &fakeClient{
		stacks: []Stack{{
			ID:           stackID,
			Name:         "prod-app",
			Status:       "CREATE_COMPLETE",
			RoleARN:      roleARN,
			TemplateURL:  "https://s3.amazonaws.com/cfn-templates/prod-app.yaml",
			Capabilities: []string{"CAPABILITY_NAMED_IAM"},
			DriftStatus:  "IN_SYNC",
			ParameterKeys: []string{
				"DBPassword",
				"InstanceType",
			},
			Outputs: []StackOutput{
				{Key: "ServiceEndpoint", Value: "https://api.example.com"},
			},
			Tags:         map[string]string{"Environment": "prod"},
			CreationTime: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		}},
		stackResources: map[string][]StackResource{
			stackID: {
				{LogicalID: "Queue", PhysicalID: "arn:aws:sqs:us-east-1:123456789012:prod-app", ResourceType: "AWS::SQS::Queue"},
			},
		},
	}
	scanner := Scanner{Client: client, RedactionKey: testRedactionKey(t)}

	envelopes, err := scanner.Scan(context.Background(), boundaryFor(awscloud.ServiceCloudFormation))
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationStack)
	if resource.Payload["arn"] != stackID {
		t.Fatalf("stack arn = %v, want %q", resource.Payload["arn"], stackID)
	}
	attributes := resource.Payload["attributes"].(map[string]any)
	// Parameter keys are inventory metadata; parameter values must never appear.
	keys, ok := attributes["parameter_keys"].([]string)
	if !ok || len(keys) != 2 {
		t.Fatalf("parameter_keys = %#v, want 2 keys", attributes["parameter_keys"])
	}
	if _, present := attributes["parameters"]; present {
		t.Fatalf("attributes carry a parameters field; parameter values must never be persisted")
	}
	if _, present := attributes["template_body"]; present {
		t.Fatalf("attributes carry a template_body field; template bodies must never be persisted")
	}

	relationships := findRelationships(t, envelopes)
	if !hasRelationship(relationships, awscloud.RelationshipCloudFormationStackUsesIAMRole, roleARN) {
		t.Fatalf("missing stack-to-IAM-role relationship for %q", roleARN)
	}
	if !hasRelationship(relationships, awscloud.RelationshipCloudFormationStackUsesS3TemplateURL, "https://s3.amazonaws.com/cfn-templates/prod-app.yaml") {
		t.Fatalf("missing stack-to-S3-template-URL relationship")
	}
	if !hasRelationship(relationships, awscloud.RelationshipCloudFormationStackUsesResourceType, "AWS::SQS::Queue") {
		t.Fatalf("missing stack-to-resource-type relationship")
	}
}

func TestScanRedactsSecretLikeStackOutputs(t *testing.T) {
	stackID := "arn:aws:cloudformation:us-east-1:123456789012:stack/secrets/def-456"
	client := &fakeClient{
		stacks: []Stack{{
			ID:     stackID,
			Name:   "secrets",
			Status: "UPDATE_COMPLETE",
			Outputs: []StackOutput{
				{Key: "ServiceUrl", Value: "https://api.example.com"},
				{Key: "DatabasePassword", Value: "hunter2-should-be-redacted"},
				// Compound key wrapping a multi-word sensitive policy entry
				// (connection_string) must redact end to end at emission.
				{Key: "DatabaseConnectionString", Value: "postgres://u:p@h/db"},
			},
		}},
	}
	scanner := Scanner{Client: client, RedactionKey: testRedactionKey(t)}

	envelopes, err := scanner.Scan(context.Background(), boundaryFor(awscloud.ServiceCloudFormation))
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resource := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationStack)
	attributes := resource.Payload["attributes"].(map[string]any)
	outputs, ok := attributes["outputs"].([]map[string]any)
	if !ok || len(outputs) != 3 {
		t.Fatalf("outputs = %#v, want 3 entries", attributes["outputs"])
	}
	secretKeys := map[string]struct{}{"DatabasePassword": {}, "DatabaseConnectionString": {}}
	for _, out := range outputs {
		key, _ := out["key"].(string)
		if _, secret := secretKeys[key]; secret {
			if _, present := out["value"]; present {
				t.Fatalf("secret-like output %q carried a cleartext value: %#v", key, out)
			}
			if out["redacted"] == nil {
				t.Fatalf("secret-like output %q missing redaction marker: %#v", key, out)
			}
		}
		if key == "ServiceUrl" && out["value"] != "https://api.example.com" {
			t.Fatalf("non-secret output value = %v, want cleartext", out["value"])
		}
	}
}

func TestScanEmitsStackSetInstanceChangeSetDriftAndType(t *testing.T) {
	client := &fakeClient{
		stackSets: []StackSet{{
			ID:                    "ss-1",
			Name:                  "org-baseline",
			ARN:                   "arn:aws:cloudformation:us-east-1:123456789012:stackset/org-baseline:ss-1",
			Status:                "ACTIVE",
			PermissionModel:       "SERVICE_MANAGED",
			AdministrationRoleARN: "arn:aws:iam::123456789012:role/admin",
			ParameterKeys:         []string{"Region"},
		}},
		stackInstances: map[string][]StackInstance{
			"org-baseline": {
				{StackSetName: "org-baseline", Account: "222222222222", Region: "us-west-2", Status: "CURRENT"},
			},
		},
		stacks: []Stack{{
			ID:     "arn:aws:cloudformation:us-east-1:123456789012:stack/app/g-1",
			Name:   "app",
			Status: "CREATE_COMPLETE",
		}},
		changeSets: map[string][]ChangeSet{
			"arn:aws:cloudformation:us-east-1:123456789012:stack/app/g-1": {
				{ID: "cs-1", Name: "deploy-1", StackName: "app", Status: "CREATE_COMPLETE", ExecutionStatus: "AVAILABLE"},
			},
		},
		drifts: map[string]StackDriftResult{
			"arn:aws:cloudformation:us-east-1:123456789012:stack/app/g-1": {
				TotalChecked: 3, DriftedCount: 1, InSyncCount: 2,
			},
		},
		types: []RegisteredType{{
			ARN:      "arn:aws:cloudformation:us-east-1:123456789012:type/resource/My-Org-Widget",
			TypeName: "My::Org::Widget",
			Kind:     "RESOURCE",
		}},
	}
	scanner := Scanner{Client: client, RedactionKey: testRedactionKey(t)}

	envelopes, err := scanner.Scan(context.Background(), boundaryFor(awscloud.ServiceCloudFormation))
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	stackSet := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationStackSet)
	if stackSet.Payload["name"] != "org-baseline" {
		t.Fatalf("stack set name = %v, want org-baseline", stackSet.Payload["name"])
	}
	if attrs := stackSet.Payload["attributes"].(map[string]any); attrs["template_body"] != nil {
		t.Fatalf("stack set attributes carry template_body; must never be persisted")
	}

	instance := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationStackInstance)
	if attrs := instance.Payload["attributes"].(map[string]any); attrs["account"] != "222222222222" {
		t.Fatalf("stack instance account = %v, want 222222222222", attrs["account"])
	}

	changeSet := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationChangeSet)
	if changeSet.Payload["name"] != "deploy-1" {
		t.Fatalf("change set name = %v, want deploy-1", changeSet.Payload["name"])
	}

	drift := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationStackDrift)
	if attrs := drift.Payload["attributes"].(map[string]any); attrs["drifted_count"] != 1 {
		t.Fatalf("drift drifted_count = %v, want 1", attrs["drifted_count"])
	}

	regType := findResource(t, envelopes, awscloud.ResourceTypeCloudFormationType)
	if regType.Payload["name"] != "My::Org::Widget" {
		t.Fatalf("type name = %v, want My::Org::Widget", regType.Payload["name"])
	}

	relationships := findRelationships(t, envelopes)
	if !hasRelationship(relationships, awscloud.RelationshipCloudFormationStackSetContainsStackInstance, "222222222222") {
		t.Fatalf("missing stack-set-to-instance relationship")
	}
}

func boundaryFor(serviceKind string) awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         serviceKind,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	}
}

func findResource(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			return envelope
		}
	}
	t.Fatalf("no resource envelope of type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func findRelationships(t *testing.T, envelopes []facts.Envelope) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			out = append(out, envelope)
		}
	}
	return out
}

func hasRelationship(envelopes []facts.Envelope, relationshipType, target string) bool {
	for _, envelope := range envelopes {
		if envelope.Payload["relationship_type"] != relationshipType {
			continue
		}
		switch target {
		case envelope.Payload["target_resource_id"], envelope.Payload["target_arn"], envelope.Payload["target_type"]:
			return true
		}
		if attrs, ok := envelope.Payload["attributes"].(map[string]any); ok {
			if attrs["account"] == target {
				return true
			}
		}
	}
	return false
}

type fakeClient struct {
	stacks         []Stack
	stackResources map[string][]StackResource
	stackSets      []StackSet
	changeSets     map[string][]ChangeSet
	drifts         map[string]StackDriftResult
	stackInstances map[string][]StackInstance
	types          []RegisteredType
}

func (f *fakeClient) ListStacks(context.Context) ([]Stack, error) { return f.stacks, nil }

func (f *fakeClient) ListStackResources(_ context.Context, stackID string) ([]StackResource, error) {
	return f.stackResources[stackID], nil
}

func (f *fakeClient) ListStackSets(context.Context) ([]StackSet, error) { return f.stackSets, nil }

func (f *fakeClient) ListChangeSets(_ context.Context, stackID string) ([]ChangeSet, error) {
	return f.changeSets[stackID], nil
}

func (f *fakeClient) ListStackResourceDrifts(_ context.Context, stackID string) (StackDriftResult, error) {
	return f.drifts[stackID], nil
}

func (f *fakeClient) ListStackInstances(_ context.Context, stackSetName string) ([]StackInstance, error) {
	return f.stackInstances[stackSetName], nil
}

func (f *fakeClient) ListTypes(context.Context) ([]RegisteredType, error) { return f.types, nil }

var _ Client = (*fakeClient)(nil)
