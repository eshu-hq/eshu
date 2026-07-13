// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package costcounting implements R-16 deterministic cost-counting assertions
// for the Eshu deterministic replay framework (epic #4102).
//
// # What it proves
//
// Each golden scenario drives a production reducer graph writer through an
// in-memory counting executor backed by a real
// [go.opentelemetry.io/otel/sdk/metric.ManualReader] so the production
// eshu_dp_* instruments record their actual values without a graph backend.
// After the write call the test collects those instrument values and asserts
// each one is within a committed per-scenario budget.
//
// This catches algorithmic regressions (N+1 writes, quadratic fan-out) that do
// not show up in unit tests because they only surface when the real projection
// writer runs over a realistic materialization size.
//
// # Scenarios (C-14, issue #4367)
//
// One scenario per distinct reducer_domain (specs/fact-kind-registry.v1.yaml),
// each driving that domain's production graph writer:
//
//   - code_graph_projection (cost_counting_test.go) drives
//     [storage/cypher.CanonicalNodeWriter] over a committed cassette
//     materialization (testdata/cassettes/replayoffline/nested-directory-tree.json).
//     The repository/directory canonical writes it exercises ARE the
//     code-graph canonical projection path (canonical_code_graph
//     projection_hook), so this scenario also backs the projection:code_graph_projection
//     coverage claim without needing a second test.
//   - semantic_entity_materialization (semantic_entity_cost_test.go) drives
//     [storage/cypher.SemanticEntityWriter.WriteSemanticEntities] over two
//     in-package fixture rows through a [storage/cypher.InstrumentedExecutor] —
//     the same wrapper go/cmd/reducer/observed_service_wiring.go applies to
//     the real Neo4j/NornicDB executor.
//   - documentation_materialization (documentation_edges_cost_test.go) drives
//     [storage/cypher.EdgeWriter.WriteEdges] over two in-package fixture rows
//     with EdgeWriter.Instruments set — the same field
//     go/cmd/reducer/endpoint_presence_wiring.go newHandlerEdgeWriter sets on
//     the production edge writer.
//
// The semantic-entity and documentation-edges scenarios have no committed
// cassette: their writers operate over flat reducer rows
// (reducer.SemanticEntityRow, reducer.SharedProjectionIntentRow), not a
// projector.CanonicalMaterialization, so their deterministic input is an
// in-package Go literal fixture (the same convention semantic_entity_test.go
// already uses), and their budget JSON records that explicitly in place of a
// cassette path.
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
//   - eshu_dp_canonical_atomic_writes_total (code_graph_projection) —
//     incremented by the canonical writer's recordAtomicWrite helper on each
//     write-mode event ([storage/cypher.CanonicalNodeWriter.Write]).
//     eshu_dp_canonical_nodes_written_total and
//     eshu_dp_canonical_edges_written_total are registered but currently 0
//     because the in-memory executor doesn't run real graph queries.
//   - eshu_dp_neo4j_batches_executed_total (semantic_entity_materialization) —
//     incremented once per UNWIND-shaped statement (a statement whose
//     Parameters carry a "rows" key) by
//     [storage/cypher.InstrumentedExecutor]'s recordStatementBatchMetrics
//     helper on Execute or ExecuteGroup.
//   - eshu_dp_shared_edge_write_groups_total (documentation_materialization) —
//     incremented once per grouped [storage/cypher.EdgeWriter.WriteEdges]
//     transaction by EdgeWriter.recordGroupedWrite.
//
// The counting executor also tracks the raw statement count as a secondary
// signal for diagnostics; the PRIMARY budget assertion is always the
// instrument value read off the otel reader.
//
// # Negative control
//
// Each scenario includes a deliberately-N+1 projection variant that invokes
// its writer once per input row/directory item instead of once for the whole
// batch, producing a count that scales linearly with input size rather than
// staying constant. The cost assertion on the same budget MUST fail for this
// variant, proving the gate catches algorithmic blowups. This is a real
// negative control: the N+1 variant drives the SAME production writer the
// positive scenario does and reads the SAME instrument off the SAME reader;
// removing the assertion makes the test fail loudly.
//
// # Budget files
//
// Each cassette-driven scenario is accompanied by a .cost-budget.json file in
// the same testdata directory; the in-package-fixture scenarios (semantic
// entity, documentation edges) commit their budget file to the same directory
// with a "cassette" field explaining there is no cassette. The file records
// the maximum allowed value for each asserted instrument. Cassette-backed
// budgets are generated alongside cassettes (R-6 refresh path); the two
// fixture-backed budgets are hand-edited alongside their fixture rows in the
// same reviewed diff. Both keep the diff reviewable.
package costcounting
