// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedpermissions

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testStoreARN      = "arn:aws:verifiedpermissions::123456789012:policy-store/PSEXAMPLEabcdefg111111"
	testStoreID       = "PSEXAMPLEabcdefg111111"
	testPolicyID      = "SPEXAMPLEabcdefg222222"
	testSourceID      = "ISEXAMPLEabcdefg333333"
	testUserPoolARN   = "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_1a2b3c4d5"
	testUserPoolID    = "us-east-1_1a2b3c4d5"
	testOIDCSourceID  = "ISEXAMPLEabcdefg444444"
	testOIDCIssuerURL = "https://auth.example.com"
)

func TestScannerEmitsVerifiedPermissionsMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{PolicyStores: []PolicyStore{{
		ARN:                testStoreARN,
		ID:                 testStoreID,
		Description:        "prod authz",
		ValidationMode:     "STRICT",
		DeletionProtection: "ENABLED",
		EncryptionState:    "KMS",
		CedarVersion:       "CEDAR_4",
		CreatedDate:        time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastUpdatedDate:    time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		Tags:               map[string]string{"Environment": "prod"},
		Policies: []Policy{{
			ID:            testPolicyID,
			PolicyStoreID: testStoreID,
			PolicyType:    "STATIC",
			Effect:        "Permit",
			CreatedDate:   time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
		}},
		IdentitySources: []IdentitySource{{
			ID:                  testSourceID,
			PolicyStoreID:       testStoreID,
			PrincipalEntityType: "MyCorp::User",
			ProviderKind:        "cognito",
			CognitoUserPoolARN:  testUserPoolARN,
			ClientIDCount:       2,
			CreatedDate:         time.Date(2026, 5, 14, 12, 10, 0, 0, time.UTC),
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Policy store resource node.
	store := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedPermissionsPolicyStore)
	if got, want := store.Payload["resource_id"], testStoreARN; got != want {
		t.Fatalf("store resource_id = %#v, want %q", got, want)
	}
	if got, want := store.Payload["arn"], testStoreARN; got != want {
		t.Fatalf("store arn = %#v, want %q", got, want)
	}
	storeAttrs := attributesOf(t, store)
	assertAttribute(t, storeAttrs, "validation_mode", "STRICT")
	assertAttribute(t, storeAttrs, "deletion_protection", "ENABLED")
	assertAttribute(t, storeAttrs, "encryption_state", "KMS")
	assertAttribute(t, storeAttrs, "cedar_version", "CEDAR_4")
	assertAttribute(t, storeAttrs, "policy_store_id", testStoreID)

	// Policy resource node.
	policy := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedPermissionsPolicy)
	wantPolicyID := testStoreID + "/" + testPolicyID
	if got, want := policy.Payload["resource_id"], wantPolicyID; got != want {
		t.Fatalf("policy resource_id = %#v, want %q", got, want)
	}
	policyAttrs := attributesOf(t, policy)
	assertAttribute(t, policyAttrs, "policy_type", "STATIC")
	assertAttribute(t, policyAttrs, "effect", "Permit")
	assertAttribute(t, policyAttrs, "policy_id", testPolicyID)

	// Identity source resource node.
	source := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedPermissionsIdentitySource)
	wantSourceID := testStoreID + "/" + testSourceID
	if got, want := source.Payload["resource_id"], wantSourceID; got != want {
		t.Fatalf("identity source resource_id = %#v, want %q", got, want)
	}
	sourceAttrs := attributesOf(t, source)
	assertAttribute(t, sourceAttrs, "provider_kind", "cognito")
	assertAttribute(t, sourceAttrs, "principal_entity_type", "MyCorp::User")
	assertAttribute(t, sourceAttrs, "client_id_count", 2)

	// policy -> store edge, keyed by the store ARN the store node publishes.
	policyInStore := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedPermissionsPolicyInStore)
	assertEdgeTarget(t, policyInStore, awscloud.ResourceTypeVerifiedPermissionsPolicyStore, testStoreARN)
	if got, want := policyInStore.Payload["source_resource_id"], wantPolicyID; got != want {
		t.Fatalf("policy->store source_resource_id = %#v, want %q", got, want)
	}

	// identity source -> store edge.
	sourceInStore := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedPermissionsIdentitySourceInStore)
	assertEdgeTarget(t, sourceInStore, awscloud.ResourceTypeVerifiedPermissionsPolicyStore, testStoreARN)
	if got, want := sourceInStore.Payload["source_resource_id"], wantSourceID; got != want {
		t.Fatalf("source->store source_resource_id = %#v, want %q", got, want)
	}

	// identity source -> Cognito user pool edge, keyed by the BARE user pool id
	// the Cognito scanner publishes for a user pool node.
	sourceCognito := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool)
	assertEdgeTarget(t, sourceCognito, awscloud.ResourceTypeCognitoUserPool, testUserPoolID)
	// The user pool node publishes the bare id as resource_id, so the edge leaves
	// target_arn empty (relguard contract); the ARN survives as an attribute.
	if got := sourceCognito.Payload["target_arn"]; got != "" {
		t.Fatalf("source->cognito target_arn = %#v, want empty (bare-id-keyed target)", got)
	}
	cognitoAttrs, _ := sourceCognito.Payload["attributes"].(map[string]any)
	if got, want := cognitoAttrs["user_pool_arn"], testUserPoolARN; got != want {
		t.Fatalf("source->cognito attribute user_pool_arn = %#v, want %q", got, want)
	}

	// No Cedar source / schema / policy body leakage anywhere in payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"statement", "policy_body", "cedar", "cedar_source", "schema",
			"schema_body", "definition", "principal", "resource", "client_ids",
			"token", "client_secret",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Verified Permissions scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerEmitsOIDCIdentitySourceWithoutCognitoEdge(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{PolicyStores: []PolicyStore{{
		ARN: testStoreARN,
		ID:  testStoreID,
		IdentitySources: []IdentitySource{{
			ID:            testOIDCSourceID,
			PolicyStoreID: testStoreID,
			ProviderKind:  "oidc",
			OpenIDIssuer:  testOIDCIssuerURL,
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	source := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedPermissionsIdentitySource)
	assertAttribute(t, attributesOf(t, source), "openid_issuer", testOIDCIssuerURL)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool {
			t.Fatalf("OIDC identity source must not emit a Cognito user pool edge")
		}
	}
}

func TestScannerExtractsUserPoolIDFromGovCloudARN(t *testing.T) {
	govPoolARN := "arn:aws-us-gov:cognito-idp:us-gov-west-1:123456789012:userpool/us-gov-west-1_govpool99"
	client := fakeClient{snapshot: Snapshot{PolicyStores: []PolicyStore{{
		ARN: "arn:aws-us-gov:verifiedpermissions::123456789012:policy-store/PSGOV",
		ID:  "PSGOV",
		IdentitySources: []IdentitySource{{
			ID:                 testSourceID,
			PolicyStoreID:      "PSGOV",
			ProviderKind:       "cognito",
			CognitoUserPoolARN: govPoolARN,
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool)
	if got, want := edge.Payload["target_resource_id"], "us-gov-west-1_govpool99"; got != want {
		t.Fatalf("GovCloud user pool target_resource_id = %#v, want %q", got, want)
	}
	govAttrs, _ := edge.Payload["attributes"].(map[string]any)
	if got, want := govAttrs["user_pool_arn"], govPoolARN; got != want {
		t.Fatalf("GovCloud user pool attribute user_pool_arn = %#v, want %q", got, want)
	}
}

func TestScannerOmitsCognitoEdgeForMalformedARN(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{PolicyStores: []PolicyStore{{
		ARN: testStoreARN,
		ID:  testStoreID,
		IdentitySources: []IdentitySource{{
			ID:                 testSourceID,
			PolicyStoreID:      testStoreID,
			ProviderKind:       "cognito",
			CognitoUserPoolARN: "not-a-userpool-arn",
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool {
			t.Fatalf("malformed user pool ARN must skip the Cognito edge, not dangle it")
		}
	}
}

func TestScannerOmitsRelationshipsForEmptyStore(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{PolicyStores: []PolicyStore{{
		ARN: testStoreARN,
		ID:  testStoreID,
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
		t.Fatalf("empty account returned %d envelopes, want 0", len(envelopes))
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	store := PolicyStore{ARN: testStoreARN, ID: testStoreID}
	storeID := policyStoreResourceID(store)
	policy := Policy{ID: testPolicyID, PolicyStoreID: testStoreID}
	source := IdentitySource{ID: testSourceID, PolicyStoreID: testStoreID, CognitoUserPoolARN: testUserPoolARN}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		policyInStoreRelationship(boundary, storeID, policy),
		identitySourceInStoreRelationship(boundary, storeID, source),
		identitySourceCognitoRelationship(boundary, source),
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		PolicyStores: []PolicyStore{{ARN: testStoreARN, ID: testStoreID}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Verified Permissions ListPolicies throttled after SDK retries; policy metadata omitted for this scan",
			SourceRecordID: "verifiedpermissions_policies_throttled",
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
		ServiceKind:         awscloud.ServiceVerifiedPermissions,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:verifiedpermissions:1",
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
