// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package worker is the shared-projection substrate: the partition worker,
// runner, lease heartbeat, batch selection, readiness gating, and
// acceptance/repo-refresh machinery that every generic shared-projection
// domain (code_calls, repo_dependency, handles_route, runs_in, rationale,
// inheritance, sql_relationships, shell_exec, documentation, codeowners,
// submodule pins, workload_dependency, deployable_unit_edges) is drained
// through (issue #6061).
//
// [ProcessPartitionOnce] is the entry point one partition cycle runs: claim
// the partition lease, [SelectPartitionBatch] a batch of ready intents,
// retract/write their canonical edges through the caller-supplied
// [sharedintent.EdgeWriter], mark the batch completed, release the lease.
// [Runner] loops [ProcessPartitionOnce] across every domain
// and partition, sequentially or with a bounded worker pool.
//
// # Why this is a leaf
//
// Before this package existed, this machinery lived in the reducer root
// beside the domain families that need it, so a family that wanted the
// generic worker (or a symbol it exported) had to import the root — and the
// root imports the families. That is the import cycle issue #6061 keeps
// running into: 23-plus domains and 7 dedicated projection runners depend on
// this substrate today, and none of them could become a subpackage while it
// stayed in the same package as the families it drains.
//
// This package therefore holds the worker's concurrency core: partition
// selection and dedup, lease claim/heartbeat/release, readiness and
// property-keyed presence gating, the repo-wide-retract fence, and the
// runner's config/telemetry. It depends on the already-hoisted leaves
// [sharedintent] (intent row shape, ports, pure row/partition helpers),
// [gpphase] (readiness phase/keyspace vocabulary, presence-key derivations),
// and [contract] (the Domain* constants the shared-projection domain list and
// readiness-phase mapping read), plus internal/reducer/payloadcore,
// internal/reducer/inheritance (one evidence-source constant),
// internal/cpubudget, internal/telemetry, and the standard library — and it
// must never import the reducer root.
//
// # What stays at the root
//
// The seven dedicated projection runners (code/call/projection,
// repo_dependency_projection_*) and the shared-projection edge-materialization
// handlers stay in the reducer root for now: they are a later hoist in issue
// #6061's PR sequence. They call this package's exported surface —
// [DefaultBatchLimit], [DefaultLeaseTTL], [DefaultPollInterval],
// [DefaultEvidenceSource], [MergePartitionProcessResult],
// [MaxIntentWaitSeconds], [RecordStepDurations],
// [AcceptanceTelemetry], [AcceptanceLookupEvent],
// [ReadinessPhase], and [GraphProjectionPhaseKeyForAcceptance]
// — through thin root forwarders/aliases under their original unexported
// spellings, so no call site in those still-root files changed.
//
// The root also keeps aliases and forwarders under its original,
// pre-#6061 exported spellings for its own and cross-package callers, each
// pointing at this package's de-stuttered name: SharedProjectionRunner
// ([Runner]), SharedProjectionRunnerConfig ([RunnerConfig]),
// DefaultSharedProjectionLeaseOwnerPrefix ([DefaultLeaseOwnerPrefix]),
// LoadSharedProjectionConfig ([LoadConfig]), PartitionProcessorConfig,
// ProcessPartitionOnce, SharedProjectionPartitionCandidateReader
// ([PartitionCandidateReader]), SharedProjectionUnhashedCandidateReader
// ([UnhashedCandidateReader]), LatestIntentsByRepoAndPartition,
// FilterAuthoritativeIntents, SelectionPhaseDurations, FirstProjectionLookup,
// and SharedProjectionUnroutableWriter (aliased to [UnroutableWriter]).
//
// # Naming
//
// This package's own exported surface drops the Shared/SharedProjection
// prefix that stuttered against its own intents/shared/worker path (e.g.
// [Runner] not SharedProjectionRunner, [IntentReader] not SharedIntentReader,
// [RecordStepDurations] not RecordSharedProjectionStepDurations). The three
// names kept as-is — [LatestIntentsByRepoAndPartition],
// [FilterAuthoritativeIntents], and repair.Repairer in the sibling
// intents/phase/repair package — do not stutter against this path and are
// unchanged. The root spellings above are unaffected: they are the
// pre-existing public surface and stay byte-identical for every caller
// outside the reducer.
package worker
