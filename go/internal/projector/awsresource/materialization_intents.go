// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsresource

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildAWSResourceMaterializationReducerIntent enqueues one reducer intent that
// materializes the scope generation's aws_resource facts into canonical
// CloudResource graph nodes (issue #805, #6057 extraction). It mirrors the AWS
// runtime-drift trigger: a single scope-keyed intent when any aws_resource fact
// is present.
//
// The intent is anchored to the first aws_resource fact in original generation
// order (FirstOfKind) so the reducer claim is stable across reprojections of
// the same generation.
//
// The aws_resource_materialization:<scope> entity key is not private to this
// family. Other AWS builders reuse the same key so their reducer handlers wait
// on the CloudResource substrate this domain publishes, and
// internal/storage/postgres derives the cloud-resource-node queue conflict key
// (reducerCloudResourceNodeConflictKey) from the prefix only for a domain
// whose resource-conflict policy is marked safe, which today is
// DomainAWSResourceMaterialization alone; the sibling AWS families are risky
// or blocked and group by resource_scope or the default. Changing the literal
// changes readiness gating for every family sharing the key, and conflict
// grouping for this domain.
func BuildAWSResourceMaterializationReducerIntent(
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
		Domain:       reducer.DomainAWSResourceMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws runtime resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
