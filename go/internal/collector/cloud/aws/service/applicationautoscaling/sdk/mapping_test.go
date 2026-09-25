// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsaastypes "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// TestMapScalingPolicyMapsCreationTime proves the SDK policy creation time is
// carried into the scanner-owned model so the emitted creation_time attribute is
// populated when AWS provides it.
func TestMapScalingPolicyMapsCreationTime(t *testing.T) {
	created := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	mapped := mapScalingPolicy(awsaastypes.ScalingPolicy{
		PolicyARN:    awsv2.String("arn:aws:autoscaling:us-east-1:123456789012:scalingPolicy:uuid:resource/dynamodb/table/orders:policyName/orders-read"),
		PolicyName:   awsv2.String("orders-read"),
		CreationTime: awsv2.Time(created),
	})
	if !mapped.CreationTime.Equal(created) {
		t.Fatalf("mapped CreationTime = %v, want %v", mapped.CreationTime, created)
	}
}

// TestMapScalingPolicyOmitsMissingCreationTime proves an absent SDK creation
// time maps to the zero value, which the scanner omits from the attribute
// payload rather than emitting an epoch timestamp.
func TestMapScalingPolicyOmitsMissingCreationTime(t *testing.T) {
	mapped := mapScalingPolicy(awsaastypes.ScalingPolicy{
		PolicyName: awsv2.String("orders-read"),
	})
	if !mapped.CreationTime.IsZero() {
		t.Fatalf("mapped CreationTime = %v, want zero", mapped.CreationTime)
	}
}

// TestAppendThrottleWarningSkipsNil proves a nil warning leaves the stream
// unchanged so a successful namespace does not record a phantom omission.
func TestAppendThrottleWarningSkipsNil(t *testing.T) {
	warnings := []aws.WarningObservation{{WarningKind: aws.WarningThrottleSustained}}
	got := appendThrottleWarning(warnings, nil)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (nil warning must not append)", len(got))
	}
}

// TestAppendThrottleWarningRecordsEveryNamespace proves each throttled namespace
// records its own warning. The component marker is shared, but the omission is
// per-namespace, so suppressing later namespaces would hide which namespaces had
// metadata dropped. The function must append every distinct observation.
func TestAppendThrottleWarningRecordsEveryNamespace(t *testing.T) {
	var warnings []aws.WarningObservation
	warnings = appendThrottleWarning(warnings, &aws.WarningObservation{
		WarningKind:    aws.WarningThrottleSustained,
		SourceRecordID: "applicationautoscaling_scaling_policies_throttled_dynamodb",
	})
	warnings = appendThrottleWarning(warnings, &aws.WarningObservation{
		WarningKind:    aws.WarningThrottleSustained,
		SourceRecordID: "applicationautoscaling_scaling_policies_throttled_ecs",
	})
	if len(warnings) != 2 {
		t.Fatalf("len = %d, want 2 (per-namespace omissions must each be recorded)", len(warnings))
	}
}
