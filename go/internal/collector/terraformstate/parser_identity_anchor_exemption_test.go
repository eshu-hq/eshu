// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// The #5870 option-2 proofs: an identity join key survives a SchemaUnknown
// classification, and the operator can see which provider caused it.
//
// #6017 closed the deployment-wide case by making an empty schema bundle a
// startup error. It cannot reach the case here, which is narrower and more
// common: the bundle LOADS, but does not cover one provider. Every attribute of
// that provider's resources then classifies SchemaUnknown per attribute and
// fail-closes into a redaction marker -- including `arn`, which is not a value
// but the key the drift join runs on.
//
// The consequence is a wrong answer, not a missing one. The AWS drift loader
// inner-joins state rows on `payload->'attributes'->>'arn'`, and a marker object
// serializes to JSON text that equals no ARN, so the row is dropped at the
// database. cloudruntime.Classify then sees state == nil and reports
// orphaned_cloud_resource: "nothing manages this" for a resource Terraform
// demonstrably manages.

// uncoveredProviderState is a state file whose resource type the schema bundle
// in these tests does not cover. `covered_marker` is the control: an ordinary
// scalar on the same resource, which MUST stay redacted so the exemption is
// visibly narrow rather than a blanket opt-out.
const uncoveredProviderState = `{
	"serial":17,
	"lineage":"lineage-123",
	"resources":[{
		"mode":"managed",
		"type":"acme_widget",
		"name":"main",
		"instances":[{"attributes":{
			"arn":"arn:aws:acme:us-east-1:123456789012:widget/main",
			"id":"widget-main",
			"self_link":"https://acme.example.com/projects/p/widgets/main",
			"covered_marker":"ordinary-scalar-value"
		}}]
	}]
}`

// parseUncoveredProvider parses uncoveredProviderState against a resolver that
// deliberately covers a DIFFERENT resource type, so acme_widget is uncovered
// exactly the way a provider missing from the bundle is.
func parseUncoveredProvider(t *testing.T, sensitiveKeys []string) terraformstate.ParseResult {
	t.Helper()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver([2]string{"aws_s3_bucket", "acl"})
	if len(sensitiveKeys) > 0 {
		rules, err := redact.NewRuleSet("test-schema", sensitiveKeys)
		if err != nil {
			t.Fatalf("NewRuleSet() error = %v, want nil", err)
		}
		options.RedactionRules = rules
	}

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(uncoveredProviderState), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return result
}

// uncoveredResourceAttributes returns the single emitted resource fact's
// attributes.
func uncoveredResourceAttributes(t *testing.T, result terraformstate.ParseResult) map[string]any {
	t.Helper()

	resources := factsByKind(result.Facts, facts.TerraformStateResourceFactKind)
	if len(resources) != 1 {
		t.Fatalf("resource facts = %d, want 1", len(resources))
	}
	attributes, ok := resources[0].Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resources[0].Payload["attributes"])
	}
	return attributes
}

// isRedactionMarker reports whether a decoded attribute is the collector's
// {"marker","reason","source"} object rather than a real value.
func isRedactionMarker(value any) bool {
	marker, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, ok = marker["marker"]
	return ok
}

// TestIdentityJoinKeysSurviveUnknownProviderSchema is the accuracy proof.
//
// The three exempted keys are the ones a downstream reader JOINS on: the AWS
// drift loader on `arn`, and the multi-cloud loader on
// COALESCE(arn, id, self_link). Redacting a join key does not protect a value,
// it corrupts graph truth -- the row silently leaves the join and its resource
// is reported as an orphan.
func TestIdentityJoinKeysSurviveUnknownProviderSchema(t *testing.T) {
	t.Parallel()

	attributes := uncoveredResourceAttributes(t, parseUncoveredProvider(t, nil))

	for key, want := range map[string]string{
		"arn":       "arn:aws:acme:us-east-1:123456789012:widget/main",
		"id":        "widget-main",
		"self_link": "https://acme.example.com/projects/p/widgets/main",
	} {
		if got := attributes[key]; got != want {
			t.Fatalf(
				"attributes[%q] = %#v, want %q: an uncovered provider must not lose its join key, or every one of "+
					"its resources reports orphaned_cloud_resource (#5870)",
				key, got, want,
			)
		}
	}
}

// TestOrdinaryScalarStillRedactedUnderUnknownProviderSchema is the other half,
// and the one that keeps this a carve-out rather than a hole. Everything that
// is not a join key still fails closed on an unknown schema.
func TestOrdinaryScalarStillRedactedUnderUnknownProviderSchema(t *testing.T) {
	t.Parallel()

	attributes := uncoveredResourceAttributes(t, parseUncoveredProvider(t, nil))

	if !isRedactionMarker(attributes["covered_marker"]) {
		t.Fatalf(
			"attributes[covered_marker] = %#v, want a redaction marker: the exemption covers join keys only",
			attributes["covered_marker"],
		)
	}
}

// TestOperatorSensitiveKeyStillBeatsTheIdentityExemption proves the exemption
// cannot override an operator's explicit instruction.
//
// It holds because redact.RuleSet.Classify tests isSensitiveSource BEFORE it
// consults schema trust, so an operator naming `arn` in
// ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS still gets a marker. Pinned rather than
// left to that ordering, because a future reshuffle of Classify would silently
// invert it.
func TestOperatorSensitiveKeyStillBeatsTheIdentityExemption(t *testing.T) {
	t.Parallel()

	attributes := uncoveredResourceAttributes(t, parseUncoveredProvider(t, []string{"password", "arn"}))

	if !isRedactionMarker(attributes["arn"]) {
		t.Fatalf(
			"attributes[arn] = %#v, want a redaction marker: an operator-declared sensitive key outranks the "+
				"identity exemption",
			attributes["arn"],
		)
	}
	// The other two are untouched by that operator rule and still exempt, so
	// this also proves the sensitive-key match is per key rather than per row.
	if got, want := attributes["id"], "widget-main"; got != want {
		t.Fatalf("attributes[id] = %#v, want %q", got, want)
	}
}

// TestCorrelationAnchorsStayWithheldUnderUnknownProviderSchema pins the
// guarantee this change deliberately does NOT relax.
//
// correlationAnchors emits hashed anchor values, and it decides through
// redactsAnchor, which calls the BARE schemaTrust. The exemption lives on the
// attribute-classification path only, so an uncovered provider still publishes
// no anchors -- the join is rescued through the attribute the loader actually
// reads, not by widening the anchor contract.
func TestCorrelationAnchorsStayWithheldUnderUnknownProviderSchema(t *testing.T) {
	t.Parallel()

	resources := factsByKind(parseUncoveredProvider(t, nil).Facts, facts.TerraformStateResourceFactKind)
	if len(resources) != 1 {
		t.Fatalf("resource facts = %d, want 1", len(resources))
	}
	anchors, ok := resources[0].Payload["correlation_anchors"].([]any)
	if ok && len(anchors) != 0 {
		t.Fatalf(
			"correlation_anchors = %#v, want none: the anchor contract is decided by the bare schemaTrust and is "+
				"not part of the #5870 exemption",
			anchors,
		)
	}
}

// TestUncoveredProviderIsReportedAsAWarning is the observability half of the
// #5870 acceptance criteria.
//
// The exemption keeps the join alive, which is worth doing on its own — but on
// its own it would also make a stale schema bundle INVISIBLE, because the
// symptom that used to reveal it (a wave of orphans) is exactly what the
// exemption removes. The operator has to be told which provider the bundle does
// not cover, or the fix becomes a permanent crutch nobody notices.
func TestUncoveredProviderIsReportedAsAWarning(t *testing.T) {
	t.Parallel()

	warnings := factsByKind(parseUncoveredProvider(t, nil).Facts, facts.TerraformStateWarningFactKind)

	var found map[string]any
	for _, warning := range warnings {
		if warning.Payload["warning_kind"] == "provider_schema_not_covered" {
			found = warning.Payload
			break
		}
	}
	if found == nil {
		t.Fatalf("no provider_schema_not_covered warning among %d warning facts (#5870)", len(warnings))
	}
	if got, want := found["resource_type"], "acme_widget"; got != want {
		t.Fatalf("warning resource_type = %#v, want %q", got, want)
	}
	if got, want := found["provider"], "acme"; got != want {
		t.Fatalf("warning provider = %#v, want %q", got, want)
	}
	if got, want := found["severity"], "warning"; got != want {
		t.Fatalf("warning severity = %#v, want %q: an uncovered provider needs schema work", got, want)
	}
	if got, want := found["actionability"], "provider_schema_support"; got != want {
		t.Fatalf("warning actionability = %#v, want %q", got, want)
	}
}

// TestCoveredProviderEmitsNoUncoveredWarning keeps the detector from becoming
// noise. A resource type the bundle knows must produce no such warning at all,
// or every healthy parse ships one and operators learn to ignore the signal.
func TestCoveredProviderEmitsNoUncoveredWarning(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver([2]string{"aws_s3_bucket", "acl"})
	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed","type":"aws_s3_bucket","name":"logs",
			"instances":[{"attributes":{"acl":"private"}}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	for _, warning := range factsByKind(result.Facts, facts.TerraformStateWarningFactKind) {
		if warning.Payload["warning_kind"] == "provider_schema_not_covered" {
			t.Fatalf("unexpected provider_schema_not_covered warning for a covered resource type: %#v", warning.Payload)
		}
	}
}

// TestNearMissJoinKeyIsNotExempt closes the codex review finding.
//
// The exempt set is matched verbatim. A trimmed lookup would upgrade an
// attribute literally named " id" or "arn " and persist its raw value, while
// the downstream SQL joins the exact JSON keys `id`, `arn`, and `self_link` --
// so the near-match cannot repair any join and only exposes an unknown-schema
// value that should have stayed redacted. Strictly a leak with no upside.
func TestNearMissJoinKeyIsNotExempt(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver([2]string{"aws_s3_bucket", "acl"})
	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed","type":"acme_widget","name":"main",
			"instances":[{"attributes":{" id":"leading-space","arn ":"trailing-space"}}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	attributes := uncoveredResourceAttributes(t, result)
	for _, key := range []string{" id", "arn "} {
		if !isRedactionMarker(attributes[key]) {
			t.Fatalf("attributes[%q] = %#v, want a redaction marker: only the verbatim join keys are exempt", key, attributes[key])
		}
	}
}

// TestUncoveredProviderReportedForTagsOnlyInstance closes the second codex
// finding.
//
// The detector used to record from inside attribute classification, which
// classifyAttributes never reaches for an instance carrying only tag maps (it
// skips them) or no attributes at all. Such a resource type was silently
// uncovered. Recording at the instance boundary fixes it, and this is the shape
// that proves it.
func TestUncoveredProviderReportedForTagsOnlyInstance(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver([2]string{"aws_s3_bucket", "acl"})
	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed","type":"acme_widget","name":"main",
			"instances":[{"attributes":{"tags":{"Name":"only-a-tag-map"}}}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	for _, warning := range factsByKind(result.Facts, facts.TerraformStateWarningFactKind) {
		if warning.Payload["warning_kind"] == "provider_schema_not_covered" {
			return
		}
	}
	t.Fatal("no provider_schema_not_covered warning for a tags-only instance: the schema gap must not go silent (#5870)")
}

// TestUncoveredTypeOfCoveredProviderUsesItsOwnReason closes the third codex
// finding.
//
// A resource type newer than the bundle is not a missing provider. Both answer
// HasResourceType false, but the operator actions differ — refresh the bundle
// versus add the provider — so they carry different reasons.
func TestUncoveredTypeOfCoveredProviderUsesItsOwnReason(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		resourceType string
		wantReason   string
	}{
		{
			name:         "provider absent from the bundle",
			resourceType: "acme_widget",
			wantReason:   "provider_not_in_schema_bundle",
		},
		{
			// aws IS in the bundle (aws_s3_bucket), this type simply is not.
			name:         "provider present, type newer than the bundle",
			resourceType: "aws_brand_new_service",
			wantReason:   "resource_type_not_in_schema_bundle",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			options := parseFixtureOptions(t)
			options.SchemaResolver = newStubResolver([2]string{"aws_s3_bucket", "acl"})
			state := `{
				"serial":17,
				"lineage":"lineage-123",
				"resources":[{
					"mode":"managed","type":"` + testCase.resourceType + `","name":"main",
					"instances":[{"attributes":{"other":"value"}}]
				}]
			}`

			result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			for _, warning := range factsByKind(result.Facts, facts.TerraformStateWarningFactKind) {
				if warning.Payload["warning_kind"] != "provider_schema_not_covered" {
					continue
				}
				if got := warning.Payload["reason"]; got != testCase.wantReason {
					t.Fatalf("reason = %#v, want %q", got, testCase.wantReason)
				}
				return
			}
			t.Fatalf("no provider_schema_not_covered warning for %q", testCase.resourceType)
		})
	}
}
