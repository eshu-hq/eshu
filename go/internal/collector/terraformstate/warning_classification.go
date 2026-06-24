// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import "github.com/eshu-hq/eshu/go/internal/tfstatewarning"

const (
	// WarningSeverityInfo marks accepted or expected guardrail warnings.
	WarningSeverityInfo = tfstatewarning.SeverityInfo
	// WarningSeverityWarning marks non-blocking evidence that may need follow-up.
	WarningSeverityWarning = tfstatewarning.SeverityWarning
	// WarningSeverityBlocking marks missing evidence that blocks tfstate truth.
	WarningSeverityBlocking = tfstatewarning.SeverityBlocking

	// WarningActionabilityAcceptedGuardrail marks intentional safe drops.
	WarningActionabilityAcceptedGuardrail = tfstatewarning.ActionabilityAcceptedGuardrail
	// WarningActionabilityProviderSchemaSupport marks missing provider schema work.
	WarningActionabilityProviderSchemaSupport = tfstatewarning.ActionabilityProviderSchemaSupport
	// WarningActionabilityBlockingEvidence marks missing source evidence.
	WarningActionabilityBlockingEvidence = tfstatewarning.ActionabilityBlockingEvidence
	// WarningActionabilityAcceptedNormalization marks harmless source normalization.
	WarningActionabilityAcceptedNormalization = tfstatewarning.ActionabilityAcceptedNormalization
	// WarningActionabilitySourceNormalizationReview marks non-blocking source-shape review.
	WarningActionabilitySourceNormalizationReview = tfstatewarning.ActionabilitySourceNormalizationReview
)

// WarningClassification describes the operator-facing severity and next action
// for one stable Terraform-state warning_kind/reason pair.
type WarningClassification = tfstatewarning.Classification

// ClassifyWarning maps stable Terraform-state warning_kind/reason pairs to the
// operator-facing severity/actionability contract. It returns ok=false for
// unsupported pairs so new warning rows do not silently inherit the wrong
// operational meaning.
func ClassifyWarning(warningKind string, reason string) (WarningClassification, bool) {
	return tfstatewarning.Classify(warningKind, reason)
}
