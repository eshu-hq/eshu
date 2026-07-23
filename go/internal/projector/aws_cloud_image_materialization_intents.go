// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// awsCloudImageLambdaFunctionUsesImageRelationshipType is the one raw AWS
// relationship_type value DomainAWSCloudImageMaterialization resolves to a
// graph edge (issue #5450). It duplicates the reducer's own
// lambdaFunctionUsesImageRelationshipType constant
// (go/internal/reducer/aws_cloud_image_join.go) as a literal rather than
// importing it: the reducer package's copy is unexported, and this projector
// package already decodes aws_relationship facts through its own
// decodeAWSRelationship wrapper rather than depending on reducer internals
// for fact-shape access — the two copies are kept in lockstep by
// TestBuildProjectionQueuesAWSCloudImageMaterialization, which fails if the
// reducer's handler ever renames the relationship_type it actually resolves
// without this trigger predicate following.
//
// ecs_task_definition_uses_image is deliberately NOT a trigger here: the
// handler recognizes that relationship_type but always skips it (tag-only,
// stays Postgres-only per the #5472 EXACT-ONLY policy — see
// docs/internal/aws-relationship-edge-materialization-design.md §12), so a
// generation containing only that relationship type has no cloud-image work
// to enqueue.
const awsCloudImageLambdaFunctionUsesImageRelationshipType = "lambda_function_uses_image"

// buildAWSCloudImageMaterializationReducerIntent enqueues one reducer intent
// that projects the scope generation's lambda_function_uses_image
// aws_relationship facts into a canonical CloudResource -> ContainerImage
// graph edge (issue #5450). The intent is anchored to the first matching
// aws_relationship fact so the reducer claim is stable across reprojections
// of the same generation, and is only enqueued when at least one
// lambda_function_uses_image relationship exists (a generation with only
// ecs_task_definition_uses_image, or with unrelated relationship types,
// enqueues nothing — AWSCloudImageMaterializationHandler.Handle would do
// real work only for the former).
//
// The entity key intentionally matches the AWS resource materialization
// intent ("aws_resource_materialization:<scope>") so
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
	envelope, ok := index.firstOfKindMatching(facts.AWSRelationshipFactKind, func(envelope facts.Envelope) bool {
		relationship, err := decodeAWSRelationship(envelope)
		if err != nil {
			return false
		}
		return relationship.RelationshipType == awsCloudImageLambdaFunctionUsesImageRelationshipType
	})
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainAWSCloudImageMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "aws lambda_function_uses_image relationship observed",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
