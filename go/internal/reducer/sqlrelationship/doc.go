// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sqlrelationship materializes the sql_relationship_materialization
// reducer domain: it derives canonical SQL relationship edges --
// READS_FROM, WRITES_TO, REFERENCES_TABLE, HAS_COLUMN, TRIGGERS, EXECUTES,
// INDEXES, MIGRATES, and QUERIES_TABLE -- from content_entity facts describing
// SQL tables, columns, views, functions, triggers, indexes, and migrations,
// plus embedded-SQL evidence carried on parsed file facts, and publishes them
// as durable, file-scoped shared-projection intents (#2868).
//
// [ExtractSQLRelationshipRows] builds an in-memory index of every SQL entity
// in the batch, then derives edges from each entity's metadata (referenced
// tables, source tables, table/function names, migration targets), resolving
// each target name against the index with same-repo, prefer-same-file, and
// dual-kind (SqlTable-then-SqlView) matching rules a caller can inspect
// through the returned SQLRelationshipRowStats rather than a silent drop.
// [SQLRelationshipMaterializationHandler.Handle] wraps that extraction with
// fact loading, delta-scope detection, and promotion to
// [BuildSharedIntentRows]' file-scoped per-edge intents plus one whole-scope
// per-repo refresh intent, reusing the #2898 refresh-fence mechanism so the
// generic partitioned worker (reducer root) can project them concurrently
// without a cross-partition retract race (#2910).
//
// Five names are exported beyond what this package's own Handle path needs
// because the shell_exec family, which has not moved out of the reducer root
// yet, reuses this exact delta-scope and embedded-code-index machinery for its
// own materialization rather than duplicating it: DeltaScope and
// BuildDeltaScope and MergeRepositoryIDs (shell_exec_materialization.go,
// shell_exec_intents.go) and EmbeddedSQLFunctionIDsByNameLine and
// EmbeddedSQLFunctionKey (shell_exec_materialization.go), issue #6061.
//
// EvidenceSource, FilePartitionKey, WholeScopePartitionKey and
// PartitionKeyVersion are NOT consumed by shell_exec. They are exported for
// the reducer root's generic refresh-fence and partition-convergence proofs;
// AGENTS.md names each consumer.
//
// BuildRefreshIntents, BuildSharedIntentRows and BuildRetractRows are exported
// for a different reason. shell_exec reuses none of them — it owns
// buildShellExecRefreshIntents in shell_exec_intents.go. Their callers outside
// this package are all reducer-root tests:
//
//   - BuildRefreshIntents: sibling_edge_intent_delta_gate_test.go, the shared
//     table that drives this family and inheritance through one set of
//     assertions.
//   - BuildSharedIntentRows: sql_relationship_partition_convergence_test.go and
//     sibling_edge_intent_retract_reachability_test.go.
//   - BuildRetractRows: sql_relationship_partition_convergence_test.go, which
//     asserts the retract and refresh partition keys converge — a property only
//     a caller holding both halves can check. It also has one in-package
//     caller, its own test in sql_relationship_delta_scope_test.go, so the
//     export exists for the root test rather than for lack of any caller.
//
// This package imports [github.com/eshu-hq/eshu/go/internal/reducer/contract]
// (the dependency-neutral domain/intent/result vocabulary),
// [github.com/eshu-hq/eshu/go/internal/reducer/factload] (the scoped fact
// loader), [github.com/eshu-hq/eshu/go/internal/reducer/payloadcore]
// (deref/trim/convert helpers), [github.com/eshu-hq/eshu/go/internal/reducer/schemadecode]
// (the typed-payload decode seam), and
// [github.com/eshu-hq/eshu/go/internal/reducer/sharedintent] (the shared
// projection intent builder and refresh-fence helpers), plus
// [github.com/eshu-hq/eshu/go/internal/facts],
// [github.com/eshu-hq/eshu/go/pkg/log] (the structured-log helpers
// sql_relationship_materialization.go uses), and the generated
// sdk/go/factschema/codegraph/v1 package. It must never import the parent
// reducer package or a sibling domain-family subpackage -- see AGENTS.md.
package sqlrelationship
