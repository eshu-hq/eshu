// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// observabilityResourceTypes is the closed set of AWS-native observability
// object resource types (issue #391). An intent to materialize COVERS edges is
// only worth enqueuing when at least one of these is present in the generation:
// without an observability object there can be no coverage edge. It mirrors the
// reducer's observabilityResourceSignals map so the trigger and the classifier
// agree on what counts as an observability object.
var observabilityResourceTypes = map[string]struct{}{
	"aws_cloudwatch_alarm":           {},
	"aws_cloudwatch_composite_alarm": {},
	"aws_cloudwatch_dashboard":       {},
	"aws_cloudwatch_logs_log_group":  {},
	"aws_xray_sampling_rule":         {},
	"aws_xray_group":                 {},
}

// buildObservabilityCoverageMaterializationReducerIntent enqueues one reducer
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
func buildObservabilityCoverageMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if _, ok := observabilityResourceTypes[awsResourceTypeForEnvelope(envelope)]; !ok {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainObservabilityCoverageMaterialization,
			EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "aws observability resource facts observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

// awsResourceTypeForEnvelope returns the resource_type string from an
// aws_resource fact payload, or empty when absent.
func awsResourceTypeForEnvelope(envelope facts.Envelope) string {
	if envelope.Payload == nil {
		return ""
	}
	resourceType, _ := envelope.Payload["resource_type"].(string)
	return resourceType
}
