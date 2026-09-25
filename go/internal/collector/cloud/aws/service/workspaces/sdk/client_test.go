// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsworkspaces "github.com/aws/aws-sdk-go-v2/service/workspaces"
	awsworkspacestypes "github.com/aws/aws-sdk-go-v2/service/workspaces/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsWorkSpacesMetadataOnly(t *testing.T) {
	api := &fakeWorkSpacesAPI{
		directoryPages: []*awsworkspaces.DescribeWorkspaceDirectoriesOutput{
			{
				Directories: []awsworkspacestypes.WorkspaceDirectory{{
					DirectoryId:              awsv2.String("d-1234567890"),
					DirectoryName:            awsv2.String("corp.example.com"),
					State:                    awsworkspacestypes.WorkspaceDirectoryStateRegistered,
					DirectoryType:            awsworkspacestypes.WorkspaceDirectoryTypeAdConnector,
					IamRoleId:                awsv2.String("arn:aws:iam::123456789012:role/workspaces_DefaultRole"),
					WorkspaceSecurityGroupId: awsv2.String("sg-cccc3333"),
					SubnetIds:                []string{"subnet-aaaa1111", "subnet-bbbb2222"},
					IpGroupIds:               []string{"wsipg-1234567890"},
					RegistrationCode:         awsv2.String("SLiad+ABCDEF"), // must NOT leak into the model
				}},
				NextToken: awsv2.String("more"),
			},
			{Directories: []awsworkspacestypes.WorkspaceDirectory{}},
		},
		bundlePages: []*awsworkspaces.DescribeWorkspaceBundlesOutput{{
			Bundles: []awsworkspacestypes.WorkspaceBundle{{
				BundleId:    awsv2.String("wsb-1234567890"),
				Name:        awsv2.String("Standard"),
				Owner:       awsv2.String("AMAZON"),
				BundleType:  awsworkspacestypes.BundleTypeRegular,
				ComputeType: &awsworkspacestypes.ComputeType{Name: awsworkspacestypes.ComputeStandard},
				RootStorage: &awsworkspacestypes.RootStorage{Capacity: awsv2.String("80")},
				UserStorage: &awsworkspacestypes.UserStorage{Capacity: awsv2.String("50")},
				ImageId:     awsv2.String("wsi-abc123"),
			}},
		}},
		ipGroupPages: []*awsworkspaces.DescribeIpGroupsOutput{{
			Result: []awsworkspacestypes.WorkspacesIpGroup{{
				GroupId:   awsv2.String("wsipg-1234567890"),
				GroupName: awsv2.String("office-cidrs"),
				GroupDesc: awsv2.String("corp ranges"),
				UserRules: []awsworkspacestypes.IpRuleItem{
					{IpRule: awsv2.String("203.0.113.0/24"), RuleDesc: awsv2.String("hq")},
				},
			}},
		}},
		workspacePages: []*awsworkspaces.DescribeWorkspacesOutput{{
			Workspaces: []awsworkspacestypes.Workspace{{
				WorkspaceId:                 awsv2.String("ws-1234567890"),
				DirectoryId:                 awsv2.String("d-1234567890"),
				BundleId:                    awsv2.String("wsb-1234567890"),
				State:                       awsworkspacestypes.WorkspaceStateAvailable,
				UserName:                    awsv2.String("alice"),
				ComputerName:                awsv2.String("EC2AMAZ-ABC"),
				VolumeEncryptionKey:         awsv2.String("arn:aws:kms:us-east-1:123456789012:key/1234abcd"),
				RootVolumeEncryptionEnabled: awsv2.Bool(true),
				IpAddress:                   awsv2.String("10.0.0.5"), // must NOT leak into the model
			}},
		}},
		tags: map[string][]awsworkspacestypes.Tag{
			"ws-1234567890": {{Key: awsv2.String("CostCenter"), Value: awsv2.String("1234")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Directories) != 1 {
		t.Fatalf("len(Directories) = %d, want 1", len(snapshot.Directories))
	}
	directory := snapshot.Directories[0]
	if directory.ID != "d-1234567890" {
		t.Fatalf("directory ID = %q, want d-1234567890", directory.ID)
	}
	if directory.DirectoryType != "AD_CONNECTOR" {
		t.Fatalf("directory type = %q, want AD_CONNECTOR", directory.DirectoryType)
	}
	if len(directory.SubnetIDs) != 2 {
		t.Fatalf("directory subnets = %#v, want 2", directory.SubnetIDs)
	}
	if len(directory.IPGroupIDs) != 1 || directory.IPGroupIDs[0] != "wsipg-1234567890" {
		t.Fatalf("directory ip groups = %#v, want [wsipg-1234567890]", directory.IPGroupIDs)
	}

	if len(snapshot.Bundles) != 1 {
		t.Fatalf("len(Bundles) = %d, want 1", len(snapshot.Bundles))
	}
	bundle := snapshot.Bundles[0]
	if bundle.ComputeType != "STANDARD" {
		t.Fatalf("bundle compute type = %q, want STANDARD", bundle.ComputeType)
	}
	if bundle.RootVolumeSizeGib != "80" || bundle.UserVolumeSizeGib != "50" {
		t.Fatalf("bundle volumes = %q/%q, want 80/50", bundle.RootVolumeSizeGib, bundle.UserVolumeSizeGib)
	}

	if len(snapshot.IPGroups) != 1 {
		t.Fatalf("len(IPGroups) = %d, want 1", len(snapshot.IPGroups))
	}
	group := snapshot.IPGroups[0]
	if len(group.Rules) != 1 || group.Rules[0].CIDR != "203.0.113.0/24" {
		t.Fatalf("ip group rules = %#v, want one 203.0.113.0/24", group.Rules)
	}

	if len(snapshot.Workspaces) != 1 {
		t.Fatalf("len(Workspaces) = %d, want 1", len(snapshot.Workspaces))
	}
	workspace := snapshot.Workspaces[0]
	if workspace.ID != "ws-1234567890" {
		t.Fatalf("workspace ID = %q, want ws-1234567890", workspace.ID)
	}
	if workspace.State != "AVAILABLE" {
		t.Fatalf("workspace state = %q, want AVAILABLE", workspace.State)
	}
	if workspace.UserName != "alice" {
		t.Fatalf("workspace user = %q, want alice", workspace.UserName)
	}
	if workspace.VolumeEncryptionKey != "arn:aws:kms:us-east-1:123456789012:key/1234abcd" {
		t.Fatalf("workspace key = %q, want the kms ARN", workspace.VolumeEncryptionKey)
	}
	if !workspace.RootVolumeEncryptionEnabled {
		t.Fatalf("RootVolumeEncryptionEnabled = false, want true")
	}
	if workspace.Tags["CostCenter"] != "1234" {
		t.Fatalf("workspace tag CostCenter = %q, want 1234", workspace.Tags["CostCenter"])
	}
	if api.workspaceCalls != 1 {
		t.Fatalf("DescribeWorkspaces calls = %d, want 1", api.workspaceCalls)
	}
	if api.directoryCalls != 2 {
		t.Fatalf("DescribeWorkspaceDirectories calls = %d, want 2 (paginated)", api.directoryCalls)
	}
}

type fakeWorkSpacesAPI struct {
	directoryPages []*awsworkspaces.DescribeWorkspaceDirectoriesOutput
	directoryCalls int
	bundlePages    []*awsworkspaces.DescribeWorkspaceBundlesOutput
	bundleCalls    int
	ipGroupPages   []*awsworkspaces.DescribeIpGroupsOutput
	ipGroupCalls   int
	workspacePages []*awsworkspaces.DescribeWorkspacesOutput
	workspaceCalls int
	tags           map[string][]awsworkspacestypes.Tag
}

func (f *fakeWorkSpacesAPI) DescribeWorkspaces(
	_ context.Context,
	_ *awsworkspaces.DescribeWorkspacesInput,
	_ ...func(*awsworkspaces.Options),
) (*awsworkspaces.DescribeWorkspacesOutput, error) {
	if f.workspaceCalls >= len(f.workspacePages) {
		return &awsworkspaces.DescribeWorkspacesOutput{}, nil
	}
	page := f.workspacePages[f.workspaceCalls]
	f.workspaceCalls++
	return page, nil
}

func (f *fakeWorkSpacesAPI) DescribeWorkspaceDirectories(
	_ context.Context,
	_ *awsworkspaces.DescribeWorkspaceDirectoriesInput,
	_ ...func(*awsworkspaces.Options),
) (*awsworkspaces.DescribeWorkspaceDirectoriesOutput, error) {
	if f.directoryCalls >= len(f.directoryPages) {
		return &awsworkspaces.DescribeWorkspaceDirectoriesOutput{}, nil
	}
	page := f.directoryPages[f.directoryCalls]
	f.directoryCalls++
	return page, nil
}

func (f *fakeWorkSpacesAPI) DescribeWorkspaceBundles(
	_ context.Context,
	_ *awsworkspaces.DescribeWorkspaceBundlesInput,
	_ ...func(*awsworkspaces.Options),
) (*awsworkspaces.DescribeWorkspaceBundlesOutput, error) {
	if f.bundleCalls >= len(f.bundlePages) {
		return &awsworkspaces.DescribeWorkspaceBundlesOutput{}, nil
	}
	page := f.bundlePages[f.bundleCalls]
	f.bundleCalls++
	return page, nil
}

func (f *fakeWorkSpacesAPI) DescribeIpGroups(
	_ context.Context,
	_ *awsworkspaces.DescribeIpGroupsInput,
	_ ...func(*awsworkspaces.Options),
) (*awsworkspaces.DescribeIpGroupsOutput, error) {
	if f.ipGroupCalls >= len(f.ipGroupPages) {
		return &awsworkspaces.DescribeIpGroupsOutput{}, nil
	}
	page := f.ipGroupPages[f.ipGroupCalls]
	f.ipGroupCalls++
	return page, nil
}

func (f *fakeWorkSpacesAPI) DescribeTags(
	_ context.Context,
	input *awsworkspaces.DescribeTagsInput,
	_ ...func(*awsworkspaces.Options),
) (*awsworkspaces.DescribeTagsOutput, error) {
	return &awsworkspaces.DescribeTagsOutput{
		TagList: f.tags[awsv2.ToString(input.ResourceId)],
	}, nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceWorkSpaces,
	}
}
