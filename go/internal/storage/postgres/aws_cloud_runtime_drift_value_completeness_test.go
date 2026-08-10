// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// TestClassifyLambdaImagePackagedWithoutObservedImageURIDoesNotConverge is the
// surviving half of #5861.
//
// #5904 closed the REDACTED trigger: a redaction marker on any allowlisted
// comparable suppresses the whole scalar set, so the pair reports
// value_comparison_inconclusive instead of converging. It deliberately did not
// suppress on a genuinely ABSENT comparable, because every zip-packaged Lambda
// lacks image_uri by design and would otherwise go inconclusive -- the
// objection #5861 itself records.
//
// That leaves a third state the rule cannot currently name: a comparable that
// is absent but SHOULD have been observed. An Image-packaged Lambda has an
// image_uri by definition, so an observed side reporting
// package_type="Image" with no image_uri is unobservable evidence, not
// absent evidence. It is reachable: when GetFunction returns a nil output the
// AWS client falls back to the ListFunctions FunctionConfiguration
// (go/internal/collector/awscloud/services/lambda/awssdk/client.go:89-94),
// which carries PackageType but no Code block, so mapFunction yields
// PackageType "Image" with an empty ImageURI.
//
// Untreated, that produces Comparable=2, Compared=1, Drifted=0,
// Inconclusive()=false -> Classify returns "" -> BuildCandidates drops the ARN
// -> the generation-authoritative retire DELETES whatever finding it held.
// Deleting a true drift on unreadable evidence is the exact failure #5904
// exists to prevent, reached through absence instead of redaction.
func TestClassifyLambdaImagePackagedWithoutObservedImageURIDoesNotConverge(t *testing.T) {
	t.Parallel()

	cloudPayload := []byte(`{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:app",
		"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:app",
		"resource_type": "aws_lambda_function",
		"attributes": {"package_type": "Image", "image_uri": "", "version": "7"}
	}`)
	cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:lambda", cloudPayload)
	if !ok {
		t.Fatal("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}

	statePayload := []byte(`{
		"address": "module.app.aws_lambda_function.app",
		"type": "aws_lambda_function",
		"attributes": {
			"arn": "arn:aws:lambda:us-east-1:123456789012:function:app",
			"package_type": "Image",
			"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/app:v2",
			"version": "7"
		}
	}`)
	state, ok, _ := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.app.aws_lambda_function.app", statePayload)
	if !ok {
		t.Fatal("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	config := &cloudruntime.ResourceRow{Address: state.Address, ResourceType: state.ResourceType}

	comparison := cloudruntime.ClassifyValueComparison(cloud, state)
	if comparison.Compared != 0 {
		t.Fatalf(
			"ClassifyValueComparison() Compared = %d, want 0: a partial comparison (version equal, image_uri "+
				"unobservable) is exactly what converges and lets the retire delete a true finding",
			comparison.Compared,
		)
	}
	if kind := cloudruntime.Classify(cloud, state, config); kind != cloudruntime.FindingKindValueComparisonInconclusive {
		t.Fatalf(
			"Classify() = %q, want %q: an Image-packaged Lambda with no observed image_uri is unobservable, not absent",
			kind, cloudruntime.FindingKindValueComparisonInconclusive,
		)
	}
}

// TestClassifyLambdaZipPackagedAbsentImageURIStillCompares is the #5861 noise
// objection made executable, and it must pass both BEFORE and AFTER the fix.
//
// A zip-packaged Lambda has no image_uri by design. Suppressing on mere
// absence would put every one of them on a value_comparison_inconclusive row,
// which is the reason #5904 restricted its rule to redaction. Keying the new
// suppression on package_type=="Image" leaves zip untouched by construction,
// and this pins that: version still compares, and a real version drift is
// still reported as drift rather than swallowed as uncertainty.
func TestClassifyLambdaZipPackagedAbsentImageURIStillCompares(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name            string
		observedVersion string
		declaredVersion string
		want            cloudruntime.FindingKind
	}{
		{name: "converges when equal", observedVersion: "7", declaredVersion: "7", want: ""},
		{
			name:            "still reports drift when different",
			observedVersion: "7",
			declaredVersion: "9",
			want:            cloudruntime.FindingKindImageVersionDrift,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cloudPayload := []byte(`{
				"arn": "arn:aws:lambda:us-east-1:123456789012:function:zip",
				"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:zip",
				"resource_type": "aws_lambda_function",
				"attributes": {"package_type": "Zip", "version": "` + testCase.observedVersion + `"}
			}`)
			cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:lambda", cloudPayload)
			if !ok {
				t.Fatal("awsRuntimeResourceRowFromPayload() ok = false, want true")
			}

			statePayload := []byte(`{
				"address": "module.zip.aws_lambda_function.zip",
				"type": "aws_lambda_function",
				"attributes": {
					"arn": "arn:aws:lambda:us-east-1:123456789012:function:zip",
					"package_type": "Zip",
					"version": "` + testCase.declaredVersion + `"
				}
			}`)
			state, ok, _ := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.zip.aws_lambda_function.zip", statePayload)
			if !ok {
				t.Fatal("awsRuntimeStateRowFromPayload() ok = false, want true")
			}
			config := &cloudruntime.ResourceRow{Address: state.Address, ResourceType: state.ResourceType}

			if got := cloudruntime.ClassifyValueComparison(cloud, state).Compared; got != 1 {
				t.Fatalf(
					"ClassifyValueComparison() Compared = %d, want 1: a zip-packaged Lambda must still compare "+
						"version -- suppressing it would put every zip Lambda on an inconclusive row (#5861)",
					got,
				)
			}
			if kind := cloudruntime.Classify(cloud, state, config); kind != testCase.want {
				t.Fatalf("Classify() = %q, want %q", kind, testCase.want)
			}
		})
	}
}
