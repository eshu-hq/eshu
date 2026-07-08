// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildS3LogsToMaterializationReducerIntent enqueues one reducer intent that
// projects the scope generation's s3_bucket_posture logging_target_bucket fields
// into canonical LOGS_TO graph edges (issue #1144 PR2). The intent is anchored
// to the first posture fact that has a non-blank logging_target_bucket so the
// reducer claim is stable across reprojections of the same generation, and is
// only enqueued when at least one bucket has access logging enabled
// (logging-disabled-only generations enqueue nothing).
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the edge handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that #805
// PR1 publishes on the cloud_resource_uid keyspace for the same acceptance unit
// — LOGS_TO edges never project before the S3 bucket nodes commit.
func buildS3LogsToMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.S3BucketPostureFactKind {
			continue
		}
		posture, err := decodeS3BucketPosture(envelope)
		if err != nil {
			continue
		}
		target := codegraphDerefString(posture.LoggingTargetBucket)
		if strings.TrimSpace(target) == "" {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainS3LogsToMaterialization,
			EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "s3 bucket access logging observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
