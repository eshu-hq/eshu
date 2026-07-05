// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// observabilityMetadataView is the flat, typed read view the coverage-metadata
// classifier reads instead of raw payloadString map lookups. Every field is the
// decoded value of the same-named payload key an observability fact can carry;
// an absent optional key decodes to nil and reads here as "" through the field
// accessors, byte-identical to payloadString's absent-key behavior. The view is
// produced by decodeObservabilityMetadataView, which routes each envelope
// through the typed contracts seam per its fact kind, so a fact missing its
// required source_instance_id (or provider_object_uid for the four observed
// kinds that require it) dead-letters input_invalid instead of yielding a view
// with an empty source anchor.
type observabilityMetadataView struct {
	// Object-ref candidates, in the classifier's exact fallback order.
	providerObjectUID           string
	dashboardUID                string
	datasourceUID               string
	alertRuleUID                string
	folderUID                   string
	resourceIdentity            string
	resourceIdentityFingerprint string
	resourceName                string
	pipelineName                string
	selectorIdentityFingerprint string
	ruleGroup                   string
	ruleName                    string
	alertRuleNameFingerprint    string
	recordRuleNameFingerprint   string
	routeDestinationFingerprint string
	labelIdentityFingerprint    string
	traceTagIdentityFingerprint string
	tagName                     string
	seriesFingerprint           string
	appName                     string

	// Provider/class/outcome/freshness/service reads.
	provider                   string
	backendKind                string
	sourceKind                 string
	sourceClass                string
	resourceClass              string
	observabilityResourceClass string
	resourceKind               string
	outcome                    string
	freshnessState             string
	warningKind                string
	driftCandidateReason       string
	declaredMatchState         string
	serviceHints               string
	serviceRef                 string
}

// deref returns the pointed-to string or "" when the pointer is nil, matching
// payloadString's "absent key -> empty string" contract for the optional fields.
func derefOr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// objectRefCandidates returns the object-ref candidate values in the exact
// priority order observabilityMetadataObjectRef read them from the raw payload,
// so the typed path selects the same first-non-empty ref.
func (v observabilityMetadataView) objectRefCandidates() []string {
	return []string{
		v.providerObjectUID,
		v.dashboardUID,
		v.datasourceUID,
		v.alertRuleUID,
		v.folderUID,
		v.resourceIdentity,
		v.resourceIdentityFingerprint,
		v.resourceName,
		v.pipelineName,
		v.selectorIdentityFingerprint,
		v.ruleGroup,
		v.ruleName,
		v.alertRuleNameFingerprint,
		v.recordRuleNameFingerprint,
		v.routeDestinationFingerprint,
		v.labelIdentityFingerprint,
		v.traceTagIdentityFingerprint,
		v.tagName,
		v.seriesFingerprint,
		v.appName,
	}
}

// decodeObservabilityMetadataView decodes one observability envelope into the
// flat metadata view through the typed contracts seam, dispatching on fact kind.
// The classifier never asks it to decode observability.source_instance (that
// kind carries no coverage object and is filtered upstream), so it is not in the
// switch; any other unrecognized observability kind is a programming error the
// caller guards against by only passing kinds ObservabilitySchemaVersion knows.
//
// A decode error is returned verbatim (already a self-classifying
// *factDecodeError from the decode<Kind> wrapper) so the caller can route it
// through partitionDecodeFailures for per-fact input_invalid quarantine.
func decodeObservabilityMetadataView(env facts.Envelope) (observabilityMetadataView, error) {
	switch env.FactKind {
	case facts.ObservabilityDeclaredFolderFactKind:
		s, err := decodeObservabilityDeclaredFolder(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredFolder(s), nil
	case facts.ObservabilityDeclaredDashboardFactKind:
		s, err := decodeObservabilityDeclaredDashboard(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredDashboard(s), nil
	case facts.ObservabilityDeclaredDatasourceFactKind:
		s, err := decodeObservabilityDeclaredDatasource(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredDatasource(s), nil
	case facts.ObservabilityDeclaredAlertRuleFactKind:
		s, err := decodeObservabilityDeclaredAlertRule(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredAlertRule(s), nil
	case facts.ObservabilityDeclaredScrapeConfigFactKind:
		s, err := decodeObservabilityDeclaredScrapeConfig(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredScrapeConfig(s), nil
	case facts.ObservabilityDeclaredMetricRuleFactKind:
		s, err := decodeObservabilityDeclaredMetricRule(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredMetricRule(s), nil
	case facts.ObservabilityDeclaredMetricRouteFactKind:
		s, err := decodeObservabilityDeclaredMetricRoute(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredMetricRoute(s), nil
	case facts.ObservabilityDeclaredLogRouteFactKind:
		s, err := decodeObservabilityDeclaredLogRoute(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredLogRoute(s), nil
	case facts.ObservabilityDeclaredTraceRouteFactKind:
		s, err := decodeObservabilityDeclaredTraceRoute(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromDeclaredTraceRoute(s), nil
	case facts.ObservabilityAppliedResourceFactKind:
		s, err := decodeObservabilityAppliedResource(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromAppliedResource(s), nil
	case facts.ObservabilityAppliedSyncStateFactKind:
		s, err := decodeObservabilityAppliedSyncState(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromAppliedSyncState(s), nil
	case facts.ObservabilityObservedDashboardFactKind:
		s, err := decodeObservabilityObservedDashboard(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromObservedDashboard(s), nil
	case facts.ObservabilityObservedTargetFactKind:
		s, err := decodeObservabilityObservedTarget(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromObservedTarget(s), nil
	case facts.ObservabilityObservedRuleFactKind:
		s, err := decodeObservabilityObservedRule(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromObservedRule(s), nil
	case facts.ObservabilityObservedLogSignalFactKind:
		s, err := decodeObservabilityObservedLogSignal(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromObservedLogSignal(s), nil
	case facts.ObservabilityObservedTraceSignalFactKind:
		s, err := decodeObservabilityObservedTraceSignal(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromObservedTraceSignal(s), nil
	case facts.ObservabilityCoverageWarningFactKind:
		s, err := decodeObservabilityCoverageWarning(env)
		if err != nil {
			return observabilityMetadataView{}, err
		}
		return viewFromCoverageWarning(s), nil
	default:
		// A non-metadata observability kind (source_instance) or a non-observability
		// kind: no view, no error. The caller only routes metadata kinds here.
		return observabilityMetadataView{}, nil
	}
}
