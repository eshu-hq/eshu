// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestClassifyWarningStablePairs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		warningKind   string
		reason        string
		severity      string
		actionability string
	}{
		{
			name:          "unsupported composite needs provider schema support",
			warningKind:   "unsupported_composite_attribute",
			reason:        terraformstate.CompositeCaptureSkipReasonSchemaUnknown,
			severity:      "warning",
			actionability: "provider_schema_support",
		},
		{
			name:          "sensitive composite skip is an accepted guardrail",
			warningKind:   "composite_attribute_skipped",
			reason:        terraformstate.CompositeCaptureSkipReasonSensitiveSource,
			severity:      "info",
			actionability: "accepted_guardrail",
		},
		{
			name:          "missing state blocks evidence",
			warningKind:   "state_missing",
			reason:        "source_missing",
			severity:      "blocking",
			actionability: "blocking_evidence",
		},
		{
			name:          "unresolved backend expression blocks exact state discovery",
			warningKind:   "unresolved_backend_expression",
			reason:        "missing_variable_default",
			severity:      "blocking",
			actionability: "blocking_evidence",
		},
		{
			name:          "null tag maps are accepted normalization",
			warningKind:   "tag_map_dropped",
			reason:        "null_tag_map",
			severity:      "info",
			actionability: "accepted_normalization",
		},
		{
			name:          "malformed tag maps remain non-blocking review evidence",
			warningKind:   "tag_map_dropped",
			reason:        "malformed_tag_map",
			severity:      "warning",
			actionability: "source_normalization_review",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			classification, ok := terraformstate.ClassifyWarning(tc.warningKind, tc.reason)
			if !ok {
				t.Fatalf("ClassifyWarning(%q, %q) ok = false, want true", tc.warningKind, tc.reason)
			}
			if classification.Severity != tc.severity || classification.Actionability != tc.actionability {
				t.Fatalf(
					"ClassifyWarning(%q, %q) = %+v, want severity=%q actionability=%q",
					tc.warningKind,
					tc.reason,
					classification,
					tc.severity,
					tc.actionability,
				)
			}
		})
	}
}

func TestClassifyWarningRejectsUnsupportedPair(t *testing.T) {
	t.Parallel()

	if classification, ok := terraformstate.ClassifyWarning("unsupported_composite_attribute", "known_sensitive_key"); ok {
		t.Fatalf("ClassifyWarning() = %+v, true; want unsupported pair rejected", classification)
	}
}

func assertWarningClassification(
	t *testing.T,
	warning facts.Envelope,
	severity string,
	actionability string,
) {
	t.Helper()

	if got := warning.Payload["severity"]; got != severity {
		t.Fatalf("severity = %#v, want %#v in warning payload %#v", got, severity, warning.Payload)
	}
	if got := warning.Payload["actionability"]; got != actionability {
		t.Fatalf("actionability = %#v, want %#v in warning payload %#v", got, actionability, warning.Payload)
	}
}
