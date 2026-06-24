// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testWorkspaceID  = "g-abcd123456"
	testWorkspaceARN = "arn:aws:grafana:us-east-1:123456789012:/workspaces/g-abcd123456"
	testRoleARN      = "arn:aws:iam::123456789012:role/grafana-workspace-role"
	testSubnetA      = "subnet-0123456789abcdef0"
	testSubnetB      = "subnet-0123456789abcdef1"
	testSecurityGrp  = "sg-0123456789abcdef0"
)

func TestScannerEmitsGrafanaMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ID:                       testWorkspaceID,
		ARN:                      testWorkspaceARN,
		Name:                     "observability",
		Description:              "shared dashboards",
		Status:                   "ACTIVE",
		GrafanaVersion:           "10.4",
		Endpoint:                 "g-abcd123456.grafana-workspace.us-east-1.amazonaws.com",
		AccountAccessType:        "CURRENT_ACCOUNT",
		PermissionType:           "SERVICE_MANAGED",
		WorkspaceRoleARN:         testRoleARN,
		DataSources:              []string{"PROMETHEUS", "CLOUDWATCH"},
		NotificationDestinations: []string{"SNS"},
		AuthenticationProviders:  []string{"AWS_SSO"},
		SubnetIDs:                []string{testSubnetA, testSubnetB},
		SecurityGroupIDs:         []string{testSecurityGrp},
		Created:                  time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		Modified:                 time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		Tags:                     map[string]string{"Environment": "prod"},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Workspace resource node.
	workspace := resourceByType(t, envelopes, awscloud.ResourceTypeGrafanaWorkspace)
	if got, want := workspace.Payload["resource_id"], testWorkspaceARN; got != want {
		t.Fatalf("workspace resource_id = %#v, want %q", got, want)
	}
	if got, want := workspace.Payload["arn"], testWorkspaceARN; got != want {
		t.Fatalf("workspace arn = %#v, want %q", got, want)
	}
	if got, want := workspace.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("workspace state = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, workspace)
	assertAttribute(t, attrs, "workspace_id", testWorkspaceID)
	assertAttribute(t, attrs, "grafana_version", "10.4")
	assertAttribute(t, attrs, "permission_type", "SERVICE_MANAGED")
	assertAttribute(t, attrs, "data_sources", []string{"PROMETHEUS", "CLOUDWATCH"})
	assertAttribute(t, attrs, "authentication_providers", []string{"AWS_SSO"})

	// workspace -> IAM role edge, keyed by the role ARN the IAM scanner publishes.
	iamEdge := relationshipByType(t, envelopes, awscloud.RelationshipGrafanaWorkspaceUsesIAMRole)
	assertEdgeTarget(t, iamEdge, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got, want := iamEdge.Payload["source_resource_id"], testWorkspaceARN; got != want {
		t.Fatalf("workspace->iam source_resource_id = %#v, want %q", got, want)
	}
	if got, want := iamEdge.Payload["target_arn"], testRoleARN; got != want {
		t.Fatalf("workspace->iam target_arn = %#v, want %q", got, want)
	}

	// workspace -> subnet edges, keyed by the bare subnet ids the EC2 scanner publishes.
	subnetEdges := relationshipsByType(t, envelopes, awscloud.RelationshipGrafanaWorkspaceInSubnet)
	if len(subnetEdges) != 2 {
		t.Fatalf("got %d subnet edges, want 2", len(subnetEdges))
	}
	assertEdgeTargets(t, subnetEdges, awscloud.ResourceTypeEC2Subnet, testSubnetA, testSubnetB)

	// workspace -> security group edge, keyed by the bare sg id the EC2 scanner publishes.
	sgEdge := relationshipByType(t, envelopes, awscloud.RelationshipGrafanaWorkspaceUsesSecurityGroup)
	assertEdgeTarget(t, sgEdge, awscloud.ResourceTypeEC2SecurityGroup, testSecurityGrp)
	if got := sgEdge.Payload["target_arn"]; got != "" && got != nil {
		t.Fatalf("workspace->sg target_arn = %#v, want empty (bare id target)", got)
	}

	// No dashboards / alert rules / query results / auth secrets leak into payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"dashboards", "panels", "alert_rules", "query_results",
			"saml_configuration", "saml_assertion", "api_key", "api_keys",
			"service_account_token", "sso_client_secret", "credentials",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Grafana scanner must stay metadata-only and never persist auth secrets", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudWorkspaceARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govARN := "arn:aws-us-gov:grafana:us-gov-west-1:123456789012:/workspaces/g-gov12345"
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ID:               "g-gov12345",
		ARN:              govARN,
		Name:             "gov-observability",
		Status:           "ACTIVE",
		WorkspaceRoleARN: "arn:aws-us-gov:iam::123456789012:role/gov-grafana-role",
		SubnetIDs:        []string{testSubnetA},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	workspace := resourceByType(t, envelopes, awscloud.ResourceTypeGrafanaWorkspace)
	if got := workspace.Payload["resource_id"]; got != govARN {
		t.Fatalf("GovCloud workspace resource_id = %#v, want %q", got, govARN)
	}
	iamEdge := relationshipByType(t, envelopes, awscloud.RelationshipGrafanaWorkspaceUsesIAMRole)
	if got := iamEdge.Payload["source_resource_id"]; got != govARN {
		t.Fatalf("GovCloud workspace->iam source_resource_id = %#v, want %q", got, govARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ID:     testWorkspaceID,
		ARN:    testWorkspaceARN,
		Name:   "no-deps",
		Status: "ACTIVE",
		// No role ARN, no vpcConfiguration: no edges.
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

func TestScannerOmitsIAMRoleEdgeForNonARNRole(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ID:               testWorkspaceID,
		ARN:              testWorkspaceARN,
		Name:             "bad-role",
		WorkspaceRoleARN: "grafana-workspace-role",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipGrafanaWorkspaceUsesIAMRole {
			t.Fatalf("emitted IAM role edge for a non-ARN role identifier; the edge would dangle")
		}
	}
}

func TestScannerDeduplicatesVPCEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ID:               testWorkspaceID,
		ARN:              testWorkspaceARN,
		Name:             "dupes",
		SubnetIDs:        []string{testSubnetA, testSubnetA, " " + testSubnetA + " "},
		SecurityGroupIDs: []string{testSecurityGrp, testSecurityGrp},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := len(relationshipsByType(t, envelopes, awscloud.RelationshipGrafanaWorkspaceInSubnet)); got != 1 {
		t.Fatalf("subnet edges = %d, want 1 after de-dup", got)
	}
	if got := len(relationshipsByType(t, envelopes, awscloud.RelationshipGrafanaWorkspaceUsesSecurityGroup)); got != 1 {
		t.Fatalf("security group edges = %d, want 1 after de-dup", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	workspace := Workspace{
		ID:               testWorkspaceID,
		ARN:              testWorkspaceARN,
		WorkspaceRoleARN: testRoleARN,
		SubnetIDs:        []string{testSubnetA, testSubnetB},
		SecurityGroupIDs: []string{testSecurityGrp},
	}
	observations := workspaceRelationships(boundary, workspace)
	if len(observations) != 4 {
		t.Fatalf("got %d relationships, want 4 (1 role + 2 subnet + 1 sg)", len(observations))
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Workspaces: []Workspace{{ID: testWorkspaceID, ARN: testWorkspaceARN, Name: "observability"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Grafana DescribeWorkspace throttled after SDK retries; workspace metadata omitted for this scan",
			SourceRecordID: "grafana_workspaces_throttled",
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceGrafana,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:grafana:1",
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
	edges := relationshipsByType(t, envelopes, relationshipType)
	if len(edges) == 0 {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
	return edges[0]
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	t.Helper()
	var edges []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			edges = append(edges, envelope)
		}
	}
	return edges
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

func assertEdgeTargets(t *testing.T, envelopes []facts.Envelope, targetType string, wantIDs ...string) {
	t.Helper()
	got := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if tt := envelope.Payload["target_type"]; tt != targetType {
			t.Fatalf("target_type = %#v, want %q", tt, targetType)
		}
		id, _ := envelope.Payload["target_resource_id"].(string)
		got[id] = struct{}{}
	}
	for _, want := range wantIDs {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing edge target %q; got %#v", want, got)
		}
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
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
