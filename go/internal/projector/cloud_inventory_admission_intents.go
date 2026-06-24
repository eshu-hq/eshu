// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
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

// buildCloudInventoryAdmissionReducerIntent enqueues one reducer intent that
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
// such fact so the reducer claim is stable across reprojections of the same
// generation.
func buildCloudInventoryAdmissionReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if _, ok := cloudInventoryAdmissionSourceFactKinds[envelope.FactKind]; !ok {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainCloudInventoryAdmission,
			EntityKey:    "cloud_inventory_admission:" + scopeValue.ScopeID,
			Reason:       "provider cloud-inventory source facts observed",
			FactID:       envelope.FactID,
			SourceSystem: cloudInventoryAdmissionSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

// cloudInventoryAdmissionSourceSystem resolves the bounded source-system label
// for the admission intent, preferring the fact's source ref and falling back to
// its collector kind.
func cloudInventoryAdmissionSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
