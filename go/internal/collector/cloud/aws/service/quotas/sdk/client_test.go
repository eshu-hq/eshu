// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awssq "github.com/aws/aws-sdk-go-v2/service/servicequotas"
	awssqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsAppliedQuotasJoinedWithDefaults(t *testing.T) {
	quotaARN := "arn:aws:servicequotas:us-east-1:123456789012:ec2/L-1216C47A"
	api := &fakeServiceQuotasAPI{
		servicePages: []*awssq.ListServicesOutput{{
			Services: []awssqtypes.ServiceInfo{{
				ServiceCode: awsv2.String("ec2"),
				ServiceName: awsv2.String("Amazon Elastic Compute Cloud (Amazon EC2)"),
			}},
		}},
		appliedPages: map[string][]*awssq.ListServiceQuotasOutput{
			"ec2": {{
				Quotas: []awssqtypes.ServiceQuota{{
					QuotaArn:            awsv2.String(quotaARN),
					ServiceCode:         awsv2.String("ec2"),
					ServiceName:         awsv2.String("Amazon Elastic Compute Cloud (Amazon EC2)"),
					QuotaCode:           awsv2.String("L-1216C47A"),
					QuotaName:           awsv2.String("Running On-Demand Standard instances"),
					Value:               awsv2.Float64(256),
					Adjustable:          true,
					Unit:                awsv2.String("None"),
					QuotaAppliedAtLevel: awssqtypes.AppliedLevelEnumAccount,
					UsageMetric: &awssqtypes.MetricInfo{
						MetricNamespace:               awsv2.String("AWS/Usage"),
						MetricName:                    awsv2.String("ResourceCount"),
						MetricStatisticRecommendation: awsv2.String("Maximum"),
						MetricDimensions:              map[string]string{"Type": "Resource"},
					},
				}},
			}},
		},
		defaultPages: map[string][]*awssq.ListAWSDefaultServiceQuotasOutput{
			"ec2": {{
				Quotas: []awssqtypes.ServiceQuota{{
					QuotaCode: awsv2.String("L-1216C47A"),
					Value:     awsv2.Float64(5),
				}},
			}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Quotas) != 1 {
		t.Fatalf("len(Quotas) = %d, want 1", len(snapshot.Quotas))
	}
	quota := snapshot.Quotas[0]
	if quota.ARN != quotaARN {
		t.Fatalf("quota ARN = %q, want %q", quota.ARN, quotaARN)
	}
	if quota.AppliedValue == nil || *quota.AppliedValue != 256 {
		t.Fatalf("AppliedValue = %v, want 256", quota.AppliedValue)
	}
	if quota.DefaultValue == nil || *quota.DefaultValue != 5 {
		t.Fatalf("DefaultValue = %v, want 5", quota.DefaultValue)
	}
	if !quota.Overridden {
		t.Fatalf("Overridden = false, want true for raised quota")
	}
	if quota.UsageMetric == nil || quota.UsageMetric.Namespace != "AWS/Usage" {
		t.Fatalf("UsageMetric = %#v, want namespace AWS/Usage", quota.UsageMetric)
	}
	if quota.AppliedLevel != "ACCOUNT" {
		t.Fatalf("AppliedLevel = %q, want ACCOUNT", quota.AppliedLevel)
	}
}

func TestClientPaginatesServicesAndQuotas(t *testing.T) {
	api := &fakeServiceQuotasAPI{
		servicePages: []*awssq.ListServicesOutput{
			{
				Services:  []awssqtypes.ServiceInfo{{ServiceCode: awsv2.String("ec2")}},
				NextToken: awsv2.String("svc-next"),
			},
			{
				Services: []awssqtypes.ServiceInfo{{ServiceCode: awsv2.String("lambda")}},
			},
		},
		appliedPages: map[string][]*awssq.ListServiceQuotasOutput{
			"ec2": {
				{
					Quotas:    []awssqtypes.ServiceQuota{{QuotaCode: awsv2.String("L-A"), Value: awsv2.Float64(1)}},
					NextToken: awsv2.String("q-next"),
				},
				{
					Quotas: []awssqtypes.ServiceQuota{{QuotaCode: awsv2.String("L-B"), Value: awsv2.Float64(2)}},
				},
			},
			"lambda": {{
				Quotas: []awssqtypes.ServiceQuota{{QuotaCode: awsv2.String("L-C"), Value: awsv2.Float64(3)}},
			}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Quotas) != 3 {
		t.Fatalf("len(Quotas) = %d, want 3 across two services and two pages", len(snapshot.Quotas))
	}
}

func TestClientReturnsEmptyForNoServices(t *testing.T) {
	api := &fakeServiceQuotasAPI{servicePages: []*awssq.ListServicesOutput{{}}}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Quotas) != 0 {
		t.Fatalf("len(Quotas) = %d, want 0 for empty account", len(snapshot.Quotas))
	}
}

type fakeServiceQuotasAPI struct {
	servicePages []*awssq.ListServicesOutput
	serviceCall  int
	appliedPages map[string][]*awssq.ListServiceQuotasOutput
	appliedCalls map[string]int
	defaultPages map[string][]*awssq.ListAWSDefaultServiceQuotasOutput
	defaultCalls map[string]int
}

func (f *fakeServiceQuotasAPI) ListServices(
	_ context.Context,
	_ *awssq.ListServicesInput,
	_ ...func(*awssq.Options),
) (*awssq.ListServicesOutput, error) {
	if f.serviceCall >= len(f.servicePages) {
		return &awssq.ListServicesOutput{}, nil
	}
	page := f.servicePages[f.serviceCall]
	f.serviceCall++
	return page, nil
}

func (f *fakeServiceQuotasAPI) ListServiceQuotas(
	_ context.Context,
	input *awssq.ListServiceQuotasInput,
	_ ...func(*awssq.Options),
) (*awssq.ListServiceQuotasOutput, error) {
	if f.appliedCalls == nil {
		f.appliedCalls = map[string]int{}
	}
	name := awsv2.ToString(input.ServiceCode)
	pages := f.appliedPages[name]
	idx := f.appliedCalls[name]
	if idx >= len(pages) {
		return &awssq.ListServiceQuotasOutput{}, nil
	}
	f.appliedCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeServiceQuotasAPI) ListAWSDefaultServiceQuotas(
	_ context.Context,
	input *awssq.ListAWSDefaultServiceQuotasInput,
	_ ...func(*awssq.Options),
) (*awssq.ListAWSDefaultServiceQuotasOutput, error) {
	if f.defaultCalls == nil {
		f.defaultCalls = map[string]int{}
	}
	name := awsv2.ToString(input.ServiceCode)
	pages := f.defaultPages[name]
	idx := f.defaultCalls[name]
	if idx >= len(pages) {
		return &awssq.ListAWSDefaultServiceQuotasOutput{}, nil
	}
	f.defaultCalls[name] = idx + 1
	return pages[idx], nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceServiceQuotas,
	}
}
