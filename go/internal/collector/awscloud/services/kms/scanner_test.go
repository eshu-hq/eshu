// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kms

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsKMSKeyAliasAndGrantMetadataOnly(t *testing.T) {
	keyID := "1234abcd-12ab-34cd-56ef-1234567890ab"
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/" + keyID
	aliasARN := "arn:aws:kms:us-east-1:123456789012:alias/orders"
	grantID := "0c237476b39f8bcaf6cba5a7c91d2c83c5b0c4e7a1e7e7f3a9d1f24b6c8c1d0e"
	grantee := "arn:aws:iam::123456789012:role/eshu-app"
	retiring := "arn:aws:iam::123456789012:role/eshu-admin"
	client := fakeClient{keys: []Key{{
		ID:                 keyID,
		ARN:                keyARN,
		Description:        "Orders application key",
		KeyManager:         "CUSTOMER",
		KeyUsage:           "ENCRYPT_DECRYPT",
		KeySpec:            "SYMMETRIC_DEFAULT",
		KeyState:           "Enabled",
		Origin:             "AWS_KMS",
		CreationDate:       "2026-05-10T12:00:00Z",
		Enabled:            true,
		MultiRegion:        true,
		MultiRegionKeyType: "PRIMARY",
		EncryptionAlgorithms: []string{
			"SYMMETRIC_DEFAULT",
		},
		RotationEnabled:     true,
		RotationStatusKnown: true,
		PolicyRevisionNames: []string{"default"},
		Tags:                map[string]string{"Environment": "prod"},
		Aliases: []Alias{{
			Name:        "alias/orders",
			ARN:         aliasARN,
			TargetKeyID: keyID,
			LastUpdated: "2026-05-12T08:30:00Z",
		}},
		Grants: []Grant{{
			ID:                grantID,
			Name:              "eshu-app-grant",
			CreationDate:      "2026-05-11T09:15:00Z",
			GranteePrincipal:  grantee,
			RetiringPrincipal: retiring,
			IssuingAccount:    "123456789012",
			Operations:        []string{"Encrypt", "Decrypt", "GenerateDataKey", "DescribeKey"},
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	key := resourceByType(t, envelopes, awscloud.ResourceTypeKMSKey)
	if got, want := key.Payload["arn"], keyARN; got != want {
		t.Fatalf("key arn = %#v, want %q", got, want)
	}
	if got, want := key.Payload["resource_id"], keyID; got != want {
		t.Fatalf("key resource_id = %#v, want %q", got, want)
	}
	keyAttributes := attributesOf(t, key)
	assertAttribute(t, keyAttributes, "key_manager", "CUSTOMER")
	assertAttribute(t, keyAttributes, "key_usage", "ENCRYPT_DECRYPT")
	assertAttribute(t, keyAttributes, "key_spec", "SYMMETRIC_DEFAULT")
	assertAttribute(t, keyAttributes, "key_state", "Enabled")
	assertAttribute(t, keyAttributes, "origin", "AWS_KMS")
	assertAttribute(t, keyAttributes, "rotation_enabled", true)
	assertAttribute(t, keyAttributes, "rotation_status_known", true)
	assertAttribute(t, keyAttributes, "multi_region", true)
	assertAttribute(t, keyAttributes, "multi_region_key_type", "PRIMARY")
	assertAttribute(t, keyAttributes, "encryption_algorithms", []string{"SYMMETRIC_DEFAULT"})
	assertAttribute(t, keyAttributes, "policy_revision_names", []string{"default"})

	// Forbidden payload classes: key policy Statement bodies, key material,
	// plaintext data, or cryptographic operation results.
	for _, forbidden := range []string{
		"policy",
		"policy_document",
		"policy_statements",
		"policy_body",
		"statement",
		"key_material",
		"plaintext",
		"ciphertext_blob",
		"data_key",
		"data_key_plaintext",
		"signature",
		"mac",
	} {
		if _, exists := keyAttributes[forbidden]; exists {
			t.Fatalf("%q attribute persisted on key; scanner must never store policy bodies or cryptographic outputs", forbidden)
		}
	}

	alias := resourceByType(t, envelopes, awscloud.ResourceTypeKMSAlias)
	if got, want := alias.Payload["arn"], aliasARN; got != want {
		t.Fatalf("alias arn = %#v, want %q", got, want)
	}
	if got, want := alias.Payload["name"], "alias/orders"; got != want {
		t.Fatalf("alias name = %#v, want %q", got, want)
	}
	aliasAttributes := attributesOf(t, alias)
	assertAttribute(t, aliasAttributes, "target_key_id", keyID)

	grant := resourceByType(t, envelopes, awscloud.ResourceTypeKMSGrant)
	if got, want := grant.Payload["name"], "eshu-app-grant"; got != want {
		t.Fatalf("grant name = %#v, want %q", got, want)
	}
	grantAttributes := attributesOf(t, grant)
	assertAttribute(t, grantAttributes, "grant_id", grantID)
	assertAttribute(t, grantAttributes, "grantee_principal", grantee)
	assertAttribute(t, grantAttributes, "retiring_principal", retiring)
	assertAttribute(t, grantAttributes, "operations", []string{"Encrypt", "Decrypt", "GenerateDataKey", "DescribeKey"})

	// Grant encryption contexts must never be persisted.
	for _, forbidden := range []string{
		"encryption_context",
		"encryption_context_subset",
		"encryption_context_equals",
		"constraints",
	} {
		if _, exists := grantAttributes[forbidden]; exists {
			t.Fatalf("%q attribute persisted on grant; scanner must never store encryption contexts", forbidden)
		}
	}

	assertRelationshipType(t, envelopes, awscloud.RelationshipKMSAliasTargetsKey)
	assertRelationshipType(t, envelopes, awscloud.RelationshipKMSGrantOnKey)

	// An ARN-shaped grantee mirrors the IAM principal scheme: the target
	// identity is "AWS:<arn>", target_arn is populated, and principal_type is
	// recorded for downstream reducers.
	granteeRel := relationshipByType(t, envelopes, awscloud.RelationshipKMSGrantForGrantee)
	assertPayload(t, granteeRel, "target_resource_id", "AWS:"+grantee)
	assertPayload(t, granteeRel, "target_arn", grantee)
	assertPayload(t, granteeRel, "target_type", awscloud.ResourceTypeIAMPrincipal)
	assertAttribute(t, attributesOf(t, granteeRel), "principal_type", "AWS")
}

// TestScannerEncodesServicePrincipalGranteeWithoutARN proves a grantee that is
// a service principal (for example "s3.amazonaws.com") is encoded as
// "Service:<principal>" and never populates target_arn, since a service
// principal is not an ARN. This guards the IAM-aligned principal scheme so a
// service principal is not mistaken for an ARN downstream.
func TestScannerEncodesServicePrincipalGranteeWithoutARN(t *testing.T) {
	keyID := "1234abcd-12ab-34cd-56ef-1234567890ab"
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/" + keyID
	servicePrincipal := "s3.amazonaws.com"
	client := fakeClient{keys: []Key{{
		ID:         keyID,
		ARN:        keyARN,
		KeyManager: "CUSTOMER",
		KeyUsage:   "ENCRYPT_DECRYPT",
		KeySpec:    "SYMMETRIC_DEFAULT",
		KeyState:   "Enabled",
		Origin:     "AWS_KMS",
		Grants: []Grant{{
			ID:                   "service-grant",
			GranteePrincipal:     servicePrincipal,
			GranteePrincipalType: "Service",
			Operations:           []string{"Decrypt"},
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	granteeRel := relationshipByType(t, envelopes, awscloud.RelationshipKMSGrantForGrantee)
	assertPayload(t, granteeRel, "target_resource_id", "Service:"+servicePrincipal)
	assertPayload(t, granteeRel, "target_type", awscloud.ResourceTypeIAMPrincipal)
	if got := granteeRel.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for a service principal grantee", got)
	}
	assertAttribute(t, attributesOf(t, granteeRel), "principal_type", "Service")
}

func TestScannerOmitsRotationStatusWhenNotReported(t *testing.T) {
	client := fakeClient{keys: []Key{{
		ID:                  "asymmetric-key",
		ARN:                 "arn:aws:kms:us-east-1:123456789012:key/asymmetric-key",
		KeyManager:          "CUSTOMER",
		KeyUsage:            "SIGN_VERIFY",
		KeySpec:             "RSA_2048",
		KeyState:            "Enabled",
		Origin:              "AWS_KMS",
		RotationStatusKnown: false,
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	key := resourceByType(t, envelopes, awscloud.ResourceTypeKMSKey)
	keyAttributes := attributesOf(t, key)
	if got, exists := keyAttributes["rotation_enabled"]; exists {
		t.Fatalf("rotation_enabled persisted (%#v) when rotation status unknown; omit it", got)
	}
	if _, exists := keyAttributes["rotation_status_known"]; !exists {
		t.Fatalf("rotation_status_known missing; want explicit false")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
	if !strings.Contains(err.Error(), "client is required") {
		t.Fatalf("Scan() error = %v, want client-required message", err)
	}
}

// TestClientInterfaceExposesNoCryptographicOrLifecycleOperations is the
// security gate the issue calls out by name. The test reflects over the
// scanner-owned Client interface and asserts that no method name appears for
// any cryptographic operation or lifecycle mutation. Adding such a method
// would be a contract violation, not a feature.
func TestClientInterfaceExposesNoCryptographicOrLifecycleOperations(t *testing.T) {
	forbidden := []string{
		// Cryptographic operations.
		"Encrypt",
		"Decrypt",
		"GenerateDataKey",
		"GenerateDataKeyPair",
		"GenerateDataKeyPairWithoutPlaintext",
		"GenerateDataKeyWithoutPlaintext",
		"Sign",
		"Verify",
		"ReEncrypt",
		"GenerateMac",
		"VerifyMac",
		"DeriveSharedSecret",
		// Lifecycle mutations.
		"CreateKey",
		"ScheduleKeyDeletion",
		"CancelKeyDeletion",
		"EnableKey",
		"DisableKey",
		"EnableKeyRotation",
		"DisableKeyRotation",
		"PutKeyPolicy",
		"CreateGrant",
		"RevokeGrant",
		"RetireGrant",
		"ReplicateKey",
		"ImportKeyMaterial",
		"DeleteImportedKeyMaterial",
		"UpdateKeyDescription",
		"UpdateAlias",
		"CreateAlias",
		"DeleteAlias",
		"TagResource",
		"UntagResource",
	}
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		for _, banned := range forbidden {
			if method.Name == banned {
				t.Fatalf("Client interface exposes forbidden method %q; KMS scanner contract is metadata-only", banned)
			}
			if strings.Contains(method.Name, banned) {
				t.Fatalf("Client interface method %q resembles forbidden operation %q; tighten the contract", method.Name, banned)
			}
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceKMS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:kms:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	keys []Key
}

func (c fakeClient) ListKeys(context.Context) ([]Key, error) {
	return c.keys, nil
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

func assertRelationshipType(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
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

func assertPayload(t *testing.T, envelope facts.Envelope, key string, want any) {
	t.Helper()
	got, exists := envelope.Payload[key]
	if !exists {
		t.Fatalf("missing payload key %q in %#v", key, envelope.Payload)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("payload %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch wantTyped := want.(type) {
	case []string:
		gotTyped, ok := got.([]string)
		if !ok || len(gotTyped) != len(wantTyped) {
			return false
		}
		for i := range gotTyped {
			if gotTyped[i] != wantTyped[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
