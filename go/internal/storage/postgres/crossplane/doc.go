// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package crossplanestore holds the Postgres-backed Crossplane cross-scope
// SATISFIED_BY redrive sweep: when a scope generation carrying an active
// CrossplaneXRD activates, the sweep discovers OTHER scopes' active,
// unresolved K8sResource Claims matching that XRD's (group, claim_kind) and
// durably requests each target scope's SATISFIED_BY materialization on its
// stable reducer work item. This closes the XRD-lag false-negative window
// (issue #5476).
//
// CrossplaneSatisfiedByRedriveSweeper runs the durable, bounded, paged sweep.
// Sweep is the live post-Ack trigger path; SweepBatch is the startup/periodic
// catch-up recovery path; both share runClaimedFanOut. CrossplaneRedriveStateStore
// durably tracks claim/completion per (xrd_scope_id, xrd_generation_id) with
// FOR UPDATE SKIP LOCKED convergence and fencing-token completion.
// CrossplaneRedriveTargetLedgerStore durably records already-satisfied
// (target scope, XRD identity) pairs; only
// crossplane.CrossplaneSatisfiedByMaterializationHandler writes it, strictly
// after committing a SATISFIED_BY edge, never the sweep.
//
// It is the `crossplane/` leaf of the storage/postgres split (#6693). This
// package must not import the parent postgres package. Its tests import root
// postgres only for the SQLDB/SQLQueryer wrapper types, ApplyBootstrap, and
// the ReducerQueue/FactStore constructors; Go test-only symbols do not cross
// package boundaries, so the shared seed and assertion helpers live in this
// package's own external test files.
package crossplanestore
