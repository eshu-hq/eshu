// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsgrafana "github.com/aws/aws-sdk-go-v2/service/grafana"
	awsgrafanatypes "github.com/aws/aws-sdk-go-v2/service/grafana/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsGrafanaMetadataOnly(t *testing.T) {
	created := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeGrafanaAPI{
		workspacePages: []*awsgrafana.ListWorkspacesOutput{{
			Workspaces: []awsgrafanatypes.WorkspaceSummary{{Id: aws.String("g-abcd123456")}},
		}},
		descriptions: map[string]*awsgrafanatypes.WorkspaceDescription{
			"g-abcd123456": {
				Id:                aws.String("g-abcd123456"),
				Name:              aws.String("observability"),
				Status:            awsgrafanatypes.WorkspaceStatusActive,
				GrafanaVersion:    aws.String("10.4"),
				Endpoint:          aws.String("g-abcd123456.grafana-workspace.us-east-1.amazonaws.com"),
				AccountAccessType: awsgrafanatypes.AccountAccessTypeCurrentAccount,
				PermissionType:    awsgrafanatypes.PermissionTypeServiceManaged,
				WorkspaceRoleArn:  aws.String("arn:aws:iam::123456789012:role/grafana-workspace-role"),
				DataSources:       []awsgrafanatypes.DataSourceType{awsgrafanatypes.DataSourceTypePrometheus, awsgrafanatypes.DataSourceTypeCloudwatch},
				Authentication: &awsgrafanatypes.AuthenticationSummary{
					Providers: []awsgrafanatypes.AuthenticationProviderTypes{awsgrafanatypes.AuthenticationProviderTypesAwsSso},
				},
				VpcConfiguration: &awsgrafanatypes.VpcConfiguration{
					SubnetIds:        []string{"subnet-0123456789abcdef0", "subnet-0123456789abcdef1"},
					SecurityGroupIds: []string{"sg-0123456789abcdef0"},
				},
				Created:  aws.Time(created),
				Modified: aws.Time(created),
			},
		},
		tags: map[string]map[string]string{
			"arn:aws:grafana:us-east-1:123456789012:/workspaces/g-abcd123456": {"Environment": "prod"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Workspaces) != 1 {
		t.Fatalf("len(Workspaces) = %d, want 1", len(snapshot.Workspaces))
	}
	workspace := snapshot.Workspaces[0]
	wantARN := "arn:aws:grafana:us-east-1:123456789012:/workspaces/g-abcd123456"
	if workspace.ARN != wantARN {
		t.Fatalf("workspace ARN = %q, want %q (partition-aware synthesis)", workspace.ARN, wantARN)
	}
	if workspace.WorkspaceRoleARN != "arn:aws:iam::123456789012:role/grafana-workspace-role" {
		t.Fatalf("workspace role = %q", workspace.WorkspaceRoleARN)
	}
	if len(workspace.DataSources) != 2 || workspace.DataSources[0] != "PROMETHEUS" {
		t.Fatalf("data sources = %#v, want [PROMETHEUS CLOUDWATCH]", workspace.DataSources)
	}
	if len(workspace.AuthenticationProviders) != 1 || workspace.AuthenticationProviders[0] != "AWS_SSO" {
		t.Fatalf("auth providers = %#v, want [AWS_SSO]", workspace.AuthenticationProviders)
	}
	if len(workspace.SubnetIDs) != 2 {
		t.Fatalf("subnet ids = %#v, want 2", workspace.SubnetIDs)
	}
	if len(workspace.SecurityGroupIDs) != 1 || workspace.SecurityGroupIDs[0] != "sg-0123456789abcdef0" {
		t.Fatalf("security group ids = %#v, want [sg-0123456789abcdef0]", workspace.SecurityGroupIDs)
	}
	if workspace.Tags["Environment"] != "prod" {
		t.Fatalf("workspace tag Environment = %q, want prod", workspace.Tags["Environment"])
	}
}

func TestClientSynthesizesGovCloudWorkspaceARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	api := &fakeGrafanaAPI{
		workspacePages: []*awsgrafana.ListWorkspacesOutput{{
			Workspaces: []awsgrafanatypes.WorkspaceSummary{{Id: aws.String("g-gov12345")}},
		}},
		descriptions: map[string]*awsgrafanatypes.WorkspaceDescription{
			"g-gov12345": {Id: aws.String("g-gov12345"), Status: awsgrafanatypes.WorkspaceStatusActive},
		},
	}
	client := &Client{client: api, boundary: boundary}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	wantARN := "arn:aws-us-gov:grafana:us-gov-west-1:123456789012:/workspaces/g-gov12345"
	if got := snapshot.Workspaces[0].ARN; got != wantARN {
		t.Fatalf("GovCloud workspace ARN = %q, want %q", got, wantARN)
	}
}

func TestClientReturnsEmptyForEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeGrafanaAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Workspaces) != 0 {
		t.Fatalf("len(Workspaces) = %d, want 0 for empty account", len(snapshot.Workspaces))
	}
}

type fakeGrafanaAPI struct {
	workspacePages []*awsgrafana.ListWorkspacesOutput
	listCall       int
	descriptions   map[string]*awsgrafanatypes.WorkspaceDescription
	tags           map[string]map[string]string
}

func (f *fakeGrafanaAPI) ListWorkspaces(
	_ context.Context,
	_ *awsgrafana.ListWorkspacesInput,
	_ ...func(*awsgrafana.Options),
) (*awsgrafana.ListWorkspacesOutput, error) {
	if f.listCall >= len(f.workspacePages) {
		return &awsgrafana.ListWorkspacesOutput{}, nil
	}
	page := f.workspacePages[f.listCall]
	f.listCall++
	return page, nil
}

func (f *fakeGrafanaAPI) DescribeWorkspace(
	_ context.Context,
	input *awsgrafana.DescribeWorkspaceInput,
	_ ...func(*awsgrafana.Options),
) (*awsgrafana.DescribeWorkspaceOutput, error) {
	desc := f.descriptions[aws.ToString(input.WorkspaceId)]
	return &awsgrafana.DescribeWorkspaceOutput{Workspace: desc}, nil
}

func (f *fakeGrafanaAPI) ListTagsForResource(
	_ context.Context,
	input *awsgrafana.ListTagsForResourceInput,
	_ ...func(*awsgrafana.Options),
) (*awsgrafana.ListTagsForResourceOutput, error) {
	return &awsgrafana.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceGrafana,
	}
}
