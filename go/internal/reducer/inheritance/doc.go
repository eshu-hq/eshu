// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package inheritance turns parser-emitted type-hierarchy metadata into durable
// shared-projection intents for the INHERITS, IMPLEMENTS, OVERRIDES and ALIASES
// edges.
//
// The family owns one handler, [MaterializationHandler], and the pipeline behind
// it: [ExtractRows] indexes a generation's content_entity facts by
// (repo_id, entity_name) and resolves each declared base, implemented interface
// and PHP trait adaptation against that index, producing canonical child/parent
// edge rows. [BuildSharedIntentRows] then promotes those rows to intents.
// Resolution is intra-repository name matching only; a base that names no
// in-corpus entity yields no edge, and cross-repository inheritance is out of
// scope.
//
// # Two kinds of intent, and why
//
// Each repository gets exactly one whole-scope refresh intent
// ([BuildRefreshIntents]) that owns the domain's single retract, and each edge
// gets a write-only per-edge intent under a file-scoped partition key, marked
// retract_via_refresh so the worker fences the write behind that refresh
// (#2867/#2898). The per-edge key hashes the repo, the child's path and the edge
// identity rather than the file alone, because the partitioned runner
// deduplicates by (acceptance key, partition key): two edges sharing one key
// would collapse and one would be silently dropped.
//
// The retract the refresh owns is repo-wide by default and file-scoped on a
// delta generation, carried by [DeltaScope]. Which of those applies is a
// per-repository decision, never a scope-wide one; see
// [sharedintent.ApplyRepoRefreshDeltaScope] for the two readings that lose edges.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [factload], [payloadcore], [sharedintent], internal/codeprovenance,
// internal/facts and pkg/log, and never the parent internal/reducer package.
// The dependency runs the other way — the root's handler catalog constructs
// [MaterializationHandler] and its shared-projection runner names
// [EvidenceSource]. See AGENTS.md in this directory before adding an import.
//
// # Observability
//
// This package registers no metric instrument of its own. The
// inheritance_materialization domain runs as a standard reducer execution
// covered by eshu_dp_reducer_executions_total and
// eshu_dp_reducer_run_duration_seconds, under the reducer.run span. The
// domain is an attribute on those metrics rather than a span of its own.
// The span carries no domain attribute, so it narrows to reducer
// executions in general: isolate this family through the domain-tagged
// metrics and the structured logs below, not by filtering traces. What the family adds is diagnostic
// detail on the result and in the logs: [MaterializationHandler.Handle] emits
// per-phase wall-times through Result.SubDurations (load_facts, build_intents,
// upsert_intents, total) and the input_ready / written_rows signals through
// Result.SubSignals, plus an "inheritance materialization fact inputs" log line
// carrying content_entity_facts and entities_with_declared_parent. Those three
// counts are what distinguishes an upstream ordering stall from genuinely empty
// work when the rc-12 (INHERITS) gate goes intermittently red on loaded CI and
// does not reproduce locally (#3873).
package inheritance
