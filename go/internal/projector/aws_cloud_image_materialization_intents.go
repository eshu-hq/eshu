// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildAWSCloudImageMaterializationReducerIntent enqueues one reducer intent
// that projects the scope generation's lambda_function_uses_image
// aws_relationship facts into a canonical CloudResource -> ContainerImage
// graph edge (issue #5450).
//
// The trigger is aws_resource fact presence — the SAME persistent signal
// buildAWSResourceMaterializationReducerIntent uses — not
// lambda_function_uses_image relationship presence. This is a deliberate
// retraction-safety fix (issue #5450 follow-up review): AWS is scanned as a
// whole every generation, so aws_resource facts are present whenever the
// scope is observed at all, including a generation where a Lambda function
// switched from an Image package to Zip (or its image relationship otherwise
// disappeared). Triggering on relationship presence alone meant that
// generation would enqueue NOTHING, so
// AWSCloudImageMaterializationHandler.Handle's retract-first logic never ran
// and the PRIOR AWS_lambda_function_uses_image edge stayed in the graph
// forever — a stale deployed-image relationship, defeating the #5472
// retract-first-per-generation contract. Handle already retracts
// unconditionally (before checking whether there are any rows to write — see
// its doc), so triggering on the persistent aws_resource signal is
// sufficient: an empty current-relationship set now still runs Handle, which
// retracts the prior edge and writes zero new ones.
//
// This does cost one extra reducer run per AWS generation versus the
// old relationship-gated trigger (matching DomainAWSResourceMaterialization's
// and DomainAWSRelationshipMaterialization's own per-generation cost, which
// this domain already shares an entity key and readiness phase with) — see
// the B-9 handler budget and docs/internal/aws-relationship-edge-materialization-design.md
// §12 for the accepted cost/correctness tradeoff.
//
// The intent is anchored to the first aws_resource fact so the reducer claim
// is stable across reprojections of the same generation. The entity key
// intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so
// AWSCloudImageMaterializationHandler.sourceNodesReady resolves the exact
// GraphProjectionPhaseCanonicalNodesCommitted row DomainAWSResourceMaterialization
// publishes on the CloudResource keyspace for the same acceptance unit — the
// edge never projects before the source Lambda function CloudResource node
// commits. There is no analogous readiness phase for the target
// :ContainerImage node: OCI registry canonical nodes materialize through the
// source-local projector path (internal/projector/oci_registry_canonical.go),
// independent of this reducer's scope-generation phases, so an unscanned
// image is a graceful two-MATCH-MERGE no-op inside the handler rather than a
// second readiness gate here (matching every other AWS edge domain's
// forward-looking-target handling).
func buildAWSCloudImageMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.AWSResourceFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainAWSCloudImageMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "aws runtime resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
