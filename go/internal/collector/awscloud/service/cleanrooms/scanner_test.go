// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanrooms

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testCollaborationARN = "arn:aws:cleanrooms:us-east-1:123456789012:collaboration/c1d2e3f4"
	testConfiguredARN    = "arn:aws:cleanrooms:us-east-1:123456789012:configuredtable/t1a2b3c4"
	testMembershipARN    = "arn:aws:cleanrooms:us-east-1:123456789012:membership/m1n2o3p4"
)

func TestScannerEmitsCleanRoomsMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Collaborations: []Collaboration{{
			ARN:                testCollaborationARN,
			ID:                 "c1d2e3f4",
			Name:               "ad-attribution",
			CreatorAccountID:   "123456789012",
			CreatorDisplayName: "Publisher",
			MemberStatus:       "ACTIVE",
			AnalyticsEngine:    "SPARK",
			CreateTime:         time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:               map[string]string{"Environment": "prod"},
		}},
		ConfiguredTables: []ConfiguredTable{{
			ARN:                testConfiguredARN,
			ID:                 "t1a2b3c4",
			Name:               "impressions",
			AnalysisMethod:     "DIRECT_QUERY",
			AnalysisRuleTypes:  []string{"AGGREGATION"},
			AllowedColumnCount: 3,
			TableReferenceKind: "glue",
			GlueDatabaseName:   "analytics",
			GlueTableName:      "impressions",
			CreateTime:         time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
			Tags:               map[string]string{"Team": "data"},
		}},
		Memberships: []Membership{{
			ARN:                           testMembershipARN,
			ID:                            "m1n2o3p4",
			CollaborationARN:              testCollaborationARN,
			CollaborationID:               "c1d2e3f4",
			CollaborationName:             "ad-attribution",
			CollaborationCreatorAccountID: "123456789012",
			MemberAbilities:               []string{"CAN_QUERY"},
			Status:                        "ACTIVE",
			CreateTime:                    time.Date(2026, 5, 14, 12, 10, 0, 0, time.UTC),
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	collaboration := resourceByType(t, envelopes, awscloud.ResourceTypeCleanRoomsCollaboration)
	if got, want := collaboration.Payload["resource_id"], testCollaborationARN; got != want {
		t.Fatalf("collaboration resource_id = %#v, want %q", got, want)
	}
	collabAttrs := attributesOf(t, collaboration)
	assertAttribute(t, collabAttrs, "analytics_engine", "SPARK")
	assertAttribute(t, collabAttrs, "creator_account_id", "123456789012")

	table := resourceByType(t, envelopes, awscloud.ResourceTypeCleanRoomsConfiguredTable)
	if got, want := table.Payload["resource_id"], testConfiguredARN; got != want {
		t.Fatalf("configured table resource_id = %#v, want %q", got, want)
	}
	tableAttrs := attributesOf(t, table)
	assertAttribute(t, tableAttrs, "analysis_method", "DIRECT_QUERY")
	assertAttribute(t, tableAttrs, "allowed_column_count", 3)
	assertAttribute(t, tableAttrs, "table_reference_kind", "glue")

	membership := resourceByType(t, envelopes, awscloud.ResourceTypeCleanRoomsMembership)
	if got, want := membership.Payload["resource_id"], testMembershipARN; got != want {
		t.Fatalf("membership resource_id = %#v, want %q", got, want)
	}

	// configured table -> Glue table edge, keyed by the "<database>/<table>"
	// resource_id the Glue scanner publishes for a table node.
	tableGlue := relationshipByType(t, envelopes, awscloud.RelationshipCleanRoomsConfiguredTableUsesGlueTable)
	assertEdgeTarget(t, tableGlue, awscloud.ResourceTypeGlueTable, "analytics/impressions")
	if got, want := tableGlue.Payload["source_resource_id"], testConfiguredARN; got != want {
		t.Fatalf("table->glue source_resource_id = %#v, want %q", got, want)
	}
	if got := tableGlue.Payload["target_arn"]; got != "" {
		t.Fatalf("table->glue target_arn = %#v, want empty (Glue table uses name-keyed resource_id)", got)
	}

	// membership -> collaboration internal edge, keyed by the collaboration ARN.
	memberEdge := relationshipByType(t, envelopes, awscloud.RelationshipCleanRoomsMembershipInCollaboration)
	assertEdgeTarget(t, memberEdge, awscloud.ResourceTypeCleanRoomsCollaboration, testCollaborationARN)
	if got, want := memberEdge.Payload["source_resource_id"], testMembershipARN; got != want {
		t.Fatalf("membership->collaboration source_resource_id = %#v, want %q", got, want)
	}
	if got, want := memberEdge.Payload["target_arn"], testCollaborationARN; got != want {
		t.Fatalf("membership->collaboration target_arn = %#v, want %q", got, want)
	}

	// No analysis-rule SQL, query bodies, allowed-column names, or secrets anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"allowed_columns", "columns", "analysis_rule", "analysis_rules",
			"query", "query_string", "sql", "secret_arn", "secret",
			"account_identifier", "results", "protected_query",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Clean Rooms scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSkipsGlueEdgeForNonGlueTableReference(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ConfiguredTables: []ConfiguredTable{{
		ARN:                testConfiguredARN,
		ID:                 "t1a2b3c4",
		Name:               "snowflake-table",
		TableReferenceKind: "snowflake",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship for non-Glue configured table: %#v", envelope.Payload)
		}
	}
}

func TestScannerSkipsGlueEdgeWhenGlueNameMissing(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ConfiguredTables: []ConfiguredTable{{
		ARN:                testConfiguredARN,
		ID:                 "t1a2b3c4",
		Name:               "partial-glue",
		TableReferenceKind: "glue",
		GlueDatabaseName:   "analytics",
		// GlueTableName missing: no edge, never dangle.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship when Glue table name missing: %#v", envelope.Payload)
		}
	}
}

func TestScannerKeysGlueEdgeWithTableNameOnly(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ConfiguredTables: []ConfiguredTable{{
		ARN:                testConfiguredARN,
		ID:                 "t1a2b3c4",
		TableReferenceKind: "Glue", // case-insensitive match
		GlueTableName:      "events",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipCleanRoomsConfiguredTableUsesGlueTable)
	assertEdgeTarget(t, edge, awscloud.ResourceTypeGlueTable, "events")
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	table := ConfiguredTable{
		ARN:                testConfiguredARN,
		ID:                 "t1a2b3c4",
		TableReferenceKind: "glue",
		GlueDatabaseName:   "analytics",
		GlueTableName:      "impressions",
	}
	membership := Membership{
		ARN:              testMembershipARN,
		ID:               "m1n2o3p4",
		CollaborationARN: testCollaborationARN,
	}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		configuredTableGlueRelationship(boundary, table),
		membershipCollaborationRelationship(boundary, membership),
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
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Collaborations: []Collaboration{{ARN: testCollaborationARN, ID: "c1d2e3f4", Name: "ad"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Clean Rooms ListMemberships throttled after SDK retries; membership metadata omitted for this scan",
			SourceRecordID: "cleanrooms_memberships_throttled",
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
		ServiceKind:         awscloud.ServiceCleanRooms,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cleanrooms:1",
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
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
