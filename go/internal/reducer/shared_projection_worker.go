// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// maxSharedSelectionScanLimit mirrors [worker]'s unexported scan cap (moved
// there, issue #6061). It cannot be a const alias to an unexported constant
// in another package, so this is a fixed, documented copy of the same
// literal; it is a stable bound, not a tunable, so drift risk is low.
const maxSharedSelectionScanLimit = 10_000

// SharedProjectionEdgeWriter is the root spelling of [sharedintent.EdgeWriter].
type SharedProjectionEdgeWriter = sharedintent.EdgeWriter

// PartitionLeaseManager is the root spelling of
// [sharedintent.PartitionLeaseManager].
type PartitionLeaseManager = sharedintent.PartitionLeaseManager

// SharedIntentReader is the root spelling of [worker.IntentReader].
type SharedIntentReader = worker.IntentReader

// AcceptedGenerationLookup is the root spelling of
// [sharedintent.AcceptedGenerationLookup].
type AcceptedGenerationLookup = sharedintent.AcceptedGenerationLookup

// AcceptedGenerationPrefetch is the root spelling of
// [sharedintent.AcceptedGenerationPrefetch].
type AcceptedGenerationPrefetch = sharedintent.AcceptedGenerationPrefetch

// PartitionBatchResult is the root spelling of [worker.PartitionBatchResult].
type PartitionBatchResult = worker.PartitionBatchResult

// PartitionProcessorConfig is the root spelling of
// [worker.PartitionProcessorConfig].
type PartitionProcessorConfig = worker.PartitionProcessorConfig

// PartitionProcessResult is the root spelling of
// [worker.PartitionProcessResult].
type PartitionProcessResult = worker.PartitionProcessResult

// SelectPartitionBatch forwards to [worker.SelectPartitionBatch].
func SelectPartitionBatch(
	ctx context.Context,
	reader SharedIntentReader,
	domain string,
	partitionID, partitionCount int,
	batchLimit int,
	acceptedGen AcceptedGenerationLookup,
	prefetch AcceptedGenerationPrefetch,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
) (PartitionBatchResult, error) {
	return worker.SelectPartitionBatch(ctx, reader, domain, partitionID, partitionCount, batchLimit, acceptedGen, prefetch, readinessLookup, readinessPrefetch, endpointPresence)
}

// ProcessPartitionOnce forwards to [worker.ProcessPartitionOnce].
func ProcessPartitionOnce(
	ctx context.Context,
	now time.Time,
	cfg PartitionProcessorConfig,
	leaseManager PartitionLeaseManager,
	reader SharedIntentReader,
	edgeWriter SharedProjectionEdgeWriter,
	acceptedGen AcceptedGenerationLookup,
	prefetch AcceptedGenerationPrefetch,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
	refreshFence SharedProjectionRefreshFenceLookup,
	firstProjection FirstProjectionLookup,
	unroutableWriter SharedProjectionUnroutableWriter,
) (result PartitionProcessResult, retErr error) {
	return worker.ProcessPartitionOnce(ctx, now, cfg, leaseManager, reader, edgeWriter, acceptedGen, prefetch, readinessLookup, readinessPrefetch, endpointPresence, refreshFence, firstProjection, unroutableWriter)
}
