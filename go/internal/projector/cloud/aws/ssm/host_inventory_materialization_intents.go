// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildHostInventoryMaterializationReducerIntent enqueues one reducer intent
// that materializes the scope generation's admitted SSM host inventory facts
// into canonical observation graph nodes and platform-pairing edges
// (eshu-hq/eshu#9558). It mirrors the EC2 node trigger: a single scope-keyed
// intent when any SSM host fact is present, anchored to the earliest such
// fact so the reducer claim is stable across reprojections of the same
// generation.
//
// The intent uses its own DISTINCT entity key
// ("ssm_host_inventory_materialization:<scope>"), shared with no other
// domain: SSM observations never gate another domain's readiness, and no
// other domain gates on them.
//
// Either kind triggers alone: a platform-only generation still updates
// platform observations, and an application-only generation still supersedes
// application snapshots. The anchor prefers the earliest application fact
// and falls back to the earliest platform fact, deterministically.
func BuildHostInventoryMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	anchor, ok := lookup.FirstOfKind(cloud.SSMHostApplicationFactKind)
	if !ok {
		anchor, ok = lookup.FirstOfKind(cloud.SSMHostPlatformFactKind)
	}
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSSMHostInventoryMaterialization,
		EntityKey:    "ssm_host_inventory_materialization:" + scopeID,
		Reason:       "ssm host inventory facts observed",
		FactID:       anchor.FactID,
		SourceSystem: projectorintent.SourceSystem(anchor),
	}, true
}
