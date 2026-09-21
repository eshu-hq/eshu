// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amp

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testWorkspaceARN = "arn:aws:aps:us-east-1:123456789012:workspace/ws-12345678-1234-abcd-5678-1234567890ab"
	testWorkspaceID  = "ws-12345678-1234-abcd-5678-1234567890ab"
	testNamespaceARN = "arn:aws:aps:us-east-1:123456789012:rulegroupsnamespace/ws-12345678-1234-abcd-5678-1234567890ab/alerts"
	testScraperARN   = "arn:aws:aps:us-east-1:123456789012:scraper/s-abcd1234-5678-90ab-cdef-1234567890ab"
	testScraperID    = "s-abcd1234-5678-90ab-cdef-1234567890ab"
	testKMSARN       = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testClusterARN   = "arn:aws:eks:us-east-1:123456789012:cluster/prod"
)

func TestScannerEmitsAMPMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Workspaces: []Workspace{{
			ARN:         testWorkspaceARN,
			WorkspaceID: testWorkspaceID,
			Alias:       "platform-metrics",
			Status:      "ACTIVE",
			KMSKeyARN:   testKMSARN,
			CreatedAt:   time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:        map[string]string{"Environment": "prod"},
			RuleGroupsNamespaces: []RuleGroupsNamespace{{
				ARN:        testNamespaceARN,
				Name:       "alerts",
				Status:     "ACTIVE",
				CreatedAt:  time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
				ModifiedAt: time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
				Tags:       map[string]string{"Team": "observability"},
			}},
		}},
		Scrapers: []Scraper{{
			ARN:                     testScraperARN,
			ScraperID:               testScraperID,
			Alias:                   "prod-collector",
			Status:                  "ACTIVE",
			RoleARN:                 "arn:aws:iam::123456789012:role/aps-scraper",
			SourceEKSClusterARN:     testClusterARN,
			DestinationWorkspaceARN: testWorkspaceARN,
			SubnetIDs:               []string{"subnet-aaaa1111", "subnet-bbbb2222"},
			SecurityGroupIDs:        []string{"sg-cccc3333"},
			CreatedAt:               time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC),
			Tags:                    map[string]string{"Team": "observability"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Workspace resource node, keyed by ARN.
	workspace := resourceByType(t, envelopes, awscloud.ResourceTypeAMPWorkspace)
	if got, want := workspace.Payload["resource_id"], testWorkspaceARN; got != want {
		t.Fatalf("workspace resource_id = %#v, want %q", got, want)
	}
	if got, want := workspace.Payload["arn"], testWorkspaceARN; got != want {
		t.Fatalf("workspace arn = %#v, want %q", got, want)
	}
	if got, want := workspace.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("workspace state = %#v, want %q", got, want)
	}
	wsAttrs := attributesOf(t, workspace)
	assertAttribute(t, wsAttrs, "workspace_id", testWorkspaceID)
	assertAttribute(t, wsAttrs, "alias", "platform-metrics")
	assertAttribute(t, wsAttrs, "kms_key_arn", testKMSARN)
	// prometheus_endpoint has no list-only data source: ListWorkspaces returns a
	// WorkspaceSummary that does not carry the endpoint, and the adapter is
	// forbidden from calling DescribeWorkspace, so the scanner must not emit an
	// always-empty attribute.
	if _, exists := wsAttrs["prometheus_endpoint"]; exists {
		t.Fatalf("prometheus_endpoint attribute emitted; no list-only source exists, must be omitted")
	}

	// Rule-groups namespace resource node (name only).
	namespace := resourceByType(t, envelopes, awscloud.ResourceTypeAMPRuleGroupsNamespace)
	if got, want := namespace.Payload["resource_id"], testNamespaceARN; got != want {
		t.Fatalf("namespace resource_id = %#v, want %q", got, want)
	}
	nsAttrs := attributesOf(t, namespace)
	assertAttribute(t, nsAttrs, "namespace_name", "alerts")
	assertAttribute(t, nsAttrs, "workspace_id", testWorkspaceARN)

	// Scraper resource node.
	scraper := resourceByType(t, envelopes, awscloud.ResourceTypeAMPScraper)
	if got, want := scraper.Payload["resource_id"], testScraperARN; got != want {
		t.Fatalf("scraper resource_id = %#v, want %q", got, want)
	}
	scAttrs := attributesOf(t, scraper)
	assertAttribute(t, scAttrs, "scraper_id", testScraperID)
	assertAttribute(t, scAttrs, "subnet_ids", []string{"subnet-aaaa1111", "subnet-bbbb2222"})
	assertAttribute(t, scAttrs, "security_group_ids", []string{"sg-cccc3333"})

	// workspace -> KMS key edge.
	wsKMS := relationshipByType(t, envelopes, awscloud.RelationshipAMPWorkspaceUsesKMSKey)
	assertEdgeTarget(t, wsKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := wsKMS.Payload["source_resource_id"], testWorkspaceARN; got != want {
		t.Fatalf("workspace->kms source_resource_id = %#v, want %q", got, want)
	}
	if got, want := wsKMS.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("workspace->kms target_arn = %#v, want %q", got, want)
	}

	// namespace -> workspace edge, keyed by the workspace ARN the workspace node publishes.
	nsWs := relationshipByType(t, envelopes, awscloud.RelationshipAMPRuleGroupsNamespaceInWorkspace)
	assertEdgeTarget(t, nsWs, awscloud.ResourceTypeAMPWorkspace, testWorkspaceARN)
	if got, want := nsWs.Payload["source_resource_id"], testNamespaceARN; got != want {
		t.Fatalf("namespace->workspace source_resource_id = %#v, want %q", got, want)
	}
	if got, want := nsWs.Payload["target_arn"], testWorkspaceARN; got != want {
		t.Fatalf("namespace->workspace target_arn = %#v, want %q", got, want)
	}

	// scraper -> EKS cluster edge, keyed by the cluster ARN the EKS node publishes.
	scEKS := relationshipByType(t, envelopes, awscloud.RelationshipAMPScraperScrapesEKSCluster)
	assertEdgeTarget(t, scEKS, awscloud.ResourceTypeEKSCluster, testClusterARN)
	if got, want := scEKS.Payload["target_arn"], testClusterARN; got != want {
		t.Fatalf("scraper->eks target_arn = %#v, want %q", got, want)
	}

	// scraper -> workspace edge.
	scWs := relationshipByType(t, envelopes, awscloud.RelationshipAMPScraperSendsToWorkspace)
	assertEdgeTarget(t, scWs, awscloud.ResourceTypeAMPWorkspace, testWorkspaceARN)

	// scraper -> subnet (bare id) and -> security group (bare id) edges.
	scSubnet := relationshipByType(t, envelopes, awscloud.RelationshipAMPScraperUsesSubnet)
	assertEdgeTarget(t, scSubnet, awscloud.ResourceTypeEC2Subnet, "subnet-aaaa1111")
	if got := scSubnet.Payload["target_arn"]; got != "" {
		t.Fatalf("scraper->subnet target_arn = %#v, want empty (bare subnet id)", got)
	}
	scGroup := relationshipByType(t, envelopes, awscloud.RelationshipAMPScraperUsesSecurityGroup)
	assertEdgeTarget(t, scGroup, awscloud.ResourceTypeEC2SecurityGroup, "sg-cccc3333")

	// No data-plane leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"rule_groups_data", "rules", "rule_definitions", "scrape_configuration",
			"scrape_config", "alert_manager_definition", "samples", "metrics",
			"query_results", "time_series",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; AMP scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ARN:         testWorkspaceARN,
		WorkspaceID: testWorkspaceID,
		Status:      "ACTIVE",
		// No KMS key, no namespaces: no KMS edge, no namespace edges.
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

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ARN:         testWorkspaceARN,
		WorkspaceID: testWorkspaceID,
		KMSKeyARN:   "alias/amp-metrics",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	wsKMS := relationshipByType(t, envelopes, awscloud.RelationshipAMPWorkspaceUsesKMSKey)
	if got, want := wsKMS.Payload["target_resource_id"], "alias/amp-metrics"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := wsKMS.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerEmitsScrapersWithoutWorkspaces(t *testing.T) {
	// Scrapers are an account-level list and can exist even when the only
	// workspace they target is in another account/region scan slice. The
	// scraper resource and its edges still emit cleanly.
	client := fakeClient{snapshot: Snapshot{Scrapers: []Scraper{{
		ARN:                     testScraperARN,
		ScraperID:               testScraperID,
		Status:                  "ACTIVE",
		SourceEKSClusterARN:     testClusterARN,
		DestinationWorkspaceARN: testWorkspaceARN,
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	scraper := resourceByType(t, envelopes, awscloud.ResourceTypeAMPScraper)
	if got, want := scraper.Payload["resource_id"], testScraperARN; got != want {
		t.Fatalf("scraper resource_id = %#v, want %q", got, want)
	}
	relationshipByType(t, envelopes, awscloud.RelationshipAMPScraperScrapesEKSCluster)
	relationshipByType(t, envelopes, awscloud.RelationshipAMPScraperSendsToWorkspace)
}

func TestScannerReturnsCleanlyForEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() returned %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	workspace := Workspace{ARN: testWorkspaceARN, WorkspaceID: testWorkspaceID, KMSKeyARN: testKMSARN}
	workspaceID := workspaceResourceID(workspace)
	namespace := RuleGroupsNamespace{ARN: testNamespaceARN, Name: "alerts"}
	scraper := Scraper{
		ARN:                     testScraperARN,
		ScraperID:               testScraperID,
		SourceEKSClusterARN:     testClusterARN,
		DestinationWorkspaceARN: testWorkspaceARN,
		SubnetIDs:               []string{"subnet-aaaa1111"},
		SecurityGroupIDs:        []string{"sg-cccc3333"},
	}
	var observations []awscloud.RelationshipObservation
	if rel := workspaceKMSRelationship(boundary, workspace); rel != nil {
		observations = append(observations, *rel)
	}
	if rel := namespaceInWorkspaceRelationship(boundary, workspaceID, namespace); rel != nil {
		observations = append(observations, *rel)
	}
	observations = append(observations, scraperRelationships(boundary, scraper)...)
	if len(observations) == 0 {
		t.Fatalf("expected non-empty relationship set for fully populated fixture")
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
		Workspaces: []Workspace{{ARN: testWorkspaceARN, WorkspaceID: testWorkspaceID}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "AMP ListRuleGroupsNamespaces throttled after SDK retries; namespace metadata omitted for this scan",
			SourceRecordID: "amp_rule_groups_namespaces_throttled",
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
		ServiceKind:         awscloud.ServiceAMP,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:amp:1",
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
