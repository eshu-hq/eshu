// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildS3InternetExposureMaterializationReducerIntent enqueues one reducer
// intent that derives internet-exposure properties from s3_bucket_posture facts
// for the scope generation. The entity key intentionally matches the AWS
// resource materialization intent so the handler and durable queue both gate on
// the same CloudResource canonical-nodes readiness row.
func buildS3InternetExposureMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.S3BucketPostureFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainS3InternetExposureMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "s3 bucket posture observed",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
