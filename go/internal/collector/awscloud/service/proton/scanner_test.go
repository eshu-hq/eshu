// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proton

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testEnvironmentARN     = "arn:aws:proton:us-east-1:123456789012:environment/prod"
	testServiceARN         = "arn:aws:proton:us-east-1:123456789012:service/orders"
	testEnvTemplateARN     = "arn:aws:proton:us-east-1:123456789012:environment-template/fargate-env"
	testServiceTemplateARN = "arn:aws:proton:us-east-1:123456789012:service-template/lb-web"
	testRoleARN            = "arn:aws:iam::123456789012:role/proton-service-role"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		Environments: []Environment{{
			ARN:                  testEnvironmentARN,
			Name:                 "prod",
			TemplateName:         "fargate-env",
			TemplateMajorVersion: "1",
			TemplateMinorVersion: "0",
			Provisioning:         "CUSTOMER_MANAGED",
			DeploymentStatus:     "SUCCEEDED",
			Description:          "production environment",
			ProtonServiceRoleArn: testRoleARN,
			CreatedAt:            time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:                 map[string]string{"Environment": "prod"},
		}},
		Services: []Service{{
			ARN:                     testServiceARN,
			Name:                    "orders",
			TemplateName:            "lb-web",
			Status:                  "ACTIVE",
			Description:             "orders service",
			BranchName:              "main",
			RepositoryID:            "acme/orders",
			RepositoryConnectionArn: "arn:aws:codestar-connections:us-east-1:123456789012:connection/abc",
			CreatedAt:               time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
			Tags:                    map[string]string{"Team": "checkout"},
		}},
		EnvironmentTemplates: []Template{{
			ARN:                testEnvTemplateARN,
			Name:               "fargate-env",
			DisplayName:        "Fargate Environment",
			Provisioning:       "CUSTOMER_MANAGED",
			RecommendedVersion: "1.0",
		}},
		ServiceTemplates: []Template{{
			ARN:                testServiceTemplateARN,
			Name:               "lb-web",
			DisplayName:        "Load Balanced Web",
			Provisioning:       "CUSTOMER_MANAGED",
			RecommendedVersion: "2.1",
		}},
		ServicePlacements: []ServicePlacement{
			{ServiceName: "orders", EnvironmentName: "prod"},
			// Duplicate placement (second instance in same environment) must
			// collapse to a single edge.
			{ServiceName: "orders", EnvironmentName: "prod"},
		},
	}
}

func TestScannerEmitsProtonMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	environment := resourceByType(t, envelopes, awscloud.ResourceTypeProtonEnvironment)
	if got, want := environment.Payload["resource_id"], testEnvironmentARN; got != want {
		t.Fatalf("environment resource_id = %#v, want %q", got, want)
	}
	envAttrs := attributesOf(t, environment)
	assertAttribute(t, envAttrs, "template_name", "fargate-env")
	assertAttribute(t, envAttrs, "provisioning", "CUSTOMER_MANAGED")

	service := resourceByType(t, envelopes, awscloud.ResourceTypeProtonService)
	if got, want := service.Payload["resource_id"], testServiceARN; got != want {
		t.Fatalf("service resource_id = %#v, want %q", got, want)
	}
	svcAttrs := attributesOf(t, service)
	assertAttribute(t, svcAttrs, "template_name", "lb-web")
	assertAttribute(t, svcAttrs, "repository_id", "acme/orders")

	envTemplate := resourceByType(t, envelopes, awscloud.ResourceTypeProtonEnvironmentTemplate)
	if got, want := envTemplate.Payload["resource_id"], testEnvTemplateARN; got != want {
		t.Fatalf("environment template resource_id = %#v, want %q", got, want)
	}
	svcTemplate := resourceByType(t, envelopes, awscloud.ResourceTypeProtonServiceTemplate)
	if got, want := svcTemplate.Payload["resource_id"], testServiceTemplateARN; got != want {
		t.Fatalf("service template resource_id = %#v, want %q", got, want)
	}

	// environment -> IAM role edge, keyed by the role ARN the IAM scanner publishes.
	envRole := relationshipByType(t, envelopes, awscloud.RelationshipProtonEnvironmentUsesRole)
	assertEdgeTarget(t, envRole, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got, want := envRole.Payload["source_resource_id"], testEnvironmentARN; got != want {
		t.Fatalf("environment->role source_resource_id = %#v, want %q", got, want)
	}
	if got, want := envRole.Payload["target_arn"], testRoleARN; got != want {
		t.Fatalf("environment->role target_arn = %#v, want %q", got, want)
	}

	// service -> environment edge, keyed by the environment ARN the environment
	// node publishes, and deduped to exactly one.
	placements := relationshipsByType(envelopes, awscloud.RelationshipProtonServiceInEnvironment)
	if len(placements) != 1 {
		t.Fatalf("service-in-environment edges = %d, want 1 (duplicate instances must collapse)", len(placements))
	}
	assertEdgeTarget(t, placements[0], awscloud.ResourceTypeProtonEnvironment, testEnvironmentARN)
	if got, want := placements[0].Payload["source_resource_id"], testServiceARN; got != want {
		t.Fatalf("service->environment source_resource_id = %#v, want %q", got, want)
	}
	if got, want := placements[0].Payload["target_arn"], testEnvironmentARN; got != want {
		t.Fatalf("service->environment target_arn = %#v, want %q", got, want)
	}

	// No spec/template body or input parameter leakage anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"spec", "service_spec", "pipeline_spec", "schema", "template_schema",
			"input_parameters", "inputs", "parameters", "manifest", "body",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Proton scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerOmitsServiceEnvironmentEdgeForUnknownEnvironment(t *testing.T) {
	snapshot := fullSnapshot()
	snapshot.ServicePlacements = []ServicePlacement{
		{ServiceName: "orders", EnvironmentName: "shared-account-env"},
	}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := relationshipsByType(envelopes, awscloud.RelationshipProtonServiceInEnvironment); len(got) != 0 {
		t.Fatalf("service-in-environment edges = %d, want 0 (unresolved environment must skip, not dangle)", len(got))
	}
}

func TestScannerOmitsRoleEdgeForNonARNRole(t *testing.T) {
	snapshot := fullSnapshot()
	snapshot.Environments[0].ProtonServiceRoleArn = "proton-service-role"
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := relationshipsByType(envelopes, awscloud.RelationshipProtonEnvironmentUsesRole); len(got) != 0 {
		t.Fatalf("environment-uses-role edges = %d, want 0 for non-ARN role identifier", len(got))
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Environments: []Environment{{
		ARN:  testEnvironmentARN,
		Name: "prod",
		// No service role, no placements: no edges.
	}}}}
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

func TestScannerHandlesEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() on empty account returned %d envelopes, want 0", len(envelopes))
	}
}

func TestScannerSynthesizesGovCloudRoleEdge(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govRole := "arn:aws-us-gov:iam::123456789012:role/proton-service-role"
	govEnvARN := "arn:aws-us-gov:proton:us-gov-west-1:123456789012:environment/prod"
	client := fakeClient{snapshot: Snapshot{Environments: []Environment{{
		ARN:                  govEnvARN,
		Name:                 "prod",
		ProtonServiceRoleArn: govRole,
	}}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	envRole := relationshipByType(t, envelopes, awscloud.RelationshipProtonEnvironmentUsesRole)
	if got := envRole.Payload["target_arn"]; got != govRole {
		t.Fatalf("GovCloud environment->role target_arn = %#v, want %q", got, govRole)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	environment := Environment{ARN: testEnvironmentARN, Name: "prod", ProtonServiceRoleArn: testRoleARN}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		environmentRoleRelationship(boundary, environment),
		serviceInEnvironmentRelationship(boundary, testServiceARN, testServiceARN, testEnvironmentARN),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Environments: []Environment{{ARN: testEnvironmentARN, Name: "prod"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Proton ListServices throttled after SDK retries; service metadata omitted for this scan",
			SourceRecordID: "proton_services_throttled",
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
		ServiceKind:         awscloud.ServiceProton,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:proton:1",
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
