// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloudruntimedrift

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildAWSCloudRuntimeDriftReducerIntent enqueues one reducer intent that asks
// the reducer to run the bounded AWS ARN join against active Terraform-state
// and Terraform-config facts (issue #6053 epic, #6057 extraction). The
// trigger is the mere presence of an aws_resource fact in the scope
// generation: the projector stays source-local and never joins AWS resources
// to Terraform evidence itself, so any aws_resource observation is enough to
// ask the reducer to re-run its own bounded join and re-classify drift for
// the scope.
//
// The intent is anchored to the first aws_resource fact in original
// generation order (FirstOfKind) so the reducer claim is stable across
// reprojections of the same generation.
func BuildAWSCloudRuntimeDriftReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AWSResourceFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainAWSCloudRuntimeDrift,
		EntityKey:    "aws_cloud_runtime_drift:" + scopeID,
		Reason:       "aws runtime resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
