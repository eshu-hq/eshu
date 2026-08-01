// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import (
	"reflect"
	"strings"
	"testing"
)

// Unit coverage for the #5837 value-axis split: ClassifyValueComparison has to
// tell "every comparison ran and agreed" apart from "no comparison could run",
// because Classify answers convergence for the first and explicit uncertainty
// for the second, and the drifted list is empty in both.

const valueComparisonEC2ARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-0comparison"

const valueComparisonLambdaARN = "arn:aws:lambda:us-east-1:123456789012:function:gapatoms"

func TestClassifyValueComparisonSeparatesComparedFromUncomparable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		cloudAttrs       map[string]string
		stateAttrs       map[string]string
		wantCompared     int
		wantUncomparable []string
		wantInconclusive bool
	}{
		{
			name:             "both_sides_present",
			cloudAttrs:       map[string]string{"ami": "ami-a"},
			stateAttrs:       map[string]string{"ami": "ami-a"},
			wantCompared:     1,
			wantUncomparable: nil,
		},
		{
			name:             "state_side_redacted",
			cloudAttrs:       map[string]string{"ami": "ami-a"},
			stateAttrs:       nil,
			wantCompared:     0,
			wantUncomparable: []string{"ami"},
			wantInconclusive: true,
		},
		{
			// attrValue treats an empty value as missing, which is what a
			// decoder that read the key but found nothing usable produces.
			name:             "empty_value_counts_as_missing",
			cloudAttrs:       map[string]string{"ami": "ami-a"},
			stateAttrs:       map[string]string{"ami": ""},
			wantCompared:     0,
			wantUncomparable: []string{"ami"},
			wantInconclusive: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cloud := &ResourceRow{ARN: valueComparisonEC2ARN, ResourceType: "aws_ec2_instance", Attributes: tc.cloudAttrs}
			state := &ResourceRow{ARN: valueComparisonEC2ARN, ResourceType: "aws_instance", Attributes: tc.stateAttrs}

			got := ClassifyValueComparison(cloud, state)
			if got.Comparable != 1 {
				t.Fatalf("Comparable = %d, want 1", got.Comparable)
			}
			if got.Compared != tc.wantCompared {
				t.Fatalf("Compared = %d, want %d", got.Compared, tc.wantCompared)
			}
			if !reflect.DeepEqual(got.Uncomparable, tc.wantUncomparable) {
				t.Fatalf("Uncomparable = %#v, want %#v", got.Uncomparable, tc.wantUncomparable)
			}
			if got.Inconclusive() != tc.wantInconclusive {
				t.Fatalf("Inconclusive() = %v, want %v", got.Inconclusive(), tc.wantInconclusive)
			}
		})
	}
}

// TestClassifyValueComparisonUncoveredTypeIsNotInconclusive keeps the new
// finding kind off every resource type value drift does not cover. Firing there
// would put an "I could not compare" row on every EC2 volume, security group,
// and IAM role in a corpus, none of which value drift ever intended to compare.
func TestClassifyValueComparisonUncoveredTypeIsNotInconclusive(t *testing.T) {
	t.Parallel()

	cloud := &ResourceRow{ARN: "arn:aws:s3:::bucket", ResourceType: "aws_s3_bucket"}
	state := &ResourceRow{ARN: "arn:aws:s3:::bucket", ResourceType: "aws_s3_bucket"}
	config := &ResourceRow{ARN: "arn:aws:s3:::bucket", ResourceType: "aws_s3_bucket"}

	got := ClassifyValueComparison(cloud, state)
	if got.Comparable != 0 {
		t.Fatalf("Comparable = %d, want 0 for an uncovered resource type", got.Comparable)
	}
	if got.Inconclusive() {
		t.Fatal("Inconclusive() = true for a resource type value drift does not cover")
	}
	if kind := Classify(cloud, state, config); kind != "" {
		t.Fatalf("Classify() = %q, want convergence for an uncovered resource type", kind)
	}
}

// TestClassifyECSContainerImageAmbiguityOutcomes covers the ECS half of the
// same split, and the line drawn through ambiguity. The container-image
// comparison reports ambiguity for both an empty side and a multi-image
// observed set, but only the first can change between passes: an empty side
// means this pass lost evidence a healthier pass would carry, while a
// multi-image observed set is the task definition's own shape. Only the
// transient one can destroy a finding, so only it becomes one.
func TestClassifyECSContainerImageAmbiguityOutcomes(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/app:1"
	config := &ResourceRow{ARN: arn, ResourceType: "aws_ecs_task_definition"}

	cases := []struct {
		name           string
		declared       []string
		observed       []string
		wantFindingKnd FindingKind
	}{
		{
			name:           "declared_side_unreadable",
			declared:       nil,
			observed:       []string{"repo/app:v1"},
			wantFindingKnd: FindingKindValueComparisonInconclusive,
		},
		{
			// Both sides carry images, so nothing about THIS pass is degraded;
			// the classifier simply will not pair a multi-image observed set.
			// That is permanent for the ARN, so it stays the pre-existing
			// under-reporting gap rather than becoming a finding.
			name:           "multi_image_observed_cannot_be_paired",
			declared:       []string{"repo/app:v1"},
			observed:       []string{"repo/app:v1", "repo/sidecar:v1"},
			wantFindingKnd: "",
		},
		{
			name:           "single_matching_image_converges",
			declared:       []string{"repo/app:v1"},
			observed:       []string{"repo/app:v1"},
			wantFindingKnd: "",
		},
		{
			name:           "single_differing_image_is_drift",
			declared:       []string{"repo/app:v1"},
			observed:       []string{"repo/app:v2"},
			wantFindingKnd: FindingKindImageVersionDrift,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cloud := &ResourceRow{ARN: arn, ResourceType: "ecs.task_definition", ContainerImages: tc.observed}
			state := &ResourceRow{ARN: arn, ResourceType: "aws_ecs_task_definition", ContainerImages: tc.declared}
			if got := Classify(cloud, state, config); got != tc.wantFindingKnd {
				t.Fatalf("Classify() = %q, want %q", got, tc.wantFindingKnd)
			}
		})
	}
}

// TestClassifyLambdaOneOfTwoComparisonsIsStillAVerdict pins the residual #5837
// does NOT close, so it stays a known bounded gap rather than a surprise.
//
// aws_lambda_function is covered for two attributes. When image_uri is redacted
// but version compares equal, ONE comparison succeeded, so the verdict is
// convergence and an image_uri drift that exists in reality is still retired.
// Closing it needs per-attribute completeness plumbing from the collector --
// tracked as #5861 -- not a change here: making one uncomparable attribute out
// of two inconclusive would put a finding on every Lambda whose image_uri is
// legitimately absent (a zip-packaged function), which is most of them.
func TestClassifyLambdaOneOfTwoComparisonsIsStillAVerdict(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:lambda:us-east-1:123456789012:function:app"
	config := &ResourceRow{ARN: arn, ResourceType: "aws_lambda_function"}
	cloud := &ResourceRow{
		ARN:          arn,
		ResourceType: "lambda.function",
		Attributes:   map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v2", "version": "7"},
	}
	// image_uri redacted on the declared side; version present and equal.
	state := &ResourceRow{
		ARN:          arn,
		ResourceType: "aws_lambda_function",
		Attributes:   map[string]string{"version": "7"},
	}

	comparison := ClassifyValueComparison(cloud, state)
	if comparison.Comparable != 2 || comparison.Compared != 1 {
		t.Fatalf("Comparable/Compared = %d/%d, want 2/1", comparison.Comparable, comparison.Compared)
	}
	if !reflect.DeepEqual(comparison.Uncomparable, []string{"image_uri"}) {
		t.Fatalf("Uncomparable = %#v, want [image_uri]", comparison.Uncomparable)
	}
	if comparison.Inconclusive() {
		t.Fatal("Inconclusive() = true; one successful comparison is still a verdict (see #5861 for the residual)")
	}
	if got := Classify(cloud, state, config); got != "" {
		t.Fatalf("Classify() = %q, want convergence: this is the documented #5861 residual", got)
	}
}

// TestBuildCandidatesNamesUncomparableAttributes proves the inconclusive
// candidate is actionable: it says WHICH comparable attribute could not be read,
// through the same missing_evidence key the loader's coverage atoms use.
func TestBuildCandidatesNamesUncomparableAttributes(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:123456789012:us-east-1:ec2"
	rows := []AddressedRow{{
		ARN:          valueComparisonEC2ARN,
		ResourceType: "aws_instance",
		Cloud: &ResourceRow{
			ARN:          valueComparisonEC2ARN,
			ResourceType: "aws_ec2_instance",
			ScopeID:      scopeID,
			Attributes:   map[string]string{"ami": "ami-observed"},
		},
		State:  &ResourceRow{ARN: valueComparisonEC2ARN, ResourceType: "aws_instance", ScopeID: scopeID},
		Config: &ResourceRow{ARN: valueComparisonEC2ARN, ResourceType: "aws_instance", ScopeID: scopeID},
	}}

	candidates := BuildCandidates(rows, scopeID)
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
	}

	var (
		gotKind        string
		gotGapValues   []string
		gotStatusValue string
	)
	for _, atom := range candidates[0].Evidence {
		switch atom.EvidenceType {
		case EvidenceTypeFindingKind:
			gotKind = atom.Value
		case EvidenceTypeCoverageGap:
			gotGapValues = append(gotGapValues, atom.Value)
		case EvidenceTypeManagementStatus:
			gotStatusValue = atom.Value
		}
	}
	if gotKind != string(FindingKindValueComparisonInconclusive) {
		t.Fatalf("finding kind atom = %q, want %q", gotKind, FindingKindValueComparisonInconclusive)
	}
	if !reflect.DeepEqual(gotGapValues, []string{"comparable_attribute:ami"}) {
		t.Fatalf("coverage-gap atoms = %#v, want [comparable_attribute:ami]", gotGapValues)
	}
	if gotStatusValue != ManagementStatusUnknown {
		t.Fatalf("management status atom = %q, want %q", gotStatusValue, ManagementStatusUnknown)
	}
}

// TestBuildCandidatesEmitsNoCoverageGapForOtherKinds keeps the new atoms off the
// existence findings, whose missing evidence is a missing LAYER, not an
// unreadable attribute.
func TestBuildCandidatesEmitsNoCoverageGapForOtherKinds(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:123456789012:us-east-1:ec2"
	rows := []AddressedRow{{
		ARN:          valueComparisonEC2ARN,
		ResourceType: "aws_instance",
		Cloud: &ResourceRow{
			ARN:          valueComparisonEC2ARN,
			ResourceType: "aws_ec2_instance",
			ScopeID:      scopeID,
			Attributes:   map[string]string{"ami": "ami-observed"},
		},
	}}

	candidates := BuildCandidates(rows, scopeID)
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
	}
	for _, atom := range candidates[0].Evidence {
		if atom.EvidenceType == EvidenceTypeCoverageGap {
			t.Fatalf("orphaned finding carries a value-comparison coverage-gap atom %#v", atom)
		}
	}
}

// TestClassifyHealthyMultiContainerTaskDefinitionStaysSilent pins the boundary
// of the inconclusive outcome: it fires for a TRANSIENT evidence gap, never for
// the PERMANENT multi-container pairing gap.
//
// A task definition whose declared and observed image sets are identical is
// converged by any reading. ClassifyContainerImageDrift still reports it
// ambiguous, because it refuses to pair more than one observed image against a
// declared set. That ambiguity is a property of the resource's shape, not of
// this pass's evidence, so it is identical on every pass -- no pass ever wrote a
// finding for the ARN, and none can lose one. Emitting inconclusive here would
// put an un-actionable finding on every multi-container task definition forever,
// which is the noise the #5861 residual is deliberately avoiding on the lambda
// side (#5837).
func TestClassifyHealthyMultiContainerTaskDefinitionStaysSilent(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/api:9"
	images := []string{"registry.example/api:v3", "registry.example/sidecar:v1"}
	config := &ResourceRow{ARN: arn, ResourceType: "aws_ecs_task_definition"}
	state := &ResourceRow{
		ARN:             arn,
		ResourceType:    "aws_ecs_task_definition",
		ContainerImages: images,
	}
	cloud := &ResourceRow{
		ARN:             arn,
		ResourceType:    "ecs.task_definition",
		ContainerImages: images,
	}

	comparison := ClassifyValueComparison(cloud, state)
	if comparison.Inconclusive() {
		t.Fatalf("Inconclusive() = true for a converged multi-container task definition; "+
			"Comparable/Compared = %d/%d, Uncomparable = %#v",
			comparison.Comparable, comparison.Compared, comparison.Uncomparable)
	}
	if got := Classify(cloud, state, config); got != "" {
		t.Fatalf("Classify() = %q, want silence: the pairing gap is permanent, so no finding can be lost", got)
	}
}

// TestClassifyDegradedContainerImagesIsStillInconclusive is the other side of
// that boundary. Here the observed images could not be READ -- a redaction
// marker where the container-definitions JSON was expected -- so the gap is
// transient, a healthy pass would have produced a real verdict, and the retire
// must supersede rather than destroy.
func TestClassifyDegradedContainerImagesIsStillInconclusive(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/api:9"
	config := &ResourceRow{ARN: arn, ResourceType: "aws_ecs_task_definition"}
	state := &ResourceRow{
		ARN:             arn,
		ResourceType:    "aws_ecs_task_definition",
		ContainerImages: []string{"registry.example/api:v3"},
	}
	cloud := &ResourceRow{
		ARN:                     arn,
		ResourceType:            "ecs.task_definition",
		ContainerImagesDegraded: true,
	}

	comparison := ClassifyValueComparison(cloud, state)
	if !comparison.Inconclusive() {
		t.Fatalf("Inconclusive() = false for unreadable observed images; Comparable/Compared = %d/%d",
			comparison.Comparable, comparison.Compared)
	}
	if got := Classify(cloud, state, config); got != FindingKindValueComparisonInconclusive {
		t.Fatalf("Classify() = %q, want %q", got, FindingKindValueComparisonInconclusive)
	}
}

// TestGapAtomsOnlyForInconclusive is the negative half of
// TestBuildCandidatesNamesUncomparableAttributes. Deleting the kind guard in
// appendValueComparisonGapEvidence passed every suite in the repo, on both
// drift routes, which made the guard a decoration.
//
// It matters because a non-empty missing_evidence set OVERRIDES the
// status-derived fallback in the writer. An unmanaged_cloud_resource whose ami
// happens to be uncomparable would answer "ami could not be read" where the
// operator asked "what declares this resource", and the real answer would be
// gone (#5837).
func TestGapAtomsOnlyForInconclusive(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:123456789012:us-east-1:ec2"
	cloud := func() *ResourceRow {
		return &ResourceRow{
			ARN: valueComparisonEC2ARN, ResourceType: "aws_ec2_instance", ScopeID: scopeID,
			Attributes: map[string]string{"ami": "ami-observed"},
		}
	}
	state := func() *ResourceRow {
		return &ResourceRow{ARN: valueComparisonEC2ARN, ResourceType: "aws_instance", ScopeID: scopeID}
	}

	cases := []struct {
		name string
		row  AddressedRow
	}{{
		// Config absent: an existence verdict, with an equally uncomparable ami.
		name: "unmanaged_cloud_resource",
		row:  AddressedRow{ARN: valueComparisonEC2ARN, ResourceType: "aws_instance", Cloud: cloud(), State: state()},
	}, {
		// A REAL drift that ALSO has an uncomparable attribute, which is the
		// only other shape that discriminates. aws_lambda_function is covered
		// for image_uri and version: version compares and differs (so the kind
		// is image_version_drift, not inconclusive) while image_uri is absent
		// on the declared side (so Uncomparable is NON-empty and an unguarded
		// emitter has something to emit).
		//
		// A plain single-attribute drift does NOT discriminate, which is worth
		// stating because it was tried first: when every covered attribute
		// compares, Uncomparable is empty and the loop emits nothing whether
		// the guard is present or not. Same for an orphaned row, where there is
		// no declared side to be uncomparable against.
		name: "image_version_drift_with_an_uncomparable_attribute",
		row: AddressedRow{
			ARN: valueComparisonLambdaARN, ResourceType: "aws_lambda_function",
			Cloud: &ResourceRow{
				ARN: valueComparisonLambdaARN, ResourceType: "lambda.function", ScopeID: scopeID,
				Attributes: map[string]string{"image_uri": "acct.dkr.ecr/app:v2", "version": "9"},
			},
			State: &ResourceRow{
				ARN: valueComparisonLambdaARN, ResourceType: "aws_lambda_function", ScopeID: scopeID,
				Attributes: map[string]string{"version": "7"},
			},
			Config: &ResourceRow{ARN: valueComparisonLambdaARN, ResourceType: "aws_lambda_function", ScopeID: scopeID},
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidates := BuildCandidates([]AddressedRow{tc.row}, scopeID)
			if len(candidates) != 1 {
				t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
			}
			for _, atom := range candidates[0].Evidence {
				// Pin all three axes, not just the value: a future emitter that
				// keeps the prefix but changes the type or the ID would slip a
				// value-shaped atom past a value-only assertion.
				if strings.HasPrefix(atom.Value, "comparable_attribute:") ||
					strings.Contains(atom.ID, "/uncomparable/") {
					t.Fatalf("kind %s emitted id=%q type=%q value=%q; comparable_attribute atoms "+
						"belong to value_comparison_inconclusive alone", tc.name, atom.ID, atom.EvidenceType, atom.Value)
				}
			}
		})
	}
}
