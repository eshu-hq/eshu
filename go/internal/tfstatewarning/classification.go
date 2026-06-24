// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstatewarning

import "strings"

const (
	// SeverityInfo marks accepted or expected guardrail warnings.
	SeverityInfo = "info"
	// SeverityWarning marks non-blocking evidence that may need follow-up.
	SeverityWarning = "warning"
	// SeverityBlocking marks missing evidence that blocks Terraform-state truth.
	SeverityBlocking = "blocking"

	// ActionabilityAcceptedGuardrail marks intentional safe drops.
	ActionabilityAcceptedGuardrail = "accepted_guardrail"
	// ActionabilityProviderSchemaSupport marks missing provider schema work.
	ActionabilityProviderSchemaSupport = "provider_schema_support"
	// ActionabilityBlockingEvidence marks missing source evidence.
	ActionabilityBlockingEvidence = "blocking_evidence"
	// ActionabilityAcceptedNormalization marks harmless source normalization.
	ActionabilityAcceptedNormalization = "accepted_normalization"
	// ActionabilitySourceNormalizationReview marks non-blocking source-shape review.
	ActionabilitySourceNormalizationReview = "source_normalization_review"
)

const (
	reasonApprovedLocalState = "approved_local"
	reasonSourceMissing      = "source_missing"
	reasonSizeLimit          = "size_limit"
)

// Classification describes the operator-facing severity and next action for
// one stable Terraform-state warning_kind/reason pair.
type Classification struct {
	Severity      string
	Actionability string
}

// Classify maps stable Terraform-state warning_kind/reason pairs to the
// operator-facing severity/actionability contract. It returns ok=false for
// unsupported pairs so new warning rows do not silently inherit the wrong
// operational meaning.
func Classify(warningKind string, reason string) (Classification, bool) {
	warningKind = strings.TrimSpace(warningKind)
	reason = strings.TrimSpace(reason)
	switch warningKind {
	case "unsupported_composite_attribute":
		if reason == "schema_unknown" {
			return Classification{
				Severity:      SeverityWarning,
				Actionability: ActionabilityProviderSchemaSupport,
			}, true
		}
	case "composite_attribute_skipped":
		switch reason {
		case "known_sensitive_key", "unknown_rule_set", "unknown_field_kind":
			return acceptedGuardrailClassification(), true
		case "shape_mismatch":
			return Classification{
				Severity:      SeverityWarning,
				Actionability: ActionabilitySourceNormalizationReview,
			}, true
		}
	case "state_missing":
		switch reason {
		case reasonSourceMissing, "s3_not_found", "path_not_found":
			return blockingEvidenceClassification(), true
		}
	case "state_too_large":
		switch reason {
		case reasonSizeLimit, "terraform state exceeded configured size ceiling before snapshot identity could be read":
			return blockingEvidenceClassification(), true
		}
	case "unresolved_backend_expression":
		switch reason {
		case "missing_variable_default",
			"ambiguous_variable_default",
			"missing_local_value",
			"ambiguous_local_value",
			"cyclic_local_value",
			"unsupported_reference",
			"unresolved_interpolation",
			"workspace_interpolation",
			"function_call",
			"workspace_key_prefix",
			"non_exact_value":
			return blockingEvidenceClassification(), true
		}
	case "state_in_vcs":
		switch reason {
		case reasonApprovedLocalState, "terraform state file was discovered in git and explicitly approved for ingestion":
			return acceptedGuardrailClassification(), true
		}
	case "output_value_dropped":
		switch reason {
		case "sensitive_composite_output", "known_sensitive_key":
			return acceptedGuardrailClassification(), true
		}
	case "tag_map_dropped":
		switch reason {
		case "null_tag_map":
			return Classification{
				Severity:      SeverityInfo,
				Actionability: ActionabilityAcceptedNormalization,
			}, true
		case "malformed_tag_map":
			return Classification{
				Severity:      SeverityWarning,
				Actionability: ActionabilitySourceNormalizationReview,
			}, true
		case "unsupported_tag_map_shape":
			return acceptedGuardrailClassification(), true
		}
	case "tag_value_dropped":
		if reason == "non_scalar_tag_value" {
			return acceptedGuardrailClassification(), true
		}
	}
	return Classification{}, false
}

func acceptedGuardrailClassification() Classification {
	return Classification{
		Severity:      SeverityInfo,
		Actionability: ActionabilityAcceptedGuardrail,
	}
}

func blockingEvidenceClassification() Classification {
	return Classification{
		Severity:      SeverityBlocking,
		Actionability: ActionabilityBlockingEvidence,
	}
}
