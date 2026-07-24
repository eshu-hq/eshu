// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import (
	"reflect"
	"testing"
)

// TestClassifyImageVersionDriftAMIMismatch proves the AMI value-drift path:
// cloud, state, and config all present, ami differs -> image_version_drift.
func TestClassifyImageVersionDriftAMIMismatch(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	cloud := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_ec2_instance",
		Attributes:   map[string]string{"ami": "ami-000000000000000a"},
	}
	state := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_instance",
		Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
	}
	config := &ResourceRow{ARN: arn, ResourceType: "aws_instance"}

	got := Classify(cloud, state, config)
	if got != FindingKindImageVersionDrift {
		t.Fatalf("Classify() = %q, want %q", got, FindingKindImageVersionDrift)
	}
}

// TestClassifyImageVersionDriftAMIMatchNoDrift proves a matching ami never
// fires a finding.
func TestClassifyImageVersionDriftAMIMatchNoDrift(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	cloud := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_ec2_instance",
		Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
	}
	state := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_instance",
		Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
	}
	config := &ResourceRow{ARN: arn, ResourceType: "aws_instance"}

	got := Classify(cloud, state, config)
	if got != "" {
		t.Fatalf("Classify() = %q, want no finding for matching ami", got)
	}
}

// TestClassifyImageVersionDriftMissingSideIsAmbiguousNotDrift proves that
// when the comparable value is missing on either side, Classify treats it
// as "no signal" -- never a false-positive drift finding.
func TestClassifyImageVersionDriftMissingSideIsAmbiguousNotDrift(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"

	cases := []struct {
		name  string
		cloud *ResourceRow
		state *ResourceRow
	}{
		{
			name:  "missing_on_cloud_side",
			cloud: &ResourceRow{ARN: arn, ResourceType: "aws_ec2_instance"},
			state: &ResourceRow{ARN: arn, ResourceType: "aws_instance", Attributes: map[string]string{"ami": "ami-0123456789abcdef0"}},
		},
		{
			name:  "missing_on_state_side",
			cloud: &ResourceRow{ARN: arn, ResourceType: "aws_ec2_instance", Attributes: map[string]string{"ami": "ami-000000000000000a"}},
			state: &ResourceRow{ARN: arn, ResourceType: "aws_instance"},
		},
		{
			name:  "missing_on_both_sides",
			cloud: &ResourceRow{ARN: arn, ResourceType: "aws_ec2_instance"},
			state: &ResourceRow{ARN: arn, ResourceType: "aws_instance"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := &ResourceRow{ARN: arn, ResourceType: "aws_instance"}
			got := Classify(tc.cloud, tc.state, config)
			if got != "" {
				t.Fatalf("Classify() = %q, want no finding (ambiguous, not drift) when a value is missing on one side", got)
			}
		})
	}
}

// TestClassifyImageVersionDriftLambdaImageAndVersion proves both Lambda
// comparable attributes (image_uri, version) independently trigger drift.
func TestClassifyImageVersionDriftLambdaImageAndVersion(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo"
	config := &ResourceRow{ARN: arn, ResourceType: "aws_lambda_function"}

	cases := []struct {
		name           string
		cloudAttrs     map[string]string
		stateAttrs     map[string]string
		wantFindsDrift bool
	}{
		{
			name:           "image_uri_mismatch",
			cloudAttrs:     map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v2", "version": "$LATEST"},
			stateAttrs:     map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v1", "version": "$LATEST"},
			wantFindsDrift: true,
		},
		{
			name:           "version_mismatch",
			cloudAttrs:     map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v1", "version": "3"},
			stateAttrs:     map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v1", "version": "$LATEST"},
			wantFindsDrift: true,
		},
		{
			name:           "both_match",
			cloudAttrs:     map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v1", "version": "$LATEST"},
			stateAttrs:     map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v1", "version": "$LATEST"},
			wantFindsDrift: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cloud := &ResourceRow{ARN: arn, ResourceType: "lambda.function", Attributes: tc.cloudAttrs}
			state := &ResourceRow{ARN: arn, ResourceType: "aws_lambda_function", Attributes: tc.stateAttrs}
			got := Classify(cloud, state, config)
			gotDrift := got == FindingKindImageVersionDrift
			if gotDrift != tc.wantFindsDrift {
				t.Fatalf("Classify() = %q (drift=%v), want drift=%v", got, gotDrift, tc.wantFindsDrift)
			}
		})
	}
}

func TestClassifyValueDriftReturnsDeterministicDriftedAttributes(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	cloud := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_ec2_instance",
		Attributes:   map[string]string{"ami": "ami-000000000000000a"},
	}
	state := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_instance",
		Attributes:   map[string]string{"ami": "ami-0123456789abcdef0"},
	}

	got := ClassifyValueDrift(cloud, state)
	want := []DriftedAttribute{{Key: "ami", Declared: "ami-0123456789abcdef0", Observed: "ami-000000000000000a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ClassifyValueDrift() = %#v, want %#v", got, want)
	}
}

func TestClassifyValueDriftNilInputsYieldNil(t *testing.T) {
	t.Parallel()

	row := &ResourceRow{ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-1", ResourceType: "aws_instance"}
	if got := ClassifyValueDrift(nil, row); got != nil {
		t.Fatalf("ClassifyValueDrift(nil, row) = %#v, want nil", got)
	}
	if got := ClassifyValueDrift(row, nil); got != nil {
		t.Fatalf("ClassifyValueDrift(row, nil) = %#v, want nil", got)
	}
}

// TestClassifyContainerImageDrift covers the ECS membership/ambiguity rules:
// one observed image compared against the declared set. Multiple observed
// images -- or a missing side -- must never be treated as deterministic
// drift; only a single observed image cleanly outside the declared set is.
func TestClassifyContainerImageDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		declared      []string
		observed      []string
		wantDrift     bool
		wantAmbiguous bool
	}{
		{
			name:      "single_declared_single_observed_match",
			declared:  []string{"repo/app:v1"},
			observed:  []string{"repo/app:v1"},
			wantDrift: false,
		},
		{
			name:      "single_declared_single_observed_mismatch",
			declared:  []string{"repo/app:v1"},
			observed:  []string{"repo/app:v2"},
			wantDrift: true,
		},
		{
			name:      "multi_declared_observed_is_member",
			declared:  []string{"repo/app:v1", "repo/sidecar:v1"},
			observed:  []string{"repo/sidecar:v1"},
			wantDrift: false,
		},
		{
			name:      "multi_declared_observed_not_member",
			declared:  []string{"repo/app:v1", "repo/sidecar:v1"},
			observed:  []string{"repo/other:v9"},
			wantDrift: true,
		},
		{
			name:          "missing_declared_side",
			declared:      nil,
			observed:      []string{"repo/app:v1"},
			wantAmbiguous: true,
		},
		{
			name:          "missing_observed_side",
			declared:      []string{"repo/app:v1"},
			observed:      nil,
			wantAmbiguous: true,
		},
		{
			name:          "multiple_observed_images_ambiguous",
			declared:      []string{"repo/app:v1"},
			observed:      []string{"repo/app:v1", "repo/app:v2"},
			wantAmbiguous: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotDrift, gotAmbiguous := ClassifyContainerImageDrift(tc.declared, tc.observed)
			if gotDrift != tc.wantDrift {
				t.Fatalf("ClassifyContainerImageDrift() drift = %v, want %v", gotDrift, tc.wantDrift)
			}
			if gotAmbiguous != tc.wantAmbiguous {
				t.Fatalf("ClassifyContainerImageDrift() ambiguous = %v, want %v", gotAmbiguous, tc.wantAmbiguous)
			}
		})
	}
}

// TestClassifyImageVersionDriftECSContainerImage exercises the ECS
// task-definition path end to end through Classify.
func TestClassifyImageVersionDriftECSContainerImage(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1"
	config := &ResourceRow{ARN: arn, ResourceType: "aws_ecs_task_definition"}

	cases := []struct {
		name      string
		declared  []string
		observed  []string
		wantFinds bool
	}{
		{name: "mismatch_fires_drift", declared: []string{"repo/app:v1"}, observed: []string{"repo/app:v2"}, wantFinds: true},
		{name: "match_no_drift", declared: []string{"repo/app:v1"}, observed: []string{"repo/app:v1"}, wantFinds: false},
		{name: "ambiguous_multi_observed_no_drift", declared: []string{"repo/app:v1"}, observed: []string{"repo/app:v1", "repo/app:v2"}, wantFinds: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cloud := &ResourceRow{ARN: arn, ResourceType: "ecs.task_definition", ContainerImages: tc.observed}
			state := &ResourceRow{ARN: arn, ResourceType: "aws_ecs_task_definition", ContainerImages: tc.declared}
			got := Classify(cloud, state, config)
			gotFinds := got == FindingKindImageVersionDrift
			if gotFinds != tc.wantFinds {
				t.Fatalf("Classify() = %q (drift=%v), want drift=%v", got, gotFinds, tc.wantFinds)
			}
		})
	}
}

// TestClassifyExistenceKindsTakePrecedenceOverValueDrift is a regression
// guard: orphaned/unmanaged existence classification must never be
// shadowed by the new value-drift path.
func TestClassifyExistenceKindsTakePrecedenceOverValueDrift(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	cloud := &ResourceRow{ARN: arn, ResourceType: "aws_ec2_instance", Attributes: map[string]string{"ami": "ami-a"}}

	if got := Classify(cloud, nil, nil); got != FindingKindOrphanedCloudResource {
		t.Fatalf("Classify() = %q, want %q", got, FindingKindOrphanedCloudResource)
	}

	state := &ResourceRow{ARN: arn, ResourceType: "aws_instance", Attributes: map[string]string{"ami": "ami-b"}}
	if got := Classify(cloud, state, nil); got != FindingKindUnmanagedCloudResource {
		t.Fatalf("Classify() = %q, want %q (existence must win even though values also differ)", got, FindingKindUnmanagedCloudResource)
	}
}
