// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildExternalPrincipalGrantMaterializationReducerIntent enqueues one
// reducer intent that projects the scope generation's metadata-only
// s3_external_principal_grant facts into canonical GRANTS_ACCESS_TO edges. The
// entity key intentionally matches the AWS resource materialization intent so
// the reducer waits for the S3 source CloudResource nodes committed by the same
// generation before creating ExternalPrincipal graph truth.
func BuildExternalPrincipalGrantMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.S3ExternalPrincipalGrantFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainS3ExternalPrincipalGrantMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "s3 external principal grant observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
