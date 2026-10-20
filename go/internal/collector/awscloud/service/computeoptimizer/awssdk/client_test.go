// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsco "github.com/aws/aws-sdk-go-v2/service/computeoptimizer"
	awscotypes "github.com/aws/aws-sdk-go-v2/service/computeoptimizer/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

type fakeAPI struct {
	summaryPages  []*awsco.GetRecommendationSummariesOutput
	summaryErr    error
	instancePages []*awsco.GetEC2InstanceRecommendationsOutput
	asgPages      []*awsco.GetAutoScalingGroupRecommendationsOutput
	volumePages   []*awsco.GetEBSVolumeRecommendationsOutput
	lambdaPages   []*awsco.GetLambdaFunctionRecommendationsOutput

	summaryCalls  int
	instanceCalls int
}

func (f *fakeAPI) GetRecommendationSummaries(context.Context, *awsco.GetRecommendationSummariesInput, ...func(*awsco.Options)) (*awsco.GetRecommendationSummariesOutput, error) {
	if f.summaryErr != nil {
		return nil, f.summaryErr
	}
	page := f.summaryPages[f.summaryCalls]
	f.summaryCalls++
	return page, nil
}

func (f *fakeAPI) GetEC2InstanceRecommendations(context.Context, *awsco.GetEC2InstanceRecommendationsInput, ...func(*awsco.Options)) (*awsco.GetEC2InstanceRecommendationsOutput, error) {
	page := f.instancePages[f.instanceCalls]
	f.instanceCalls++
	return page, nil
}

func (f *fakeAPI) GetAutoScalingGroupRecommendations(context.Context, *awsco.GetAutoScalingGroupRecommendationsInput, ...func(*awsco.Options)) (*awsco.GetAutoScalingGroupRecommendationsOutput, error) {
	return f.asgPages[0], nil
}

func (f *fakeAPI) GetEBSVolumeRecommendations(context.Context, *awsco.GetEBSVolumeRecommendationsInput, ...func(*awsco.Options)) (*awsco.GetEBSVolumeRecommendationsOutput, error) {
	return f.volumePages[0], nil
}

func (f *fakeAPI) GetLambdaFunctionRecommendations(context.Context, *awsco.GetLambdaFunctionRecommendationsInput, ...func(*awsco.Options)) (*awsco.GetLambdaFunctionRecommendationsOutput, error) {
	return f.lambdaPages[0], nil
}

func testClient(api apiClient) *Client {
	return &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceComputeOptimizer,
		},
	}
}

func TestSnapshotPaginatesAndMaps(t *testing.T) {
	api := &fakeAPI{
		summaryPages: []*awsco.GetRecommendationSummariesOutput{
			{
				RecommendationSummaries: []awscotypes.RecommendationSummary{{
					RecommendationResourceType: awscotypes.RecommendationSourceTypeEc2Instance,
					AccountId:                  aws.String("123456789012"),
					Summaries: []awscotypes.Summary{
						{Name: awscotypes.FindingOptimized, Value: 3},
						{Name: awscotypes.FindingOverProvisioned, Value: 1},
					},
					SavingsOpportunity: &awscotypes.SavingsOpportunity{SavingsOpportunityPercentage: 12.5},
				}},
				NextToken: aws.String("page2"),
			},
			{RecommendationSummaries: nil, NextToken: nil},
		},
		instancePages: []*awsco.GetEC2InstanceRecommendationsOutput{
			{
				InstanceRecommendations: []awscotypes.InstanceRecommendation{{
					InstanceArn:         aws.String("arn:aws:ec2:us-east-1:123456789012:instance/i-0abc"),
					InstanceName:        aws.String("checkout-1"),
					CurrentInstanceType: aws.String("m5.2xlarge"),
					Finding:             awscotypes.FindingOverProvisioned,
					RecommendationOptions: []awscotypes.InstanceRecommendationOption{
						{InstanceType: aws.String("m5.4xlarge"), Rank: 2},
						{InstanceType: aws.String("m5.xlarge"), Rank: 1, SavingsOpportunity: &awscotypes.SavingsOpportunity{SavingsOpportunityPercentage: 30}},
					},
					Tags: []awscotypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
				}},
				NextToken: nil,
			},
		},
		asgPages: []*awsco.GetAutoScalingGroupRecommendationsOutput{{
			AutoScalingGroupRecommendations: []awscotypes.AutoScalingGroupRecommendation{{
				AutoScalingGroupArn:  aws.String("arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web-asg"),
				AutoScalingGroupName: aws.String("web-asg"),
				Finding:              awscotypes.FindingNotOptimized,
				CurrentConfiguration: &awscotypes.AutoScalingGroupConfiguration{InstanceType: aws.String("c5.large")},
				RecommendationOptions: []awscotypes.AutoScalingGroupRecommendationOption{{
					Configuration: &awscotypes.AutoScalingGroupConfiguration{InstanceType: aws.String("c6g.large")},
					Rank:          1,
				}},
			}},
		}},
		volumePages: []*awsco.GetEBSVolumeRecommendationsOutput{{
			VolumeRecommendations: []awscotypes.VolumeRecommendation{{
				VolumeArn:            aws.String("arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc"),
				Finding:              awscotypes.EBSFindingNotOptimized,
				CurrentConfiguration: &awscotypes.VolumeConfiguration{VolumeType: aws.String("gp2")},
				VolumeRecommendationOptions: []awscotypes.VolumeRecommendationOption{{
					Configuration: &awscotypes.VolumeConfiguration{VolumeType: aws.String("gp3")},
					Rank:          1,
				}},
			}},
		}},
		lambdaPages: []*awsco.GetLambdaFunctionRecommendationsOutput{{
			LambdaFunctionRecommendations: []awscotypes.LambdaFunctionRecommendation{{
				FunctionArn:       aws.String("arn:aws:lambda:us-east-1:123456789012:function:checkout"),
				FunctionVersion:   aws.String("$LATEST"),
				CurrentMemorySize: 512,
				Finding:           awscotypes.LambdaFunctionRecommendationFindingNotOptimized,
				MemorySizeRecommendationOptions: []awscotypes.LambdaFunctionMemoryRecommendationOption{{
					MemorySize: 256, Rank: 1,
				}},
			}},
		}},
	}

	snapshot, err := testClient(api).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if api.summaryCalls != 2 {
		t.Fatalf("summary page calls = %d, want 2 (paginated)", api.summaryCalls)
	}
	if len(snapshot.Summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(snapshot.Summaries))
	}
	if got := snapshot.Summaries[0].FindingCounts["Optimized"]; got != 3 {
		t.Fatalf("Optimized count = %v, want 3", got)
	}
	if len(snapshot.InstanceRecommendations) != 1 {
		t.Fatalf("instance recs = %d, want 1", len(snapshot.InstanceRecommendations))
	}
	if got := snapshot.InstanceRecommendations[0].RecommendedInstanceType; got != "m5.xlarge" {
		t.Fatalf("recommended instance type = %q, want m5.xlarge (rank-1 option)", got)
	}
	if got := snapshot.InstanceRecommendations[0].SavingsOpportunityPercentage; got != 30 {
		t.Fatalf("savings percentage = %v, want 30", got)
	}
	if got := snapshot.AutoScalingGroupRecommendations[0].RecommendedInstanceType; got != "c6g.large" {
		t.Fatalf("recommended asg type = %q, want c6g.large", got)
	}
	if got := snapshot.VolumeRecommendations[0].RecommendedVolumeType; got != "gp3" {
		t.Fatalf("recommended volume type = %q, want gp3", got)
	}
	if got := snapshot.LambdaFunctionRecommendations[0].RecommendedMemorySize; got != 256 {
		t.Fatalf("recommended memory size = %v, want 256", got)
	}
}

func TestSnapshotReturnsEmptyWhenNotEnrolled(t *testing.T) {
	api := &fakeAPI{summaryErr: &awscotypes.OptInRequiredException{Message: aws.String("This account must opt in")}}
	snapshot, err := testClient(api).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil for not-enrolled account", err)
	}
	if len(snapshot.Summaries) != 0 || len(snapshot.InstanceRecommendations) != 0 {
		t.Fatalf("Snapshot() returned data for not-enrolled account: %#v", snapshot)
	}
}

func TestMapSummaryDropsCostDataPoints(t *testing.T) {
	mapped := mapSummary(awscotypes.RecommendationSummary{
		RecommendationResourceType: awscotypes.RecommendationSourceTypeLambdaFunction,
		AccountId:                  aws.String("123456789012"),
		SavingsOpportunity: &awscotypes.SavingsOpportunity{
			SavingsOpportunityPercentage: 9.9,
			EstimatedMonthlySavings:      &awscotypes.EstimatedMonthlySavings{Currency: awscotypes.CurrencyUsd, Value: 1234.56},
		},
	})
	if mapped.SavingsOpportunityPercentage != 9.9 {
		t.Fatalf("savings percentage = %v, want 9.9", mapped.SavingsOpportunityPercentage)
	}
	if mapped.ResourceType != "LambdaFunction" {
		t.Fatalf("resource type = %q, want LambdaFunction", mapped.ResourceType)
	}
}
