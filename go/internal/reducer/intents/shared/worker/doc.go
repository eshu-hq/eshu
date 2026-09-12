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
// running into: 23-plus domains and the dedicated projection runners
// (code-call and repo-dependency) depend on this substrate, and none of them could become a subpackage while it
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
// # Who calls this package
//
// The code-call projection runner (code/call/projection) imports this
// package directly. The repo-dependency projection runner and the
// shared-projection edge-materialization handlers still live in the
// reducer root.
//
// Most of the root's own test files (and one production file,
// repo_dependency_projection_concurrency_proof.go) call this package's
// exported surface directly — [ReadinessPhase], [ReadinessKeyspace],
// [FilterRowsByReadiness], [SelectPartitionBatch], [RowUsesRefreshFence],
// [PlanRepoWideRetractWork], and [GraphProjectionPhaseKeyForRow] — rather
// than through a root forwarder: those forwarders had no caller outside
// the reducer root's own tests, so the H5 root-remnant fold (issue #6061
// decision D12) deleted them instead of keeping them as compat entries.
//
// The root keeps aliases and forwarders under their original, pre-#6061
// exported spellings only for the names that still have a caller outside
// this package's own tests: SharedProjectionRunner ([Runner]),
// SharedProjectionRunnerConfig ([RunnerConfig]),
// DefaultSharedProjectionLeaseOwnerPrefix ([DefaultLeaseOwnerPrefix]),
// LoadSharedProjectionConfig ([LoadConfig]), PartitionProcessorConfig,
// PartitionProcessResult, ProcessPartitionOnce,
// SharedProjectionPartitionCandidateReader ([PartitionCandidateReader]),
// SharedProjectionUnhashedCandidateReader ([UnhashedCandidateReader]),
// LatestIntentsByRepoAndPartition, FilterAuthoritativeIntents,
// SharedProjectionRefreshFenceLookup ([RefreshFenceLookup]),
// FirstProjectionLookup, SharedProjectionUnroutableWriter (aliased to
// [UnroutableWriter]), sharedAcceptanceLookupEvent
// ([AcceptanceLookupEvent]), sharedAcceptanceTelemetry
// ([AcceptanceTelemetry]), the unexported default* constants (aliased to
// [DefaultBatchLimit], [DefaultPollInterval], [DefaultEvidenceSource]),
// and sharedProjectionDomains (copied from [Domains]'s result once at
// init, since Go has no cross-package var/const alias).
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
