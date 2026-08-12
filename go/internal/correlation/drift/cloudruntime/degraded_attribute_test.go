// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import (
	"reflect"
	"testing"
)

const lambdaDegradedARN = "arn:aws:lambda:us-east-1:123456789012:function:app"

// lambdaDegradedRows builds the cloud/state/config triple every test in this
// file classifies, so each case differs only in the fields it names rather than
// in three near-identical literals. degradedOn is the state-side
// DegradedAttributes set.
func lambdaDegradedRows(
	observed, declared map[string]string,
	degradedOn []string,
) (cloud, state, config *ResourceRow) {
	cloud = &ResourceRow{
		ARN:          lambdaDegradedARN,
		ResourceType: "lambda.function",
		Attributes:   observed,
	}
	state = &ResourceRow{
		ARN:                lambdaDegradedARN,
		ResourceType:       "aws_lambda_function",
		Attributes:         declared,
		DegradedAttributes: degradedOn,
	}
	config = &ResourceRow{ARN: lambdaDegradedARN, ResourceType: "aws_lambda_function"}
	return cloud, state, config
}

// TestDegradedAttributeKeepsTheReadableComparisonsVerdict is the accuracy half
// of #5861.
//
// A resource type covered for two comparables, one of which this pass could not
// READ, still knows everything it needs about the other one. Suppressing the
// whole set restated a proven drift as uncertainty; carrying the unreadable key
// as degraded keeps the proven comparison AND its declared_/observed_ evidence.
func TestDegradedAttributeKeepsTheReadableComparisonsVerdict(t *testing.T) {
	t.Parallel()

	cloud, state, config := lambdaDegradedRows(
		map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v2", "version": "7"},
		map[string]string{"version": "9"},
		[]string{"image_uri"},
	)

	comparison := ClassifyValueComparison(cloud, state)
	if comparison.Comparable != 2 || comparison.Compared != 1 {
		t.Fatalf("Comparable/Compared = %d/%d, want 2/1", comparison.Comparable, comparison.Compared)
	}
	if !reflect.DeepEqual(comparison.Degraded, []string{"image_uri"}) {
		t.Fatalf("Degraded = %#v, want [image_uri]", comparison.Degraded)
	}
	want := []DriftedAttribute{{Key: "version", Declared: "9", Observed: "7"}}
	if !reflect.DeepEqual(comparison.Drifted, want) {
		t.Fatalf("Drifted = %#v, want %#v: the readable comparison is still proof", comparison.Drifted, want)
	}
	if got := Classify(cloud, state, config); got != FindingKindImageVersionDrift {
		t.Fatalf("Classify() = %q, want %q: a proven drift is not restated as uncertainty (#5861)", got, FindingKindImageVersionDrift)
	}
}

// TestDegradedAttributeDeclinesConvergenceWhenReadableComparisonsAgree is the
// non-destructive half.
//
// This is the shape the issue is named for: the readable comparison agrees, so
// nothing contradicts convergence -- but a comparable this resource type IS
// covered for could not be read, so the pass has no standing to retire a
// finding a healthier pass wrote. Convergence here is what deletes it.
func TestDegradedAttributeDeclinesConvergenceWhenReadableComparisonsAgree(t *testing.T) {
	t.Parallel()

	cloud, state, config := lambdaDegradedRows(
		map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v2", "version": "7"},
		map[string]string{"version": "7"},
		[]string{"image_uri"},
	)

	comparison := ClassifyValueComparison(cloud, state)
	if comparison.Compared != 1 || len(comparison.Drifted) != 0 {
		t.Fatalf("Compared/Drifted = %d/%d, want 1/0", comparison.Compared, len(comparison.Drifted))
	}
	if !comparison.Inconclusive() {
		t.Fatal("Inconclusive() = false: an unreadable comparable must never let a pass converge and retire (#5861)")
	}
	if got := Classify(cloud, state, config); got != FindingKindValueComparisonInconclusive {
		t.Fatalf("Classify() = %q, want %q", got, FindingKindValueComparisonInconclusive)
	}
}

// TestAbsentAttributeIsNotDegraded pins the noise line #5861 records as the
// reason not to widen Inconclusive() on absence.
//
// A zip-packaged Lambda has no image_uri by design. Nothing was unreadable, so
// nothing is degraded, and the pair keeps converging on an equal version rather
// than putting an inconclusive row on every zip function in a corpus.
func TestAbsentAttributeIsNotDegraded(t *testing.T) {
	t.Parallel()

	cloud, state, config := lambdaDegradedRows(
		map[string]string{"version": "7"},
		map[string]string{"version": "7"},
		nil,
	)

	comparison := ClassifyValueComparison(cloud, state)
	if len(comparison.Degraded) != 0 {
		t.Fatalf("Degraded = %#v, want empty: absence is not degradation", comparison.Degraded)
	}
	if comparison.Inconclusive() {
		t.Fatal("Inconclusive() = true: a genuinely absent comparable must still converge (#5861 noise objection)")
	}
	if got := Classify(cloud, state, config); got != "" {
		t.Fatalf("Classify() = %q, want convergence", got)
	}
}

// TestDegradedOnEitherSideCounts proves the rule is side-agnostic. Whichever
// side could not be read, the pass is equally unable to say the comparables
// agree, so both must decline convergence.
func TestDegradedOnEitherSideCounts(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name           string
		cloudDegraded  []string
		stateDegraded  []string
		wantDegradedOn []string
	}{
		{name: "observed side", cloudDegraded: []string{"image_uri"}, wantDegradedOn: []string{"image_uri"}},
		{name: "declared side", stateDegraded: []string{"image_uri"}, wantDegradedOn: []string{"image_uri"}},
		{
			name:           "both sides names the key once",
			cloudDegraded:  []string{"image_uri"},
			stateDegraded:  []string{"image_uri"},
			wantDegradedOn: []string{"image_uri"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cloud, state, config := lambdaDegradedRows(
				map[string]string{"version": "7"},
				map[string]string{"version": "7"},
				testCase.stateDegraded,
			)
			cloud.DegradedAttributes = testCase.cloudDegraded

			comparison := ClassifyValueComparison(cloud, state)
			if !reflect.DeepEqual(comparison.Degraded, testCase.wantDegradedOn) {
				t.Fatalf("Degraded = %#v, want %#v", comparison.Degraded, testCase.wantDegradedOn)
			}
			if got := Classify(cloud, state, config); got != FindingKindValueComparisonInconclusive {
				t.Fatalf("Classify() = %q, want %q", got, FindingKindValueComparisonInconclusive)
			}
		})
	}
}

// TestDegradedAttributesFollowAllowlistOrder pins determinism. The keys become
// evidence atoms, so a map-iteration order would make the same degraded pass
// emit a different candidate on every run.
func TestDegradedAttributesFollowAllowlistOrder(t *testing.T) {
	t.Parallel()

	// Named in reverse allowlist order on purpose: the output must not inherit
	// the caller's ordering.
	cloud, state, _ := lambdaDegradedRows(nil, nil, []string{"version", "image_uri"})

	comparison := ClassifyValueComparison(cloud, state)
	want := ValueAttributeAllowlistFor("aws_lambda_function")
	if !reflect.DeepEqual(comparison.Degraded, want) {
		t.Fatalf("Degraded = %#v, want allowlist order %#v", comparison.Degraded, want)
	}
}

// TestUnknownDegradedAttributeIsIgnored keeps a decoder bug from inventing
// coverage. Degraded only ever speaks for keys this resource type is actually
// covered for; a key outside the allowlist is not a comparable, so naming it
// must not manufacture an inconclusive finding.
func TestUnknownDegradedAttributeIsIgnored(t *testing.T) {
	t.Parallel()

	cloud, state, config := lambdaDegradedRows(
		map[string]string{"version": "7"},
		map[string]string{"version": "7"},
		[]string{"runtime"},
	)

	comparison := ClassifyValueComparison(cloud, state)
	if len(comparison.Degraded) != 0 {
		t.Fatalf("Degraded = %#v, want empty for a non-allowlisted key", comparison.Degraded)
	}
	if got := Classify(cloud, state, config); got != "" {
		t.Fatalf("Classify() = %q, want convergence", got)
	}
}

// TestECSDegradedContainerImagesCountAsDegraded generalizes the flag this
// change is modeled on. ContainerImagesDegraded already meant "the evidence
// existed and we could not use it"; it now feeds the same Degraded signal the
// scalar allowlist does, so a future ECS scalar comparable cannot converge
// alongside an unreadable container_definitions.
func TestECSDegradedContainerImagesCountAsDegraded(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/app:3"
	cloud := &ResourceRow{ARN: arn, ResourceType: "ecs.task_definition", ContainerImages: []string{"acct.dkr.ecr.us-east-1.amazonaws.com/app:v2"}}
	state := &ResourceRow{ARN: arn, ResourceType: ecsTaskDefinitionResourceType, ContainerImagesDegraded: true}
	config := &ResourceRow{ARN: arn, ResourceType: ecsTaskDefinitionResourceType}

	comparison := ClassifyValueComparison(cloud, state)
	if !reflect.DeepEqual(comparison.Degraded, []string{containerImageAttributeKey}) {
		t.Fatalf("Degraded = %#v, want [%s]", comparison.Degraded, containerImageAttributeKey)
	}
	if got := Classify(cloud, state, config); got != FindingKindValueComparisonInconclusive {
		t.Fatalf("Classify() = %q, want %q", got, FindingKindValueComparisonInconclusive)
	}
}

// TestECSAbsentContainerImagesAreNotDegraded keeps the distinction the ECS flag
// was introduced for: a task definition that simply carried no images on this
// pass is uncomparable, but it is not evidence we failed to read.
func TestECSAbsentContainerImagesAreNotDegraded(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/app:3"
	cloud := &ResourceRow{ARN: arn, ResourceType: "ecs.task_definition", ContainerImages: []string{"acct.dkr.ecr.us-east-1.amazonaws.com/app:v2"}}
	state := &ResourceRow{ARN: arn, ResourceType: ecsTaskDefinitionResourceType}

	comparison := ClassifyValueComparison(cloud, state)
	if len(comparison.Degraded) != 0 {
		t.Fatalf("Degraded = %#v, want empty", comparison.Degraded)
	}
	// Still inconclusive, but through Compared == 0 rather than degradation.
	if !comparison.Inconclusive() {
		t.Fatal("Inconclusive() = false, want true")
	}
}

// TestDriftCandidateNamesDegradedAttributes proves an image_version_drift
// finding reached on partial evidence SAYS so.
//
// Before this change the gap atoms were emitted for value_comparison_inconclusive
// only, on the reasoning that a drift finding already carries the pair for the
// attribute that compared. That held while a degraded comparable forced the
// whole pair to inconclusive. Now that a drift verdict can be reached with one
// comparable unread, the operator needs to see which one.
func TestDriftCandidateNamesDegradedAttributes(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:123456789012:us-east-1:lambda"
	cloud, state, config := lambdaDegradedRows(
		map[string]string{"image_uri": "acct.dkr.ecr.us-east-1.amazonaws.com/app:v2", "version": "7"},
		map[string]string{"version": "9"},
		[]string{"image_uri"},
	)

	candidates := BuildCandidates([]AddressedRow{{
		ARN:    lambdaDegradedARN,
		Cloud:  cloud,
		State:  state,
		Config: config,
	}}, scopeID)
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
	}

	var gaps, observed []string
	for _, atom := range candidates[0].Evidence {
		switch atom.EvidenceType {
		case EvidenceTypeCoverageGap:
			gaps = append(gaps, atom.Value)
		case EvidenceTypeObservedValue:
			observed = append(observed, atom.Key)
		}
	}
	if !reflect.DeepEqual(gaps, []string{"comparable_attribute:image_uri"}) {
		t.Fatalf("coverage-gap atoms = %#v, want [comparable_attribute:image_uri]", gaps)
	}
	if !reflect.DeepEqual(observed, []string{"observed_version"}) {
		t.Fatalf("observed atoms = %#v, want [observed_version]", observed)
	}
}

// TestDriftCandidateOmitsGapAtomsForMerelyAbsentAttributes is the noise guard
// on the atom surface. A zip-packaged Lambda drifting on version has an
// uncomparable image_uri too, but nothing was unreadable -- emitting a coverage
// gap there would tell every zip function's drift finding to go look for
// missing collector coverage that does not exist.
func TestDriftCandidateOmitsGapAtomsForMerelyAbsentAttributes(t *testing.T) {
	t.Parallel()

	const scopeID = "aws:123456789012:us-east-1:lambda"
	cloud, state, config := lambdaDegradedRows(
		map[string]string{"version": "7"},
		map[string]string{"version": "9"},
		nil,
	)

	candidates := BuildCandidates([]AddressedRow{{
		ARN:    lambdaDegradedARN,
		Cloud:  cloud,
		State:  state,
		Config: config,
	}}, scopeID)
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
	}
	for _, atom := range candidates[0].Evidence {
		if atom.EvidenceType == EvidenceTypeCoverageGap {
			t.Fatalf("unexpected coverage-gap atom %q: absence is not a coverage gap", atom.Value)
		}
	}
}
