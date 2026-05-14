package lambda

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsLambdaFactsWithRedactedEnvironmentAndRelationships(t *testing.T) {
	key, err := redact.NewKey([]byte("lambda-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	functionARN := "arn:aws:lambda:us-east-1:123456789012:function:api"
	imageURI := "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod"
	client := fakeClient{
		functions: []Function{{
			ARN:              functionARN,
			Name:             "api",
			Runtime:          "nodejs20.x",
			RoleARN:          "arn:aws:iam::123456789012:role/api-lambda",
			Handler:          "index.handler",
			PackageType:      "Image",
			ImageURI:         imageURI,
			ResolvedImageURI: "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api@sha256:abc123",
			Version:          "$LATEST",
			State:            "Active",
			LastModified:     time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
			Environment: map[string]string{
				"DATABASE_URL": "postgres://user:password@example.internal/app",
				"LOG_LEVEL":    "public-looking-but-still-runtime-config",
			},
			VPCConfig: VPCConfig{
				VPCID:            "vpc-123",
				SubnetIDs:        []string{"subnet-a", "subnet-b"},
				SecurityGroupIDs: []string{"sg-123"},
				IPv6AllowedForDS: true,
			},
			Tags: map[string]string{"environment": "prod"},
		}},
		aliases: map[string][]Alias{
			functionARN: {{
				ARN:             functionARN + ":prod",
				Name:            "prod",
				FunctionARN:     functionARN,
				FunctionVersion: "12",
				RoutingWeights:  map[string]float64{"13": 0.1},
			}},
		},
		eventSourceMappings: map[string][]EventSourceMapping{
			functionARN: {{
				ARN:            "arn:aws:lambda:us-east-1:123456789012:event-source-mapping:11111111-2222-3333-4444-555555555555",
				UUID:           "11111111-2222-3333-4444-555555555555",
				FunctionARN:    functionARN,
				EventSourceARN: "arn:aws:sqs:us-east-1:123456789012:api-events",
				State:          "Enabled",
				BatchSize:      10,
			}},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	assertResourceType(t, envelopes, awscloud.ResourceTypeLambdaFunction)
	assertResourceType(t, envelopes, awscloud.ResourceTypeLambdaAlias)
	assertResourceType(t, envelopes, awscloud.ResourceTypeLambdaEventSourceMapping)
	assertRelationship(t, envelopes, awscloud.RelationshipLambdaAliasTargetsFunction)
	assertRelationship(t, envelopes, awscloud.RelationshipLambdaEventSourceMappingTargetsFunction)
	assertRelationship(t, envelopes, awscloud.RelationshipLambdaFunctionUsesImage)
	assertRelationship(t, envelopes, awscloud.RelationshipLambdaFunctionUsesExecutionRole)
	assertRelationship(t, envelopes, awscloud.RelationshipLambdaFunctionUsesSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipLambdaFunctionUsesSecurityGroup)

	function := resourceByType(t, envelopes, awscloud.ResourceTypeLambdaFunction)
	attributes := attributesOf(t, function)
	env, ok := attributes["environment"].(map[string]any)
	if !ok {
		t.Fatalf("environment = %#v, want map", attributes["environment"])
	}
	redacted, ok := env["DATABASE_URL"].(map[string]any)
	if !ok {
		t.Fatalf("redacted env value = %#v, want redaction map", env["DATABASE_URL"])
	}
	marker, ok := redacted["marker"].(string)
	if !ok {
		t.Fatalf("redacted env marker = %#v, want string", redacted["marker"])
	}
	if !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("redacted env marker = %#v, want hmac-sha256 marker", marker)
	}
	if strings.Contains(marker, "postgres://") {
		t.Fatalf("redacted env marker leaked raw value: %q", marker)
	}
	if got := redacted["ruleset_version"]; got != awscloud.RedactionPolicyVersion {
		t.Fatalf("redacted env ruleset_version = %q, want %q", got, awscloud.RedactionPolicyVersion)
	}
	if got := redacted["reason"]; got != redact.ReasonKnownSensitiveKey {
		t.Fatalf("redacted env reason = %q, want %q", got, redact.ReasonKnownSensitiveKey)
	}
	logLevel, ok := env["LOG_LEVEL"].(map[string]any)
	if !ok {
		t.Fatalf("LOG_LEVEL redacted env value = %#v, want redaction map", env["LOG_LEVEL"])
	}
	if got := logLevel["reason"]; got != redact.ReasonUnknownProviderSchema {
		t.Fatalf("LOG_LEVEL reason = %q, want %q", got, redact.ReasonUnknownProviderSchema)
	}
	if got := logLevel["ruleset_version"]; got != awscloud.RedactionPolicyVersion {
		t.Fatalf("LOG_LEVEL ruleset_version = %q, want %q", got, awscloud.RedactionPolicyVersion)
	}
	if got := attributes["image_uri"]; got != imageURI {
		t.Fatalf("image_uri = %#v, want %q", got, imageURI)
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	key, err := redact.NewKey([]byte("lambda-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECS
	_, err = Scanner{Client: fakeClient{}, RedactionKey: key}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceLambda,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:lambda:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	functions           []Function
	aliases             map[string][]Alias
	eventSourceMappings map[string][]EventSourceMapping
}

func (c fakeClient) ListFunctions(context.Context) ([]Function, error) {
	return c.functions, nil
}

func (c fakeClient) ListAliases(_ context.Context, function Function) ([]Alias, error) {
	return c.aliases[function.ARN], nil
}

func (c fakeClient) ListEventSourceMappings(_ context.Context, function Function) ([]EventSourceMapping, error) {
	return c.eventSourceMappings[function.ARN], nil
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) {
	t.Helper()
	_ = resourceByType(t, envelopes, resourceType)
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
