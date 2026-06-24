// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssoadmin

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsIdentityCenterMetadataAndRelationships(t *testing.T) {
	instanceARN := "arn:aws:sso:::instance/ssoins-1111111111111111"
	permSetARN := "arn:aws:sso:::permissionSet/ssoins-1111111111111111/ps-2222222222222222"
	appARN := "arn:aws:sso::123456789012:application/ssoins-1111111111111111/apl-3333333333333333"
	issuerARN := "arn:aws:sso::123456789012:trustedTokenIssuer/ssoins-1111111111111111/tti-4444444444444444"
	snapshot := Snapshot{
		Instances: []Instance{{
			ARN:             instanceARN,
			IdentityStoreID: "d-9999999999",
			Name:            "primary",
			OwnerAccountID:  "123456789012",
			Status:          "ACTIVE",
			CreatedAt:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			Tags:            map[string]string{"Environment": "prod"},
			PermissionSets: []PermissionSet{{
				ARN:             permSetARN,
				InstanceARN:     instanceARN,
				Name:            "AdministratorAccess",
				Description:     "Full admin",
				SessionDuration: "PT8H",
				RelayState:      "https://console.aws.amazon.com/",
				ManagedPolicies: []ManagedPolicyReference{{
					ARN:  "arn:aws:iam::aws:policy/AdministratorAccess",
					Name: "AdministratorAccess",
				}},
				CustomerManagedPolicies: []CustomerManagedPolicyReference{{
					Name: "least-privilege-app",
					Path: "/",
				}},
			}},
			AccountAssignments: []AccountAssignment{{
				InstanceARN:      instanceARN,
				PermissionSetARN: permSetARN,
				AccountID:        "210987654321",
				PrincipalID:      "f81d4fae-7dec-11d0-a765-00a0c91e6bf6",
				PrincipalType:    "GROUP",
			}},
			TrustedTokenIssuers: []TrustedTokenIssuer{{
				ARN:         issuerARN,
				InstanceARN: instanceARN,
				Name:        "corp-oidc",
				Type:        "OIDC_JWT",
			}},
		}},
		Applications: []Application{{
			ARN:                    appARN,
			InstanceARN:            instanceARN,
			Name:                   "internal-portal",
			Description:            "Internal portal",
			ApplicationAccountID:   "123456789012",
			ApplicationProviderARN: "arn:aws:sso::aws:applicationProvider/custom",
			IdentityStoreARN:       "arn:aws:identitystore::123456789012:identitystore/d-9999999999",
			Status:                 "ENABLED",
			PortalVisibility:       "ENABLED",
		}},
		Principals: []Principal{{
			ID:          "f81d4fae-7dec-11d0-a765-00a0c91e6bf6",
			Type:        "GROUP",
			DisplayName: "platform-admins",
		}},
	}

	envelopes, err := newTestScanner(t, fakeClient{snapshot: snapshot}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	instance := resourceByType(t, envelopes, awscloud.ResourceTypeSSOAdminInstance)
	if got, want := instance.Payload["arn"], instanceARN; got != want {
		t.Fatalf("instance arn = %#v, want %q", got, want)
	}
	instanceAttrs := attributesOf(t, instance)
	if got, want := instanceAttrs["identity_store_id"], "d-9999999999"; got != want {
		t.Fatalf("identity_store_id = %#v, want %q", got, want)
	}
	// The account-assignment tally must use the *_count naming shared by the
	// other instance counters; the legacy *_cnt key must not be emitted.
	if got, want := instanceAttrs["account_assignment_count"], 1; got != want {
		t.Fatalf("account_assignment_count = %#v, want %d", got, want)
	}
	if _, exists := instanceAttrs["account_assignment_cnt"]; exists {
		t.Fatalf("legacy attribute account_assignment_cnt persisted; use account_assignment_count")
	}

	permSet := resourceByType(t, envelopes, awscloud.ResourceTypeSSOAdminPermissionSet)
	permAttrs := attributesOf(t, permSet)
	if got, want := permAttrs["session_duration"], "PT8H"; got != want {
		t.Fatalf("session_duration = %#v, want %q", got, want)
	}
	if got, want := permAttrs["relay_state"], "https://console.aws.amazon.com/"; got != want {
		t.Fatalf("relay_state = %#v, want %q", got, want)
	}
	assertNoInlinePolicyAttributes(t, permAttrs)

	assignment := resourceByType(t, envelopes, awscloud.ResourceTypeSSOAdminAccountAssignment)
	assignAttrs := attributesOf(t, assignment)
	if got, want := assignAttrs["principal_type"], "GROUP"; got != want {
		t.Fatalf("principal_type = %#v, want %q", got, want)
	}
	if got, want := assignAttrs["target_account_id"], "210987654321"; got != want {
		t.Fatalf("target_account_id = %#v, want %q", got, want)
	}

	app := resourceByType(t, envelopes, awscloud.ResourceTypeSSOAdminApplication)
	appAttrs := attributesOf(t, app)
	if _, exists := appAttrs["access_scope"]; exists {
		t.Fatalf("access_scope persisted; application access-scope attributes must never be stored")
	}
	if _, exists := appAttrs["access_scope_authorized_targets"]; exists {
		t.Fatalf("access scope authorized targets persisted; must never be stored")
	}

	resourceByType(t, envelopes, awscloud.ResourceTypeSSOAdminTrustedTokenIssuer)

	principal := resourceByType(t, envelopes, awscloud.ResourceTypeSSOAdminPrincipal)
	principalAttrs := attributesOf(t, principal)
	display, ok := principalAttrs["display_name"].(map[string]any)
	if !ok {
		t.Fatalf("display_name = %#v, want redaction marker map", principalAttrs["display_name"])
	}
	if _, hasMarker := display["marker"]; !hasMarker {
		t.Fatalf("display_name missing redaction marker: %#v", display)
	}
	assertNoRawString(t, envelopes, "platform-admins")

	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminAssignmentUsesPermissionSet)
	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminAssignmentTargetsAccount)
	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminAssignmentGrantsPrincipal)
	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminPermissionSetUsesManagedPolicy)
	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy)
	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminPermissionSetInInstance)
	assertRelationship(t, envelopes, awscloud.RelationshipSSOAdminApplicationInInstance)

	// Both policy relationships must target the canonical IAM policy resource
	// type so downstream correlation matches the IAM scanner's typing.
	managed := relationshipByType(t, envelopes, awscloud.RelationshipSSOAdminPermissionSetUsesManagedPolicy)
	if got, want := targetTypeOf(t, managed), awscloud.ResourceTypeIAMPolicy; got != want {
		t.Fatalf("managed policy target_type = %q, want %q", got, want)
	}

	// Customer-managed policy relationship must reference the name only, never a
	// policy body.
	cmp := relationshipByType(t, envelopes, awscloud.RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy)
	if got, want := targetTypeOf(t, cmp), awscloud.ResourceTypeIAMPolicy; got != want {
		t.Fatalf("customer managed policy target_type = %q, want %q", got, want)
	}
	cmpAttrs := attributesOf(t, cmp)
	if got, want := cmpAttrs["policy_name"], "least-privilege-app"; got != want {
		t.Fatalf("customer managed policy_name = %#v, want %q", got, want)
	}
	if _, exists := cmpAttrs["policy_document"]; exists {
		t.Fatalf("customer managed policy_document persisted; only name reference is allowed")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceOrganizations

	_, err := newTestScanner(t, fakeClient{}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsWarnings(t *testing.T) {
	snapshot := Snapshot{
		Warnings: []awscloud.WarningObservation{{
			WarningKind: "identitycenter_no_instance",
			ErrorClass:  "empty",
			Message:     "no Identity Center instance in this account",
		}},
	}
	envelopes, err := newTestScanner(t, fakeClient{snapshot: snapshot}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	var warnings int
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSWarningFactKind {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("warning count = %d, want 1", warnings)
	}
}

func newTestScanner(t *testing.T, client Client) Scanner {
	t.Helper()
	return Scanner{Client: client, RedactionKey: testKey(t)}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSSOAdmin,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ssoadmin:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	relationshipByType(t, envelopes, relationshipType)
}

// targetTypeOf returns the relationship envelope's serialized target_type.
func targetTypeOf(t *testing.T, envelope facts.Envelope) string {
	t.Helper()
	targetType, ok := envelope.Payload["target_type"].(string)
	if !ok {
		t.Fatalf("target_type = %#v, want string", envelope.Payload["target_type"])
	}
	return targetType
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// assertNoInlinePolicyAttributes proves the scanner never persists the
// least-privilege model that GetInlinePolicyForPermissionSet would return.
func assertNoInlinePolicyAttributes(t *testing.T, attributes map[string]any) {
	t.Helper()
	for _, forbidden := range []string{
		"inline_policy",
		"inline_policy_document",
		"permissions_boundary",
		"permissions_boundary_document",
		"policy_document",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("permission set persisted forbidden attribute %q", forbidden)
		}
	}
}

// assertNoRawString proves a sensitive raw value never lands in any persisted
// payload map after redaction.
func assertNoRawString(t *testing.T, envelopes []facts.Envelope, raw string) {
	t.Helper()
	for _, envelope := range envelopes {
		if containsRawString(envelope.Payload, raw) {
			t.Fatalf("raw sensitive value %q persisted in payload %#v", raw, envelope.Payload)
		}
	}
}

func containsRawString(value any, raw string) bool {
	switch typed := value.(type) {
	case string:
		return typed == raw
	case map[string]any:
		for _, v := range typed {
			if containsRawString(v, raw) {
				return true
			}
		}
	case []any:
		for _, v := range typed {
			if containsRawString(v, raw) {
				return true
			}
		}
	case map[string]string:
		for _, v := range typed {
			if v == raw {
				return true
			}
		}
	}
	return false
}
