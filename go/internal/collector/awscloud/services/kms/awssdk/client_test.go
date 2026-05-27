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
	if api.getKeyPolicyCalls != 0 {
		t.Fatalf("GetKeyPolicy called %d times; scanner must never read policy bodies", api.getKeyPolicyCalls)
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
		"GetKeyPolicy", // Excluded so policy Statement bodies are unreachable.
		"GetParametersForImport",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		method := iface.Method(i)
		for _, banned := range forbidden {
			if method.Name == banned {
				t.Fatalf("apiClient exposes forbidden method %q; KMS adapter contract is metadata-only", banned)
			}
		}
	}
}

type fakeKMSAPI struct {
	listKeysPages    []*awskms.ListKeysOutput
	listKeysCalls    int
	describeKey      map[string]*kmstypes.KeyMetadata
	listAliasesPages []*awskms.ListAliasesOutput
	listAliasesCalls int

	listGrantsByKey   map[string][]*awskms.ListGrantsOutput
	listGrantsCounter map[string]int

	listPoliciesByKey   map[string][]*awskms.ListKeyPoliciesOutput
	listPoliciesCounter map[string]int

	rotationByKey             map[string]*awskms.GetKeyRotationStatusOutput
	rotationErr               error
	getKeyRotationStatusCalls int

	listTagsByKey   map[string][]*awskms.ListResourceTagsOutput
	listTagsCounter map[string]int

	getKeyPolicyCalls int
}

func (f *fakeKMSAPI) ListKeys(_ context.Context, _ *awskms.ListKeysInput, _ ...func(*awskms.Options)) (*awskms.ListKeysOutput, error) {
	if f.listKeysCalls >= len(f.listKeysPages) {
		return &awskms.ListKeysOutput{}, nil
	}
	page := f.listKeysPages[f.listKeysCalls]
	f.listKeysCalls++
	return page, nil
}

func (f *fakeKMSAPI) DescribeKey(_ context.Context, input *awskms.DescribeKeyInput, _ ...func(*awskms.Options)) (*awskms.DescribeKeyOutput, error) {
	id := aws.ToString(input.KeyId)
	metadata, ok := f.describeKey[id]
	if !ok {
		return &awskms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String(id)}}, nil
	}
	return &awskms.DescribeKeyOutput{KeyMetadata: metadata}, nil
}

func (f *fakeKMSAPI) ListAliases(_ context.Context, _ *awskms.ListAliasesInput, _ ...func(*awskms.Options)) (*awskms.ListAliasesOutput, error) {
	if f.listAliasesCalls >= len(f.listAliasesPages) {
		return &awskms.ListAliasesOutput{}, nil
	}
	page := f.listAliasesPages[f.listAliasesCalls]
	f.listAliasesCalls++
	return page, nil
}

func (f *fakeKMSAPI) ListGrants(_ context.Context, input *awskms.ListGrantsInput, _ ...func(*awskms.Options)) (*awskms.ListGrantsOutput, error) {
	id := aws.ToString(input.KeyId)
	if f.listGrantsCounter == nil {
		f.listGrantsCounter = map[string]int{}
	}
	pages := f.listGrantsByKey[id]
	index := f.listGrantsCounter[id]
	if index >= len(pages) {
		return &awskms.ListGrantsOutput{}, nil
	}
	f.listGrantsCounter[id] = index + 1
	return pages[index], nil
}

func (f *fakeKMSAPI) ListKeyPolicies(_ context.Context, input *awskms.ListKeyPoliciesInput, _ ...func(*awskms.Options)) (*awskms.ListKeyPoliciesOutput, error) {
	id := aws.ToString(input.KeyId)
	if f.listPoliciesCounter == nil {
		f.listPoliciesCounter = map[string]int{}
	}
	pages := f.listPoliciesByKey[id]
	index := f.listPoliciesCounter[id]
	if index >= len(pages) {
		return &awskms.ListKeyPoliciesOutput{}, nil
	}
	f.listPoliciesCounter[id] = index + 1
	return pages[index], nil
}

func (f *fakeKMSAPI) GetKeyRotationStatus(_ context.Context, input *awskms.GetKeyRotationStatusInput, _ ...func(*awskms.Options)) (*awskms.GetKeyRotationStatusOutput, error) {
	f.getKeyRotationStatusCalls++
	if f.rotationErr != nil {
		return nil, f.rotationErr
	}
	id := aws.ToString(input.KeyId)
	if output, ok := f.rotationByKey[id]; ok {
		return output, nil
	}
	return &awskms.GetKeyRotationStatusOutput{}, nil
}

func (f *fakeKMSAPI) ListResourceTags(_ context.Context, input *awskms.ListResourceTagsInput, _ ...func(*awskms.Options)) (*awskms.ListResourceTagsOutput, error) {
	id := aws.ToString(input.KeyId)
	if f.listTagsCounter == nil {
		f.listTagsCounter = map[string]int{}
	}
	pages := f.listTagsByKey[id]
	index := f.listTagsCounter[id]
	if index >= len(pages) {
		return &awskms.ListResourceTagsOutput{}, nil
	}
	f.listTagsCounter[id] = index + 1
	return pages[index], nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

var _ apiClient = (*fakeKMSAPI)(nil)
