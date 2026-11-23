// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package completionstore holds the Postgres-backed cross-scope completion
// queue: producers publish completion events, the reducer claims and fans
// them out to eligible consumers, and consumers defer until every producer
// domain they read through reports ready.
//
// CrossScopeCompletionStore is the durable completion-event queue over the
// cross_scope_completion_events table (Claim, Heartbeat, Retry, Fanout,
// NewCrossScopeCompletionStore; FanoutCrossScopeCompletionQuery is exported
// for the staying root plan tests, which EXPLAIN the shipped query rather
// than a copy). Fanout schedules the active-generation consumers whose
// handler reads the producer's output and never a manifest marker (#10972).
// For a producer that feeds supply_chain_impact it then counts the markers it
// left alone into ManifestMarkersSkipped with a separate read-only statement
// run after the fan-out committed, under its own bounded context; a failed
// count logs a warning and leaves the field zero without failing the committed
// fan-out. CrossScopeProducerReadinessStore answers per-producer-domain
// readiness through ProducerScopeQuiescence, which reports every registered
// ingestion scope under a set of collector kinds alongside the
// quiescent-active subset: a kind with no registered scope at all is ready,
// not blocked.
//
// It is the `scope/completion/` leaf of the storage/postgres split (#6693),
// nested under the `scope/` leaf. This package must not import the parent
// postgres package. Its tests import root postgres only for the SQLDB
// wrapper type and ApplyBootstrap; Go test-only symbols do not cross package boundaries, so
// seed helpers shared with the staying root completion tests are kept as
// twin copies with a pointer comment on each side.
package completionstore
