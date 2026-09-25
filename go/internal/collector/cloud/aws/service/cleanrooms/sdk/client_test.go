// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscleanrooms "github.com/aws/aws-sdk-go-v2/service/cleanrooms"
	awscleanroomstypes "github.com/aws/aws-sdk-go-v2/service/cleanrooms/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsCleanRoomsMetadataOnly(t *testing.T) {
	collaborationARN := "arn:aws:cleanrooms:us-east-1:123456789012:collaboration/c1"
	configuredARN := "arn:aws:cleanrooms:us-east-1:123456789012:configuredtable/t1"
	membershipARN := "arn:aws:cleanrooms:us-east-1:123456789012:membership/m1"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeCleanRoomsAPI{
		collaborationPages: []*awscleanrooms.ListCollaborationsOutput{{
			CollaborationList: []awscleanroomstypes.CollaborationSummary{{
				Arn:                awsv2.String(collaborationARN),
				Id:                 awsv2.String("c1"),
				Name:               awsv2.String("ad-attribution"),
				CreatorAccountId:   awsv2.String("123456789012"),
				CreatorDisplayName: awsv2.String("Publisher"),
				MemberStatus:       awscleanroomstypes.MemberStatusActive,
				AnalyticsEngine:    awscleanroomstypes.AnalyticsEngineSpark,
				CreateTime:         awsv2.Time(createdAt),
				UpdateTime:         awsv2.Time(createdAt),
			}},
		}},
		configuredTablePages: []*awscleanrooms.ListConfiguredTablesOutput{{
			ConfiguredTableSummaries: []awscleanroomstypes.ConfiguredTableSummary{{
				Arn:               awsv2.String(configuredARN),
				Id:                awsv2.String("t1"),
				Name:              awsv2.String("impressions"),
				AnalysisMethod:    awscleanroomstypes.AnalysisMethodDirectQuery,
				AnalysisRuleTypes: []awscleanroomstypes.ConfiguredTableAnalysisRuleType{awscleanroomstypes.ConfiguredTableAnalysisRuleTypeAggregation},
				CreateTime:        awsv2.Time(createdAt),
				UpdateTime:        awsv2.Time(createdAt),
			}},
		}},
		configuredTableDetails: map[string]*awscleanroomstypes.ConfiguredTable{
			"t1": {
				Arn:            awsv2.String(configuredARN),
				Id:             awsv2.String("t1"),
				Name:           awsv2.String("impressions"),
				AllowedColumns: []string{"user_id", "campaign_id", "event_time"},
				TableReference: &awscleanroomstypes.TableReferenceMemberGlue{
					Value: awscleanroomstypes.GlueTableReference{
						DatabaseName: awsv2.String("analytics"),
						TableName:    awsv2.String("impressions"),
					},
				},
			},
		},
		membershipPages: []*awscleanrooms.ListMembershipsOutput{{
			MembershipSummaries: []awscleanroomstypes.MembershipSummary{{
				Arn:               awsv2.String(membershipARN),
				Id:                awsv2.String("m1"),
				CollaborationArn:  awsv2.String(collaborationARN),
				CollaborationId:   awsv2.String("c1"),
				CollaborationName: awsv2.String("ad-attribution"),
				MemberAbilities:   []awscleanroomstypes.MemberAbility{awscleanroomstypes.MemberAbilityCanQuery},
				Status:            awscleanroomstypes.MembershipStatusActive,
				CreateTime:        awsv2.Time(createdAt),
				UpdateTime:        awsv2.Time(createdAt),
			}},
		}},
		tags: map[string]map[string]string{
			collaborationARN: {"Environment": "prod"},
			configuredARN:    {"Team": "data"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Collaborations) != 1 {
		t.Fatalf("len(Collaborations) = %d, want 1", len(snapshot.Collaborations))
	}
	collaboration := snapshot.Collaborations[0]
	if collaboration.ARN != collaborationARN {
		t.Fatalf("collaboration ARN = %q, want %q", collaboration.ARN, collaborationARN)
	}
	if collaboration.AnalyticsEngine != "SPARK" {
		t.Fatalf("collaboration AnalyticsEngine = %q, want SPARK", collaboration.AnalyticsEngine)
	}
	if collaboration.Tags["Environment"] != "prod" {
		t.Fatalf("collaboration tag Environment = %q, want prod", collaboration.Tags["Environment"])
	}

	if len(snapshot.ConfiguredTables) != 1 {
		t.Fatalf("len(ConfiguredTables) = %d, want 1", len(snapshot.ConfiguredTables))
	}
	table := snapshot.ConfiguredTables[0]
	if table.AnalysisMethod != "DIRECT_QUERY" {
		t.Fatalf("table AnalysisMethod = %q, want DIRECT_QUERY", table.AnalysisMethod)
	}
	if table.AllowedColumnCount != 3 {
		t.Fatalf("table AllowedColumnCount = %d, want 3", table.AllowedColumnCount)
	}
	if table.TableReferenceKind != "glue" {
		t.Fatalf("table TableReferenceKind = %q, want glue", table.TableReferenceKind)
	}
	if table.GlueDatabaseName != "analytics" || table.GlueTableName != "impressions" {
		t.Fatalf("table Glue ref = %q/%q, want analytics/impressions", table.GlueDatabaseName, table.GlueTableName)
	}
	if len(table.AnalysisRuleTypes) != 1 || table.AnalysisRuleTypes[0] != "AGGREGATION" {
		t.Fatalf("table AnalysisRuleTypes = %#v, want [AGGREGATION]", table.AnalysisRuleTypes)
	}

	if len(snapshot.Memberships) != 1 {
		t.Fatalf("len(Memberships) = %d, want 1", len(snapshot.Memberships))
	}
	membership := snapshot.Memberships[0]
	if membership.CollaborationARN != collaborationARN {
		t.Fatalf("membership CollaborationARN = %q, want %q", membership.CollaborationARN, collaborationARN)
	}
	if membership.Status != "ACTIVE" {
		t.Fatalf("membership Status = %q, want ACTIVE", membership.Status)
	}
}

func TestClientSnapshotPaginatesEverything(t *testing.T) {
	api := &fakeCleanRoomsAPI{
		collaborationPages: []*awscleanrooms.ListCollaborationsOutput{
			{
				CollaborationList: []awscleanroomstypes.CollaborationSummary{{Arn: awsv2.String("arn:aws:cleanrooms:us-east-1:1:collaboration/a"), Id: awsv2.String("a")}},
				NextToken:         awsv2.String("page2"),
			},
			{
				CollaborationList: []awscleanroomstypes.CollaborationSummary{{Arn: awsv2.String("arn:aws:cleanrooms:us-east-1:1:collaboration/b"), Id: awsv2.String("b")}},
			},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Collaborations) != 2 {
		t.Fatalf("len(Collaborations) = %d, want 2 (pagination not exhausted)", len(snapshot.Collaborations))
	}
}

type fakeCleanRoomsAPI struct {
	collaborationPages []*awscleanrooms.ListCollaborationsOutput
	collaborationCall  int

	configuredTablePages   []*awscleanrooms.ListConfiguredTablesOutput
	configuredTableCall    int
	configuredTableDetails map[string]*awscleanroomstypes.ConfiguredTable

	membershipPages []*awscleanrooms.ListMembershipsOutput
	membershipCall  int

	tags map[string]map[string]string
}

func (f *fakeCleanRoomsAPI) ListCollaborations(
	_ context.Context,
	_ *awscleanrooms.ListCollaborationsInput,
	_ ...func(*awscleanrooms.Options),
) (*awscleanrooms.ListCollaborationsOutput, error) {
	if f.collaborationCall >= len(f.collaborationPages) {
		return &awscleanrooms.ListCollaborationsOutput{}, nil
	}
	page := f.collaborationPages[f.collaborationCall]
	f.collaborationCall++
	return page, nil
}

func (f *fakeCleanRoomsAPI) ListConfiguredTables(
	_ context.Context,
	_ *awscleanrooms.ListConfiguredTablesInput,
	_ ...func(*awscleanrooms.Options),
) (*awscleanrooms.ListConfiguredTablesOutput, error) {
	if f.configuredTableCall >= len(f.configuredTablePages) {
		return &awscleanrooms.ListConfiguredTablesOutput{}, nil
	}
	page := f.configuredTablePages[f.configuredTableCall]
	f.configuredTableCall++
	return page, nil
}

func (f *fakeCleanRoomsAPI) GetConfiguredTable(
	_ context.Context,
	input *awscleanrooms.GetConfiguredTableInput,
	_ ...func(*awscleanrooms.Options),
) (*awscleanrooms.GetConfiguredTableOutput, error) {
	detail := f.configuredTableDetails[awsv2.ToString(input.ConfiguredTableIdentifier)]
	return &awscleanrooms.GetConfiguredTableOutput{ConfiguredTable: detail}, nil
}

func (f *fakeCleanRoomsAPI) ListMemberships(
	_ context.Context,
	_ *awscleanrooms.ListMembershipsInput,
	_ ...func(*awscleanrooms.Options),
) (*awscleanrooms.ListMembershipsOutput, error) {
	if f.membershipCall >= len(f.membershipPages) {
		return &awscleanrooms.ListMembershipsOutput{}, nil
	}
	page := f.membershipPages[f.membershipCall]
	f.membershipCall++
	return page, nil
}

func (f *fakeCleanRoomsAPI) ListTagsForResource(
	_ context.Context,
	input *awscleanrooms.ListTagsForResourceInput,
	_ ...func(*awscleanrooms.Options),
) (*awscleanrooms.ListTagsForResourceOutput, error) {
	return &awscleanrooms.ListTagsForResourceOutput{
		Tags: f.tags[awsv2.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceCleanRooms,
	}
}
