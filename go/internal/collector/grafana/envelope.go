// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewSourceInstanceEnvelope converts one live Grafana target snapshot into a
// source-instance fact.
func NewSourceInstanceEnvelope(ctx EnvelopeContext, stats CollectionStats) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	payload := basePayload(ctx, OutcomeObserved, FreshnessCurrent)
	payload["resource_count"] = stats.Resources
	payload["rule_count"] = stats.Rules
	payload["warning_count"] = stats.Warnings
	payload["pages_fetched"] = stats.PagesFetched
	payload["partial"] = stats.Partial
	setRedactionState(payload)
	stableKey := stableFactKey(facts.ObservabilitySourceInstanceFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
	})
	return observabilityEnvelope(ctx, facts.ObservabilitySourceInstanceFactKind, stableKey, payload, ctx.SourceInstanceID), nil
}

// NewObservedResourceEnvelope converts one bounded live Grafana resource into
// an observed-dashboard fact.
func NewObservedResourceEnvelope(ctx EnvelopeContext, resource Resource) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	resourceClass := strings.TrimSpace(resource.Class)
	if resourceClass == "" {
		return facts.Envelope{}, fmt.Errorf("grafana resource class must not be blank")
	}
	uid := firstNonBlank(resource.UID, resource.Name, fmt.Sprint(resource.ID))
	if uid == "" || uid == "0" {
		return facts.Envelope{}, fmt.Errorf("grafana resource identity must not be blank")
	}
	payload := basePayload(ctx, normalizedOutcome(resource.Outcome), normalizedFreshness(resource.FreshnessState))
	payload["resource_class"] = resourceClass
	payload["provider_object_uid"] = uid
	if value := strings.TrimSpace(resource.FolderUID); value != "" {
		payload["folder_uid"] = value
	}
	if value := strings.TrimSpace(resource.DatasourceType); value != "" {
		payload["datasource_type"] = value
	}
	if value := strings.TrimSpace(resource.DeclaredMatchState); value != "" {
		payload["declared_match_state"] = value
	} else {
		payload["declared_match_state"] = MatchStateNotCompared
	}
	setFingerprint(payload, "title", resource.Title)
	setFingerprint(payload, "name", resource.Name)
	setURLRedaction(payload, resource.URL, resource.URLRedacted)
	setTimePayload(payload, "updated_at", resource.UpdatedAt)
	if resource.ManuallyCreated {
		payload["manually_created"] = true
	}
	if value := strings.TrimSpace(resource.DriftReason); value != "" {
		payload["drift_candidate_reason"] = value
	}
	setRedactionState(payload)
	stableKey := stableFactKey(facts.ObservabilityObservedDashboardFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"resource_class":     resourceClass,
		"provider_uid":       uid,
	})
	return observabilityEnvelope(ctx, facts.ObservabilityObservedDashboardFactKind, stableKey, payload, uid), nil
}

// NewObservedRuleEnvelope converts one bounded live Grafana alert rule into an
// observed-rule fact.
func NewObservedRuleEnvelope(ctx EnvelopeContext, rule AlertRule) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	uid := strings.TrimSpace(rule.UID)
	if uid == "" {
		return facts.Envelope{}, fmt.Errorf("grafana alert rule uid must not be blank")
	}
	payload := basePayload(ctx, normalizedOutcome(rule.Outcome), normalizedFreshness(rule.FreshnessState))
	payload["resource_class"] = ResourceClassAlertRule
	payload["alert_rule_uid"] = uid
	if value := strings.TrimSpace(rule.RuleGroup); value != "" {
		payload["rule_group"] = value
	}
	if value := strings.TrimSpace(rule.FolderUID); value != "" {
		payload["folder_uid"] = value
	}
	if value := strings.TrimSpace(rule.DatasourceUID); value != "" {
		payload["datasource_uid"] = value
	}
	if value := strings.TrimSpace(rule.DeclaredMatchState); value != "" {
		payload["declared_match_state"] = value
	} else {
		payload["declared_match_state"] = MatchStateNotCompared
	}
	setFingerprint(payload, "title", rule.Title)
	setTimePayload(payload, "updated_at", rule.UpdatedAt)
	if len(rule.Model) > 0 || rule.QueryModelRedacted {
		payload["query_model_redacted"] = true
	}
	if strings.TrimSpace(rule.ContactPoint) != "" || rule.ContactPointRedacted {
		payload["contact_point_redacted"] = true
	}
	if strings.TrimSpace(rule.NotificationURL) != "" || rule.NotificationURLRedacted {
		payload["notification_url_redacted"] = true
	}
	setRedactionState(payload)
	stableKey := stableFactKey(facts.ObservabilityObservedRuleFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"alert_rule_uid":     uid,
	})
	return observabilityEnvelope(ctx, facts.ObservabilityObservedRuleFactKind, stableKey, payload, uid), nil
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
	stableKey := stableFactKey(facts.ObservabilityCoverageWarningFactKind, ctx.GenerationID, map[string]any{
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
		"resource_class":     resourceClass,
		"resource_id":        resourceID,
		"reason":             reason,
	})
	return observabilityEnvelope(
		ctx,
		facts.ObservabilityCoverageWarningFactKind,
		stableKey,
		payload,
		firstNonBlank(resourceID, reason),
	), nil
}

func basePayload(ctx EnvelopeContext, outcome string, freshness string) map[string]any {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderGrafana,
		"source_class":          SourceClassObserved,
		"source_kind":           SourceKindGrafana,
		"source_instance_id":    ctx.SourceInstanceID,
		"scope_id":              ctx.ScopeID,
		"generation_id":         ctx.GenerationID,
		"observed_at":           normalizedObservedAt(ctx.ObservedAt).Format(time.RFC3339Nano),
		"freshness_state":       freshness,
		"redaction_version":     RedactionVersion,
		"outcome":               outcome,
	}
	payload["provenance"] = map[string]any{
		"provider":           ProviderGrafana,
		"source_instance_id": ctx.SourceInstanceID,
		"scope_id":           ctx.ScopeID,
	}
	return payload
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
		return fmt.Errorf("grafana envelope scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("grafana envelope generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("grafana envelope collector_instance_id must not be blank")
	}
	if strings.TrimSpace(ctx.SourceInstanceID) == "" {
		return fmt.Errorf("grafana envelope source_instance_id must not be blank")
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
