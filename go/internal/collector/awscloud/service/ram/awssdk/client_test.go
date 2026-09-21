// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsram "github.com/aws/aws-sdk-go-v2/service/ram"
	awsramtypes "github.com/aws/aws-sdk-go-v2/service/ram/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

type fakeRAMAPI struct {
	sharePages       []*awsram.GetResourceSharesOutput
	shareCalls       int
	resourcesByShare map[string][]awsramtypes.Resource
	principalsByShr  map[string][]awsramtypes.Principal
	permsByShare     map[string][]awsramtypes.ResourceSharePermissionSummary

	resourceOwners []awsramtypes.ResourceOwner
}

func (f *fakeRAMAPI) GetResourceShares(
	_ context.Context,
	input *awsram.GetResourceSharesInput,
	_ ...func(*awsram.Options),
) (*awsram.GetResourceSharesOutput, error) {
	f.resourceOwners = append(f.resourceOwners, input.ResourceOwner)
	if f.shareCalls >= len(f.sharePages) {
		return &awsram.GetResourceSharesOutput{}, nil
	}
	page := f.sharePages[f.shareCalls]
	f.shareCalls++
	return page, nil
}

func (f *fakeRAMAPI) ListResources(
	_ context.Context,
	input *awsram.ListResourcesInput,
	_ ...func(*awsram.Options),
) (*awsram.ListResourcesOutput, error) {
	f.resourceOwners = append(f.resourceOwners, input.ResourceOwner)
	arn := ""
	if len(input.ResourceShareArns) > 0 {
		arn = input.ResourceShareArns[0]
	}
	return &awsram.ListResourcesOutput{Resources: f.resourcesByShare[arn]}, nil
}

func (f *fakeRAMAPI) ListPrincipals(
	_ context.Context,
	input *awsram.ListPrincipalsInput,
	_ ...func(*awsram.Options),
) (*awsram.ListPrincipalsOutput, error) {
	f.resourceOwners = append(f.resourceOwners, input.ResourceOwner)
	arn := ""
	if len(input.ResourceShareArns) > 0 {
		arn = input.ResourceShareArns[0]
	}
	return &awsram.ListPrincipalsOutput{Principals: f.principalsByShr[arn]}, nil
}

func (f *fakeRAMAPI) ListResourceSharePermissions(
	_ context.Context,
	input *awsram.ListResourceSharePermissionsInput,
	_ ...func(*awsram.Options),
) (*awsram.ListResourceSharePermissionsOutput, error) {
	arn := aws.ToString(input.ResourceShareArn)
	return &awsram.ListResourceSharePermissionsOutput{Permissions: f.permsByShare[arn]}, nil
}

func TestClientListResourceSharesReadsOwnerSelfMetadata(t *testing.T) {
	shareARN := "ram-arn:share/orders"
	api := &fakeRAMAPI{
		sharePages: []*awsram.GetResourceSharesOutput{{
			ResourceShares: []awsramtypes.ResourceShare{{
				ResourceShareArn:        aws.String(shareARN),
				Name:                    aws.String("orders-share"),
				Status:                  awsramtypes.ResourceShareStatusActive,
				OwningAccountId:         aws.String("123456789012"),
				AllowExternalPrincipals: aws.Bool(true),
				FeatureSet:              awsramtypes.ResourceShareFeatureSetStandard,
				Tags:                    []awsramtypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
			}},
		}},
		resourcesByShare: map[string][]awsramtypes.Resource{
			shareARN: {{
				Arn:                 aws.String("ec2-arn:subnet/subnet-abc"),
				Type:                aws.String("ec2:subnet"),
				Status:              awsramtypes.ResourceStatusAvailable,
				ResourceRegionScope: awsramtypes.ResourceRegionScopeRegional,
			}},
		},
		principalsByShr: map[string][]awsramtypes.Principal{
			shareARN: {{Id: aws.String("210987654321"), External: aws.Bool(false)}},
		},
		permsByShare: map[string][]awsramtypes.ResourceSharePermissionSummary{
			shareARN: {{
				Arn:            aws.String("ram-arn:permission/subnet"),
				Name:           aws.String("AWSRAMDefaultPermissionSubnet"),
				Version:        aws.String("3"),
				PermissionType: awsramtypes.PermissionTypeAwsManaged,
				ResourceType:   aws.String("ec2:subnet"),
				DefaultVersion: aws.Bool(true),
			}},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceRAM},
	}

	shares, err := adapter.ListResourceShares(context.Background())
	if err != nil {
		t.Fatalf("ListResourceShares() error = %v, want nil", err)
	}
	if got, want := len(shares), 1; got != want {
		t.Fatalf("len(shares) = %d, want %d", got, want)
	}
	share := shares[0]
	if share.ARN != shareARN {
		t.Fatalf("ARN = %q, want %q", share.ARN, shareARN)
	}
	if share.Status != "ACTIVE" {
		t.Fatalf("Status = %q, want ACTIVE", share.Status)
	}
	if !share.AllowExternalPrincipals {
		t.Fatalf("AllowExternalPrincipals = false, want true")
	}
	if share.OwningAccountID != "123456789012" {
		t.Fatalf("OwningAccountID = %q, want 123456789012", share.OwningAccountID)
	}
	if share.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", share.Tags)
	}
	if got, want := len(share.Resources), 1; got != want {
		t.Fatalf("len(Resources) = %d, want %d", got, want)
	}
	if share.Resources[0].Type != "ec2:subnet" {
		t.Fatalf("Resource Type = %q, want ec2:subnet", share.Resources[0].Type)
	}
	if got, want := len(share.Principals), 1; got != want {
		t.Fatalf("len(Principals) = %d, want %d", got, want)
	}
	if share.Principals[0].ID != "210987654321" {
		t.Fatalf("Principal ID = %q, want 210987654321", share.Principals[0].ID)
	}
	if got, want := len(share.Permissions), 1; got != want {
		t.Fatalf("len(Permissions) = %d, want %d", got, want)
	}
	if share.Permissions[0].Version != "3" {
		t.Fatalf("Permission Version = %q, want 3", share.Permissions[0].Version)
	}
	for _, owner := range api.resourceOwners {
		if owner != awsramtypes.ResourceOwnerSelf {
			t.Fatalf("read used ResourceOwner %q, want SELF (owner-account inventory only)", owner)
		}
	}
}

func TestClientListResourceSharesPaginatesShares(t *testing.T) {
	api := &fakeRAMAPI{
		sharePages: []*awsram.GetResourceSharesOutput{
			{
				ResourceShares: []awsramtypes.ResourceShare{{ResourceShareArn: aws.String("ram-arn:share/a")}},
				NextToken:      aws.String("page-2"),
			},
			{
				ResourceShares: []awsramtypes.ResourceShare{{ResourceShareArn: aws.String("ram-arn:share/b")}},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceRAM},
	}

	shares, err := adapter.ListResourceShares(context.Background())
	if err != nil {
		t.Fatalf("ListResourceShares() error = %v, want nil", err)
	}
	if got, want := len(shares), 2; got != want {
		t.Fatalf("len(shares) = %d, want %d (both pages drained)", got, want)
	}
	if api.shareCalls != 2 {
		t.Fatalf("GetResourceShares calls = %d, want 2", api.shareCalls)
	}
}

var _ apiClient = (*fakeRAMAPI)(nil)
