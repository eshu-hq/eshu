// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdetective "github.com/aws/aws-sdk-go-v2/service/detective"
	detectivetypes "github.com/aws/aws-sdk-go-v2/service/detective/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	detectiveservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/detective"
)

// TestAPIClientInterfaceExcludesInvestigationAndMutationAPIs is the security
// gate for the Amazon Detective SDK adapter. Detective's investigation,
// indicator, and finding-datasource reads expose security-investigation
// content, and its create/delete/tag/invitation operations mutate state. The
// adapter must reach none of them. The test reflects over the adapter's
// internal apiClient interface and FAILS if a forbidden operation becomes
// reachable, while the three allowed metadata list reads stay reachable.
func TestAPIClientInterfaceExcludesInvestigationAndMutationAPIs(t *testing.T) {
	allowed := map[string]struct{}{
		"ListGraphs":          {},
		"ListMembers":         {},
		"ListTagsForResource": {},
	}
	forbidden := []string{
		// Investigation / indicator reads expose security-investigation content.
		"GetInvestigation", "ListInvestigations", "StartInvestigation",
		"UpdateInvestigationState", "ListIndicators",
		// Datasource and member detail reads.
		"BatchGetGraphMemberDatasources", "BatchGetMembershipDatasources",
		"GetMembers", "ListDatasourcePackages", "UpdateDatasourcePackages",
		// Mutation surface.
		"Create", "Delete", "Update", "Tag", "Untag", "Put",
		"Accept", "Reject", "Disassociate", "Enable", "Disable",
		"Start", "Stop", "Monitoring",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient exposes method %q outside the metadata-only allow-set", name)
		}
	}
	// The forbidden-substring scan runs only against methods outside the
	// allow-set, so the safe ListTagsForResource read is not mis-flagged by the
	// "Tag" mutation token while any newly added method is still caught.
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if _, ok := allowed[name]; ok {
			continue
		}
		for _, banned := range forbidden {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes method %q containing forbidden operation %q; Detective adapter is metadata-only", name, banned)
			}
		}
	}
}

func TestClientReadsGraphsMembersAndTagsAndDropsEmail(t *testing.T) {
	created := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	graphARN := "arn:aws:detective:us-east-1:123456789012:graph:abc123def456abc123def456abc12345"
	api := &fakeDetectiveAPI{
		graphPages: []*awsdetective.ListGraphsOutput{{
			GraphList: []detectivetypes.Graph{{
				Arn:         aws.String(graphARN),
				CreatedTime: aws.Time(created),
			}},
		}},
		memberPages: map[string][]*awsdetective.ListMembersOutput{
			graphARN: {{
				MemberDetails: []detectivetypes.MemberDetail{{
					AccountId:       aws.String("111122223333"),
					AdministratorId: aws.String("123456789012"),
					GraphArn:        aws.String(graphARN),
					Status:          detectivetypes.MemberStatusEnabled,
					InvitationType:  detectivetypes.InvitationTypeOrganization,
					InvitedTime:     aws.Time(created),
					UpdatedTime:     aws.Time(created),
					// EmailAddress is personal contact data and must be dropped.
					EmailAddress: aws.String("security@example.com"),
					DatasourcePackageIngestStates: map[string]detectivetypes.DatasourcePackageIngestState{
						"DETECTIVE_CORE": detectivetypes.DatasourcePackageIngestStateStarted,
					},
				}},
			}},
		},
		tags: map[string]map[string]string{
			graphARN: {"Environment": "prod"},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceDetective},
	}

	graphs, err := adapter.ListGraphs(context.Background())
	if err != nil {
		t.Fatalf("ListGraphs() error = %v, want nil", err)
	}
	if len(graphs) != 1 || graphs[0].ARN != graphARN {
		t.Fatalf("graphs = %#v, want one graph %q", graphs, graphARN)
	}
	if graphs[0].CreatedAt != "2026-05-27T12:00:00Z" {
		t.Fatalf("graph CreatedAt = %q, want RFC3339 timestamp", graphs[0].CreatedAt)
	}

	members, err := adapter.ListMembers(context.Background(), graphARN)
	if err != nil {
		t.Fatalf("ListMembers() error = %v, want nil", err)
	}
	if len(members) != 1 || members[0].AccountID != "111122223333" {
		t.Fatalf("members = %#v, want one member 111122223333", members)
	}
	if !slices.Contains(members[0].DatasourcePackages, "DETECTIVE_CORE") {
		t.Fatalf("member DatasourcePackages = %#v, want DETECTIVE_CORE", members[0].DatasourcePackages)
	}
	// The scanner-owned MemberAccount type has no field that can carry email.
	memberType := reflect.TypeOf(members[0])
	for _, banned := range []string{"Email", "EmailAddress", "VolumeUsage", "PercentOfGraph", "MasterId"} {
		if _, ok := memberType.FieldByName(banned); ok {
			t.Fatalf("MemberAccount exposes field %q; Detective members are email-free and usage-free", banned)
		}
	}

	tags, err := adapter.ListTags(context.Background(), graphARN)
	if err != nil {
		t.Fatalf("ListTags() error = %v, want nil", err)
	}
	if tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v, want Environment=prod", tags)
	}

	for _, forbidden := range []string{
		"GetInvestigation", "ListInvestigations", "ListIndicators",
		"BatchGetMembershipDatasources", "GetMembers",
	} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden Detective call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

func TestClientListGraphsSkipsBlankARNAndStopsOnRepeatedToken(t *testing.T) {
	api := &fakeDetectiveAPI{
		graphPages: []*awsdetective.ListGraphsOutput{{
			GraphList: []detectivetypes.Graph{
				{Arn: aws.String("   ")},
				{Arn: aws.String("arn:aws-us-gov:detective:us-gov-west-1:123456789012:graph:gov0000000000000000000000000000")},
			},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-gov-west-1", ServiceKind: awscloud.ServiceDetective},
	}

	graphs, err := adapter.ListGraphs(context.Background())
	if err != nil {
		t.Fatalf("ListGraphs() error = %v, want nil", err)
	}
	if len(graphs) != 1 {
		t.Fatalf("graphs = %#v, want exactly one (blank ARN skipped)", graphs)
	}
	if !strings.HasPrefix(graphs[0].ARN, "arn:aws-us-gov:") {
		t.Fatalf("graph ARN = %q, want GovCloud-partition ARN passed through unchanged", graphs[0].ARN)
	}
}

type fakeDetectiveAPI struct {
	graphPages  []*awsdetective.ListGraphsOutput
	memberPages map[string][]*awsdetective.ListMembersOutput
	tags        map[string]map[string]string
	calls       []string

	graphCursor  int
	memberCursor map[string]int
}

func (f *fakeDetectiveAPI) ListGraphs(_ context.Context, _ *awsdetective.ListGraphsInput, _ ...func(*awsdetective.Options)) (*awsdetective.ListGraphsOutput, error) {
	f.calls = append(f.calls, "ListGraphs")
	if f.graphCursor >= len(f.graphPages) {
		return &awsdetective.ListGraphsOutput{}, nil
	}
	page := f.graphPages[f.graphCursor]
	f.graphCursor++
	return page, nil
}

func (f *fakeDetectiveAPI) ListMembers(_ context.Context, input *awsdetective.ListMembersInput, _ ...func(*awsdetective.Options)) (*awsdetective.ListMembersOutput, error) {
	f.calls = append(f.calls, "ListMembers")
	graphARN := aws.ToString(input.GraphArn)
	if f.memberCursor == nil {
		f.memberCursor = map[string]int{}
	}
	pages := f.memberPages[graphARN]
	cursor := f.memberCursor[graphARN]
	if cursor >= len(pages) {
		return &awsdetective.ListMembersOutput{}, nil
	}
	f.memberCursor[graphARN] = cursor + 1
	return pages[cursor], nil
}

func (f *fakeDetectiveAPI) ListTagsForResource(_ context.Context, input *awsdetective.ListTagsForResourceInput, _ ...func(*awsdetective.Options)) (*awsdetective.ListTagsForResourceOutput, error) {
	f.calls = append(f.calls, "ListTagsForResource")
	return &awsdetective.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(input.ResourceArn)]}, nil
}

// Compile-time assurance the fake satisfies the adapter's read surface and the
// scanner Client contract the adapter implements.
var (
	_ apiClient               = (*fakeDetectiveAPI)(nil)
	_ detectiveservice.Client = (*Client)(nil)
)
