// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudinventory

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// cloudInventoryAdmissionSourceFactKinds is the closed set of provider
// cloud-inventory source fact kinds whose presence in a scope generation
// triggers shared cloud-inventory admission. It stays in lockstep with the
// fact-kind allowlist the admission evidence loader reads.
var cloudInventoryAdmissionSourceFactKinds = map[string]struct{}{
	facts.AWSResourceFactKind:        {},
	facts.GCPCloudResourceFactKind:   {},
	facts.AzureCloudResourceFactKind: {},
}

// BuildCloudInventoryAdmissionReducerIntent enqueues one reducer intent that
// admits the scope generation's provider cloud-inventory source facts
// (aws_resource, gcp_cloud_resource, azure_cloud_resource) into the shared
// canonical CloudResource identity keyspace as reducer_cloud_resource_identity
// rows, which back GET /api/v0/cloud/inventory (#2209). Without this trigger the
// admission handler — though registered and wired — never receives an intent, so
// the canonical inventory readback returns zero rows even when raw CloudResource
// rows exist.
//
// It mirrors the AWS resource materialization trigger: a single scope-keyed
// intent when any cloud-inventory source fact is present, anchored to the first
// such fact in original input order so the reducer claim is stable across
// reprojections of the same generation. The source-system label is the shared
// two-tier projectorintent.SourceSystem fallback (SourceRef.SourceSystem, then
// CollectorKind).
func BuildCloudInventoryAdmissionReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstMatchingKindPredicate(
		func(kind string) bool {
			_, isSource := cloudInventoryAdmissionSourceFactKinds[kind]
			return isSource
		},
		func(facts.Envelope) bool { return true },
	)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCloudInventoryAdmission,
		EntityKey:    "cloud_inventory_admission:" + scopeID,
		Reason:       "provider cloud-inventory source facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
