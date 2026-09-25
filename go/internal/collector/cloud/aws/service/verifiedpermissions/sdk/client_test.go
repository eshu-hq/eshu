// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsvp "github.com/aws/aws-sdk-go-v2/service/verifiedpermissions"
	awsvptypes "github.com/aws/aws-sdk-go-v2/service/verifiedpermissions/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsVerifiedPermissionsMetadataOnly(t *testing.T) {
	storeARN := "arn:aws:verifiedpermissions::123456789012:policy-store/PSEXAMPLEabcdefg111111"
	storeID := "PSEXAMPLEabcdefg111111"
	userPoolARN := "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_1a2b3c4d5"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeVPAPI{
		storePages: []*awsvp.ListPolicyStoresOutput{{
			PolicyStores: []awsvptypes.PolicyStoreItem{{
				Arn:           awsv2.String(storeARN),
				PolicyStoreId: awsv2.String(storeID),
				Description:   awsv2.String("prod authz"),
				CreatedDate:   awsv2.Time(createdAt),
			}},
		}},
		storeDetail: map[string]*awsvp.GetPolicyStoreOutput{
			storeID: {
				Arn:                awsv2.String(storeARN),
				PolicyStoreId:      awsv2.String(storeID),
				ValidationSettings: &awsvptypes.ValidationSettings{Mode: awsvptypes.ValidationModeStrict},
				DeletionProtection: awsvptypes.DeletionProtectionEnabled,
				CedarVersion:       awsvptypes.CedarVersionCedar4,
				EncryptionState:    &awsvptypes.EncryptionStateMemberKmsEncryptionState{},
				CreatedDate:        awsv2.Time(createdAt),
				LastUpdatedDate:    awsv2.Time(createdAt),
				Tags:               map[string]string{"Environment": "prod"},
			},
		},
		policyPages: map[string][]*awsvp.ListPoliciesOutput{
			storeID: {{
				Policies: []awsvptypes.PolicyItem{{
					PolicyId:        awsv2.String("SPEXAMPLE222222"),
					PolicyStoreId:   awsv2.String(storeID),
					PolicyType:      awsvptypes.PolicyTypeStatic,
					Effect:          awsvptypes.PolicyEffectPermit,
					CreatedDate:     awsv2.Time(createdAt),
					LastUpdatedDate: awsv2.Time(createdAt),
				}},
			}},
		},
		identityPages: map[string][]*awsvp.ListIdentitySourcesOutput{
			storeID: {{
				IdentitySources: []awsvptypes.IdentitySourceItem{{
					IdentitySourceId:    awsv2.String("ISEXAMPLE333333"),
					PolicyStoreId:       awsv2.String(storeID),
					PrincipalEntityType: awsv2.String("MyCorp::User"),
					CreatedDate:         awsv2.Time(createdAt),
					LastUpdatedDate:     awsv2.Time(createdAt),
					Configuration: &awsvptypes.ConfigurationItemMemberCognitoUserPoolConfiguration{
						Value: awsvptypes.CognitoUserPoolConfigurationItem{
							UserPoolArn: awsv2.String(userPoolARN),
							ClientIds:   []string{"clientA", "clientB"},
							Issuer:      awsv2.String("https://cognito-idp.us-east-1.amazonaws.com/us-east-1_1a2b3c4d5"),
						},
					},
				}},
			}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.PolicyStores) != 1 {
		t.Fatalf("len(PolicyStores) = %d, want 1", len(snapshot.PolicyStores))
	}
	store := snapshot.PolicyStores[0]
	if store.ARN != storeARN {
		t.Fatalf("store ARN = %q, want %q", store.ARN, storeARN)
	}
	if store.ValidationMode != "STRICT" {
		t.Fatalf("store ValidationMode = %q, want STRICT", store.ValidationMode)
	}
	if store.DeletionProtection != "ENABLED" {
		t.Fatalf("store DeletionProtection = %q, want ENABLED", store.DeletionProtection)
	}
	if store.EncryptionState != "KMS" {
		t.Fatalf("store EncryptionState = %q, want KMS (label only, never key ARN)", store.EncryptionState)
	}
	if store.CedarVersion != "CEDAR_4" {
		t.Fatalf("store CedarVersion = %q, want CEDAR_4", store.CedarVersion)
	}
	if store.Tags["Environment"] != "prod" {
		t.Fatalf("store tag Environment = %q, want prod", store.Tags["Environment"])
	}
	if len(store.Policies) != 1 {
		t.Fatalf("len(Policies) = %d, want 1", len(store.Policies))
	}
	policy := store.Policies[0]
	if policy.PolicyType != "STATIC" || policy.Effect != "Permit" {
		t.Fatalf("policy = %+v, want STATIC/Permit", policy)
	}
	if len(store.IdentitySources) != 1 {
		t.Fatalf("len(IdentitySources) = %d, want 1", len(store.IdentitySources))
	}
	source := store.IdentitySources[0]
	if source.ProviderKind != "cognito" {
		t.Fatalf("source ProviderKind = %q, want cognito", source.ProviderKind)
	}
	if source.CognitoUserPoolARN != userPoolARN {
		t.Fatalf("source CognitoUserPoolARN = %q, want %q", source.CognitoUserPoolARN, userPoolARN)
	}
	if source.ClientIDCount != 2 {
		t.Fatalf("source ClientIDCount = %d, want 2 (count only, never client id values)", source.ClientIDCount)
	}
}

func TestClientMapsDeprecatedIdentitySourceDetails(t *testing.T) {
	storeID := "PSDEP"
	userPoolARN := "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_legacy01"
	api := &fakeVPAPI{
		storePages: []*awsvp.ListPolicyStoresOutput{{
			PolicyStores: []awsvptypes.PolicyStoreItem{{
				Arn:           awsv2.String("arn:aws:verifiedpermissions::123456789012:policy-store/PSDEP"),
				PolicyStoreId: awsv2.String(storeID),
				CreatedDate:   awsv2.Time(time.Now()),
			}},
		}},
		storeDetail: map[string]*awsvp.GetPolicyStoreOutput{storeID: {PolicyStoreId: awsv2.String(storeID)}},
		identityPages: map[string][]*awsvp.ListIdentitySourcesOutput{
			storeID: {{
				IdentitySources: []awsvptypes.IdentitySourceItem{{
					IdentitySourceId: awsv2.String("ISDEP"),
					PolicyStoreId:    awsv2.String(storeID),
					CreatedDate:      awsv2.Time(time.Now()),
					LastUpdatedDate:  awsv2.Time(time.Now()),
					Details: &awsvptypes.IdentitySourceItemDetails{
						UserPoolArn: awsv2.String(userPoolARN),
						ClientIds:   []string{"only-one"},
					},
				}},
			}},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	source := snapshot.PolicyStores[0].IdentitySources[0]
	if source.ProviderKind != "cognito" || source.CognitoUserPoolARN != userPoolARN {
		t.Fatalf("deprecated details mapping = %+v, want cognito/%q", source, userPoolARN)
	}
	if source.ClientIDCount != 1 {
		t.Fatalf("deprecated ClientIDCount = %d, want 1", source.ClientIDCount)
	}
}

type fakeVPAPI struct {
	storePages    []*awsvp.ListPolicyStoresOutput
	storeCall     int
	storeDetail   map[string]*awsvp.GetPolicyStoreOutput
	policyPages   map[string][]*awsvp.ListPoliciesOutput
	policyCalls   map[string]int
	identityPages map[string][]*awsvp.ListIdentitySourcesOutput
	identityCalls map[string]int
}

func (f *fakeVPAPI) ListPolicyStores(
	_ context.Context,
	_ *awsvp.ListPolicyStoresInput,
	_ ...func(*awsvp.Options),
) (*awsvp.ListPolicyStoresOutput, error) {
	if f.storeCall >= len(f.storePages) {
		return &awsvp.ListPolicyStoresOutput{}, nil
	}
	page := f.storePages[f.storeCall]
	f.storeCall++
	return page, nil
}

func (f *fakeVPAPI) GetPolicyStore(
	_ context.Context,
	input *awsvp.GetPolicyStoreInput,
	_ ...func(*awsvp.Options),
) (*awsvp.GetPolicyStoreOutput, error) {
	return f.storeDetail[awsv2.ToString(input.PolicyStoreId)], nil
}

func (f *fakeVPAPI) ListPolicies(
	_ context.Context,
	input *awsvp.ListPoliciesInput,
	_ ...func(*awsvp.Options),
) (*awsvp.ListPoliciesOutput, error) {
	if f.policyCalls == nil {
		f.policyCalls = map[string]int{}
	}
	id := awsv2.ToString(input.PolicyStoreId)
	pages := f.policyPages[id]
	idx := f.policyCalls[id]
	if idx >= len(pages) {
		return &awsvp.ListPoliciesOutput{}, nil
	}
	f.policyCalls[id] = idx + 1
	return pages[idx], nil
}

func (f *fakeVPAPI) ListIdentitySources(
	_ context.Context,
	input *awsvp.ListIdentitySourcesInput,
	_ ...func(*awsvp.Options),
) (*awsvp.ListIdentitySourcesOutput, error) {
	if f.identityCalls == nil {
		f.identityCalls = map[string]int{}
	}
	id := awsv2.ToString(input.PolicyStoreId)
	pages := f.identityPages[id]
	idx := f.identityCalls[id]
	if idx >= len(pages) {
		return &awsvp.ListIdentitySourcesOutput{}, nil
	}
	f.identityCalls[id] = idx + 1
	return pages[idx], nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceVerifiedPermissions,
	}
}
