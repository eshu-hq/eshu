// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repair drains the exact graph-projection-phase repair queue and
// republishes missing readiness rows when their bounded generation is still
// authoritative (issue #6061; moved here from the reducer root's
// graph_projection_phase_repair_runner.go).
//
// [Repairer] is the entry point: [Repairer.Run] polls [Repairer.RunOnce] in a
// loop with exponential backoff on empty or failed cycles. One cycle lists
// due repairs from a [gpphase.PhaseRepairQueue], checks whether the phase is
// already published (in which case the row is simply deleted), checks
// whether the row's generation is still accepted (a stale row is deleted
// rather than replayed), and republishes through a [gpphase.PhasePublisher]
// otherwise — marking the row failed with a retry-delay deadline on a
// further publish error.
//
// # Why this is separate from gpphase
//
// [gpphase] owns the repair SHAPE ([gpphase.PhaseRepair],
// [gpphase.PhaseRepairQueue], [gpphase.PhaseRepairsFromStates]) because that
// shape is plain data and pure builders, the same bar every other gpphase
// symbol holds. This package owns the repair QUEUE DRAIN: it is
// orchestration over a queue and a publisher, with its own polling loop,
// backoff, and telemetry — a different kind of thing, and one no domain
// family needs to import directly.
//
// This package imports [gpphase] (the repair shape and phase/keyspace
// vocabulary), [sharedintent] (the accepted-generation lookup and acceptance
// key), and internal/telemetry, plus the standard library. It must never
// import the reducer root.
//
// The root keeps aliases under the original names — GraphProjectionPhaseRepairer
// (aliased to [Repairer]) and GraphProjectionPhaseRepairerConfig (aliased to
// [Config]) — so cmd/reducer's construction and internal/reducer's
// service.go field keep their existing spelling.
package repair
