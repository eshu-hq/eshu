// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package organizations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsOrganizationsMetadataOnlyFactsAndRelationships(t *testing.T) {
	key := testRedactionKey(t)
	policyARN := "arn:aws:organizations::123456789012:policy/o-exampleorgid/service_control_policy/p-abcd1234"
	client := fakeClient{snapshot: Snapshot{
		Organization: Organization{
			ID:                "o-exampleorgid",
			ARN:               "arn:aws:organizations::123456789012:organization/o-exampleorgid",
			ManagementAccount: "123456789012",
			FeatureSet:        "ALL",
		},
		Roots: []Root{{
			ID:   "r-root",
			ARN:  "arn:aws:organizations::123456789012:root/o-exampleorgid/r-root",
			Name: "Root",
			PolicyTypes: []PolicyTypeSummary{{
				Type:   "SERVICE_CONTROL_POLICY",
				Status: "ENABLED",
			}},
			Tags: map[string]string{"Environment": "prod"},
		}},
		OrganizationalUnits: []OrganizationalUnit{{
			ID:       "ou-root-platform",
			ARN:      "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform",
			Name:     "Platform",
			ParentID: "r-root",
			Tags:     map[string]string{"Owner": "platform"},
		}, {
			ID:       "ou-root-platform-payments",
			ARN:      "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform-payments",
			Name:     "Payments",
			ParentID: "ou-root-platform",
		}},
		Accounts: []Account{{
			ID:        "111122223333",
			ARN:       "arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333",
			Email:     "owner@example.com",
			Name:      "payments-prod",
			Status:    "ACTIVE",
			ParentID:  "ou-root-platform-payments",
			JoinedAt:  time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
			JoinedVia: "INVITED",
			Tags:      map[string]string{"Team": "payments"},
		}, {
			ID:       "123456789012",
			ARN:      "arn:aws:organizations::123456789012:account/o-exampleorgid/123456789012",
			Email:    "root@example.com",
			Name:     "management",
			Status:   "ACTIVE",
			ParentID: "r-root",
		}},
		Policies: []Policy{{
			ID:   "p-abcd1234",
			ARN:  policyARN,
			Name: "deny-public-s3",
			Type: "SERVICE_CONTROL_POLICY",
			Targets: []PolicyTarget{{
				ID:   "ou-root-platform-payments",
				ARN:  "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform-payments",
				Name: "Payments",
				Type: "ORGANIZATIONAL_UNIT",
			}, {
				ID:   "111122223333",
				ARN:  "arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333",
				Name: "payments-prod",
				Type: "ACCOUNT",
			}},
			Tags: map[string]string{"Policy": "baseline"},
		}},
		DelegatedAdministrators: []DelegatedAdministrator{{
			AccountID:           "111122223333",
			AccountARN:          "arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333",
			ServicePrincipal:    "config.amazonaws.com",
			DelegationEnabledAt: time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC),
		}},
	}}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	assertResourceType(t, envelopes, awscloud.ResourceTypeOrganizationsRoot)
	assertResourceType(t, envelopes, awscloud.ResourceTypeOrganizationsOrganizationalUnit)
	account := assertResourceType(t, envelopes, awscloud.ResourceTypeOrganizationsAccount)
	assertResourceType(t, envelopes, awscloud.ResourceTypeOrganizationsPolicy)
	delegatedAdmin := assertResourceType(t, envelopes, awscloud.ResourceTypeOrganizationsDelegatedAdministrator)
	assertRelationshipType(t, envelopes, awscloud.RelationshipOrganizationsAccountInOU)
	assertRelationshipType(t, envelopes, awscloud.RelationshipOrganizationsOUInOU)
	assertRelationshipType(t, envelopes, awscloud.RelationshipOrganizationsAccountInRoot)
	assertRelationshipType(t, envelopes, awscloud.RelationshipOrganizationsPolicyTargetsResource)
	delegatedAdminRelationship := assertRelationshipType(
		t,
		envelopes,
		awscloud.RelationshipOrganizationsDelegatedAdminForAccount,
	)

	accountAttrs := attributesOf(t, account)
	for _, field := range []string{"email", "name"} {
		value, ok := accountAttrs[field].(map[string]any)
		if !ok {
			t.Fatalf("account %s = %#v, want redaction payload", field, accountAttrs[field])
		}
		marker, _ := value["marker"].(string)
		if !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
			t.Fatalf("account %s marker = %#v, want HMAC marker", field, marker)
		}
		if strings.Contains(marker, "owner@example.com") || strings.Contains(marker, "payments-prod") {
			t.Fatalf("account %s marker leaked raw account data: %q", field, marker)
		}
	}

	accountARN := "arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333"
	if got := delegatedAdmin.Payload["arn"]; got != "" {
		t.Fatalf("delegated admin arn = %#v, want empty; binding must not reuse account ARN", got)
	}
	delegatedAttrs := attributesOf(t, delegatedAdmin)
	if got := delegatedAttrs["account_arn"]; got != accountARN {
		t.Fatalf("delegated admin account_arn = %#v, want %q", got, accountARN)
	}
	if got := delegatedAdminRelationship.Payload["source_arn"]; got != "" {
		t.Fatalf("delegated admin relationship source_arn = %#v, want empty binding source ARN", got)
	}
	if got := delegatedAdminRelationship.Payload["target_arn"]; got != accountARN {
		t.Fatalf("delegated admin relationship target_arn = %#v, want account ARN", got)
	}

	for _, envelope := range envelopes {
		assertNoPolicyBody(t, envelope)
		assertNoRawValue(t, envelope.Payload, "owner@example.com")
		assertNoRawValue(t, envelope.Payload, "payments-prod")
	}
}

func TestScannerEmitsOrgAccessSkippedWarning(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Warnings: []awscloud.WarningObservation{{
		WarningKind:    awscloud.WarningOrganizationsOrgAccessSkipped,
		ErrorClass:     "org_access_denied",
		Message:        "Organizations metadata scan skipped because credentials are not management or delegated-admin credentials",
		SourceRecordID: "organizations:org-aware-skip",
		Attributes: map[string]any{
			"skip_reason": "org_access_denied",
		},
	}}}}

	envelopes, err := (Scanner{Client: client, RedactionKey: testRedactionKey(t)}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("len(envelopes) = %d, want %d", got, want)
	}
	if got := envelopes[0].Payload["warning_kind"]; got != awscloud.WarningOrganizationsOrgAccessSkipped {
		t.Fatalf("warning_kind = %#v, want %q", got, awscloud.WarningOrganizationsOrgAccessSkipped)
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scan() error = %q, want redaction key", err)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := (Scanner{Client: fakeClient{}, RedactionKey: testRedactionKey(t)}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceOrganizations,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:organizations:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC),
	}
}

func testRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("organizations-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

type fakeClient struct {
	snapshot Snapshot
	err      error
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, c.err
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
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
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
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
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

func assertNoPolicyBody(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, _ := envelope.Payload["attributes"].(map[string]any)
	for _, forbidden := range []string{"content", "document", "policy_document", "statement", "condition", "not_action"} {
		if _, exists := envelope.Payload[forbidden]; exists {
			t.Fatalf("%s persisted at top level in %#v", forbidden, envelope.Payload)
		}
		if attributes != nil {
			if _, exists := attributes[forbidden]; exists {
				t.Fatalf("%s persisted in attributes %#v", forbidden, attributes)
			}
		}
	}
}

func assertNoRawValue(t *testing.T, value any, forbidden string) {
	t.Helper()
	switch typed := value.(type) {
	case string:
		if strings.Contains(typed, forbidden) {
			t.Fatalf("payload leaked %q in %q", forbidden, typed)
		}
	case map[string]any:
		for _, nested := range typed {
			assertNoRawValue(t, nested, forbidden)
		}
	case []any:
		for _, nested := range typed {
			assertNoRawValue(t, nested, forbidden)
		}
	case []string:
		for _, nested := range typed {
			assertNoRawValue(t, nested, forbidden)
		}
	case []map[string]string:
		for _, nested := range typed {
			for _, nestedValue := range nested {
				assertNoRawValue(t, nestedValue, forbidden)
			}
		}
	}
}
