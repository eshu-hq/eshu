// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildRDSPostureMaterializationReducerIntent enqueues one reducer intent that
// projects the scope generation's rds_instance_posture facts onto already
// materialized RDS CloudResource nodes (issue #1233). The intent is anchored to
// the first posture fact so the reducer claim is stable across reprojections of
// the same generation. Public and private instances both enqueue because
// encryption, backup, deletion-protection, IAM database auth, and operational
// posture remain queryable evidence even when no public endpoint exists.
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that the
// CloudResource materialization publishes for the same acceptance unit. RDS
// posture properties never project before the target CloudResource nodes commit.
func buildRDSPostureMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.RDSInstancePostureFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainRDSPostureMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "rds posture facts observed",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
