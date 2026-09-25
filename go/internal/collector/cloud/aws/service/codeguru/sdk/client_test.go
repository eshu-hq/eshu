// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsprofiler "github.com/aws/aws-sdk-go-v2/service/codeguruprofiler"
	profilertypes "github.com/aws/aws-sdk-go-v2/service/codeguruprofiler/types"
	awsreviewer "github.com/aws/aws-sdk-go-v2/service/codegurureviewer"
	reviewertypes "github.com/aws/aws-sdk-go-v2/service/codegurureviewer/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsCodeGuruMetadataOnly(t *testing.T) {
	associationARN := "arn:aws:codeguru-reviewer:us-east-1:123456789012:association:abc"
	groupARN := "arn:aws:codeguru-profiler:us-east-1:123456789012:profilingGroup/payments-api"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	enabled := true

	reviewer := &fakeReviewerAPI{
		listPages: []*awsreviewer.ListRepositoryAssociationsOutput{{
			RepositoryAssociationSummaries: []reviewertypes.RepositoryAssociationSummary{{
				AssociationArn: awsv2.String(associationARN),
				AssociationId:  awsv2.String("abc"),
				Name:           awsv2.String("payments-api"),
				Owner:          awsv2.String("123456789012"),
				ProviderType:   reviewertypes.ProviderTypeCodeCommit,
				State:          reviewertypes.RepositoryAssociationStateAssociated,
			}},
		}},
		describe: map[string]*awsreviewer.DescribeRepositoryAssociationOutput{
			associationARN: {
				RepositoryAssociation: &reviewertypes.RepositoryAssociation{
					AssociationArn:   awsv2.String(associationARN),
					Name:             awsv2.String("payments-api"),
					Owner:            awsv2.String("123456789012"),
					ProviderType:     reviewertypes.ProviderTypeCodeCommit,
					State:            reviewertypes.RepositoryAssociationStateAssociated,
					CreatedTimeStamp: awsv2.Time(createdAt),
					KMSKeyDetails: &reviewertypes.KMSKeyDetails{
						EncryptionOption: reviewertypes.EncryptionOptionCmCmk,
						KMSKeyId:         awsv2.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
					},
				},
				Tags: map[string]string{"Team": "payments"},
			},
		},
	}
	profiler := &fakeProfilerAPI{
		listPages: []*awsprofiler.ListProfilingGroupsOutput{{
			ProfilingGroups: []profilertypes.ProfilingGroupDescription{{
				Arn:             awsv2.String(groupARN),
				Name:            awsv2.String("payments-api"),
				ComputePlatform: profilertypes.ComputePlatformAwslambda,
				AgentOrchestrationConfig: &profilertypes.AgentOrchestrationConfig{
					ProfilingEnabled: &enabled,
				},
				CreatedAt: awsv2.Time(createdAt),
				Tags:      map[string]string{"Team": "payments"},
			}},
		}},
	}

	client := &Client{reviewer: reviewer, profiler: profiler, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.RepositoryAssociations) != 1 {
		t.Fatalf("len(RepositoryAssociations) = %d, want 1", len(snapshot.RepositoryAssociations))
	}
	association := snapshot.RepositoryAssociations[0]
	if association.ARN != associationARN {
		t.Fatalf("association ARN = %q, want %q", association.ARN, associationARN)
	}
	if association.ProviderType != "CodeCommit" {
		t.Fatalf("association ProviderType = %q, want CodeCommit", association.ProviderType)
	}
	if association.Owner != "123456789012" {
		t.Fatalf("association Owner = %q, want 123456789012", association.Owner)
	}
	if association.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/abc" {
		t.Fatalf("association KMSKeyID = %q, want the describe-reported key", association.KMSKeyID)
	}
	if association.EncryptionOption != "CUSTOMER_MANAGED_CMK" {
		t.Fatalf("association EncryptionOption = %q, want CUSTOMER_MANAGED_CMK", association.EncryptionOption)
	}
	if association.Tags["Team"] != "payments" {
		t.Fatalf("association tag Team = %q, want payments", association.Tags["Team"])
	}

	if len(snapshot.ProfilingGroups) != 1 {
		t.Fatalf("len(ProfilingGroups) = %d, want 1", len(snapshot.ProfilingGroups))
	}
	group := snapshot.ProfilingGroups[0]
	if group.ARN != groupARN {
		t.Fatalf("group ARN = %q, want %q", group.ARN, groupARN)
	}
	if group.ComputePlatform != "AWSLambda" {
		t.Fatalf("group ComputePlatform = %q, want AWSLambda", group.ComputePlatform)
	}
	if group.ProfilingEnabled == nil || !*group.ProfilingEnabled {
		t.Fatalf("group ProfilingEnabled = %#v, want true", group.ProfilingEnabled)
	}
}

func TestClientListProfilingGroupsRequestsDescription(t *testing.T) {
	profiler := &fakeProfilerAPI{
		listPages: []*awsprofiler.ListProfilingGroupsOutput{{
			ProfilingGroups: []profilertypes.ProfilingGroupDescription{{
				Arn:  awsv2.String("arn:aws:codeguru-profiler:us-east-1:123456789012:profilingGroup/g"),
				Name: awsv2.String("g"),
			}},
		}},
	}
	client := &Client{reviewer: &fakeReviewerAPI{}, profiler: profiler, boundary: testBoundary()}
	if _, err := client.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if !profiler.includeDescriptionSeen {
		t.Fatalf("ListProfilingGroups was not called with IncludeDescription=true")
	}
}

type fakeReviewerAPI struct {
	listPages []*awsreviewer.ListRepositoryAssociationsOutput
	listCall  int
	describe  map[string]*awsreviewer.DescribeRepositoryAssociationOutput
}

func (f *fakeReviewerAPI) ListRepositoryAssociations(
	_ context.Context,
	_ *awsreviewer.ListRepositoryAssociationsInput,
	_ ...func(*awsreviewer.Options),
) (*awsreviewer.ListRepositoryAssociationsOutput, error) {
	if f.listCall >= len(f.listPages) {
		return &awsreviewer.ListRepositoryAssociationsOutput{}, nil
	}
	page := f.listPages[f.listCall]
	f.listCall++
	return page, nil
}

func (f *fakeReviewerAPI) DescribeRepositoryAssociation(
	_ context.Context,
	input *awsreviewer.DescribeRepositoryAssociationInput,
	_ ...func(*awsreviewer.Options),
) (*awsreviewer.DescribeRepositoryAssociationOutput, error) {
	if out, ok := f.describe[awsv2.ToString(input.AssociationArn)]; ok {
		return out, nil
	}
	return &awsreviewer.DescribeRepositoryAssociationOutput{}, nil
}

type fakeProfilerAPI struct {
	listPages              []*awsprofiler.ListProfilingGroupsOutput
	listCall               int
	includeDescriptionSeen bool
}

func (f *fakeProfilerAPI) ListProfilingGroups(
	_ context.Context,
	input *awsprofiler.ListProfilingGroupsInput,
	_ ...func(*awsprofiler.Options),
) (*awsprofiler.ListProfilingGroupsOutput, error) {
	if awsv2.ToBool(input.IncludeDescription) {
		f.includeDescriptionSeen = true
	}
	if f.listCall >= len(f.listPages) {
		return &awsprofiler.ListProfilingGroupsOutput{}, nil
	}
	page := f.listPages[f.listCall]
	f.listCall++
	return page, nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceCodeGuru,
	}
}
