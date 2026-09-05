// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticentity turns parser-emitted content_entity facts into
// canonical Annotation, Typedef, TypeAlias, TypeAnnotation, Component,
// Module, ImplBlock, Protocol, ProtocolImplementation, Variable, and
// callable Function semantic nodes, and writes them into the graph backend.
//
// The family owns one handler, [SemanticEntityMaterializationHandler], and
// the extraction pipeline behind it: [ExtractSemanticEntityRows] and
// [ExtractSemanticEntityRowsForRepo] filter a generation's content_entity
// facts down to the entity types and per-language callable heuristics that
// qualify as a semantic entity (see isSemanticEntityType and its
// per-language helpers in materialization_helpers.go), then sort the
// resulting [SemanticEntityRow] values into a deterministic write order.
// [SemanticEntityWriter] persists them; the canonical Cypher-backed
// implementation lives in internal/storage/cypher.
//
// # Delta scoping
//
// extractSemanticDeltaProjectionScope narrows the write and retract to the
// changed and deleted files a delta generation reports, instead of the whole
// repository. Which reading applies (file-scoped vs. repo-wide) is a
// per-repository decision carried on each generation's repository fact, not
// a scope-wide one.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches reducer/contract,
// reducer/factload, reducer/gpphase, reducer/payloadcore, internal/facts and
// pkg/log, and never
// the parent internal/reducer package. The dependency runs the other way —
// the root's handler catalog constructs [SemanticEntityMaterializationHandler]
// and wires its FactLoader, Writer,
// PriorGenerationCheck and PhasePublisher, and its RepairQueue when the root
// repair queue is present. See AGENTS.md in this
// directory before adding an import.
//
// [GraphProjectionPhaseRepairQueue] and [GraphProjectionPhaseRepair] are
// declared locally in graph_ports.go rather than imported from the reducer
// root, because the root's equivalents are still shared production logic for
// families that have not moved out of root yet. Go requires exact type
// identity for a method whose parameter names a struct, so the root's
// concrete repair queue cannot satisfy this package's interface directly
// even though every GraphProjectionPhaseRepair field matches; the root wires
// it through semanticEntityRepairQueueAdapter
// (internal/reducer/semantic_entity_repair_queue_adapter.go), a narrow
// translation between the two named types, not a duplicated implementation.
//
// # Observability
//
// This package registers no metric instrument of its own. The
// semantic_entity_materialization domain runs as a standard reducer
// execution covered by eshu_dp_reducer_executions_total and
// eshu_dp_reducer_run_duration_seconds, under the reducer.run span. The
// domain is an attribute on those metrics rather than a span of its own.
// [SemanticEntityMaterializationHandler.Handle] additionally emits a
// "semantic entity materialization completed" structured log carrying
// fact_count, repo_count, row_count, skip_retract, delta_projection,
// delta_file_count, and the per-stage wall-time fields named in that log
// call.
package semanticentity
