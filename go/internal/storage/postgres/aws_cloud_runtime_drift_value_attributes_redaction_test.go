// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// Redaction-marker regressions for the aws_cloud_runtime_drift value-drift
// decoders (#5859). Split out of aws_cloud_runtime_drift_value_attributes_test.go
// to keep both files under the repo's 500-line cap; the tests there cover the
// happy-path attribute population these are the failure counterpart to.

// redactionMarkerJSON renders the exact JSON object shape the terraform-state
// collector's redactionMap (go/internal/collector/terraformstate/identity.go)
// and the AWS-cloud collector's RedactString/ClassifyStackOutput
// (go/internal/collector/awscloud/redaction.go) both persist in place of a
// fail-closed-redacted scalar: {"marker": "redacted:hmac-sha256:...",
// "reason": ..., "source": ...}. The hex digest is a fixed fixture value --
// only the "redacted:hmac-sha256:" prefix is load-bearing for recognition.
func redactionMarkerJSON(reason, source string) string {
	return fmt.Sprintf(
		`{"marker":"redacted:hmac-sha256:%s","reason":%s,"source":%s}`,
		strings.Repeat("a", 64),
		mustJSONString(reason),
		mustJSONString(source),
	)
}

// TestAWSRuntimeStateRowFromPayloadTreatsRedactionMarkerAsAbsentAMI is the
// #5859 regression at the decoder boundary: when the terraform-state
// collector fail-closed-redacts "ami" (nil or unparseable provider-schema
// bundle), the persisted attribute is the marker object above, not a plain
// string. Before the fix, stateDeclaredValueAttributes ran it through
// coerceJSONString's default fmt.Sprint(map) branch and produced a non-empty
// garbage string such as
// "map[marker:redacted:hmac-sha256:... reason:unknown_provider_schema
// source:resources.*.attributes.ami]" -- which is present, non-empty, and
// compares unequal to any real observed AMI. This test proves the decoder
// instead treats a redacted "ami" as wholly absent from row.Attributes.
func TestAWSRuntimeStateRowFromPayloadTreatsRedactionMarkerAsAbsentAMI(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"ami": ` + redactionMarkerJSON("unknown_provider_schema", "resources.*.attributes.ami") + `
		}
	}`)

	row, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	if len(row.Attributes) != 0 {
		t.Fatalf("row.Attributes = %#v, want empty (redaction marker must not survive as a comparable value)", row.Attributes)
	}
}

// TestClassifyValueDriftSuppressesRedactionMarkerDeclaredSide is the
// end-to-end #5859 regression at the classifier level: a Terraform-declared
// "ami" that is a still-redacted marker, compared against a real
// AWS-observed AMI, must NOT produce image_version_drift. Acting on that
// finding would tell an operator to reconcile real infrastructure against a
// value that was never actually declared -- it is an artifact of a broken
// provider-schema bundle, not real drift.
//
// The exact kind is asserted, not merely "!= FindingKindImageVersionDrift",
// because the weaker form passes on both sides of the outcome that matters.
// "ami" is aws_instance's ONLY cloudruntime.ValueAttributeAllowlistFor entry,
// so a suppressed marker leaves ClassifyValueComparison with Comparable=1 and
// Compared=0 -- which #5837 defines as value_comparison_inconclusive, NOT the
// "" that means convergence. That distinction is load-bearing: "" would drop
// the ARN from the candidate set and let the generation-authoritative retire
// delete a still-true finding. This is the seam where the #5859 decoder fix
// and the #5837 classifier outcome meet, so it is pinned exactly.
func TestClassifyValueDriftSuppressesRedactionMarkerDeclaredSide(t *testing.T) {
	t.Parallel()

	statePayload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"ami": ` + redactionMarkerJSON("unknown_provider_schema", "resources.*.attributes.ami") + `
		}
	}`)
	state, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", statePayload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}

	cloudPayload := []byte(`{
		"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		"resource_id": "i-0123456789abcdef0",
		"resource_type": "aws_ec2_instance",
		"attributes": {"ami_id": "ami-000000000000000a"}
	}`)
	cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:ec2", cloudPayload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}

	config := &cloudruntime.ResourceRow{
		Address:      state.Address,
		ResourceType: state.ResourceType,
	}

	if drifted := cloudruntime.ClassifyValueDrift(cloud, state); len(drifted) != 0 {
		t.Fatalf("ClassifyValueDrift() = %#v, want no drift for a redacted declared value", drifted)
	}
	if kind := cloudruntime.Classify(cloud, state, config); kind != cloudruntime.FindingKindValueComparisonInconclusive {
		t.Fatalf(
			"Classify() = %q, want %q when the declared AMI is a redaction marker",
			kind,
			cloudruntime.FindingKindValueComparisonInconclusive,
		)
	}
}

// TestClassifyValueDriftSuppressesRedactionMarkerObservedSide covers the
// symmetric case: the AWS-observed side carries a redaction marker (the
// AWS-cloud collector's own RedactString/ClassifyStackOutput produce the
// identical {"marker","reason","source"} shape -- see
// go/internal/collector/awscloud/redaction.go) while Terraform state
// declares a real value. Comparing a real declared AMI against an observed
// marker must not fire image_version_drift either.
func TestClassifyValueDriftSuppressesRedactionMarkerObservedSide(t *testing.T) {
	t.Parallel()

	statePayload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"ami": "ami-0123456789abcdef0"
		}
	}`)
	state, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", statePayload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}

	cloudPayload := []byte(`{
		"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		"resource_id": "i-0123456789abcdef0",
		"resource_type": "aws_ec2_instance",
		"attributes": {"ami_id": ` + redactionMarkerJSON("unknown_provider_schema", "attributes.ami_id") + `}
	}`)
	cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:ec2", cloudPayload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}

	if drifted := cloudruntime.ClassifyValueDrift(cloud, state); len(drifted) != 0 {
		t.Fatalf("ClassifyValueDrift() = %#v, want no drift for a redacted observed value", drifted)
	}
	config := &cloudruntime.ResourceRow{Address: state.Address, ResourceType: state.ResourceType}
	if kind := cloudruntime.Classify(cloud, state, config); kind != cloudruntime.FindingKindValueComparisonInconclusive {
		t.Fatalf(
			"Classify() = %q, want %q when the observed AMI is a redaction marker",
			kind,
			cloudruntime.FindingKindValueComparisonInconclusive,
		)
	}
}

// TestClassifyValueDriftSuppressesRedactionMarkerBothSides covers both sides
// redacted simultaneously (e.g. a schema bundle broken on both the state and
// cloud collectors in the same scan): still no drift, since neither side has
// a comparable value.
func TestClassifyValueDriftSuppressesRedactionMarkerBothSides(t *testing.T) {
	t.Parallel()

	statePayload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"ami": ` + redactionMarkerJSON("unknown_provider_schema", "resources.*.attributes.ami") + `
		}
	}`)
	state, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", statePayload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}

	cloudPayload := []byte(`{
		"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		"resource_id": "i-0123456789abcdef0",
		"resource_type": "aws_ec2_instance",
		"attributes": {"ami_id": ` + redactionMarkerJSON("unknown_provider_schema", "attributes.ami_id") + `}
	}`)
	cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:ec2", cloudPayload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}

	if drifted := cloudruntime.ClassifyValueDrift(cloud, state); len(drifted) != 0 {
		t.Fatalf("ClassifyValueDrift() = %#v, want no drift when both sides are redacted", drifted)
	}
	config := &cloudruntime.ResourceRow{Address: state.Address, ResourceType: state.ResourceType}
	if kind := cloudruntime.Classify(cloud, state, config); kind != cloudruntime.FindingKindValueComparisonInconclusive {
		t.Fatalf(
			"Classify() = %q, want %q when both AMIs are redaction markers",
			kind,
			cloudruntime.FindingKindValueComparisonInconclusive,
		)
	}
}

// TestAWSRuntimeStateRowFromPayloadPreservesValueResemblingMarkerText proves
// the marker recognition is shape-based (a JSON object with a "marker"
// field), not a textual prefix match on the raw attribute value -- a
// genuine string-typed AMI value must never be false-negative dropped just
// because its text happens to start with the same "redacted:hmac-sha256:"
// prefix real markers use.
func TestAWSRuntimeStateRowFromPayloadPreservesValueResemblingMarkerText(t *testing.T) {
	t.Parallel()

	const literalValue = "redacted:hmac-sha256:not-actually-a-marker-just-a-string"
	payload := []byte(`{
		"address": "module.ecs.aws_instance.supply-chain-demo",
		"type": "aws_instance",
		"attributes": {
			"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
			"ami": ` + mustJSONString(literalValue) + `
		}
	}`)

	row, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.ecs.aws_instance.supply-chain-demo", payload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	want := map[string]string{"ami": literalValue}
	if !reflect.DeepEqual(row.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v (a plain string must never be treated as a redaction marker)", row.Attributes, want)
	}
}

// TestClassifyLambdaPartialRedactionDoesNotConverge is the regression for the
// worst failure this branch could have introduced. Suppressing a redacted
// value is only safe while suppression cannot be mistaken for convergence.
//
// aws_instance has ONE allowlisted comparable, so erasing it leaves
// Comparable=1, Compared=0 -- #5837's value_comparison_inconclusive, a durable
// row. aws_lambda_function has TWO. Erase only image_uri and leave version
// comparing equal, and a naive per-attribute suppression yields Comparable=2,
// Compared=1: Inconclusive() is false because Compared != 0, Classify returns
// "", BuildCandidates drops the ARN, and the generation-authoritative retire
// DELETES whatever finding that ARN held. Measured on this branch before the
// all-or-nothing rule:
//
//	Comparable=2 Compared=1 Drifted=0 Inconclusive=false  Classify() = ""
//
// That is strictly worse than the bug being fixed. The old garbage-string
// behavior at least left a durable (wrong) row; this would delete a true one,
// silently, on a condition that is sticky per deployment. So a redacted
// comparable suppresses its resource type's WHOLE scalar set: no comparison
// runs, Compared drops to 0, and the pair reports uncertainty instead.
//
// The cost is deliberate and bounded -- a real `version` drift alongside an
// unreadable `image_uri` reports as inconclusive rather than as drift. That
// still names a gap on a durable row an operator can act on, and it can never
// delete. It also matches how main already treats the ECS side, where an
// unreadable container_definitions makes the whole image comparison
// uncomparable rather than partial.
func TestClassifyLambdaPartialRedactionDoesNotConverge(t *testing.T) {
	t.Parallel()

	statePayload := []byte(`{
		"address": "module.fn.aws_lambda_function.demo",
		"type": "aws_lambda_function",
		"attributes": {
			"arn": "arn:aws:lambda:us-east-1:123456789012:function:demo",
			"image_uri": ` + redactionMarkerJSON("unknown_provider_schema", "resources.*.attributes.image_uri") + `,
			"version": "7"
		}
	}`)
	state, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.fn.aws_lambda_function.demo", statePayload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	if _, present := state.Attributes["image_uri"]; present {
		t.Fatalf("row.Attributes = %#v, want no image_uri (a redacted comparable must not survive)", state.Attributes)
	}
	if _, present := state.Attributes["version"]; present {
		t.Fatalf("row.Attributes = %#v, want no version either: one redacted comparable suppresses the whole "+
			"scalar set, or a partial comparison converges and the retire deletes a true finding", state.Attributes)
	}

	cloudPayload := []byte(`{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:demo",
		"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:demo",
		"resource_type": "aws_lambda_function",
		"attributes": {"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:v9", "version": "7"}
	}`)
	cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:lambda", cloudPayload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	config := &cloudruntime.ResourceRow{Address: state.Address, ResourceType: state.ResourceType}

	comparison := cloudruntime.ClassifyValueComparison(cloud, state)
	if comparison.Compared != 0 {
		t.Fatalf("ClassifyValueComparison() Compared = %d, want 0 (a partial comparison is what converges)", comparison.Compared)
	}
	if kind := cloudruntime.Classify(cloud, state, config); kind != cloudruntime.FindingKindValueComparisonInconclusive {
		t.Fatalf("Classify() = %q, want %q -- convergence here would let the retire delete a still-true finding",
			kind, cloudruntime.FindingKindValueComparisonInconclusive)
	}
}

// TestClassifyLambdaUnredactedPartialEvidenceStillCompares is the
// false-positive guard for the all-or-nothing rule above: image_uri is
// legitimately absent on every zip-packaged Lambda, and that must stay a
// verdict rather than becoming inconclusive on most functions in a corpus
// (the #5861 objection). Only a REDACTED comparable suppresses the set; a
// genuinely missing one does not.
func TestClassifyLambdaUnredactedPartialEvidenceStillCompares(t *testing.T) {
	t.Parallel()

	statePayload := []byte(`{
		"address": "module.fn.aws_lambda_function.zip",
		"type": "aws_lambda_function",
		"attributes": {
			"arn": "arn:aws:lambda:us-east-1:123456789012:function:zip",
			"version": "7"
		}
	}`)
	state, ok := awsRuntimeStateRowFromPayload("state_snapshot:s3:hash", "module.fn.aws_lambda_function.zip", statePayload)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload() ok = false, want true")
	}
	want := map[string]string{"version": "7"}
	if !reflect.DeepEqual(state.Attributes, want) {
		t.Fatalf("row.Attributes = %#v, want %#v (a genuinely absent image_uri must not suppress version)",
			state.Attributes, want)
	}

	cloudPayload := []byte(`{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:zip",
		"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:zip",
		"resource_type": "aws_lambda_function",
		"attributes": {"version": "9"}
	}`)
	cloud, ok := awsRuntimeResourceRowFromPayload("aws:123456789012:us-east-1:lambda", cloudPayload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload() ok = false, want true")
	}
	config := &cloudruntime.ResourceRow{Address: state.Address, ResourceType: state.ResourceType}

	if kind := cloudruntime.Classify(cloud, state, config); kind != cloudruntime.FindingKindImageVersionDrift {
		t.Fatalf("Classify() = %q, want %q (a real version drift on a zip Lambda is still a verdict)",
			kind, cloudruntime.FindingKindImageVersionDrift)
	}
}

// BenchmarkStateDeclaredValueAttributes measures the decode path this branch
// changed, on the shape it runs on in production: a real, non-redacted scalar.
// comparableScalarAttr adds two failed type assertions per allowlisted key
// (map[string]any, then []any) ahead of the coerceJSONString call the old code
// made directly, so the cost is the assertions and nothing else -- no
// allocation, no parsing, no I/O. Recorded so the No-Regression Evidence in
// go/internal/storage/postgres/README.md names a measurement rather than an
// assumption.
func BenchmarkStateDeclaredValueAttributes(b *testing.B) {
	attributes := map[string]any{
		"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		"ami":           "ami-0123456789abcdef0",
		"instance_type": "t3.micro",
	}
	b.ReportAllocs()
	for b.Loop() {
		attrs, _, _, _ := stateDeclaredValueAttributes("aws_instance", attributes)
		if len(attrs) != 1 {
			b.Fatalf("stateDeclaredValueAttributes() = %#v, want one comparable attribute", attrs)
		}
	}
}
