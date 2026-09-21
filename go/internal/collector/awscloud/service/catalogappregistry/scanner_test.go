// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalogappregistry

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testApplicationARN    = "arn:aws:servicecatalog:us-east-1:123456789012:/applications/app-0abc123"
	testAttributeGroupARN = "arn:aws:servicecatalog:us-east-1:123456789012:/attribute-groups/ag-0def456"
	testStackARN          = "arn:aws:cloudformation:us-east-1:123456789012:stack/prod-network/abc-123"
)

func TestScannerEmitsAppRegistryMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		AttributeGroups: []AttributeGroup{{
			ID:             "ag-0def456",
			ARN:            testAttributeGroupARN,
			Name:           "cost-center",
			Description:    "Cost center metadata",
			CreationTime:   time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			LastUpdateTime: time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
			Tags:           map[string]string{"Team": "platform"},
		}},
		Applications: []Application{{
			ID:                 "app-0abc123",
			ARN:                testApplicationARN,
			Name:               "payments",
			Description:        "Payments application portfolio",
			CreationTime:       time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			LastUpdateTime:     time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
			Tags:               map[string]string{"Environment": "prod"},
			AttributeGroupARNs: []string{testAttributeGroupARN},
			AssociatedResources: []AssociatedResource{
				{ARN: testStackARN, Name: "prod-network", ResourceType: "CFN_STACK"},
				{ARN: "TAG-VALUE", Name: "tagged", ResourceType: "RESOURCE_TAG_VALUE"},
			},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Application resource node.
	app := resourceByType(t, envelopes, awscloud.ResourceTypeServiceCatalogAppRegistryApplication)
	if got, want := app.Payload["resource_id"], testApplicationARN; got != want {
		t.Fatalf("application resource_id = %#v, want %q", got, want)
	}
	if got, want := app.Payload["arn"], testApplicationARN; got != want {
		t.Fatalf("application arn = %#v, want %q", got, want)
	}
	appAttrs := attributesOf(t, app)
	assertAttribute(t, appAttrs, "application_id", "app-0abc123")
	assertAttribute(t, appAttrs, "attribute_group_count", 1)
	assertAttribute(t, appAttrs, "associated_resource_count", 2)

	// Attribute group resource node.
	group := resourceByType(t, envelopes, awscloud.ResourceTypeServiceCatalogAppRegistryAttributeGroup)
	if got, want := group.Payload["resource_id"], testAttributeGroupARN; got != want {
		t.Fatalf("attribute group resource_id = %#v, want %q", got, want)
	}
	groupAttrs := attributesOf(t, group)
	assertAttribute(t, groupAttrs, "attribute_group_id", "ag-0def456")

	// application -> attribute group edge, keyed by the group ARN the group node publishes.
	appGroup := relationshipByType(t, envelopes, awscloud.RelationshipServiceCatalogAppRegistryApplicationHasAttributeGroup)
	assertEdgeTarget(t, appGroup, awscloud.ResourceTypeServiceCatalogAppRegistryAttributeGroup, testAttributeGroupARN)
	if got, want := appGroup.Payload["source_resource_id"], testApplicationARN; got != want {
		t.Fatalf("app->group source_resource_id = %#v, want %q", got, want)
	}
	if got, want := appGroup.Payload["target_arn"], testAttributeGroupARN; got != want {
		t.Fatalf("app->group target_arn = %#v, want %q", got, want)
	}

	// application -> CloudFormation stack edge, keyed by the stack ARN the cloudformation scanner publishes.
	appStack := relationshipByType(t, envelopes, awscloud.RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack)
	assertEdgeTarget(t, appStack, awscloud.ResourceTypeCloudFormationStack, testStackARN)
	if got, want := appStack.Payload["source_resource_id"], testApplicationARN; got != want {
		t.Fatalf("app->stack source_resource_id = %#v, want %q", got, want)
	}
	if got, want := appStack.Payload["target_arn"], testStackARN; got != want {
		t.Fatalf("app->stack target_arn = %#v, want %q", got, want)
	}

	// RESOURCE_TAG_VALUE association must not create a dangling stack edge.
	stackEdges := relationshipsByType(envelopes, awscloud.RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack)
	if len(stackEdges) != 1 {
		t.Fatalf("got %d stack edges, want 1 (RESOURCE_TAG_VALUE must be skipped)", len(stackEdges))
	}

	// No attribute-group content body or tag value anywhere in resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"attributes_body", "attribute_content", "content", "metadata_body",
			"tag_value", "resource_details",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; AppRegistry scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceServiceCatalogAppRegistry + "  "
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   "app-0abc123",
		ARN:  testApplicationARN,
		Name: "payments",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceServiceCatalogAppRegistry; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}

func TestScannerSkipsNonStackAssociationEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   "app-0abc123",
		ARN:  testApplicationARN,
		Name: "payments",
		AssociatedResources: []AssociatedResource{
			{ARN: "not-an-arn", ResourceType: "CFN_STACK"},
			{ARN: testApplicationARN, ResourceType: "RESOURCE_TAG_VALUE"},
		},
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

func TestScannerDeduplicatesAttributeGroupEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:                 "app-0abc123",
		ARN:                testApplicationARN,
		Name:               "payments",
		AttributeGroupARNs: []string{testAttributeGroupARN, testAttributeGroupARN},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edges := relationshipsByType(envelopes, awscloud.RelationshipServiceCatalogAppRegistryApplicationHasAttributeGroup)
	if len(edges) != 1 {
		t.Fatalf("got %d attribute-group edges, want 1 (duplicates must be collapsed)", len(edges))
	}
}

func TestScannerSynthesizesGovCloudStackEdge(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govStackARN := "arn:aws-us-gov:cloudformation:us-gov-west-1:123456789012:stack/gov-net/xyz-789"
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   "app-gov",
		ARN:  "arn:aws-us-gov:servicecatalog:us-gov-west-1:123456789012:/applications/app-gov",
		Name: "gov-app",
		AssociatedResources: []AssociatedResource{
			{ARN: govStackARN, ResourceType: "CFN_STACK"},
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	appStack := relationshipByType(t, envelopes, awscloud.RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack)
	if got := appStack.Payload["target_resource_id"]; got != govStackARN {
		t.Fatalf("GovCloud app->stack target_resource_id = %#v, want %q", got, govStackARN)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	application := Application{
		ID:                 "app-0abc123",
		ARN:                testApplicationARN,
		Name:               "payments",
		AttributeGroupARNs: []string{testAttributeGroupARN},
		AssociatedResources: []AssociatedResource{
			{ARN: testStackARN, ResourceType: "CFN_STACK"},
		},
	}
	var observations []awscloud.RelationshipObservation
	observations = append(observations, applicationAttributeGroupRelationships(boundary, application)...)
	observations = append(observations, applicationStackRelationships(boundary, application)...)
	if len(observations) != 2 {
		t.Fatalf("got %d observations, want 2 for fully populated fixture", len(observations))
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
		Applications: []Application{{ID: "app-0abc123", ARN: testApplicationARN, Name: "payments"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "AppRegistry ListAssociatedResources throttled after SDK retries; association metadata omitted for this scan",
			SourceRecordID: "appregistry_associations_throttled",
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
		ServiceKind:         awscloud.ServiceServiceCatalogAppRegistry,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:servicecatalogappregistry:1",
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
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			out = append(out, envelope)
		}
	}
	return out
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
