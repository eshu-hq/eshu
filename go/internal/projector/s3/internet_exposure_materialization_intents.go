// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildInternetExposureMaterializationReducerIntent enqueues one reducer
// intent that derives internet-exposure properties from s3_bucket_posture facts
// for the scope generation. The entity key intentionally matches the AWS
// resource materialization intent so the handler and durable queue both gate on
// the same CloudResource canonical-nodes readiness row.
func BuildInternetExposureMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.S3BucketPostureFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainS3InternetExposureMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "s3 bucket posture observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
