// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildObservabilityCoverageDecisionsMalformedDimensionValueQuarantines
// proves the #4631 typed-attribute-decode fix: a cloudwatch_alarm_observes_metric
// fact whose dimension "value" is present but not a JSON string must
// dead-letter as a visible input_invalid quarantine rather than silently
// resolving to a metric-name-only rejection that looks like ordinary
// uncovered evidence.
func TestBuildObservabilityCoverageDecisionsMalformedDimensionValueQuarantines(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:bad-dimension"
	envelopes := []facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "bad-dimension", false),
		{
			FactID:   "alarm-rel-bad",
			FactKind: facts.AWSRelationshipFactKind,
			Payload: map[string]any{
				"account_id":         testCoverageAccount,
				"region":             testCoverageRegion,
				"relationship_type":  "cloudwatch_alarm_observes_metric",
				"source_resource_id": alarmARN,
				"source_arn":         alarmARN,
				"target_resource_id": "AWS/EC2/CPUUtilization",
				"target_type":        "aws_cloudwatch_metric",
				"attributes": map[string]any{
					"dimensions": []any{
						map[string]any{"name": "InstanceId", "value": 12345},
					},
				},
			},
		},
	}
	decisions, quarantined, err := BuildObservabilityCoverageDecisions(envelopes)
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 for a malformed dimension value", len(quarantined))
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want input_invalid", quarantined[0].classification)
	}
	// The alarm resource fact itself is unaffected by the relationship's decode
	// failure (it is a separate fact) and still classifies through the ordinary
	// no-evidence path, exactly like TestBuildObservabilityCoverageDecisionsRejectsMetricNameOnlyAlarm
	// — the fix is that the malformed dimension value is now a counted,
	// logged dead-letter instead of silently vanishing into that same "no
	// evidence" bucket with no trace it was ever malformed.
	var rejected *ObservabilityCoverageCorrelationDecision
	for i := range decisions {
		if decisions[i].ObservabilityObjectRef == alarmARN {
			rejected = &decisions[i]
		}
	}
	if rejected == nil {
		t.Fatalf("expected a rejected decision for the alarm resource fact; decisions=%+v", decisions)
	}
	assertCoverageOutcome(t, *rejected, ObservabilityCoverageRejected, "rejected")
}

// TestBuildObservabilityCoverageDecisionsMalformedXRayServiceNameQuarantines
// proves the #4631 typed-attribute-decode fix: an
// xray_sampling_rule_matches_service fact whose nested "service_name" is
// present but not a JSON string must dead-letter as a visible input_invalid
// quarantine, not silently coerce it via fmt.Sprint into a wrong service name
// the derived coverage decision would key on.
func TestBuildObservabilityCoverageDecisionsMalformedXRayServiceNameQuarantines(t *testing.T) {
	t.Parallel()

	const ruleARN = "arn:aws:xray:us-east-1:111122223333:sampling-rule/bad-service"
	envelopes := []facts.Envelope{
		awsResourceFact("rule-res", "aws_xray_sampling_rule", ruleARN, ruleARN, "bad-service", false),
		{
			FactID:   "rule-rel-bad",
			FactKind: facts.AWSRelationshipFactKind,
			Payload: map[string]any{
				"account_id":         testCoverageAccount,
				"region":             testCoverageRegion,
				"relationship_type":  "xray_sampling_rule_matches_service",
				"source_resource_id": ruleARN,
				"source_arn":         ruleARN,
				"target_resource_id": "xray-service:bad",
				"target_type":        "aws_xray_service_correlation",
				"attributes": map[string]any{
					"service_name": []any{"not", "a", "string"},
				},
			},
		},
	}
	_, quarantined, err := BuildObservabilityCoverageDecisions(envelopes)
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 for a malformed xray service_name", len(quarantined))
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want input_invalid", quarantined[0].classification)
	}
}
