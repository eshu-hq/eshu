// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloudruntimedrift

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// candidateFactKinds are the fact kinds triggerFact ever inspects: GCP and
// Azure cloud-inventory facts. aws_resource is deliberately excluded; see
// BuildMultiCloudRuntimeDriftReducerIntent for why.
var candidateFactKinds = []string{
	facts.GCPCloudResourceFactKind,
	facts.AzureCloudResourceFactKind,
}

// BuildMultiCloudRuntimeDriftReducerIntent enqueues DomainMultiCloudRuntimeDrift
// for one scope generation whenever GCP or Azure cloud-inventory facts are
// present (issue #5759, closing the "registered but never enqueued" gap left
// since #1997/#1998). The trigger set is deliberately {gcp_cloud_resource,
// azure_cloud_resource} and does NOT include aws_resource: DomainAWSCloudRuntimeDrift
// already publishes AWS runtime-drift findings end-to-end
// (reducer_aws_cloud_runtime_drift_finding), so an AWS-only scope generation
// must not enqueue this domain at all -- doing so would load evidence and
// evaluate candidates purely to filter every one of them away.
//
// A scope that carries AWS facts alongside GCP/Azure facts still enqueues here
// for its GCP/Azure coverage. The shared cloud_resource_uid evidence loader
// (PostgresMultiCloudRuntimeDriftEvidenceLoader) joins all three providers'
// inventory facts into one keyspace for implementation reuse, so it can still
// return AWS rows in that case; MultiCloudRuntimeDriftHandler.Handle's
// excludeAWSOwnedRows is the publish-time filter that drops them before any
// finding is written, so the two domains never disagree about the same AWS
// resource. Partitioning at trigger time (which provider facts are present)
// and at publish time (which provider a resolved row belongs to) are separate
// decisions because the trigger only sees this generation's fact kinds, not
// which provider rows the loader's join will actually resolve.
func BuildMultiCloudRuntimeDriftReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(triggerFact, candidateFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainMultiCloudRuntimeDrift,
		EntityKey:    "multi_cloud_runtime_drift:" + scopeID,
		Reason:       "gcp or azure cloud resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// triggerFact accepts every envelope FirstAcrossKinds hands it.
// candidateFactKinds already restricts the scan to gcp_cloud_resource and
// azure_cloud_resource, so no per-envelope filtering is needed beyond kind
// membership.
func triggerFact(facts.Envelope) bool { return true }
