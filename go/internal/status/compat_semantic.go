// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the semantic
// family that moved to [semantic] (issue #6775). It carries no behavior
// change: every alias names the same type and every constant the same value,
// so the packages importing internal/status keep compiling unchanged. Each
// entry is deleted once its last caller has moved to the leaf; see the
// importer-migration child issue. A later semantic move adds a stanza here and
// never creates a second compat file for this family.
//
// WithSemanticProviderProfiles is deliberately NOT here: it decorates the root
// Reader interface, so it stays real code in provider_profile_reader.go rather
// than a forwarder.

import "github.com/eshu-hq/eshu/go/internal/status/semantic"

// Semantic extraction and provider profile sections.
//
// Deprecated: use the [semantic] names.
type (
	SemanticExtractionStatus                    = semantic.ExtractionStatus
	SemanticExtractionQueueSnapshot             = semantic.ExtractionQueueSnapshot
	SemanticExtractionBudgetSnapshot            = semantic.ExtractionBudgetSnapshot
	SemanticExtractionAuditSnapshot             = semantic.ExtractionAuditSnapshot
	SemanticExtractionDecisionCount             = semantic.ExtractionDecisionCount
	SemanticExtractionBudgetDecisionCount       = semantic.ExtractionBudgetDecisionCount
	SemanticExtractionProviderProfileQueueCount = semantic.ExtractionProviderProfileQueueCount
	SemanticProviderProfileStatus               = semantic.ProviderProfileStatus
)

// Semantic extraction states and reasons.
//
// Deprecated: use the [semantic] constants.
const (
	SemanticExtractionUnavailable                  = semantic.ExtractionUnavailable
	SemanticExtractionAvailable                    = semantic.ExtractionAvailable
	SemanticExtractionAvailableButDisabledForScope = semantic.ExtractionAvailableButDisabledForScope
	SemanticExtractionDisabledByPolicy             = semantic.ExtractionDisabledByPolicy
	SemanticExtractionProviderUnhealthy            = semantic.ExtractionProviderUnhealthy

	SemanticExtractionReasonProviderNotConfigured = semantic.ExtractionReasonProviderNotConfigured
	SemanticExtractionReasonScopeDisabled         = semantic.ExtractionReasonScopeDisabled
	SemanticExtractionReasonPolicyDisabled        = semantic.ExtractionReasonPolicyDisabled

	SemanticProviderProfileConfigured = semantic.ProviderProfileConfigured
	SemanticProviderProfileHealthy    = semantic.ProviderProfileHealthy
	SemanticProviderProfileUnhealthy  = semantic.ProviderProfileUnhealthy
)

// SemanticExtractionSupportedStates returns the stable extraction states.
//
// Deprecated: use [semantic.ExtractionSupportedStates].
func SemanticExtractionSupportedStates() []string { return semantic.ExtractionSupportedStates() }

// SemanticProviderProfileSupportedStates returns the stable profile states.
//
// Deprecated: use [semantic.ProviderProfileSupportedStates].
func SemanticProviderProfileSupportedStates() []string {
	return semantic.ProviderProfileSupportedStates()
}
