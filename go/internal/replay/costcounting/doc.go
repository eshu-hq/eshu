// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package costcounting implements R-16 deterministic cost-counting assertions
// for the Eshu deterministic replay framework (epic #4102).
//
// # What it proves
//
// Each golden scenario drives the production [storage/cypher.CanonicalNodeWriter]
// through an in-memory counting executor backed by a real
// [go.opentelemetry.io/otel/sdk/metric.ManualReader] so the production
// eshu_dp_* instruments record their actual values without a graph backend.
// After the Write call the test collects those instrument values and asserts
// each one is within a committed per-scenario budget.
//
// This catches algorithmic regressions (N+1 writes, quadratic fan-out) that do
// not show up in unit tests because they only surface when the real projection
// writer runs over a realistic materialization size.
//
// # Relation to Epic B
//
// R-16 complements the Epic B B-8/B-9 wall-clock bench gates: counts here,
// nanoseconds there. Both share the same deterministic cassette corpus (R-6
// refresh path) so the two gates converge on one reviewable diff.
//
// # Instruments read
//
// The assertions read these production eshu_dp_* instruments via the manual
// reader, not a hand-counted statement list:
//
//   - eshu_dp_canonical_atomic_writes_total — incremented by the canonical
//     writer's recordAtomicWrite helper on each write-mode event
//     ([storage/cypher.CanonicalNodeWriter.Write]).
//   - eshu_dp_canonical_nodes_written_total and
//     eshu_dp_canonical_edges_written_total are registered but currently 0
//     because the in-memory executor doesn't run real graph queries.
//
// The counting executor also tracks the raw statement count as a secondary
// signal for diagnostics; the PRIMARY budget assertion is always the
// instrument value read off the otel reader.
//
// # Negative control
//
// Each test file includes a deliberately-N+1 projection variant that invokes
// the writer once per directory item, producing a statement count that scales
// linearly with input size rather than staying constant. The cost assertion
// on the same budget MUST fail for this variant, proving the gate catches
// algorithmic blowups. This is a real negative control: the N+1 variant
// drives the SAME production writer the positive scenario does and reads the
// SAME instrument off the SAME reader; removing the assertion makes the test
// fail loudly.
//
// # Budget files
//
// Each scenario cassette is accompanied by a .cost-budget.json file in the
// same testdata directory. The file records the maximum allowed value for each
// asserted instrument. Budgets are generated alongside cassettes (R-6 refresh
// path) and their diff is reviewable.
package costcounting
