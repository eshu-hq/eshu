// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListKeysEmitsMetadataAndDropsEncryptionContext(t *testing.T) {
	keyID := "1234abcd-12ab-34cd-56ef-1234567890ab"
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/" + keyID
	aliasARN := "arn:aws:kms:us-east-1:123456789012:alias/orders"
	creation := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	api := &fakeKMSAPI{
		listKeysPages: []*awskms.ListKeysOutput{{
			Keys: []kmstypes.KeyListEntry{{KeyId: aws.String(keyID), KeyArn: aws.String(keyARN)}},
		}},
		describeKey: map[string]*kmstypes.KeyMetadata{
			keyID: {
				KeyId:        aws.String(keyID),
				Arn:          aws.String(keyARN),
				Description:  aws.String("Orders application key"),
				KeyManager:   kmstypes.KeyManagerTypeCustomer,
				KeyUsage:     kmstypes.KeyUsageTypeEncryptDecrypt,
				KeySpec:      kmstypes.KeySpecSymmetricDefault,
				KeyState:     kmstypes.KeyStateEnabled,
				Origin:       kmstypes.OriginTypeAwsKms,
				CreationDate: aws.Time(creation),
				Enabled:      true,
				MultiRegion:  aws.Bool(true),
				MultiRegionConfiguration: &kmstypes.MultiRegionConfiguration{
					MultiRegionKeyType: kmstypes.MultiRegionKeyTypePrimary,
					PrimaryKey:         &kmstypes.MultiRegionKey{Arn: aws.String(keyARN)},
				},
				EncryptionAlgorithms: []kmstypes.EncryptionAlgorithmSpec{kmstypes.EncryptionAlgorithmSpecSymmetricDefault},
			},
		},
		listAliasesPages: []*awskms.ListAliasesOutput{{
			Aliases: []kmstypes.AliasListEntry{{
				AliasName:       aws.String("alias/orders"),
				AliasArn:        aws.String(aliasARN),
				TargetKeyId:     aws.String(keyID),
				LastUpdatedDate: aws.Time(creation),
			}},
		}},
		listGrantsByKey: map[string][]*awskms.ListGrantsOutput{
			keyID: {{
				Grants: []kmstypes.GrantListEntry{{
					GrantId:           aws.String("grant-1"),
					Name:              aws.String("eshu-app-grant"),
					CreationDate:      aws.Time(creation),
					GranteePrincipal:  aws.String("arn:aws:iam::123456789012:role/eshu-app"),
					RetiringPrincipal: aws.String("arn:aws:iam::123456789012:role/eshu-admin"),
					IssuingAccount:    aws.String("123456789012"),
					Operations:        []kmstypes.GrantOperation{kmstypes.GrantOperationEncrypt, kmstypes.GrantOperationDecrypt},
					// Constraints intentionally set so the test proves the
					// adapter does NOT propagate encryption context pairs.
					Constraints: &kmstypes.GrantConstraints{
						EncryptionContextSubset: map[string]string{"tenant": "private-data"},
						EncryptionContextEquals: map[string]string{"workload": "secret-things"},
					},
				}},
			}},
		},
		listPoliciesByKey: map[string][]*awskms.ListKeyPoliciesOutput{
			keyID: {{PolicyNames: []string{"default"}}},
		},
		rotationByKey: map[string]*awskms.GetKeyRotationStatusOutput{
			keyID: {KeyRotationEnabled: true},
		},
		listTagsByKey: map[string][]*awskms.ListResourceTagsOutput{
			keyID: {{Tags: []kmstypes.Tag{{TagKey: aws.String("Environment"), TagValue: aws.String("prod")}}}},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	}

	keys, err := adapter.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys() error = %v, want nil", err)
	}
	if got, want := len(keys), 1; got != want {
		t.Fatalf("len(keys) = %d, want %d", got, want)
	}
	key := keys[0]
	if key.ID != keyID {
		t.Fatalf("ID = %q, want %q", key.ID, keyID)
	}
	if key.ARN != keyARN {
		t.Fatalf("ARN = %q, want %q", key.ARN, keyARN)
	}
	if key.KeyManager != string(kmstypes.KeyManagerTypeCustomer) {
		t.Fatalf("KeyManager = %q", key.KeyManager)
	}
	if key.KeyUsage != string(kmstypes.KeyUsageTypeEncryptDecrypt) {
		t.Fatalf("KeyUsage = %q", key.KeyUsage)
	}
	if !key.MultiRegion {
		t.Fatalf("MultiRegion = false, want true")
	}
	if key.MultiRegionKeyType != string(kmstypes.MultiRegionKeyTypePrimary) {
		t.Fatalf("MultiRegionKeyType = %q", key.MultiRegionKeyType)
	}
	if got, want := key.PolicyRevisionNames, []string{"default"}; !equalStrings(got, want) {
		t.Fatalf("PolicyRevisionNames = %#v, want %#v", got, want)
	}
	if !key.RotationStatusKnown || !key.RotationEnabled {
		t.Fatalf("RotationStatusKnown=%v, RotationEnabled=%v, want both true", key.RotationStatusKnown, key.RotationEnabled)
	}
	if len(key.Aliases) != 1 || key.Aliases[0].Name != "alias/orders" {
		t.Fatalf("Aliases = %#v, want one alias/orders", key.Aliases)
	}
	if len(key.Grants) != 1 {
		t.Fatalf("Grants len = %d, want 1", len(key.Grants))
	}
	grant := key.Grants[0]
	if grant.ID != "grant-1" || grant.GranteePrincipal != "arn:aws:iam::123456789012:role/eshu-app" {
		t.Fatalf("Grant identity = %#v", grant)
	}
	if grant.GranteePrincipalType != "AWS" {
		t.Fatalf("GranteePrincipalType = %q, want %q for an ARN grantee", grant.GranteePrincipalType, "AWS")
	}
	// Grant ops list flows through but encryption contexts do not exist on
	// the scanner-owned type at all, so the SDK adapter cannot leak them.
	wantOps := []string{string(kmstypes.GrantOperationEncrypt), string(kmstypes.GrantOperationDecrypt)}
	if !equalStrings(grant.Operations, wantOps) {
		t.Fatalf("Grant Operations = %#v, want %#v", grant.Operations, wantOps)
	}
	grantValue := reflect.ValueOf(grant)
	for i := 0; i < grantValue.NumField(); i++ {
		field := grantValue.Type().Field(i)
		lower := strings.ToLower(field.Name)
		for _, banned := range []string{"context", "constraint"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("Grant struct field %q leaks an encryption-context surface", field.Name)
			}
		}
	}
	// PR4b of #1134 (owner-approved): the adapter now reads the key policy with
	// GetKeyPolicy to derive normalized, metadata-only resource-policy
	// statements. It is called once per policy name from ListKeyPolicies. This
	// fixture supplies no policy document, so no statements are derived and the
	// raw policy body never reaches the scanner-owned Key.
	if api.getKeyPolicyCalls != 1 {
		t.Fatalf("GetKeyPolicy called %d times; want 1 (one read per policy name)", api.getKeyPolicyCalls)
	}
	if len(key.ResourcePolicyStatements) != 0 {
		t.Fatalf("ResourcePolicyStatements = %#v, want none for an empty policy document", key.ResourcePolicyStatements)
	}
	keyValue := reflect.ValueOf(key)
	for i := 0; i < keyValue.NumField(); i++ {
		field := keyValue.Type().Field(i)
		lower := strings.ToLower(field.Name)
		// The scanner-owned Key must never carry a raw policy document or
		// statement body field; only the derived ResourcePolicyStatements
		// projection and the policy-revision-name list are allowed.
		if strings.Contains(lower, "document") || lower == "policy" || lower == "policybody" {
			t.Fatalf("Key struct field %q exposes a raw policy-body surface", field.Name)
		}
	}
}

func TestClientListKeysOmitsRotationStatusForAsymmetricKeys(t *testing.T) {
	keyID := "asymmetric-key"
	api := &fakeKMSAPI{
		listKeysPages: []*awskms.ListKeysOutput{{
			Keys: []kmstypes.KeyListEntry{{KeyId: aws.String(keyID)}},
		}},
		describeKey: map[string]*kmstypes.KeyMetadata{
			keyID: {
				KeyId:      aws.String(keyID),
				KeyManager: kmstypes.KeyManagerTypeCustomer,
				KeyUsage:   kmstypes.KeyUsageTypeSignVerify,
				KeySpec:    kmstypes.KeySpecRsa2048,
				KeyState:   kmstypes.KeyStateEnabled,
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	}

	keys, err := adapter.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys() error = %v, want nil", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].RotationStatusKnown {
		t.Fatalf("RotationStatusKnown = true for asymmetric key; want false (no GetKeyRotationStatus call)")
	}
	if api.getKeyRotationStatusCalls != 0 {
		t.Fatalf("GetKeyRotationStatus called %d times for asymmetric key; expected zero", api.getKeyRotationStatusCalls)
	}
}

func TestClientListKeysTreatsUnsupportedOperationAsUnknownRotation(t *testing.T) {
	keyID := "managed-key"
	api := &fakeKMSAPI{
		listKeysPages: []*awskms.ListKeysOutput{{
			Keys: []kmstypes.KeyListEntry{{KeyId: aws.String(keyID)}},
		}},
		describeKey: map[string]*kmstypes.KeyMetadata{
			keyID: {
				KeyId:      aws.String(keyID),
				KeyManager: kmstypes.KeyManagerTypeCustomer,
				KeyUsage:   kmstypes.KeyUsageTypeEncryptDecrypt,
				KeySpec:    kmstypes.KeySpecSymmetricDefault,
				KeyState:   kmstypes.KeyStateEnabled,
			},
		},
		rotationErr: &smithy.GenericAPIError{Code: "UnsupportedOperationException", Message: "not supported"},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	}

	keys, err := adapter.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys() error = %v, want nil", err)
	}
	if keys[0].RotationStatusKnown {
		t.Fatalf("RotationStatusKnown should be false after UnsupportedOperationException")
	}
}

// TestClientListKeysSurfacesUnexpectedRotationErrors proves the adapter does
// not silently downgrade an unexpected GetKeyRotationStatus failure into
// rotation_status_known=false. A credential or config outage that surfaces as
// something other than UnsupportedOperation/AccessDenied must fail the scan so
// the operator sees the real failure instead of a false "rotation unknown".
func TestClientListKeysSurfacesUnexpectedRotationErrors(t *testing.T) {
	keyID := "managed-key"
	api := &fakeKMSAPI{
		listKeysPages: []*awskms.ListKeysOutput{{
			Keys: []kmstypes.KeyListEntry{{KeyId: aws.String(keyID)}},
		}},
		describeKey: map[string]*kmstypes.KeyMetadata{
			keyID: {
				KeyId:      aws.String(keyID),
				KeyManager: kmstypes.KeyManagerTypeCustomer,
				KeyUsage:   kmstypes.KeyUsageTypeEncryptDecrypt,
				KeySpec:    kmstypes.KeySpecSymmetricDefault,
				KeyState:   kmstypes.KeyStateEnabled,
			},
		},
		// An expired/invalid credential surfaces as an authentication error,
		// not UnsupportedOperation or AccessDenied.
		rotationErr: &smithy.GenericAPIError{Code: "ExpiredTokenException", Message: "token expired"},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	}

	_, err := adapter.ListKeys(context.Background())
	if err == nil {
		t.Fatalf("ListKeys() error = nil; an unexpected GetKeyRotationStatus failure must surface, not be swallowed as rotation unknown")
	}
	if !strings.Contains(err.Error(), "ExpiredTokenException") {
		t.Fatalf("ListKeys() error = %v, want it to wrap the underlying ExpiredTokenException", err)
	}
}

// TestClientListKeyPoliciesHonorsTruncatedFlag proves listKeyPolicies uses the
// same Truncated/NextMarker termination as the other paginators: it follows a
// non-empty marker while Truncated is true, and stops once Truncated is false
// regardless of any stray marker AWS may echo back.
func TestClientListKeyPoliciesHonorsTruncatedFlag(t *testing.T) {
	keyID := "policy-paged-key"
	api := &fakeKMSAPI{
		listKeysPages: []*awskms.ListKeysOutput{{
			Keys: []kmstypes.KeyListEntry{{KeyId: aws.String(keyID)}},
		}},
		describeKey: map[string]*kmstypes.KeyMetadata{
			keyID: {
				KeyId:      aws.String(keyID),
				KeyManager: kmstypes.KeyManagerTypeCustomer,
				KeyUsage:   kmstypes.KeyUsageTypeSignVerify,
				KeySpec:    kmstypes.KeySpecRsa2048,
				KeyState:   kmstypes.KeyStateEnabled,
			},
		},
		listPoliciesByKey: map[string][]*awskms.ListKeyPoliciesOutput{
			keyID: {
				// First page is truncated and points at the next marker.
				{PolicyNames: []string{"default"}, Truncated: true, NextMarker: aws.String("next")},
				// Final page is not truncated; the stray marker must NOT trigger
				// another call.
				{PolicyNames: []string{"backup"}, Truncated: false, NextMarker: aws.String("ignored")},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	}

	keys, err := adapter.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys() error = %v, want nil", err)
	}
	if got, want := keys[0].PolicyRevisionNames, []string{"default", "backup"}; !equalStrings(got, want) {
		t.Fatalf("PolicyRevisionNames = %#v, want %#v (both pages followed)", got, want)
	}
	if api.listPoliciesTotalCalls[keyID] != 2 {
		t.Fatalf("ListKeyPolicies called %d times, want 2 (stop on Truncated=false despite stray marker)", api.listPoliciesTotalCalls[keyID])
	}
}

// TestAdapterAPIClientInterfaceForbidsCryptoAndLifecycleMethods is the
// gate the issue calls out for the SDK adapter. We reflect over the
// adapter-local apiClient interface and confirm there is no cryptographic
// operation or lifecycle mutation method reachable from this adapter.
func TestAdapterAPIClientInterfaceForbidsCryptoAndLifecycleMethods(t *testing.T) {
	forbidden := []string{
		// Cryptographic operations.
		"Encrypt", "Decrypt", "GenerateDataKey", "GenerateDataKeyPair",
		"GenerateDataKeyPairWithoutPlaintext", "GenerateDataKeyWithoutPlaintext",
		"Sign", "Verify", "ReEncrypt", "GenerateMac", "VerifyMac",
		"DeriveSharedSecret", "GenerateRandom", "GetPublicKey",
		// Lifecycle mutations.
		"CreateKey", "ScheduleKeyDeletion", "CancelKeyDeletion",
		"EnableKey", "DisableKey", "EnableKeyRotation",
		"DisableKeyRotation", "PutKeyPolicy", "CreateGrant",
		"RevokeGrant", "RetireGrant", "ReplicateKey",
		"ImportKeyMaterial", "DeleteImportedKeyMaterial",
		"UpdateKeyDescription", "CreateAlias", "UpdateAlias",
		"DeleteAlias", "TagResource", "UntagResource",
		"RotateKeyOnDemand", "UpdatePrimaryRegion",
		"GetParametersForImport",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		method := iface.Method(i)
		for _, banned := range forbidden {
			if method.Name == banned {
				t.Fatalf("apiClient exposes forbidden method %q; KMS adapter contract is metadata-only", banned)
			}
			// Match the scanner-side guard's strings.Contains check so a future
			// addition like EncryptFoo or PutKeyPolicyBar cannot slip past the
			// interface gate just because its name is not an exact match.
			if strings.Contains(method.Name, banned) {
				t.Fatalf("apiClient method %q contains forbidden operation %q; KMS adapter contract is metadata-only", method.Name, banned)
			}
		}
	}
	// GetKeyPolicy is intentionally reachable (PR4b of #1134, owner-approved): the
	// adapter reads the key policy only to derive normalized, metadata-only
	// resource-policy statements; the raw Statement body and condition values are
	// never persisted. Pin it so an accidental removal is caught.
	if _, ok := iface.MethodByName("GetKeyPolicy"); !ok {
		t.Fatalf("apiClient must expose GetKeyPolicy for derived aws_resource_policy_permission facts")
	}
	// PutKeyPolicy (a mutation) must still be unreachable even though GetKeyPolicy
	// now is. The strings.Contains scan above would not catch a removed banned
	// entry, so assert the mutation directly.
	if _, ok := iface.MethodByName("PutKeyPolicy"); ok {
		t.Fatalf("apiClient exposes PutKeyPolicy; key-policy mutation is forbidden")
	}
}
