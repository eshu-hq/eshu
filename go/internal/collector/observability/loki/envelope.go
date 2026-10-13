// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
)

// NewSourceInstanceEnvelope converts one live Loki target snapshot into a
// source-instance fact.
func NewSourceInstanceEnvelope(ctx EnvelopeContext, source SourceInstance, stats CollectionStats) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	payload := basePayload(ctx, OutcomeObserved, FreshnessCurrent)
	payload["resource_count"] = stats.Signals
	payload["rule_count"] = stats.Rules
	payload["warning_count"] = stats.Warnings
	payload["pages_fetched"] = stats.PagesFetched
	payload["partial"] = stats.Partial
	payload["truncated"] = stats.Truncated
	payload["high_cardinality_rejected"] = stats.HighCardinalityRejected
	if source.TenantPresent || source.TenantRedacted {
		payload["tenant_scope_state"] = "configured"
		payload["tenant_redacted"] = true
		if source.TenantFingerprint != "" {
			payload["tenant_fingerprint"] = source.TenantFingerprint
		}
	}
	setRedactionState(payload)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeObservabilitySourceInstance(observabilityv1.SourceInstance{
			SourceInstanceID: ctx.SourceInstanceID,
			Provider:         stringPtr(SourceKindLoki),
			SourceKind:       stringPtr(SourceKindLoki),
			SourceClass:      stringPtr(SourceClassObserved),
			Outcome:          stringPtr(OutcomeObserved),
			FreshnessState:   stringPtr(FreshnessCurrent),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := stableFactKey(facts.ObservabilitySourceInstanceFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"source_kind":        SourceKindLoki,
	})
	return observabilityEnvelope(ctx, facts.ObservabilitySourceInstanceFactKind, stableKey, payload, ctx.SourceInstanceID), nil
}

// NewObservedLogSignalEnvelope converts one bounded live Loki signal into an
// observed-log-signal fact.
func NewObservedLogSignalEnvelope(ctx EnvelopeContext, signal LogSignal) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	recordID := strings.TrimSpace(signal.ProviderObjectID)
	if recordID == "" {
		return facts.Envelope{}, fmt.Errorf("loki log signal identity must not be blank")
	}
	payload := basePayload(ctx, normalizedOutcome(signal.Outcome), normalizedFreshness(signal.FreshnessState))
	payload["resource_class"] = ResourceClassLogSignal
	payload["provider_object_uid"] = recordID
	payload["signal_kind"] = normalizedSignalKind(signal.SignalKind)
	if value := strings.TrimSpace(signal.SeriesFingerprint); value != "" {
		payload["series_fingerprint"] = value
	}
	if value := strings.TrimSpace(signal.DeclaredMatchState); value != "" {
		payload["declared_match_state"] = value
	} else {
		payload["declared_match_state"] = MatchStateNotCompared
	}
	setStringSlice(payload, "label_keys", signal.LabelKeys)
	setIntMap(payload, "label_value_counts", signal.LabelValueCounts)
	setStringSliceMap(payload, "label_value_hashes", signal.LabelValueHashes)
	setTimePayload(payload, "last_seen_at", signal.LastSeenAt)
	if signal.ManuallyCreated {
		payload["manually_created"] = true
		payload["drift_candidate_reason"] = WarningManualProviderResource
	}
	setRedactionState(payload)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeObservabilityObservedLogSignal(observabilityv1.ObservedLogSignal{
			SourceInstanceID:     ctx.SourceInstanceID,
			ProviderObjectUID:    recordID,
			SeriesFingerprint:    optionalStringPtr(signal.SeriesFingerprint),
			Provider:             stringPtr(SourceKindLoki),
			SourceKind:           stringPtr(SourceKindLoki),
			SourceClass:          stringPtr(SourceClassObserved),
			ResourceClass:        stringPtr(ResourceClassLogSignal),
			Outcome:              stringPtr(normalizedOutcome(signal.Outcome)),
			FreshnessState:       stringPtr(normalizedFreshness(signal.FreshnessState)),
			DriftCandidateReason: optionalStringPtr(payloadString(payload, "drift_candidate_reason")),
			DeclaredMatchState:   stringPtr(firstNonBlank(signal.DeclaredMatchState, MatchStateNotCompared)),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := stableFactKey(facts.ObservabilityObservedLogSignalFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"signal_uid":         recordID,
	})
	return observabilityEnvelope(ctx, facts.ObservabilityObservedLogSignalFactKind, stableKey, payload, recordID), nil
}

// NewObservedRuleEnvelope converts one bounded live Loki ruler rule into an
// observed-rule fact.
func NewObservedRuleEnvelope(ctx EnvelopeContext, rule Rule) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	recordID := firstNonBlank(rule.ProviderObjectID, rule.Namespace+"/"+rule.GroupName+":"+rule.RuleName)
	if recordID == "" || recordID == "/:" {
		return facts.Envelope{}, fmt.Errorf("loki rule identity must not be blank")
	}
	payload := basePayload(ctx, normalizedOutcome(rule.Outcome), normalizedFreshness(rule.FreshnessState))
	payload["resource_class"] = ResourceClassRule
	payload["provider_object_uid"] = recordID
	if value := strings.TrimSpace(rule.Namespace); value != "" {
		payload["namespace"] = value
	}
	if value := strings.TrimSpace(rule.GroupName); value != "" {
		payload["rule_group"] = value
	}
	if value := strings.TrimSpace(rule.RuleName); value != "" {
		payload["rule_name"] = value
	}
	if value := strings.TrimSpace(rule.RuleType); value != "" {
		payload["rule_type"] = value
	}
	if value := strings.TrimSpace(rule.DeclaredMatchState); value != "" {
		payload["declared_match_state"] = value
	} else {
		payload["declared_match_state"] = MatchStateNotCompared
	}
	setStringSlice(payload, "label_keys", rule.LabelKeys)
	setStringSlice(payload, "annotation_keys", rule.AnnotationKeys)
	setTimePayload(payload, "last_evaluation_at", rule.LastEvaluationAt)
	if strings.TrimSpace(rule.Query) != "" || rule.QueryRedacted {
		payload["query_redacted"] = true
	}
	if rule.ManuallyCreated {
		payload["manually_created"] = true
		payload["drift_candidate_reason"] = WarningManualProviderResource
	}
	setRedactionState(payload)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeObservabilityObservedRule(observabilityv1.ObservedRule{
			SourceInstanceID:     ctx.SourceInstanceID,
			ProviderObjectUID:    stringPtr(recordID),
			RuleGroup:            optionalStringPtr(rule.GroupName),
			RuleName:             optionalStringPtr(rule.RuleName),
			Provider:             stringPtr(SourceKindLoki),
			SourceKind:           stringPtr(SourceKindLoki),
			SourceClass:          stringPtr(SourceClassObserved),
			ResourceClass:        stringPtr(ResourceClassRule),
			Outcome:              stringPtr(normalizedOutcome(rule.Outcome)),
			FreshnessState:       stringPtr(normalizedFreshness(rule.FreshnessState)),
			DriftCandidateReason: optionalStringPtr(payloadString(payload, "drift_candidate_reason")),
			DeclaredMatchState:   stringPtr(firstNonBlank(rule.DeclaredMatchState, MatchStateNotCompared)),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := stableFactKey(facts.ObservabilityObservedRuleFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"rule_uid":           recordID,
	})
	return observabilityEnvelope(ctx, facts.ObservabilityObservedRuleFactKind, stableKey, payload, recordID), nil
}

// NewCoverageWarningEnvelope converts a source-local warning into an
// observability coverage-warning fact.
func NewCoverageWarningEnvelope(ctx EnvelopeContext, warning Warning) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	reason := strings.TrimSpace(warning.Reason)
	if reason == "" {
		reason = WarningPartial
	}
	resourceClass := strings.TrimSpace(warning.ResourceClass)
	if resourceClass == "" {
		resourceClass = "unknown"
	}
	resourceID := strings.TrimSpace(warning.ResourceID)
	payload := basePayload(ctx, warningOutcome(reason), warningFreshness(reason))
	payload["warning_kind"] = reason
	payload["resource_class"] = resourceClass
	if resourceID != "" {
		payload["provider_object_uid"] = resourceID
	}
	setRedactionState(payload)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeObservabilityCoverageWarning(observabilityv1.CoverageWarning{
			SourceInstanceID:  ctx.SourceInstanceID,
			ProviderObjectUID: optionalStringPtr(resourceID),
			Provider:          stringPtr(SourceKindLoki),
			SourceKind:        stringPtr(SourceKindLoki),
			SourceClass:       stringPtr(SourceClassObserved),
			ResourceClass:     stringPtr(resourceClass),
			Outcome:           stringPtr(warningOutcome(reason)),
			FreshnessState:    stringPtr(warningFreshness(reason)),
			WarningKind:       stringPtr(reason),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := stableFactKey(facts.ObservabilityCoverageWarningFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"resource_class":     resourceClass,
		"resource_id":        resourceID,
		"reason":             reason,
	})
	return observabilityEnvelope(ctx, facts.ObservabilityCoverageWarningFactKind, stableKey, payload, firstNonBlank(resourceID, reason)), nil
}

func basePayload(ctx EnvelopeContext, outcome string, freshness string) map[string]any {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              SourceKindLoki,
		"source_class":          SourceClassObserved,
		"source_kind":           SourceKindLoki,
		"source_instance_id":    ctx.SourceInstanceID,
		"scope_id":              ctx.ScopeID,
		"generation_id":         ctx.GenerationID,
		"observed_at":           normalizedObservedAt(ctx.ObservedAt).Format(time.RFC3339Nano),
		"freshness_state":       freshness,
		"redaction_version":     RedactionVersion,
		"outcome":               outcome,
	}
	payload["provenance"] = map[string]any{
		"provider":           SourceKindLoki,
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
	}
	return payload
}

func mergeContractPayload(payload map[string]any, encode func() (map[string]any, error)) error {
	encoded, err := encode()
	if err != nil {
		return err
	}
	for key, value := range encoded {
		payload[key] = value
	}
	return nil
}

func stringPtr(value string) *string {
	return &value
}

func optionalStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func observabilityEnvelope(
	ctx EnvelopeContext,
	kind string,
	stableKey string,
	payload map[string]any,
	recordID string,
) facts.Envelope {
	version, _ := facts.ObservabilitySchemaVersion(kind)
	return facts.Envelope{
		FactID: facts.StableID("ObservabilityFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    stableKey,
			"scope_id":      ctx.ScopeID,
			"generation_id": ctx.GenerationID,
		}),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         kind,
		StableFactKey:    stableKey,
		SchemaVersion:    version,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableKey,
			SourceRecordID: strings.TrimSpace(recordID),
		},
	}
}

func validateEnvelopeContext(ctx EnvelopeContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("loki envelope scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("loki envelope generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("loki envelope collector_instance_id must not be blank")
	}
	if strings.TrimSpace(ctx.SourceInstanceID) == "" {
		return fmt.Errorf("loki envelope source_instance_id must not be blank")
	}
	return nil
}

func stableFactKey(kind string, generationID string, identity map[string]any) string {
	return strings.Join([]string{kind, generationID, facts.StableID(kind, identity)}, ":")
}

func normalizedObservedAt(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func setTimePayload(payload map[string]any, key string, value time.Time) {
	if !value.IsZero() {
		payload[key] = value.UTC().Format(time.RFC3339Nano)
	}
}

func warningOutcome(reason string) string {
	switch reason {
	case WarningPermissionHidden:
		return OutcomePermissionHidden
	case WarningUnsupported:
		return OutcomeUnsupported
	case WarningManualProviderResource:
		return OutcomeObserved
	case WarningStale:
		return OutcomeStale
	case WarningHighCardinality:
		return OutcomeRejected
	default:
		return OutcomePartial
	}
}

func warningFreshness(reason string) string {
	switch reason {
	case WarningPermissionHidden:
		return FreshnessPermissionHidden
	case WarningStale:
		return FreshnessStale
	default:
		return FreshnessUnknown
	}
}

func normalizedOutcome(value string) string {
	switch strings.TrimSpace(value) {
	case OutcomePartial:
		return OutcomePartial
	case OutcomePermissionHidden:
		return OutcomePermissionHidden
	case OutcomeUnsupported:
		return OutcomeUnsupported
	case OutcomeRejected:
		return OutcomeRejected
	case OutcomeStale:
		return OutcomeStale
	default:
		return OutcomeObserved
	}
}

func normalizedFreshness(value string) string {
	switch strings.TrimSpace(value) {
	case FreshnessUnknown:
		return FreshnessUnknown
	case FreshnessStale:
		return FreshnessStale
	case FreshnessPermissionHidden:
		return FreshnessPermissionHidden
	default:
		return FreshnessCurrent
	}
}

func normalizedSignalKind(value string) string {
	switch strings.TrimSpace(value) {
	case SignalKindSeries:
		return SignalKindSeries
	default:
		return SignalKindLabelSet
	}
}
