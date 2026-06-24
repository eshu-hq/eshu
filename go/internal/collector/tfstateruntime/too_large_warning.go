// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstateruntime

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func (s ClaimedSource) stateTooLargeWarningGeneration(
	candidate terraformstate.DiscoveryCandidate,
	scopeValue scope.IngestionScope,
	candidateID string,
	sourceKey terraformstate.StateKey,
	fencingToken int64,
) (collector.CollectedGeneration, error) {
	return s.sourceWarningGeneration(
		candidate,
		scopeValue,
		candidateID,
		sourceKey,
		fencingToken,
		"state_too_large",
		"size_limit",
	)
}

func (s ClaimedSource) stateMissingWarningGeneration(
	candidate terraformstate.DiscoveryCandidate,
	scopeValue scope.IngestionScope,
	candidateID string,
	sourceKey terraformstate.StateKey,
	fencingToken int64,
) (collector.CollectedGeneration, error) {
	return s.sourceWarningGeneration(
		candidate,
		scopeValue,
		candidateID,
		sourceKey,
		fencingToken,
		"state_missing",
		"source_missing",
	)
}

func (s ClaimedSource) sourceWarningGeneration(
	candidate terraformstate.DiscoveryCandidate,
	scopeValue scope.IngestionScope,
	candidateID string,
	sourceKey terraformstate.StateKey,
	fencingToken int64,
	warningKind string,
	reason string,
) (collector.CollectedGeneration, error) {
	observedAt := s.now()
	generationValue := scope.ScopeGeneration{
		GenerationID: fmt.Sprintf(
			"terraform_state:%s:warning:%s:%s",
			scopeValue.ScopeID,
			warningKind,
			terraformstate.LocatorHash(sourceKey),
		),
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: fmt.Sprintf("warning=%s candidate=%s", warningKind, candidateID),
	}
	if err := generationValue.ValidateForScope(scopeValue); err != nil {
		return collector.CollectedGeneration{}, err
	}
	warning, err := terraformstate.NewWarningFact(terraformstate.WarningFactOptions{
		Scope:        scopeValue,
		Generation:   generationValue,
		Source:       sourceKey,
		ObservedAt:   observedAt,
		FencingToken: fencingToken,
		Warning: terraformstate.SourceWarning{
			WarningKind: warningKind,
			Reason:      reason,
			Source:      string(candidate.Source),
			Details:     s.sourceWarningDetails(candidate, scopeValue, sourceKey),
		},
	})
	if err != nil {
		return collector.CollectedGeneration{}, err
	}
	return collector.FactsFromSlice(scopeValue, generationValue, []facts.Envelope{warning}), nil
}

func (s ClaimedSource) sourceWarningDetails(
	candidate terraformstate.DiscoveryCandidate,
	scopeValue scope.IngestionScope,
	sourceKey terraformstate.StateKey,
) map[string]any {
	details := map[string]any{
		"backend_kind":       string(sourceKey.BackendKind),
		"safe_locator_hash":  scopeValue.Metadata["locator_hash"],
		"source_handle":      scopeValue.ScopeID,
		"candidate_identity": terraformstate.LocatorHash(sourceKey),
		"source_locator": redactionValueMap(redact.String(
			sourceKey.Locator,
			"terraform_state_locator",
			"terraform_state."+string(sourceKey.BackendKind)+".locator",
			s.RedactionKey,
		)),
	}
	if candidate.TargetScopeID != "" {
		details["target_scope_id"] = candidate.TargetScopeID
	}
	return details
}

func redactionValueMap(value redact.Value) map[string]any {
	return map[string]any{
		"marker": value.Marker,
		"reason": value.Reason,
		"source": value.Source,
	}
}
