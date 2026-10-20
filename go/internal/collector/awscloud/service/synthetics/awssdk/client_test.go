// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssynthetics "github.com/aws/aws-sdk-go-v2/service/synthetics"
	awssyntheticstypes "github.com/aws/aws-sdk-go-v2/service/synthetics/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsCanaryMetadataOnly(t *testing.T) {
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	roleARN := "arn:aws:iam::123456789012:role/canary-exec"
	api := &fakeSyntheticsAPI{pages: []*awssynthetics.DescribeCanariesOutput{
		{
			Canaries: []awssyntheticstypes.Canary{{
				Id:                 aws.String("abcd-1234"),
				Name:               aws.String("checkout-probe"),
				RuntimeVersion:     aws.String("syn-nodejs-puppeteer-7.0"),
				ExecutionRoleArn:   aws.String(roleARN),
				ArtifactS3Location: aws.String("checkout-artifacts/canary"),
				Status: &awssyntheticstypes.CanaryStatus{
					State: awssyntheticstypes.CanaryStateRunning,
				},
				Schedule: &awssyntheticstypes.CanaryScheduleOutput{
					Expression:        aws.String("rate(5 minutes)"),
					DurationInSeconds: aws.Int64(0),
				},
				RunConfig: &awssyntheticstypes.CanaryRunConfigOutput{
					TimeoutInSeconds: aws.Int32(60),
					MemoryInMB:       aws.Int32(1024),
					ActiveTracing:    aws.Bool(true),
				},
				ArtifactConfig: &awssyntheticstypes.ArtifactConfigOutput{
					S3Encryption: &awssyntheticstypes.S3EncryptionConfig{
						EncryptionMode: awssyntheticstypes.EncryptionModeSseKms,
						KmsKeyArn:      aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
					},
				},
				VpcConfig: &awssyntheticstypes.VpcConfigOutput{
					VpcId:            aws.String("vpc-0a1b2c3d"),
					SubnetIds:        []string{"subnet-1111", "subnet-2222"},
					SecurityGroupIds: []string{"sg-9999"},
				},
				Timeline: &awssyntheticstypes.CanaryTimeline{
					Created:      aws.Time(createdAt),
					LastModified: aws.Time(createdAt),
				},
				Tags: map[string]string{"Environment": "prod"},
			}},
		},
	}}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Canaries) != 1 {
		t.Fatalf("len(Canaries) = %d, want 1", len(snapshot.Canaries))
	}
	canary := snapshot.Canaries[0]
	wantARN := "arn:aws:synthetics:us-east-1:123456789012:canary:checkout-probe"
	if canary.ARN != wantARN {
		t.Fatalf("canary ARN = %q, want %q (partition-aware synthesized)", canary.ARN, wantARN)
	}
	if canary.State != "RUNNING" {
		t.Fatalf("canary State = %q, want RUNNING", canary.State)
	}
	if canary.ExecutionRoleARN != roleARN {
		t.Fatalf("canary ExecutionRoleARN = %q, want %q", canary.ExecutionRoleARN, roleARN)
	}
	if canary.ArtifactS3Location != "checkout-artifacts/canary" {
		t.Fatalf("canary ArtifactS3Location = %q, want checkout-artifacts/canary", canary.ArtifactS3Location)
	}
	if canary.ArtifactEncryptionMode != "SSE_KMS" {
		t.Fatalf("canary ArtifactEncryptionMode = %q, want SSE_KMS", canary.ArtifactEncryptionMode)
	}
	if canary.RunTimeoutInSeconds != 60 || canary.RunMemoryInMB != 1024 || !canary.RunActiveTracing {
		t.Fatalf("run config = %d/%d/%v, want 60/1024/true", canary.RunTimeoutInSeconds, canary.RunMemoryInMB, canary.RunActiveTracing)
	}
	if canary.VPCID != "vpc-0a1b2c3d" {
		t.Fatalf("canary VPCID = %q, want vpc-0a1b2c3d", canary.VPCID)
	}
	if len(canary.SubnetIDs) != 2 || len(canary.SecurityGroupIDs) != 1 {
		t.Fatalf("vpc ids = %#v / %#v, want 2 subnets and 1 sg", canary.SubnetIDs, canary.SecurityGroupIDs)
	}
	if canary.Tags["Environment"] != "prod" {
		t.Fatalf("canary tag Environment = %q, want prod", canary.Tags["Environment"])
	}
}

func TestClientCanaryARNPartitionAware(t *testing.T) {
	client := &Client{boundary: awscloud.Boundary{AccountID: "123456789012", Region: "cn-north-1"}}
	got := client.canaryARN("cn-probe")
	want := "arn:aws-cn:synthetics:cn-north-1:123456789012:canary:cn-probe"
	if got != want {
		t.Fatalf("canaryARN(China) = %q, want %q", got, want)
	}
	if got := (&Client{boundary: awscloud.Boundary{Region: "us-east-1"}}).canaryARN(""); got != "" {
		t.Fatalf("canaryARN(empty name) = %q, want empty", got)
	}
}

func TestClientPaginatesCanaries(t *testing.T) {
	api := &fakeSyntheticsAPI{pages: []*awssynthetics.DescribeCanariesOutput{
		{
			Canaries:  []awssyntheticstypes.Canary{{Name: aws.String("one")}},
			NextToken: aws.String("more"),
		},
		{
			Canaries: []awssyntheticstypes.Canary{{Name: aws.String("two")}},
		},
	}}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Canaries) != 2 {
		t.Fatalf("len(Canaries) = %d, want 2 across pages", len(snapshot.Canaries))
	}
	if api.calls != 2 {
		t.Fatalf("DescribeCanaries calls = %d, want 2", api.calls)
	}
}

type fakeSyntheticsAPI struct {
	pages []*awssynthetics.DescribeCanariesOutput
	calls int
}

func (f *fakeSyntheticsAPI) DescribeCanaries(
	_ context.Context,
	_ *awssynthetics.DescribeCanariesInput,
	_ ...func(*awssynthetics.Options),
) (*awssynthetics.DescribeCanariesOutput, error) {
	if f.calls >= len(f.pages) {
		return &awssynthetics.DescribeCanariesOutput{}, nil
	}
	page := f.pages[f.calls]
	f.calls++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSynthetics,
	}
}
