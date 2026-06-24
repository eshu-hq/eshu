// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrg "github.com/aws/aws-sdk-go-v2/service/resourcegroups"
	awsrgtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroups/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	rgservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/resourcegroups"
)

type fakeRGAPI struct {
	listGroups         []*awsrg.ListGroupsOutput
	groupQueries       map[string]*awsrg.GetGroupQueryOutput
	groupResources     map[string]*awsrg.ListGroupResourcesOutput
	listGroupsCalls    int
	groupQueryCalls    int
	groupResourceCalls int
}

func (f *fakeRGAPI) ListGroups(_ context.Context, _ *awsrg.ListGroupsInput, _ ...func(*awsrg.Options)) (*awsrg.ListGroupsOutput, error) {
	idx := f.listGroupsCalls
	f.listGroupsCalls++
	if idx < len(f.listGroups) {
		return f.listGroups[idx], nil
	}
	return &awsrg.ListGroupsOutput{}, nil
}

func (f *fakeRGAPI) GetGroupQuery(_ context.Context, in *awsrg.GetGroupQueryInput, _ ...func(*awsrg.Options)) (*awsrg.GetGroupQueryOutput, error) {
	f.groupQueryCalls++
	if out, ok := f.groupQueries[aws.ToString(in.Group)]; ok {
		return out, nil
	}
	return &awsrg.GetGroupQueryOutput{}, nil
}

func (f *fakeRGAPI) ListGroupResources(_ context.Context, in *awsrg.ListGroupResourcesInput, _ ...func(*awsrg.Options)) (*awsrg.ListGroupResourcesOutput, error) {
	f.groupResourceCalls++
	if out, ok := f.groupResources[aws.ToString(in.Group)]; ok {
		return out, nil
	}
	return &awsrg.ListGroupResourcesOutput{}, nil
}

func TestClientListGroupsProjectsQueryTypeAndMembers(t *testing.T) {
	groupARN := "arn:aws:resource-groups:us-east-1:123456789012:group/web"
	bucketARN := "arn:aws:s3:::assets"
	fake := &fakeRGAPI{
		listGroups: []*awsrg.ListGroupsOutput{{
			GroupIdentifiers: []awsrgtypes.GroupIdentifier{{
				GroupArn:    aws.String(groupARN),
				GroupName:   aws.String("web"),
				Description: aws.String("web tier"),
			}},
		}},
		groupQueries: map[string]*awsrg.GetGroupQueryOutput{
			groupARN: {GroupQuery: &awsrgtypes.GroupQuery{
				GroupName: aws.String("web"),
				ResourceQuery: &awsrgtypes.ResourceQuery{
					Type:  awsrgtypes.QueryTypeTagFilters10,
					Query: aws.String(`{"ResourceTypeFilters":["AWS::AllSupported"],"TagFilters":[{"Key":"tier","Values":["web"]}]}`),
				},
			}},
		},
		groupResources: map[string]*awsrg.ListGroupResourcesOutput{
			groupARN: {Resources: []awsrgtypes.ListGroupResourcesItem{{
				Identifier: &awsrgtypes.ResourceIdentifier{
					ResourceArn:  aws.String(bucketARN),
					ResourceType: aws.String("AWS::S3::Bucket"),
				},
			}}},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceResourceGroups},
	}
	groups, err := adapter.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if got, want := len(groups), 1; got != want {
		t.Fatalf("len(groups) = %d, want %d", got, want)
	}
	group := groups[0]
	if group.ARN != groupARN {
		t.Fatalf("group ARN = %q, want %q", group.ARN, groupARN)
	}
	if group.QueryType != "TAG_FILTERS_1_0" {
		t.Fatalf("group query type = %q, want TAG_FILTERS_1_0", group.QueryType)
	}
	if group.StackIdentifier != "" {
		t.Fatalf("tag-filter group stack identifier = %q, want empty", group.StackIdentifier)
	}
	if got, want := len(group.Members), 1; got != want {
		t.Fatalf("len(members) = %d, want %d", got, want)
	}
	if group.Members[0].ARN != bucketARN {
		t.Fatalf("member ARN = %q, want %q", group.Members[0].ARN, bucketARN)
	}
	// The tag-filter body (which carries tag keys and values) must never leak
	// into the projected group.
	if strings.Contains(group.StackIdentifier, "tier") || strings.Contains(group.Description, "TagFilters") {
		t.Fatalf("query body leaked into group projection: %#v", group)
	}
}

func TestClientExtractsStackIdentifierForCloudFormationGroup(t *testing.T) {
	groupARN := "arn:aws:resource-groups:us-east-1:123456789012:group/cfn"
	stackARN := "arn:aws:cloudformation:us-east-1:123456789012:stack/app/guid"
	fake := &fakeRGAPI{
		listGroups: []*awsrg.ListGroupsOutput{{
			GroupIdentifiers: []awsrgtypes.GroupIdentifier{{
				GroupArn:  aws.String(groupARN),
				GroupName: aws.String("cfn"),
			}},
		}},
		groupQueries: map[string]*awsrg.GetGroupQueryOutput{
			groupARN: {GroupQuery: &awsrgtypes.GroupQuery{
				ResourceQuery: &awsrgtypes.ResourceQuery{
					Type:  awsrgtypes.QueryTypeCloudformationStack10,
					Query: aws.String(`{"ResourceTypeFilters":["AWS::AllSupported"],"StackIdentifier":"` + stackARN + `"}`),
				},
			}},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceResourceGroups},
	}
	groups, err := adapter.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if got, want := groups[0].QueryType, "CLOUDFORMATION_STACK_1_0"; got != want {
		t.Fatalf("query type = %q, want %q", got, want)
	}
	if got, want := groups[0].StackIdentifier, stackARN; got != want {
		t.Fatalf("stack identifier = %q, want %q", got, want)
	}
}

func TestStackIdentifierFromQuery(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{`{"StackIdentifier":"arn:aws:cloudformation:us-east-1:1:stack/a/b"}`, "arn:aws:cloudformation:us-east-1:1:stack/a/b"},
		{`{"ResourceTypeFilters":["AWS::AllSupported"]}`, ""},
		{"", ""},
		{"not-json", ""},
	}
	for _, tc := range cases {
		if got := stackIdentifierFromQuery(tc.query); got != tc.want {
			t.Errorf("stackIdentifierFromQuery(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestClientSkipsMembersWithoutARN(t *testing.T) {
	groupARN := "arn:aws:resource-groups:us-east-1:123456789012:group/g"
	fake := &fakeRGAPI{
		listGroups: []*awsrg.ListGroupsOutput{{
			GroupIdentifiers: []awsrgtypes.GroupIdentifier{{GroupArn: aws.String(groupARN), GroupName: aws.String("g")}},
		}},
		groupResources: map[string]*awsrg.ListGroupResourcesOutput{
			groupARN: {Resources: []awsrgtypes.ListGroupResourcesItem{
				{Identifier: nil},
				{Identifier: &awsrgtypes.ResourceIdentifier{ResourceArn: aws.String("")}},
				{Identifier: &awsrgtypes.ResourceIdentifier{ResourceArn: aws.String("arn:aws:s3:::ok")}},
			}},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceResourceGroups},
	}
	groups, err := adapter.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if got, want := len(groups[0].Members), 1; got != want {
		t.Fatalf("len(members) = %d, want %d (empty/nil identifiers skipped)", got, want)
	}
}

func TestClientMetadataReadsSucceedAgainstEmptyFake(t *testing.T) {
	adapter := &Client{
		client:   &fakeRGAPI{},
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceResourceGroups},
	}
	groups, err := adapter.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("len(groups) = %d, want 0", len(groups))
	}
}

// TestAPIClientInterfaceExcludesMutationAPIs asserts the SDK-facing apiClient
// surface exposes only the metadata reads the adapter is allowed to call.
// apiClient is the single seam between scanner code and the AWS SDK client
// (Client.client is typed as apiClient, pinned by
// var _ apiClient = (*awsrg.Client)(nil)), so any SDK method the adapter could
// call must appear here. A regression that added a mutation read would fail to
// compile against this interface or trip this shape assertion.
func TestAPIClientInterfaceExcludesMutationAPIs(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	want := map[string]bool{
		"ListGroups":         true,
		"GetGroupQuery":      true,
		"ListGroupResources": true,
	}
	have := map[string]bool{}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		have[ifaceType.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("apiClient missing required metadata-read method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("apiClient exposes unexpected method %q; metadata-only contract violated", name)
		}
	}
	forbiddenSubstrings := []string{
		"Create", "Update", "Delete", "Put",
		"GroupResources", "UngroupResources", "Tag", "Untag",
	}
	for name := range have {
		if want[name] {
			continue
		}
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("apiClient method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}

var _ rgservice.Client = (*Client)(nil)
