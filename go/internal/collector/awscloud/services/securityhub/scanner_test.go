// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityhub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsSecurityHubMetadataOnlyFactsAndRelationships(t *testing.T) {
	key, err := redact.NewKey([]byte("securityhub-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	hubARN := "arn:aws:securityhub:us-east-1:123456789012:hub/default"
	standardARN := "arn:aws:securityhub:us-east-1::standards/aws-foundational-security-best-practices/v/1.0.0"
	subscriptionARN := "arn:aws:securityhub:us-east-1:123456789012:subscription/aws-foundational-security-best-practices/v/1.0.0"
	controlARN := "arn:aws:securityhub:us-east-1:123456789012:control/aws-foundational-security-best-practices/v/1.0.0/S3.1"
	actionARN := "arn:aws:securityhub:us-east-1:123456789012:action/custom/escalate"
	insightARN := "arn:aws:securityhub:us-east-1:123456789012:insight/custom/failed-controls"
	client := fakeClient{snapshot: Snapshot{
		Hub: Hub{
			ARN:                     hubARN,
			AutoEnableControls:      true,
			ControlFindingGenerator: "SECURITY_CONTROL",
			SubscribedAt:            time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
			Tags:                    map[string]string{"Environment": "prod"},
			AdministratorAccountID:  "999999999999",
			AdministratorStatus:     "Enabled",
			MemberEnumerationStatus: "administrator",
		},
		Members: []Member{{
			AccountID:       "111122223333",
			AdministratorID: "999999999999",
			Status:          "Enabled",
			InvitedAt:       time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2026, 5, 27, 11, 30, 0, 0, time.UTC),
		}},
		Standards: []Standard{{
			ARN:                     standardARN,
			SubscriptionARN:         subscriptionARN,
			Status:                  "READY",
			ControlsUpdatable:       "READY_FOR_UPDATES",
			StatusReasonCode:        "NONE",
			StandardsInputKeys:      []string{"regions"},
			Tags:                    map[string]string{"Framework": "aws-foundational"},
			ControlFindingGenerator: "SECURITY_CONTROL",
			Controls: []Control{{
				ARN:              controlARN,
				ID:               "S3.1",
				Title:            "S3 Block Public Access setting should be enabled",
				ControlStatus:    "ENABLED",
				SeverityRating:   "HIGH",
				Related:          []string{"CIS 2.1.2"},
				ComplianceCounts: map[string]int64{"FAILED": 7, "PASSED": 2},
			}},
		}},
		ActionTargets: []ActionTarget{{
			ARN:         actionARN,
			Name:        "escalate",
			Description: "page https://internal.example.invalid/hook?token=secret-token",
		}},
		Insights: []Insight{{
			ARN:              insightARN,
			Name:             "Failed controls",
			GroupByAttribute: "ComplianceSecurityControlId",
			ControlIDs:       []string{"S3.1"},
		}},
		FindingCounts: []FindingCount{{
			StandardID:       "aws-foundational-security-best-practices/v/1.0.0",
			ControlID:        "S3.1",
			ComplianceStatus: "FAILED",
			SeverityLabel:    "HIGH",
			WorkflowStatus:   "NEW",
			Count:            7,
		}},
	}}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubHub)
	assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubStandard)
	control := assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubControl)
	assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubMemberAccount)
	action := assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubActionTarget)
	insight := assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubInsight)
	findingCount := assertResourceType(t, envelopes, awscloud.ResourceTypeSecurityHubFindingAggregate)
	assertRelationshipType(t, envelopes, awscloud.RelationshipSecurityHubHubHasMember)
	assertRelationshipType(t, envelopes, awscloud.RelationshipSecurityHubStandardHasControl)
	assertRelationshipType(t, envelopes, awscloud.RelationshipSecurityHubInsightGroupsControl)

	controlAttributes := attributesOf(t, control)
	if got, want := controlAttributes["compliance_counts"], map[string]any{"FAILED": float64(7), "PASSED": float64(2)}; !jsonEqual(got, want) {
		t.Fatalf("control compliance_counts = %#v, want %#v", got, want)
	}
	actionAttributes := attributesOf(t, action)
	description, ok := actionAttributes["description"].(map[string]any)
	if !ok {
		t.Fatalf("action description = %#v, want redaction map", actionAttributes["description"])
	}
	marker, ok := description["marker"].(string)
	if !ok || !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("description marker = %#v, want hmac redaction marker", description["marker"])
	}
	if strings.Contains(mustJSON(t, action), "secret-token") {
		t.Fatalf("action target description leaked raw secret token: %s", mustJSON(t, action))
	}
	insightAttributes := attributesOf(t, insight)
	if got, want := insightAttributes["group_by_attribute"], "ComplianceSecurityControlId"; got != want {
		t.Fatalf("insight group_by_attribute = %#v, want %#v", got, want)
	}
	findingAttributes := attributesOf(t, findingCount)
	if got, want := findingAttributes["count"], int64(7); got != want {
		t.Fatalf("finding aggregate count = %#v, want %#v", got, want)
	}
	for _, forbidden := range []string{
		"i-0abc123private",
		"10.0.0.5",
		"terminate-process",
		"do not page customer",
		"ProductFields",
		"UserDefinedFields",
		"Remediation",
		"Network",
		"Process",
	} {
		if strings.Contains(mustJSON(t, envelopes), forbidden) {
			t.Fatalf("forbidden finding-body value %q leaked into emitted facts: %s", forbidden, mustJSON(t, envelopes))
		}
	}
}

func TestScannerNormalizesFallbackIdentitiesAndSkipsBlankMembers(t *testing.T) {
	key, err := redact.NewKey([]byte("securityhub-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	client := fakeClient{snapshot: Snapshot{
		Hub: Hub{ARN: "arn:aws:securityhub:us-east-1:123456789012:hub/default"},
		Members: []Member{{
			AccountID: "   ",
			Status:    "Enabled",
		}, {
			AccountID: " 111122223333 ",
			Status:    "Enabled",
		}},
		Standards: []Standard{{
			SubscriptionARN: "securityhub_standard:aws-foundational-security-best-practices",
			Controls: []Control{{
				ID: " EC2.1 ",
			}},
		}},
		ActionTargets: []ActionTarget{{
			Name: "  Escalate Findings  ",
		}},
		Insights: []Insight{{
			Name:             "  Failed controls  ",
			GroupByAttribute: " SecurityControlId ",
			ControlIDs:       []string{" EC2.1 "},
		}},
	}}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	assertMissingResourceID(t, envelopes, awscloud.ResourceTypeSecurityHubMemberAccount, "securityhub_member:")
	member := assertResourceID(t, envelopes, awscloud.ResourceTypeSecurityHubMemberAccount, "securityhub_member:111122223333")
	if got, want := member.Payload["name"], "111122223333"; got != want {
		t.Fatalf("member name = %#v, want %#v", got, want)
	}

	actionID := "securityhub_action:Escalate Findings"
	action := assertResourceID(t, envelopes, awscloud.ResourceTypeSecurityHubActionTarget, actionID)
	if got, want := action.Payload["name"], "Escalate Findings"; got != want {
		t.Fatalf("action name = %#v, want %#v", got, want)
	}
	if got := action.SourceRef.SourceRecordID; got != actionID {
		t.Fatalf("action SourceRecordID = %#v, want %#v", got, actionID)
	}
	assertMissingResourceID(t, envelopes, awscloud.ResourceTypeSecurityHubActionTarget, "securityhub_action:  Escalate Findings")
	assertAnchorsDoNotContain(t, action, "  Escalate Findings  ")

	insightID := "securityhub_insight:Failed controls"
	insight := assertResourceID(t, envelopes, awscloud.ResourceTypeSecurityHubInsight, insightID)
	if got, want := insight.Payload["name"], "Failed controls"; got != want {
		t.Fatalf("insight name = %#v, want %#v", got, want)
	}
	if got := insight.SourceRef.SourceRecordID; got != insightID {
		t.Fatalf("insight SourceRecordID = %#v, want %#v", got, insightID)
	}
	assertMissingResourceID(t, envelopes, awscloud.ResourceTypeSecurityHubInsight, "securityhub_insight:  Failed controls")
	assertAnchorsDoNotContain(t, insight, "  Failed controls  ")

	relationship := assertRelationshipType(t, envelopes, awscloud.RelationshipSecurityHubInsightGroupsControl)
	if got := relationship.Payload["source_resource_id"]; got != insightID {
		t.Fatalf("insight relationship source_resource_id = %#v, want %#v", got, insightID)
	}
	if got, want := relationship.SourceRef.SourceRecordID, insightID+"->securityhub_standard:aws-foundational-security-best-practices/EC2.1"; got != want {
		t.Fatalf("insight relationship SourceRecordID = %#v, want %#v", got, want)
	}
}

func TestScannerRejectsMissingRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	key, err := redact.NewKey([]byte("securityhub-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceEventBridge
	_, err = (Scanner{Client: fakeClient{}, RedactionKey: key}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

type fakeClient struct {
	snapshot Snapshot
	err      error
}

func (f fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return f.snapshot, f.err
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSecurityHub,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:securityhub:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	}
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %s", resourceType, mustJSON(t, envelopes))
	return facts.Envelope{}
}

func assertRelationshipType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %s", relationshipType, mustJSON(t, envelopes))
	return facts.Envelope{}
}

func assertResourceID(t *testing.T, envelopes []facts.Envelope, resourceType string, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType && envelope.Payload["resource_id"] == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q resource_id %q in %s", resourceType, resourceID, mustJSON(t, envelopes))
	return facts.Envelope{}
}

func assertMissingResourceID(t *testing.T, envelopes []facts.Envelope, resourceType string, resourceID string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType && envelope.Payload["resource_id"] == resourceID {
			t.Fatalf("unexpected resource_type %q resource_id %q in %s", resourceType, resourceID, mustJSON(t, envelopes))
		}
	}
}

func assertAnchorsDoNotContain(t *testing.T, envelope facts.Envelope, forbidden string) {
	t.Helper()
	anchors, ok := envelope.Payload["correlation_anchors"].([]string)
	if !ok {
		t.Fatalf("correlation_anchors = %#v, want []string", envelope.Payload["correlation_anchors"])
	}
	for _, anchor := range anchors {
		if anchor == forbidden {
			t.Fatalf("correlation_anchors contains unnormalized anchor %q: %#v", forbidden, anchors)
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

func jsonEqual(left any, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(raw)
}
