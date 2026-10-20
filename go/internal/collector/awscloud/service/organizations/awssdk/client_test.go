// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsorg "github.com/aws/aws-sdk-go-v2/service/organizations"
	awsorgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotReadsOrganizationsMetadataOnly(t *testing.T) {
	client := &fakeOrganizationsAPI{
		describeOrganization: &awsorg.DescribeOrganizationOutput{
			Organization: &awsorgtypes.Organization{
				Arn:             aws.String("arn:aws:organizations::123456789012:organization/o-exampleorgid"),
				Id:              aws.String("o-exampleorgid"),
				MasterAccountId: aws.String("123456789012"),
				FeatureSet:      awsorgtypes.OrganizationFeatureSetAll,
			},
		},
		roots: []awsorgtypes.Root{{
			Arn:  aws.String("arn:aws:organizations::123456789012:root/o-exampleorgid/r-root"),
			Id:   aws.String("r-root"),
			Name: aws.String("Root"),
			PolicyTypes: []awsorgtypes.PolicyTypeSummary{{
				Type:   awsorgtypes.PolicyTypeServiceControlPolicy,
				Status: awsorgtypes.PolicyTypeStatusEnabled,
			}},
		}},
		childOUs: map[string][]awsorgtypes.OrganizationalUnit{
			"r-root": {{
				Arn:  aws.String("arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform"),
				Id:   aws.String("ou-root-platform"),
				Name: aws.String("Platform"),
			}},
		},
		childAccounts: map[string][]awsorgtypes.Account{
			"ou-root-platform": {{
				Arn:             aws.String("arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333"),
				Email:           aws.String("owner@example.com"),
				Id:              aws.String("111122223333"),
				JoinedMethod:    awsorgtypes.AccountJoinedMethodInvited,
				JoinedTimestamp: aws.Time(time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC)),
				Name:            aws.String("payments-prod"),
				Status:          awsorgtypes.AccountStatusActive,
			}},
		},
		policies: map[awsorgtypes.PolicyType][]awsorgtypes.PolicySummary{
			awsorgtypes.PolicyTypeServiceControlPolicy: {{
				Arn:         aws.String("arn:aws:organizations::123456789012:policy/o-exampleorgid/service_control_policy/p-abcd1234"),
				AwsManaged:  false,
				Description: aws.String("baseline policy"),
				Id:          aws.String("p-abcd1234"),
				Name:        aws.String("deny-public-s3"),
				Type:        awsorgtypes.PolicyTypeServiceControlPolicy,
			}},
		},
		targets: map[string][]awsorgtypes.PolicyTargetSummary{
			"p-abcd1234": {{
				Arn:      aws.String("arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform"),
				Name:     aws.String("Platform"),
				TargetId: aws.String("ou-root-platform"),
				Type:     awsorgtypes.TargetTypeOrganizationalUnit,
			}},
		},
		delegatedAdmins: []awsorgtypes.DelegatedAdministrator{{
			Arn:                   aws.String("arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333"),
			DelegationEnabledDate: aws.Time(time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)),
			Id:                    aws.String("111122223333"),
		}},
		delegatedServices: map[string][]awsorgtypes.DelegatedService{
			"111122223333": {{
				DelegationEnabledDate: aws.Time(time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)),
				ServicePrincipal:      aws.String("config.amazonaws.com"),
			}},
		},
		tags: []awsorgtypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceOrganizations},
	}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := snapshot.Organization.ID, "o-exampleorgid"; got != want {
		t.Fatalf("Organization.ID = %q, want %q", got, want)
	}
	if got, want := len(snapshot.Roots), 1; got != want {
		t.Fatalf("len(Roots) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.OrganizationalUnits), 1; got != want {
		t.Fatalf("len(OrganizationalUnits) = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Accounts), 1; got != want {
		t.Fatalf("len(Accounts) = %d, want %d", got, want)
	}
	if got, want := snapshot.Accounts[0].Email, "owner@example.com"; got != want {
		t.Fatalf("Account.Email = %q, want raw value preserved for scanner redaction", got)
	}
	if got, want := len(snapshot.Policies), 1; got != want {
		t.Fatalf("len(Policies) = %d, want %d", got, want)
	}
	if len(snapshot.Policies[0].Targets) != 1 {
		t.Fatalf("policy targets = %#v, want one target", snapshot.Policies[0].Targets)
	}
	if got, want := len(snapshot.DelegatedAdministrators), 1; got != want {
		t.Fatalf("len(DelegatedAdministrators) = %d, want %d", got, want)
	}
	if got, want := snapshot.DelegatedAdministrators[0].ServicePrincipal, "config.amazonaws.com"; got != want {
		t.Fatalf("delegated service principal = %q, want %q", got, want)
	}
}

func TestClientSnapshotSkipsUnavailablePolicyFamilies(t *testing.T) {
	client := &fakeOrganizationsAPI{
		describeOrganization: &awsorg.DescribeOrganizationOutput{
			Organization: &awsorgtypes.Organization{
				Id:         aws.String("o-exampleorgid"),
				FeatureSet: awsorgtypes.OrganizationFeatureSetAll,
			},
		},
		roots: []awsorgtypes.Root{{
			Id: aws.String("r-root"),
			PolicyTypes: []awsorgtypes.PolicyTypeSummary{{
				Type:   awsorgtypes.PolicyTypeServiceControlPolicy,
				Status: awsorgtypes.PolicyTypeStatusEnabled,
			}},
		}},
		policies: map[awsorgtypes.PolicyType][]awsorgtypes.PolicySummary{
			awsorgtypes.PolicyTypeServiceControlPolicy: {{
				Id:   aws.String("p-scp"),
				Name: aws.String("baseline"),
				Type: awsorgtypes.PolicyTypeServiceControlPolicy,
			}},
		},
		policyErrors: map[awsorgtypes.PolicyType]error{
			awsorgtypes.PolicyTypeResourceControlPolicy: &awsorgtypes.PolicyTypeNotEnabledException{},
			awsorgtypes.PolicyTypeTagPolicy:             &awsorgtypes.PolicyTypeNotAvailableForOrganizationException{},
		},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceOrganizations},
	}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil disabled policy families skipped", err)
	}
	if got, want := len(snapshot.Policies), 1; got != want {
		t.Fatalf("len(Policies) = %d, want %d", got, want)
	}
	if got, want := snapshot.Policies[0].Type, string(awsorgtypes.PolicyTypeServiceControlPolicy); got != want {
		t.Fatalf("policy type = %q, want %q", got, want)
	}
}

func TestClientSnapshotSkipsWhenCredentialsAreNotOrgAware(t *testing.T) {
	client := &fakeOrganizationsAPI{
		describeOrganization: &awsorg.DescribeOrganizationOutput{
			Organization: &awsorgtypes.Organization{Id: aws.String("o-exampleorgid")},
		},
		listRootsErr: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "not authorized"},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "222233334444", Region: "us-west-2", ServiceKind: awscloud.ServiceOrganizations},
	}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil skipped snapshot", err)
	}
	if got, want := len(snapshot.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	if got := snapshot.Warnings[0].WarningKind; got != awscloud.WarningOrganizationsOrgAccessSkipped {
		t.Fatalf("warning kind = %q, want %q", got, awscloud.WarningOrganizationsOrgAccessSkipped)
	}
	if got := snapshot.Warnings[0].Attributes["skip_reason"]; got != "org_access_denied" {
		t.Fatalf("skip_reason = %#v, want org_access_denied", got)
	}
}

func TestNewClientForcesOrganizationsEndpointRegion(t *testing.T) {
	config := aws.Config{Region: "us-west-2"}
	client := NewClient(config, awscloud.Boundary{ServiceKind: awscloud.ServiceOrganizations}, nil, nil)
	if got, want := client.region, OrganizationsEndpointRegion; got != want {
		t.Fatalf("client region = %q, want %q", got, want)
	}
}

type fakeOrganizationsAPI struct {
	describeOrganization *awsorg.DescribeOrganizationOutput
	describeErr          error
	listRootsErr         error
	roots                []awsorgtypes.Root
	childOUs             map[string][]awsorgtypes.OrganizationalUnit
	childAccounts        map[string][]awsorgtypes.Account
	policies             map[awsorgtypes.PolicyType][]awsorgtypes.PolicySummary
	policyErrors         map[awsorgtypes.PolicyType]error
	targets              map[string][]awsorgtypes.PolicyTargetSummary
	delegatedAdmins      []awsorgtypes.DelegatedAdministrator
	delegatedServices    map[string][]awsorgtypes.DelegatedService
	tags                 []awsorgtypes.Tag
}

func (f *fakeOrganizationsAPI) DescribeOrganization(
	context.Context,
	*awsorg.DescribeOrganizationInput,
	...func(*awsorg.Options),
) (*awsorg.DescribeOrganizationOutput, error) {
	return f.describeOrganization, f.describeErr
}

func (f *fakeOrganizationsAPI) ListRoots(
	context.Context,
	*awsorg.ListRootsInput,
	...func(*awsorg.Options),
) (*awsorg.ListRootsOutput, error) {
	if f.listRootsErr != nil {
		return nil, f.listRootsErr
	}
	return &awsorg.ListRootsOutput{Roots: f.roots}, nil
}

func (f *fakeOrganizationsAPI) ListOrganizationalUnitsForParent(
	_ context.Context,
	input *awsorg.ListOrganizationalUnitsForParentInput,
	_ ...func(*awsorg.Options),
) (*awsorg.ListOrganizationalUnitsForParentOutput, error) {
	return &awsorg.ListOrganizationalUnitsForParentOutput{
		OrganizationalUnits: f.childOUs[aws.ToString(input.ParentId)],
	}, nil
}

func (f *fakeOrganizationsAPI) ListAccountsForParent(
	_ context.Context,
	input *awsorg.ListAccountsForParentInput,
	_ ...func(*awsorg.Options),
) (*awsorg.ListAccountsForParentOutput, error) {
	return &awsorg.ListAccountsForParentOutput{
		Accounts: f.childAccounts[aws.ToString(input.ParentId)],
	}, nil
}

func (f *fakeOrganizationsAPI) ListPolicies(
	_ context.Context,
	input *awsorg.ListPoliciesInput,
	_ ...func(*awsorg.Options),
) (*awsorg.ListPoliciesOutput, error) {
	if err := f.policyErrors[input.Filter]; err != nil {
		return nil, err
	}
	return &awsorg.ListPoliciesOutput{Policies: f.policies[input.Filter]}, nil
}

func (f *fakeOrganizationsAPI) ListTargetsForPolicy(
	_ context.Context,
	input *awsorg.ListTargetsForPolicyInput,
	_ ...func(*awsorg.Options),
) (*awsorg.ListTargetsForPolicyOutput, error) {
	return &awsorg.ListTargetsForPolicyOutput{Targets: f.targets[aws.ToString(input.PolicyId)]}, nil
}

func (f *fakeOrganizationsAPI) ListDelegatedAdministrators(
	context.Context,
	*awsorg.ListDelegatedAdministratorsInput,
	...func(*awsorg.Options),
) (*awsorg.ListDelegatedAdministratorsOutput, error) {
	return &awsorg.ListDelegatedAdministratorsOutput{DelegatedAdministrators: f.delegatedAdmins}, nil
}

func (f *fakeOrganizationsAPI) ListDelegatedServicesForAccount(
	_ context.Context,
	input *awsorg.ListDelegatedServicesForAccountInput,
	_ ...func(*awsorg.Options),
) (*awsorg.ListDelegatedServicesForAccountOutput, error) {
	return &awsorg.ListDelegatedServicesForAccountOutput{
		DelegatedServices: f.delegatedServices[aws.ToString(input.AccountId)],
	}, nil
}

func (f *fakeOrganizationsAPI) ListTagsForResource(
	context.Context,
	*awsorg.ListTagsForResourceInput,
	...func(*awsorg.Options),
) (*awsorg.ListTagsForResourceOutput, error) {
	return &awsorg.ListTagsForResourceOutput{Tags: f.tags}, nil
}

var _ apiClient = (*fakeOrganizationsAPI)(nil)
