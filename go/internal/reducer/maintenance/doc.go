// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package maintenance runs the reducer's periodic side-runners: generation
// liveness recovery, poison dead-letter liveness recovery (#4740), generation
// retention pruning, the graph orphan sweep, the repo-dependency
// activation-gate decorators, and the collector-readiness evidence summary
// resweep (#3466). Each one runs beside the main claim/execute/ack loop as a
// [reducer.Service] side-runner; none owns intent claiming or graph
// projection itself.
//
// [GenerationLivenessRunner] re-drives active generations that wedge past
// their activation deadline and supersedes orphaned older actives.
// [PoisonLivenessRunner] closes the gap it cannot reach: a scope whose newest
// generation is terminally dead_letter. [GenerationRetentionRunner] prunes
// superseded source-generation history in bounded transactions.
// [GraphOrphanSweepRunner] marks and deletes aged zero-relationship graph
// nodes under a single-owner partition lease. [GateAcceptedGenerationOnActive]
// and [GateAcceptedGenerationPrefetchOnActive] gate repo-dependency
// graph-projection authority on the relationship generation being active
// (published) in Postgres, closing the dual-write graph-ahead-of-Postgres
// window. [CollectorEvidenceSummaryMaintainer] keeps the
// collector_evidence_summary read model reconciled with the active fact set
// via a lease-guarded periodic atomic resweep.
//
// AcceptedGenerationLookup, AcceptedGenerationPrefetch, and
// PartitionLeaseManager are declared locally as mirrors of the identically
// named reducer-root contracts (shared_projection.go,
// shared_projection_worker.go): this package never imports internal/reducer
// (issue #6061). AcceptedGenerationLookup is a type alias, so a root-typed
// value interoperates with this package's functions without conversion.
// AcceptedGenerationPrefetch is also declared as a type alias, but its return
// type nests AcceptedGenerationLookup one level down, where the root and this
// package's named return types are no longer identical -- so, unlike the
// lookup alone, a root-typed AcceptedGenerationPrefetch value is NOT
// assignable to this package's functions without conversion; see AGENTS.md
// for the boundary adapter this requires. PartitionLeaseManager is a plain
// interface, satisfied structurally by whatever concrete lease store the
// root wires in.
//
// The exported surface is [GenerationLivenessRunner],
// [GenerationLivenessPolicy], [GenerationLivenessResult],
// [GenerationLivenessRecoverer], [GenerationLivenessRunnerConfig],
// [PoisonLivenessRunner], [PoisonLivenessPolicy], [PoisonLivenessResult],
// [PoisonLivenessRecoverer], [PoisonLivenessRunnerConfig],
// [GenerationRetentionRunner], [GenerationRetentionPolicy],
// [GenerationRetentionResult], [GenerationRetentionPruner],
// [GenerationRetentionRunnerConfig], [GraphOrphanSweepRunner],
// [GraphOrphanSweepPolicy], [GraphOrphanSweepResult], [GraphOrphanSweeper],
// [GraphOrphanSweepRunnerConfig], [ErrGraphOrphanSweeperRequired],
// [RelationshipGenerationActiveLookup], [GateAcceptedGenerationOnActive],
// [GateAcceptedGenerationPrefetchOnActive], [AcceptedGenerationLookup],
// [AcceptedGenerationPrefetch], [PartitionLeaseManager],
// [CollectorEvidenceSummaryMaintainer], [CollectorEvidenceSummaryRebuilder],
// [CollectorEvidenceSummaryLeaseManager], [CollectorEvidenceFreshnessLookup],
// and [CollectorEvidenceSummaryDomain].
package maintenance
