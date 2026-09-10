// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildLogsToMaterializationReducerIntent enqueues one reducer intent that
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
func BuildLogsToMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(facts.S3BucketPostureFactKind, func(envelope facts.Envelope) bool {
		posture, err := decodeS3BucketPosture(envelope)
		if err != nil {
			return false
		}
		return strings.TrimSpace(derefString(posture.LoggingTargetBucket)) != ""
	})
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainS3LogsToMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "s3 bucket access logging observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// derefString returns the value a *string points at, or "" when it is nil.
// Local per-package copy matching the repo convention of a small
// family-scoped deref helper (e.g. projector root's codegraphDerefString,
// ociDerefString, tfstateDerefString, ec2's derefString) rather than a shared
// one.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
