// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildS3ExternalPrincipalGrantMaterializationReducerIntent enqueues one
// reducer intent that projects the scope generation's metadata-only
// s3_external_principal_grant facts into canonical GRANTS_ACCESS_TO edges. The
// entity key intentionally matches the AWS resource materialization intent so
// the reducer waits for the S3 source CloudResource nodes committed by the same
// generation before creating ExternalPrincipal graph truth.
func buildS3ExternalPrincipalGrantMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.S3ExternalPrincipalGrantFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainS3ExternalPrincipalGrantMaterialization,
			EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "s3 external principal grant observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
