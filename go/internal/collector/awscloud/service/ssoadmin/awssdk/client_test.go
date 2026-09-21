// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsidentitystore "github.com/aws/aws-sdk-go-v2/service/identitystore"
	awsssoadmin "github.com/aws/aws-sdk-go-v2/service/ssoadmin"
	awsssoadmintypes "github.com/aws/aws-sdk-go-v2/service/ssoadmin/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestSnapshotReadsMetadataOnlyAndResolvesPrincipals(t *testing.T) {
	instanceARN := "arn:aws:sso:::instance/ssoins-1111111111111111"
	permSetARN := "arn:aws:sso:::permissionSet/ssoins-1111111111111111/ps-2222222222222222"
	fake := &fakeSSOAdmin{
		instances: []awsssoadmintypes.InstanceMetadata{{
			InstanceArn:     aws.String(instanceARN),
			IdentityStoreId: aws.String("d-9999999999"),
			Name:            aws.String("primary"),
			OwnerAccountId:  aws.String("123456789012"),
			Status:          awsssoadmintypes.InstanceStatusActive,
		}},
		permissionSets: []string{permSetARN},
		describePermissionSet: &awsssoadmintypes.PermissionSet{
			PermissionSetArn: aws.String(permSetARN),
			Name:             aws.String("AdministratorAccess"),
			Description:      aws.String("Full admin"),
			SessionDuration:  aws.String("PT8H"),
			RelayState:       aws.String("https://console.aws.amazon.com/"),
		},
		managedPolicies: []awsssoadmintypes.AttachedManagedPolicy{{
			Arn:  aws.String("arn:aws:iam::aws:policy/AdministratorAccess"),
			Name: aws.String("AdministratorAccess"),
		}},
		customerManagedPolicies: []awsssoadmintypes.CustomerManagedPolicyReference{{
			Name: aws.String("least-privilege-app"),
			Path: aws.String("/"),
		}},
		provisionedAccounts: []string{"210987654321"},
		accountAssignments: []awsssoadmintypes.AccountAssignment{{
			AccountId:        aws.String("210987654321"),
			PermissionSetArn: aws.String(permSetARN),
			PrincipalId:      aws.String("f81d4fae-7dec-11d0-a765-00a0c91e6bf6"),
			PrincipalType:    awsssoadmintypes.PrincipalTypeGroup,
		}},
		applications: []awsssoadmintypes.Application{{
			ApplicationArn: aws.String("arn:aws:sso::123456789012:application/ssoins-1111111111111111/apl-3333333333333333"),
			InstanceArn:    aws.String(instanceARN),
			Name:           aws.String("internal-portal"),
			Status:         awsssoadmintypes.ApplicationStatusEnabled,
			PortalOptions: &awsssoadmintypes.PortalOptions{
				Visibility: awsssoadmintypes.ApplicationVisibilityEnabled,
			},
		}},
		trustedTokenIssuers: []awsssoadmintypes.TrustedTokenIssuerMetadata{{
			TrustedTokenIssuerArn:  aws.String("arn:aws:sso::123456789012:trustedTokenIssuer/ssoins-1111111111111111/tti-4444444444444444"),
			Name:                   aws.String("corp-oidc"),
			TrustedTokenIssuerType: awsssoadmintypes.TrustedTokenIssuerTypeOidcJwt,
		}},
		tags: map[string]string{"Environment": "prod"},
	}
	store := &fakeIdentityStore{groupDisplayName: "platform-admins"}
	client := newTestAdapter(fake, store)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.Instances), 1; got != want {
		t.Fatalf("instances = %d, want %d", got, want)
	}
	instance := snapshot.Instances[0]
	if got, want := len(instance.PermissionSets), 1; got != want {
		t.Fatalf("permission sets = %d, want %d", got, want)
	}
	permSet := instance.PermissionSets[0]
	if permSet.SessionDuration != "PT8H" {
		t.Fatalf("SessionDuration = %q, want PT8H", permSet.SessionDuration)
	}
	if got, want := len(permSet.ManagedPolicies), 1; got != want {
		t.Fatalf("managed policies = %d, want %d", got, want)
	}
	if got, want := len(permSet.CustomerManagedPolicies), 1; got != want {
		t.Fatalf("customer managed policies = %d, want %d", got, want)
	}
	if permSet.CustomerManagedPolicies[0].Name != "least-privilege-app" {
		t.Fatalf("customer managed name = %q", permSet.CustomerManagedPolicies[0].Name)
	}
	if got, want := len(instance.AccountAssignments), 1; got != want {
		t.Fatalf("assignments = %d, want %d", got, want)
	}
	if got, want := len(instance.TrustedTokenIssuers), 1; got != want {
		t.Fatalf("trusted token issuers = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Applications), 1; got != want {
		t.Fatalf("applications = %d, want %d", got, want)
	}
	if snapshot.Applications[0].PortalVisibility != "ENABLED" {
		t.Fatalf("portal visibility = %q, want ENABLED", snapshot.Applications[0].PortalVisibility)
	}
	if got, want := len(snapshot.Principals), 1; got != want {
		t.Fatalf("principals = %d, want %d", got, want)
	}
	if snapshot.Principals[0].DisplayName != "platform-admins" {
		t.Fatalf("principal display name = %q", snapshot.Principals[0].DisplayName)
	}
	// The adapter must never call the inline-policy or access-scope reads. Those
	// are not part of the interface, so reaching them is impossible; this proves
	// the describe call was the only permission-set read performed.
	if fake.describePermissionSetCalls != 1 {
		t.Fatalf("DescribePermissionSet calls = %d, want 1", fake.describePermissionSetCalls)
	}
}

func TestSnapshotResolvesUserPrincipalDisplayName(t *testing.T) {
	instanceARN := "arn:aws:sso:::instance/ssoins-1111111111111111"
	permSetARN := "arn:aws:sso:::permissionSet/ssoins-1111111111111111/ps-2222222222222222"
	userID := "a1b2c3d4-1111-2222-3333-444455556666"
	fake := &fakeSSOAdmin{
		instances: []awsssoadmintypes.InstanceMetadata{{
			InstanceArn:     aws.String(instanceARN),
			IdentityStoreId: aws.String("d-9999999999"),
			Status:          awsssoadmintypes.InstanceStatusActive,
		}},
		permissionSets:        []string{permSetARN},
		describePermissionSet: &awsssoadmintypes.PermissionSet{PermissionSetArn: aws.String(permSetARN)},
		provisionedAccounts:   []string{"210987654321"},
		accountAssignments: []awsssoadmintypes.AccountAssignment{{
			AccountId:        aws.String("210987654321"),
			PermissionSetArn: aws.String(permSetARN),
			PrincipalId:      aws.String(userID),
			PrincipalType:    awsssoadmintypes.PrincipalTypeUser,
		}},
	}
	store := &fakeIdentityStore{userDisplayName: "Ada Lovelace"}
	client := newTestAdapter(fake, store)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.Principals), 1; got != want {
		t.Fatalf("principals = %d, want %d", got, want)
	}
	principal := snapshot.Principals[0]
	if got, want := strings.ToUpper(principal.Type), "USER"; got != want {
		t.Fatalf("principal type = %q, want %q", principal.Type, want)
	}
	// The USER dispatch branch must resolve through DescribeUser, not DescribeGroup.
	if principal.DisplayName != "Ada Lovelace" {
		t.Fatalf("principal display name = %q, want %q", principal.DisplayName, "Ada Lovelace")
	}
}

func TestSnapshotEmitsWarningWhenNoInstance(t *testing.T) {
	client := newTestAdapter(&fakeSSOAdmin{}, &fakeIdentityStore{})
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("warnings = %d, want %d", got, want)
	}
	if snapshot.Warnings[0].WarningKind != "identitycenter_no_instance" {
		t.Fatalf("warning kind = %q", snapshot.Warnings[0].WarningKind)
	}
}

func TestSnapshotEmitsWarningOnAccessDenied(t *testing.T) {
	fake := &fakeSSOAdmin{listInstancesErr: &smithy.GenericAPIError{Code: "AccessDeniedException"}}
	client := newTestAdapter(fake, &fakeIdentityStore{})
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("warnings = %d, want %d", got, want)
	}
	if snapshot.Warnings[0].WarningKind != "identitycenter_access_skipped" {
		t.Fatalf("warning kind = %q", snapshot.Warnings[0].WarningKind)
	}
}

func newTestAdapter(ssoAdmin ssoAdminAPI, store identityStoreAPI) *Client {
	return &Client{
		ssoAdmin:      ssoAdmin,
		identityStore: store,
		boundary:      awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceSSOAdmin},
	}
}

type fakeSSOAdmin struct {
	instances                  []awsssoadmintypes.InstanceMetadata
	listInstancesErr           error
	permissionSets             []string
	describePermissionSet      *awsssoadmintypes.PermissionSet
	describePermissionSetCalls int
	managedPolicies            []awsssoadmintypes.AttachedManagedPolicy
	customerManagedPolicies    []awsssoadmintypes.CustomerManagedPolicyReference
	provisionedAccounts        []string
	accountAssignments         []awsssoadmintypes.AccountAssignment
	applications               []awsssoadmintypes.Application
	trustedTokenIssuers        []awsssoadmintypes.TrustedTokenIssuerMetadata
	tags                       map[string]string
}

func (f *fakeSSOAdmin) ListInstances(_ context.Context, _ *awsssoadmin.ListInstancesInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListInstancesOutput, error) {
	if f.listInstancesErr != nil {
		return nil, f.listInstancesErr
	}
	return &awsssoadmin.ListInstancesOutput{Instances: f.instances}, nil
}

func (f *fakeSSOAdmin) ListPermissionSets(_ context.Context, _ *awsssoadmin.ListPermissionSetsInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListPermissionSetsOutput, error) {
	return &awsssoadmin.ListPermissionSetsOutput{PermissionSets: f.permissionSets}, nil
}

func (f *fakeSSOAdmin) DescribePermissionSet(_ context.Context, _ *awsssoadmin.DescribePermissionSetInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.DescribePermissionSetOutput, error) {
	f.describePermissionSetCalls++
	return &awsssoadmin.DescribePermissionSetOutput{PermissionSet: f.describePermissionSet}, nil
}

func (f *fakeSSOAdmin) ListManagedPoliciesInPermissionSet(_ context.Context, _ *awsssoadmin.ListManagedPoliciesInPermissionSetInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListManagedPoliciesInPermissionSetOutput, error) {
	return &awsssoadmin.ListManagedPoliciesInPermissionSetOutput{AttachedManagedPolicies: f.managedPolicies}, nil
}

func (f *fakeSSOAdmin) ListCustomerManagedPolicyReferencesInPermissionSet(_ context.Context, _ *awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetOutput, error) {
	return &awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetOutput{CustomerManagedPolicyReferences: f.customerManagedPolicies}, nil
}

func (f *fakeSSOAdmin) ListAccountsForProvisionedPermissionSet(_ context.Context, _ *awsssoadmin.ListAccountsForProvisionedPermissionSetInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListAccountsForProvisionedPermissionSetOutput, error) {
	return &awsssoadmin.ListAccountsForProvisionedPermissionSetOutput{AccountIds: f.provisionedAccounts}, nil
}

func (f *fakeSSOAdmin) ListAccountAssignments(_ context.Context, _ *awsssoadmin.ListAccountAssignmentsInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListAccountAssignmentsOutput, error) {
	return &awsssoadmin.ListAccountAssignmentsOutput{AccountAssignments: f.accountAssignments}, nil
}

func (f *fakeSSOAdmin) ListApplications(_ context.Context, _ *awsssoadmin.ListApplicationsInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListApplicationsOutput, error) {
	return &awsssoadmin.ListApplicationsOutput{Applications: f.applications}, nil
}

func (f *fakeSSOAdmin) ListTrustedTokenIssuers(_ context.Context, _ *awsssoadmin.ListTrustedTokenIssuersInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListTrustedTokenIssuersOutput, error) {
	return &awsssoadmin.ListTrustedTokenIssuersOutput{TrustedTokenIssuers: f.trustedTokenIssuers}, nil
}

func (f *fakeSSOAdmin) ListTagsForResource(_ context.Context, _ *awsssoadmin.ListTagsForResourceInput, _ ...func(*awsssoadmin.Options)) (*awsssoadmin.ListTagsForResourceOutput, error) {
	tags := make([]awsssoadmintypes.Tag, 0, len(f.tags))
	for key, value := range f.tags {
		tags = append(tags, awsssoadmintypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return &awsssoadmin.ListTagsForResourceOutput{Tags: tags}, nil
}

type fakeIdentityStore struct {
	groupDisplayName string
	userDisplayName  string
}

func (f *fakeIdentityStore) DescribeGroup(_ context.Context, _ *awsidentitystore.DescribeGroupInput, _ ...func(*awsidentitystore.Options)) (*awsidentitystore.DescribeGroupOutput, error) {
	return &awsidentitystore.DescribeGroupOutput{DisplayName: aws.String(f.groupDisplayName)}, nil
}

func (f *fakeIdentityStore) DescribeUser(_ context.Context, _ *awsidentitystore.DescribeUserInput, _ ...func(*awsidentitystore.Options)) (*awsidentitystore.DescribeUserOutput, error) {
	return &awsidentitystore.DescribeUserOutput{DisplayName: aws.String(f.userDisplayName)}, nil
}

var (
	_ ssoAdminAPI      = (*fakeSSOAdmin)(nil)
	_ identityStoreAPI = (*fakeIdentityStore)(nil)
)
