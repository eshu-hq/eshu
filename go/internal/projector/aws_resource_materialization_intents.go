// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildAWSResourceMaterializationReducerIntent enqueues one reducer intent that
// materializes the scope generation's aws_resource facts into canonical
// CloudResource graph nodes (issue #805). It mirrors the AWS runtime-drift
// trigger: a single scope-keyed intent when any aws_resource fact is present.
// The intent is anchored to the first aws_resource fact so the reducer claim is
// stable across reprojections of the same generation.
func buildAWSResourceMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainAWSResourceMaterialization,
			EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "aws runtime resource facts observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
