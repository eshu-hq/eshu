// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsResourcesAndRelationshipsMetadataOnly(t *testing.T) {
	envelopes := scanFixture(t, richClient())

	for _, resourceType := range []string{
		awscloud.ResourceTypeBedrockFoundationModel,
		awscloud.ResourceTypeBedrockCustomModel,
		awscloud.ResourceTypeBedrockModelCustomizationJob,
		awscloud.ResourceTypeBedrockProvisionedModelThroughput,
		awscloud.ResourceTypeBedrockGuardrail,
		awscloud.ResourceTypeBedrockAgent,
		awscloud.ResourceTypeBedrockAgentActionGroup,
		awscloud.ResourceTypeBedrockKnowledgeBase,
	} {
		resourceByType(t, envelopes, resourceType)
	}

	for _, relationshipType := range []string{
		awscloud.RelationshipBedrockCustomModelUsesBaseModel,
		awscloud.RelationshipBedrockCustomModelUsesS3Output,
		awscloud.RelationshipBedrockCustomModelFromCustomizationJob,
		awscloud.RelationshipBedrockProvisionedThroughputUsesModel,
		awscloud.RelationshipBedrockAgentUsesFoundationModel,
		awscloud.RelationshipBedrockAgentUsesKnowledgeBase,
		awscloud.RelationshipBedrockAgentHasActionGroup,
		awscloud.RelationshipBedrockActionGroupUsesLambda,
		awscloud.RelationshipBedrockKnowledgeBaseUsesS3DataSource,
		awscloud.RelationshipBedrockKnowledgeBaseUsesConfluence,
		awscloud.RelationshipBedrockKnowledgeBaseUsesSharePoint,
		awscloud.RelationshipBedrockKnowledgeBaseUsesWebCrawler,
	} {
		relationshipByType(t, envelopes, relationshipType)
	}
}

// TestScannerEmitsOnlyAllowlistedAttributeKeys is a structural redaction gate
// over the scanner-owned fixture: it proves no emitted resource carries an
// attribute key that names a forbidden IP surface (agent instructions/prompts,
// guardrail topic/content policy bodies, action-group API/function schema
// bodies, knowledge base document content/chunks, or custom-model
// hyperparameters/training data). Unlike a sentinel substring scan over this
// fixture, this assertion is not vacuous: the scanner-owned types cannot carry
// the sentinel *values*, but they could regress to carry a forbidden attribute
// *key*, and that regression is exactly what this catches. The companion
// end-to-end proof that injected SDK payloads never reach facts lives in the
// awssdk package (TestScannerNeverEmitsSDKSentinelsEndToEnd), where the real
// adapter reads SDK output that actually holds the IP.
func TestScannerEmitsOnlyAllowlistedAttributeKeys(t *testing.T) {
	envelopes := scanFixture(t, richClient())

	forbiddenKeyTokens := []string{
		"instruction",    // agent system prompt
		"prompt",         // prompt-override template body
		"policy",         // guardrail topic/content policy body
		"topic",          // guardrail topic policy
		"filter",         // guardrail content filter body
		"apischema",      // action-group API schema body
		"functionschema", // action-group function schema body
		"schema_body",    // any inline schema body
		"document",       // knowledge base ingested document content
		"chunk",          // knowledge base ingested chunks
		"hyperparameter", // custom-model hyperparameter values
		"training_data",  // custom-model training input data
	}

	checkedResources := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attributes, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			continue
		}
		checkedResources++
		for key := range attributes {
			lower := strings.ToLower(key)
			for _, token := range forbiddenKeyTokens {
				if strings.Contains(lower, token) {
					t.Fatalf("resource %v emitted forbidden attribute key %q (token %q); the high-redaction contract forbids attributes that could carry agent prompts, guardrail policies, KB content, action-group schemas, or training data",
						envelope.Payload["resource_type"], key, token)
				}
			}
		}
	}
	if checkedResources == 0 {
		t.Fatalf("no resource envelopes inspected; the fixture must emit resources for this gate to be meaningful")
	}
}

func TestScannerAgentOmitsInstructionAndPromptOverrideAttributes(t *testing.T) {
	envelopes := scanFixture(t, richClient())
	agent := resourceByType(t, envelopes, awscloud.ResourceTypeBedrockAgent)
	attributes := attributesOf(t, agent)
	for key := range attributes {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "instruction") || strings.Contains(lower, "prompt") {
			t.Fatalf("agent attributes carry forbidden key %q; instructions and prompt overrides are IP", key)
		}
	}
	if got, want := attributes["foundation_model"], "anthropic.claude-3"; got != want {
		t.Fatalf("foundation_model = %#v, want %q", got, want)
	}
}

func TestScannerGuardrailOmitsPolicyBodyAttributes(t *testing.T) {
	envelopes := scanFixture(t, richClient())
	guardrail := resourceByType(t, envelopes, awscloud.ResourceTypeBedrockGuardrail)
	attributes := attributesOf(t, guardrail)
	for key := range attributes {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "policy") || strings.Contains(lower, "topic") || strings.Contains(lower, "filter") {
			t.Fatalf("guardrail attributes carry forbidden policy key %q", key)
		}
	}
	if got, want := attributes["version"], "DRAFT"; got != want {
		t.Fatalf("version = %#v, want %q", got, want)
	}
}

func TestScannerCustomModelRelationshipTargetTypes(t *testing.T) {
	envelopes := scanFixture(t, richClient())

	base := relationshipByType(t, envelopes, awscloud.RelationshipBedrockCustomModelUsesBaseModel)
	if got, want := base.Payload["target_type"], awscloud.ResourceTypeBedrockFoundationModel; got != want {
		t.Fatalf("base model target_type = %#v, want %q", got, want)
	}

	s3 := relationshipByType(t, envelopes, awscloud.RelationshipBedrockCustomModelUsesS3Output)
	if got, want := s3.Payload["target_resource_id"], "arn:aws:s3:::custom-model-output"; got != want {
		t.Fatalf("custom model S3 output target = %#v, want %q", got, want)
	}
	if got, want := s3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("custom model S3 output target_type = %#v, want %q", got, want)
	}

	job := relationshipByType(t, envelopes, awscloud.RelationshipBedrockCustomModelFromCustomizationJob)
	if got, want := job.Payload["target_type"], awscloud.ResourceTypeBedrockModelCustomizationJob; got != want {
		t.Fatalf("custom model job target_type = %#v, want %q", got, want)
	}
}

func TestScannerActionGroupLambdaJoinTarget(t *testing.T) {
	envelopes := scanFixture(t, richClient())
	lambda := relationshipByType(t, envelopes, awscloud.RelationshipBedrockActionGroupUsesLambda)
	if got, want := lambda.Payload["target_resource_id"], "arn:aws:lambda:us-east-1:123456789012:function:order-tool"; got != want {
		t.Fatalf("action group lambda target = %#v, want %q", got, want)
	}
	if got, want := lambda.Payload["target_type"], awscloud.ResourceTypeLambdaFunction; got != want {
		t.Fatalf("action group lambda target_type = %#v, want %q", got, want)
	}
	if got, want := lambda.Payload["target_arn"], "arn:aws:lambda:us-east-1:123456789012:function:order-tool"; got != want {
		t.Fatalf("action group lambda target_arn = %#v, want %q", got, want)
	}
}

func TestScannerKnowledgeBaseDataSourceTargets(t *testing.T) {
	envelopes := scanFixture(t, richClient())

	s3 := relationshipByType(t, envelopes, awscloud.RelationshipBedrockKnowledgeBaseUsesS3DataSource)
	if got, want := s3.Payload["target_resource_id"], "arn:aws:s3:::kb-docs"; got != want {
		t.Fatalf("kb S3 data source target = %#v, want %q", got, want)
	}
	if got, want := s3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("kb S3 data source target_type = %#v, want %q", got, want)
	}

	web := relationshipByType(t, envelopes, awscloud.RelationshipBedrockKnowledgeBaseUsesWebCrawler)
	if got, want := web.Payload["target_resource_id"], "https://docs.example.com"; got != want {
		t.Fatalf("kb web crawler target = %#v, want %q", got, want)
	}
	if got, want := web.Payload["target_type"], "web_data_source"; got != want {
		t.Fatalf("kb web crawler target_type = %#v, want %q", got, want)
	}
}

func TestScannerRelationshipsNeverHaveEmptyTargetType(t *testing.T) {
	envelopes := scanFixture(t, richClient())
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		targetType, _ := envelope.Payload["target_type"].(string)
		if strings.TrimSpace(targetType) == "" {
			t.Fatalf("relationship %v has empty target_type; graph join would break", envelope.Payload["relationship_type"])
		}
		targetID, _ := envelope.Payload["target_resource_id"].(string)
		if strings.TrimSpace(targetID) == "" {
			t.Fatalf("relationship %v has empty target_resource_id", envelope.Payload["relationship_type"])
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSageMaker
	if _, err := (Scanner{Client: richClient()}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerHandlesEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: &fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() emitted %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerSurfacesListError(t *testing.T) {
	client := richClient()
	client.guardrailErr = context.DeadlineExceeded
	if _, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want propagated list error")
	}
}

func scanFixture(t *testing.T, client Client) []facts.Envelope {
	t.Helper()
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	return envelopes
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceBedrock,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:bedrock:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
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
	t.Fatalf("missing resource_type %q", resourceType)
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
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
