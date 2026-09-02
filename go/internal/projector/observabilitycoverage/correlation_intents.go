// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoverage

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// observabilityResourceTypes is the closed set of AWS-native observability
// object resource types (issue #391). The AWS branch of the correlation
// trigger only accepts an aws_resource fact whose resource_type is in this
// set: without an observability object there is no coverage signal to
// correlate. It is this package's copy of the same closed set root's
// observability-coverage materialization trigger keeps
// (go/internal/projector/observability_coverage_materialization_intents.go),
// and both mirror the reducer's observabilityResourceSignals map
// (go/internal/reducer/observability_coverage_correlation_index.go) so the
// triggers and the classifier agree on what counts as an observability
// object. A resource type added to one copy must be added to all three.
var observabilityResourceTypes = map[string]struct{}{
	"aws_cloudwatch_alarm":           {},
	"aws_cloudwatch_composite_alarm": {},
	"aws_cloudwatch_dashboard":       {},
	"aws_cloudwatch_logs_log_group":  {},
	"aws_xray_sampling_rule":         {},
	"aws_xray_group":                 {},
}

// BuildObservabilityCoverageCorrelationReducerIntent enqueues one reducer
// intent that asks the reducer's observability_coverage_correlation domain to
// correlate the scope generation's observability source facts (dashboards,
// alerts, log/trace sources) and AWS-native observability aws_resource facts
// against the monitored resources they cover (issue #391). It anchors to the
// earliest trigger fact in original input order so the reducer claim is
// stable across reprojections of the same generation.
func BuildObservabilityCoverageCorrelationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstMatchingKindPredicate(
		observabilityCoverageCorrelationCandidateFactKind,
		observabilityCoverageCorrelationTriggerFact,
	)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainObservabilityCoverageCorrelation,
		EntityKey:    "observability_coverage_correlation:" + scopeID,
		Reason:       observabilityCoverageCorrelationReason(envelope),
		FactID:       envelope.FactID,
		SourceSystem: observabilitySourceSystem(envelope),
	}, true
}

// observabilityCoverageCorrelationCandidateFactKind reports whether kind can
// EVER satisfy observabilityCoverageCorrelationTriggerFact. It mirrors that
// function's kind-level branches so FirstMatchingKindPredicate only visits
// facts of kinds that have a chance of matching; the AWS branch still needs
// its per-envelope resource_type decode, which stays in
// observabilityCoverageCorrelationTriggerFact as the final accept check.
func observabilityCoverageCorrelationCandidateFactKind(kind string) bool {
	if kind == facts.AWSResourceFactKind {
		return true
	}
	if kind == facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	_, ok := facts.ObservabilitySchemaVersion(kind)
	return ok
}

func observabilityCoverageCorrelationTriggerFact(envelope facts.Envelope) bool {
	if envelope.FactKind == facts.AWSResourceFactKind {
		_, ok := observabilityResourceTypes[awsResourceTypeForEnvelope(envelope)]
		return ok
	}
	if envelope.FactKind == facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	_, ok := facts.ObservabilitySchemaVersion(envelope.FactKind)
	return ok
}

func observabilityCoverageCorrelationReason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.AWSResourceFactKind {
		return "aws observability resource facts observed"
	}
	return "observability source facts observed"
}

// observabilitySourceSystem is the family's pre-extraction source-system
// label, kept verbatim rather than replaced with the two-tier
// projectorintent.SourceSystem: it carries a literal third fallback to
// "observability" where the shared helper returns an empty string, so the
// substitution would silently relabel an intent whose trigger fact has a
// blank source ref and blank collector kind. A package test pins the third
// tier against that substitution.
func observabilitySourceSystem(envelope facts.Envelope) string {
	if envelope.SourceRef.SourceSystem != "" {
		return envelope.SourceRef.SourceSystem
	}
	if envelope.CollectorKind != "" {
		return envelope.CollectorKind
	}
	return "observability"
}
