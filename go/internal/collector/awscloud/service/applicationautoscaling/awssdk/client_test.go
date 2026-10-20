// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaas "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	awsaastypes "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

type fakeAPI struct {
	targetsByNS map[awsaastypes.ServiceNamespace][][]awsaastypes.ScalableTarget
	throttleNS  awsaastypes.ServiceNamespace
	calls       int
}

func (f *fakeAPI) DescribeScalableTargets(
	_ context.Context,
	in *awsaas.DescribeScalableTargetsInput,
	_ ...func(*awsaas.Options),
) (*awsaas.DescribeScalableTargetsOutput, error) {
	f.calls++
	if in.ServiceNamespace == f.throttleNS {
		return nil, &smithy.GenericAPIError{Code: "ThrottlingException", Message: "rate exceeded"}
	}
	pages := f.targetsByNS[in.ServiceNamespace]
	idx := 0
	if in.NextToken != nil {
		idx = 1
	}
	if idx >= len(pages) {
		return &awsaas.DescribeScalableTargetsOutput{}, nil
	}
	out := &awsaas.DescribeScalableTargetsOutput{ScalableTargets: pages[idx]}
	if idx+1 < len(pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func (f *fakeAPI) DescribeScalingPolicies(
	_ context.Context,
	_ *awsaas.DescribeScalingPoliciesInput,
	_ ...func(*awsaas.Options),
) (*awsaas.DescribeScalingPoliciesOutput, error) {
	return &awsaas.DescribeScalingPoliciesOutput{}, nil
}

func (f *fakeAPI) DescribeScheduledActions(
	_ context.Context,
	_ *awsaas.DescribeScheduledActionsInput,
	_ ...func(*awsaas.Options),
) (*awsaas.DescribeScheduledActionsOutput, error) {
	return &awsaas.DescribeScheduledActionsOutput{}, nil
}

func newTestClient(api apiClient) *Client {
	return &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceApplicationAutoScaling},
	}
}

func TestSnapshotPaginatesAndFansOutNamespaces(t *testing.T) {
	api := &fakeAPI{targetsByNS: map[awsaastypes.ServiceNamespace][][]awsaastypes.ScalableTarget{
		awsaastypes.ServiceNamespaceDynamodb: {
			{{ResourceId: aws.String("table/orders"), ServiceNamespace: awsaastypes.ServiceNamespaceDynamodb}},
			{{ResourceId: aws.String("table/payments"), ServiceNamespace: awsaastypes.ServiceNamespaceDynamodb}},
		},
		awsaastypes.ServiceNamespaceEcs: {
			{{ResourceId: aws.String("service/prod/api"), ServiceNamespace: awsaastypes.ServiceNamespaceEcs}},
		},
	}}

	snapshot, err := newTestClient(api).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got := len(snapshot.ScalableTargets); got != 3 {
		t.Fatalf("scalable targets = %d, want 3 (2 paginated dynamodb + 1 ecs)", got)
	}
}

func TestSnapshotRecordsThrottleWarningAndContinues(t *testing.T) {
	api := &fakeAPI{
		throttleNS: awsaastypes.ServiceNamespaceDynamodb,
		targetsByNS: map[awsaastypes.ServiceNamespace][][]awsaastypes.ScalableTarget{
			awsaastypes.ServiceNamespaceEcs: {
				{{ResourceId: aws.String("service/prod/api"), ServiceNamespace: awsaastypes.ServiceNamespaceEcs}},
			},
		},
	}

	snapshot, err := newTestClient(api).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil (throttle is non-fatal)", err)
	}
	if len(snapshot.ScalableTargets) != 1 {
		t.Fatalf("scalable targets = %d, want 1 (ecs still scanned past dynamodb throttle)", len(snapshot.ScalableTargets))
	}
	var sawWarning bool
	for _, warning := range snapshot.Warnings {
		if warning.WarningKind == awscloud.WarningThrottleSustained {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("expected a sustained-throttle warning for the throttled namespace")
	}
}

func TestIsThrottleErrorClassifies(t *testing.T) {
	if !isThrottleError(&smithy.GenericAPIError{Code: "ThrottlingException"}) {
		t.Fatalf("ThrottlingException not classified as throttle")
	}
	if isThrottleError(errors.New("boom")) {
		t.Fatalf("plain error wrongly classified as throttle")
	}
}
