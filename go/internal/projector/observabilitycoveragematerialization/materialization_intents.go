// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoveragematerialization

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// observabilityResourceTypes is the closed set of AWS-native observability
// object resource types (issue #391). An intent to materialize COVERS edges is
// only worth enqueuing when at least one of these is present in the generation:
// without an observability object there can be no coverage edge. It is this
// package's copy of a three-way mirror: the sibling correlation family keeps
// the same set in
// go/internal/projector/observabilitycoverage/correlation_intents.go, and both
// mirror the reducer's observabilityResourceSignals map in
// go/internal/reducer/obscoverage/observability_coverage_correlation_index.go,
// so the triggers and the classifier agree on what counts as an observability
// object.
// A resource type added to one copy must be added to all three.
var observabilityResourceTypes = map[string]struct{}{
	"aws_cloudwatch_alarm":           {},
	"aws_cloudwatch_composite_alarm": {},
	"aws_cloudwatch_dashboard":       {},
	"aws_cloudwatch_logs_log_group":  {},
	"aws_xray_sampling_rule":         {},
	"aws_xray_group":                 {},
}

// BuildObservabilityCoverageMaterializationReducerIntent enqueues one reducer
// intent that projects the scope generation's exact observability coverage
// decisions into canonical COVERS graph edges (issue #391 PR3). The intent fires
// when any observability aws_resource fact is present, since that is the only
// way a COVERS edge can exist. It is anchored to the first such fact so the
// reducer claim is stable across reprojections of the same generation.
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the coverage edge handler's
// readiness gate resolves the exact GraphProjectionPhaseCanonicalNodesCommitted
// row that #805 PR1 publishes for the same acceptance unit — coverage edges
// never project before the CloudResource nodes commit.
func BuildObservabilityCoverageMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(facts.AWSResourceFactKind, func(envelope facts.Envelope) bool {
		_, ok := observabilityResourceTypes[awsResourceTypeForEnvelope(envelope)]
		return ok
	})
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainObservabilityCoverageMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws observability resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
