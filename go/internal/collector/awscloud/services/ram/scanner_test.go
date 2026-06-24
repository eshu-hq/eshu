// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ram_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ram"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

type fakeClient struct {
	shares []ram.ResourceShare
	err    error
}

func (f fakeClient) ListResourceShares(context.Context) ([]ram.ResourceShare, error) {
	return f.shares, f.err
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceRAM,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ram:1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
	}
}

func fullShare() ram.ResourceShare {
	return ram.ResourceShare{
		ARN:                     "ram-arn:share/orders",
		Name:                    "orders-share",
		Status:                  "ACTIVE",
		StatusMessage:           "ok",
		OwningAccountID:         "123456789012",
		AllowExternalPrincipals: true,
		FeatureSet:              "STANDARD",
		CreationTime:            time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		LastUpdatedTime:         time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC),
		Tags:                    map[string]string{"Environment": "prod"},
		Resources: []ram.SharedResource{
			{ARN: "ec2-arn:subnet/subnet-abc", Type: "ec2:subnet", Status: "AVAILABLE", RegionScope: "REGIONAL"},
		},
		Principals: []ram.Principal{
			{ID: "210987654321", External: false},
			{ID: "org-arn:ou/o-abc/ou-abc-1", External: false},
			{ID: "org-arn:organization/o-abc", External: false},
		},
		Permissions: []ram.Permission{
			{ARN: "ram-arn:permission/subnet", Name: "AWSRAMDefaultPermissionSubnet", Version: "3", PermissionType: "AWS_MANAGED", ResourceType: "ec2:subnet", Status: "ATTACHABLE", DefaultVersion: true},
		},
	}
}

func collectByType(t *testing.T, envelopes []facts.Envelope) (resources, relationships int) {
	t.Helper()
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.AWSResourceFactKind:
			resources++
		case facts.AWSRelationshipFactKind:
			relationships++
		default:
			t.Fatalf("unexpected fact kind %q", envelope.FactKind)
		}
	}
	return resources, relationships
}

// relationshipView is the subset of a relationship payload the scanner tests
// assert on.
type relationshipView struct {
	RelationshipType string
	SourceResourceID string
	TargetResourceID string
	TargetARN        string
	TargetType       string
}

func relationshipFromEnvelope(t *testing.T, envelope facts.Envelope) relationshipView {
	t.Helper()
	if envelope.FactKind != facts.AWSRelationshipFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSRelationshipFactKind)
	}
	return relationshipView{
		RelationshipType: payloadString(envelope, "relationship_type"),
		SourceResourceID: payloadString(envelope, "source_resource_id"),
		TargetResourceID: payloadString(envelope, "target_resource_id"),
		TargetARN:        payloadString(envelope, "target_arn"),
		TargetType:       payloadString(envelope, "target_type"),
	}
}

func findRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) relationshipView {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if payloadString(envelope, "relationship_type") == relationshipType {
			return relationshipFromEnvelope(t, envelope)
		}
	}
	t.Fatalf("no relationship of type %q emitted", relationshipType)
	return relationshipView{}
}

func TestScanEmitsShareResourcePrincipalAndPermissionFacts(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resources, relationships := collectByType(t, envelopes)
	// One share resource + one permission resource.
	if resources != 2 {
		t.Fatalf("resources = %d, want 2", resources)
	}
	// One share-to-resource, three share-to-principal, one share-to-permission.
	if relationships != 5 {
		t.Fatalf("relationships = %d, want 5", relationships)
	}
}

func TestScanShareResourceTargetsSharedResourceArnAndType(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := findRelationship(t, envelopes, awscloud.RelationshipRAMShareIncludesResource)
	if rel.TargetResourceID != "ec2-arn:subnet/subnet-abc" {
		t.Fatalf("TargetResourceID = %q, want shared resource ARN", rel.TargetResourceID)
	}
	if rel.TargetType != "ec2:subnet" {
		t.Fatalf("TargetType = %q, want ec2:subnet", rel.TargetType)
	}
	if rel.SourceResourceID != "ram-arn:share/orders" {
		t.Fatalf("SourceResourceID = %q, want share ARN", rel.SourceResourceID)
	}
}

func TestScanPrincipalAccountTargetsBareAccountID(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := findRelationship(t, envelopes, awscloud.RelationshipRAMShareTargetsAccount)
	if rel.TargetResourceID != "210987654321" {
		t.Fatalf("TargetResourceID = %q, want bare account id 210987654321", rel.TargetResourceID)
	}
	if rel.TargetType != awscloud.ResourceTypeOrganizationsAccount {
		t.Fatalf("TargetType = %q, want %q", rel.TargetType, awscloud.ResourceTypeOrganizationsAccount)
	}
	if rel.TargetARN != "" {
		t.Fatalf("TargetARN = %q, want empty for bare account id", rel.TargetARN)
	}
}

func TestScanPrincipalOrganizationalUnitTargetsOUArn(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := findRelationship(t, envelopes, awscloud.RelationshipRAMShareTargetsOrganizationalUnit)
	if rel.TargetResourceID != "org-arn:ou/o-abc/ou-abc-1" {
		t.Fatalf("TargetResourceID = %q, want OU ARN", rel.TargetResourceID)
	}
	if rel.TargetType != awscloud.ResourceTypeOrganizationsOrganizationalUnit {
		t.Fatalf("TargetType = %q, want %q", rel.TargetType, awscloud.ResourceTypeOrganizationsOrganizationalUnit)
	}
	if rel.TargetARN != "org-arn:ou/o-abc/ou-abc-1" {
		t.Fatalf("TargetARN = %q, want OU ARN", rel.TargetARN)
	}
}

func TestScanPrincipalOrganizationTargetsOrganizationArn(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := findRelationship(t, envelopes, awscloud.RelationshipRAMShareTargetsOrganization)
	if rel.TargetResourceID != "org-arn:organization/o-abc" {
		t.Fatalf("TargetResourceID = %q, want organization ARN", rel.TargetResourceID)
	}
	if rel.TargetType != awscloud.ResourceTypeOrganizationsRoot {
		t.Fatalf("TargetType = %q, want %q", rel.TargetType, awscloud.ResourceTypeOrganizationsRoot)
	}
}

func TestScanSharePermissionTargetsPermissionArn(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := findRelationship(t, envelopes, awscloud.RelationshipRAMShareUsesPermission)
	if rel.TargetResourceID != "ram-arn:permission/subnet" {
		t.Fatalf("TargetResourceID = %q, want permission ARN", rel.TargetResourceID)
	}
	if rel.TargetType != awscloud.ResourceTypeRAMPermission {
		t.Fatalf("TargetType = %q, want %q", rel.TargetType, awscloud.ResourceTypeRAMPermission)
	}
}

func TestScanEveryRelationshipHasNonEmptyTargetTypeAndJoinKey(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		rel := relationshipFromEnvelope(t, envelope)
		if rel.TargetType == "" {
			t.Fatalf("relationship %q has empty target_type", rel.RelationshipType)
		}
		if rel.TargetResourceID == "" {
			t.Fatalf("relationship %q has empty target_resource_id join key", rel.RelationshipType)
		}
		if rel.SourceResourceID == "" {
			t.Fatalf("relationship %q has empty source_resource_id join key", rel.RelationshipType)
		}
	}
}

func TestScanDeduplicatesPermissionResourceAcrossShares(t *testing.T) {
	shareA := fullShare()
	shareB := fullShare()
	shareB.ARN = "ram-arn:share/other"
	shareB.Name = "other-share"
	shareB.Resources = nil
	shareB.Principals = nil
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{shareA, shareB}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	permissionResources := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if resourceTypeFromEnvelope(t, envelope) == awscloud.ResourceTypeRAMPermission {
			permissionResources++
		}
	}
	if permissionResources != 1 {
		t.Fatalf("permission resources = %d, want 1 (deduplicated across shares)", permissionResources)
	}
}

func TestScanShareResourceWithBlankTypeFallsBackToGenericTargetType(t *testing.T) {
	share := fullShare()
	share.Resources = []ram.SharedResource{{ARN: "ec2-arn:subnet/subnet-blank", Type: "  ", Status: "AVAILABLE"}}
	share.Principals = nil
	share.Permissions = nil
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{share}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := findRelationship(t, envelopes, awscloud.RelationshipRAMShareIncludesResource)
	if rel.TargetType != awscloud.ResourceTypeGeneric {
		t.Fatalf("TargetType = %q, want generic %q for blank RAM type", rel.TargetType, awscloud.ResourceTypeGeneric)
	}
	if rel.TargetResourceID != "ec2-arn:subnet/subnet-blank" {
		t.Fatalf("TargetResourceID = %q, want shared resource ARN", rel.TargetResourceID)
	}
	// The blank RAM-reported type must not be carried as a false resource_type.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if payloadString(envelope, "relationship_type") != awscloud.RelationshipRAMShareIncludesResource {
			continue
		}
		if got := payloadAttribute(t, envelope, "resource_type"); got != "" {
			t.Fatalf("attribute resource_type = %q, want empty for blank RAM type", got)
		}
	}
}

func TestScanUnknownPrincipalDoesNotMasqueradeAsAccount(t *testing.T) {
	share := fullShare()
	share.Resources = nil
	share.Permissions = nil
	// A service principal is neither a bare account id, an OU ARN, nor an
	// organization/root ARN. It must not be recorded as an account edge.
	share.Principals = []ram.Principal{{ID: "ram.amazonaws.com", External: false}}
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{share}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relType := payloadString(envelope, "relationship_type")
		if relType == awscloud.RelationshipRAMShareTargetsAccount {
			t.Fatalf("unknown principal %q emitted account edge %q", "ram.amazonaws.com", relType)
		}
		if relType == awscloud.RelationshipRAMShareTargetsPrincipal {
			rel := relationshipFromEnvelope(t, envelope)
			if rel.TargetType != awscloud.ResourceTypeGeneric {
				t.Fatalf("TargetType = %q, want generic %q for unknown principal", rel.TargetType, awscloud.ResourceTypeGeneric)
			}
			if rel.TargetResourceID != "ram.amazonaws.com" {
				t.Fatalf("TargetResourceID = %q, want raw principal id", rel.TargetResourceID)
			}
			if rel.TargetARN != "" {
				t.Fatalf("TargetARN = %q, want empty for unknown principal", rel.TargetARN)
			}
		}
	}
	// And the distinct generic principal edge must exist.
	findRelationship(t, envelopes, awscloud.RelationshipRAMShareTargetsPrincipal)
}

func TestScanShareWithBlankArnFallsBackToName(t *testing.T) {
	share := fullShare()
	share.ARN = "  "
	share.Name = "orders-share"
	share.Resources = nil
	share.Principals = nil
	share.Permissions = nil
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{share}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil (blank ARN must fall back to name)", err)
	}
	resources, _ := collectByType(t, envelopes)
	if resources != 1 {
		t.Fatalf("resources = %d, want 1 share resource keyed by name", resources)
	}
	var shareResource facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSResourceFactKind {
			shareResource = envelope
		}
	}
	if got := payloadString(shareResource, "resource_id"); got != "orders-share" {
		t.Fatalf("resource_id = %q, want name fallback orders-share", got)
	}
}

func TestScanShareWithBlankArnAndNameIsSkipped(t *testing.T) {
	share := fullShare()
	share.ARN = "  "
	share.Name = "  "
	share.Resources = nil
	share.Principals = nil
	share.Permissions = nil
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{share}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil (share with no identity must be skipped)", err)
	}
	resources, relationships := collectByType(t, envelopes)
	if resources != 0 || relationships != 0 {
		t.Fatalf("resources/relationships = %d/%d, want 0/0 for a share with no identity", resources, relationships)
	}
}

func TestScanSkipsResourceAndPermissionEdgesWithEmptyArn(t *testing.T) {
	share := fullShare()
	share.Resources = []ram.SharedResource{{ARN: "", Type: "ec2:subnet"}}
	share.Principals = []ram.Principal{{ID: ""}}
	share.Permissions = []ram.Permission{{ARN: "", Name: "blank"}}
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{share}}}

	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resources, relationships := collectByType(t, envelopes)
	if resources != 1 {
		t.Fatalf("resources = %d, want 1 (share only, blank permission ARN skipped)", resources)
	}
	if relationships != 0 {
		t.Fatalf("relationships = %d, want 0 (all blank join keys skipped)", relationships)
	}
}

func TestScanRequiresClient(t *testing.T) {
	scanner := ram.Scanner{}
	if _, err := scanner.Scan(context.Background(), testBoundary()); err == nil {
		t.Fatal("Scan() error = nil, want client-required error")
	}
}

func TestScanRejectsForeignServiceKind(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{}}
	boundary := testBoundary()
	boundary.ServiceKind = "sqs"
	if _, err := scanner.Scan(context.Background(), boundary); err == nil {
		t.Fatal("Scan() error = nil, want service_kind mismatch error")
	}
}

func TestScanDefaultsServiceKindWhenEmpty(t *testing.T) {
	scanner := ram.Scanner{Client: fakeClient{shares: []ram.ResourceShare{fullShare()}}}
	boundary := testBoundary()
	boundary.ServiceKind = ""
	if _, err := scanner.Scan(context.Background(), boundary); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
}

func TestScanWrapsClientError(t *testing.T) {
	sentinel := errors.New("boom")
	scanner := ram.Scanner{Client: fakeClient{err: sentinel}}
	_, err := scanner.Scan(context.Background(), testBoundary())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Scan() error = %v, want wrapped %v", err, sentinel)
	}
}
