// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package projection runs the controlled code-call projection lane: it
// drains code-call shared-projection intents one repo/run at a time,
// claiming a partition lease, selecting one authoritative acceptance unit's
// pending rows, retracting stale edges when needed, and writing the active
// rows through the shared edge writer (issue #6061). [Runner.Run] drives the
// cycle to drain; [Runner.RunOnce]-equivalent single-cycle entry points are
// unexported (processOnce/processPartitionOnce) since nothing outside this
// package drives a single cycle directly.
//
// Selection ([selection.go]) resolves one accepted, ready, unfenced
// acceptance unit through the generic worker's shared readiness/acceptance
// machinery ([intents/shared/worker]); partitions.go and candidates.go
// classify and route rows by partition key (legacy/whole-repo/file-scoped);
// rows.go builds and issues the retract/write batches; lease.go runs the
// partition-lease heartbeat; telemetry.go records the cycle's metrics, logs,
// and validation.
//
// The reducer root imports this package as projection. It keeps the
// exported CodeCallProjectionRunner/CodeCallProjectionRunnerConfig/
// ReducerGraphDrain/DefaultCodeCallProjectionLeaseOwnerPrefix/
// DefaultCodeCallAcceptanceScanLimit/CodeCallProjectionFilePartitionKeyPrefix/
// CodeCallProjectionPartitionCandidateReader/
// CodeCallProjectionUnhashedCandidateReader/CodeCallProjectionRefreshFenceLookup
// spellings through the code-call stanza of compat_projection.go.
//
// Dependency rule: from the reducer tree this package imports only the
// shared tier (contract, gpphase, sharedintent, payloadcore) and the sibling
// leaves codecall and intents/shared/worker; outside it, the OpenTelemetry
// metric/trace APIs, the telemetry package, and the standard library. It
// never imports the reducer root.
package projection
