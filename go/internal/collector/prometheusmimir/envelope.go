// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
)

// NewSourceInstanceEnvelope converts one live metric target snapshot into a
// source-instance fact.
func NewSourceInstanceEnvelope(ctx EnvelopeContext, source SourceInstance, stats CollectionStats) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	payload := basePayload(ctx, OutcomeObserved, FreshnessCurrent)
	payload["resource_count"] = stats.Targets
	payload["rule_count"] = stats.Rules
	payload["warning_count"] = stats.Warnings
	payload["pages_fetched"] = stats.PagesFetched
	payload["partial"] = stats.Partial
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
			Provider:         stringPtr(sourceKind(ctx)),
			SourceKind:       stringPtr(sourceKind(ctx)),
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
		"source_kind":        sourceKind(ctx),
	})
	return observabilityEnvelope(ctx, facts.ObservabilitySourceInstanceFactKind, stableKey, payload, ctx.SourceInstanceID), nil
}

// NewObservedTargetEnvelope converts one bounded live target into an
// observed-target fact.
func NewObservedTargetEnvelope(ctx EnvelopeContext, target Target) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	recordID := strings.TrimSpace(target.ProviderObjectID)
	if recordID == "" {
		return facts.Envelope{}, fmt.Errorf("metric target identity must not be blank")
	}
	payload := basePayload(ctx, normalizedOutcome(target.Outcome), normalizedFreshness(target.FreshnessState))
	payload["resource_class"] = ResourceClassTarget
	payload["provider_object_uid"] = recordID
	if value := strings.TrimSpace(target.ScrapePool); value != "" {
		payload["scrape_pool"] = value
	}
	if value := strings.TrimSpace(target.Health); value != "" {
		payload["health"] = value
	}
	if value := strings.TrimSpace(target.DeclaredMatchState); value != "" {
		payload["declared_match_state"] = value
	} else {
		payload["declared_match_state"] = MatchStateNotCompared
	}
	setStringSlice(payload, "label_keys", target.LabelKeys)
	setStringSlice(payload, "discovered_label_keys", target.DiscoveredKeys)
	setURLRedaction(payload, "scrape_url", target.ScrapeURL, target.ScrapeURLRedacted)
	setTimePayload(payload, "last_scrape_at", target.LastScrapeAt)
	if target.LastErrorRedacted {
		payload["last_error_redacted"] = true
	}
	if target.ManuallyCreated {
		payload["manually_created"] = true
		payload["drift_candidate_reason"] = WarningManualProviderResource
	}
	setRedactionState(payload)
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeObservabilityObservedTarget(observabilityv1.ObservedTarget{
			SourceInstanceID:     ctx.SourceInstanceID,
			ProviderObjectUID:    recordID,
			Provider:             stringPtr(sourceKind(ctx)),
			SourceKind:           stringPtr(sourceKind(ctx)),
			SourceClass:          stringPtr(SourceClassObserved),
			ResourceClass:        stringPtr(ResourceClassTarget),
			Outcome:              stringPtr(normalizedOutcome(target.Outcome)),
			FreshnessState:       stringPtr(normalizedFreshness(target.FreshnessState)),
			DriftCandidateReason: optionalStringPtr(payloadString(payload, "drift_candidate_reason")),
			DeclaredMatchState:   stringPtr(firstNonBlank(target.DeclaredMatchState, MatchStateNotCompared)),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := stableFactKey(facts.ObservabilityObservedTargetFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"target_uid":         recordID,
	})
	return observabilityEnvelope(ctx, facts.ObservabilityObservedTargetFactKind, stableKey, payload, recordID), nil
}

// NewObservedRuleEnvelope converts one bounded live rule into an observed-rule
// fact.
func NewObservedRuleEnvelope(ctx EnvelopeContext, rule Rule) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	recordID := firstNonBlank(rule.ProviderObjectID, rule.GroupName+":"+rule.RuleName)
	if recordID == "" || recordID == ":" {
		return facts.Envelope{}, fmt.Errorf("metric rule identity must not be blank")
	}
	payload := basePayload(ctx, normalizedOutcome(rule.Outcome), normalizedFreshness(rule.FreshnessState))
	payload["resource_class"] = ResourceClassRule
	payload["provider_object_uid"] = recordID
	if value := strings.TrimSpace(rule.GroupName); value != "" {
		payload["rule_group"] = value
	}
	if value := strings.TrimSpace(rule.RuleName); value != "" {
		payload["rule_name"] = value
	}
	if value := strings.TrimSpace(rule.RuleType); value != "" {
		payload["rule_type"] = value
	}
	if value := strings.TrimSpace(rule.Health); value != "" {
		payload["health"] = value
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
			Provider:             stringPtr(sourceKind(ctx)),
			SourceKind:           stringPtr(sourceKind(ctx)),
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
			Provider:          stringPtr(sourceKind(ctx)),
			SourceKind:        stringPtr(sourceKind(ctx)),
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
		"provider":              sourceKind(ctx),
		"source_class":          SourceClassObserved,
		"source_kind":           sourceKind(ctx),
		"source_instance_id":    ctx.SourceInstanceID,
		"scope_id":              ctx.ScopeID,
		"generation_id":         ctx.GenerationID,
		"observed_at":           normalizedObservedAt(ctx.ObservedAt).Format(time.RFC3339Nano),
		"freshness_state":       freshness,
		"redaction_version":     RedactionVersion,
		"outcome":               outcome,
	}
	payload["provenance"] = map[string]any{
		"provider":           sourceKind(ctx),
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
		return fmt.Errorf("metric envelope scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("metric envelope generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("metric envelope collector_instance_id must not be blank")
	}
	if strings.TrimSpace(ctx.SourceInstanceID) == "" {
		return fmt.Errorf("metric envelope source_instance_id must not be blank")
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

func sourceKind(ctx EnvelopeContext) string {
	switch strings.TrimSpace(ctx.SourceKind) {
	case SourceKindMimir:
		return SourceKindMimir
	default:
		return SourceKindPrometheus
	}
}
