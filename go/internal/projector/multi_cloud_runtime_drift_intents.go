// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildMultiCloudRuntimeDriftReducerIntent enqueues DomainMultiCloudRuntimeDrift
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
func buildMultiCloudRuntimeDriftReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstAcrossKinds(
		func(facts.Envelope) bool { return true },
		facts.GCPCloudResourceFactKind,
		facts.AzureCloudResourceFactKind,
	)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainMultiCloudRuntimeDrift,
		EntityKey:    "multi_cloud_runtime_drift:" + scopeValue.ScopeID,
		Reason:       "gcp or azure cloud resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: multiCloudRuntimeDriftSourceSystem(envelope),
	}, true
}

// multiCloudRuntimeDriftSourceSystem resolves the bounded source-system label
// for the multi-cloud drift intent, preferring the fact's source ref and
// falling back to its collector kind.
func multiCloudRuntimeDriftSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
