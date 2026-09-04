// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rationale turns parser-emitted intent-comment metadata (WHY/HACK/
// NOTE/TODO/FIXME) into durable shared-projection intents for the EXPLAINS
// edge from an identity-only Rationale node to the code entity the comment
// precedes (issue #2230). Comment text itself stays in the Postgres
// content/fact store (design 430); this package only ever handles the
// identity-stable rationale_uid and the entity it explains.
//
// The family owns one handler, [MaterializationHandler], and the pipeline
// behind it: [ExtractRows] reads a generation's content_entity facts for
// parser-emitted rationale_comments metadata and produces one row per distinct
// (entity, comment kind, comment text). [BuildSharedIntentRows] then promotes
// those rows to intents. Rationale materialization consumes that
// parser-emitted comment metadata and emits exact EXPLAINS intents plus one
// repository refresh; edge intents retain repo-relative target paths for
// partition identity, while delta refreshes use separately
// repository-qualified delta paths so canonical stale edges retract.
//
// # Two kinds of intent, and why
//
// Each repository gets exactly one whole-scope refresh intent
// ([BuildRefreshIntents]) that owns the domain's single retract, and each edge
// gets a write-only per-edge intent under a file-scoped partition key, marked
// retract_via_refresh so the worker fences the write behind that refresh
// (#2869/#2898). The per-edge key hashes the repo, the target entity's
// repo-relative path, and the edge identity rather than the file alone,
// because the partitioned runner deduplicates by (acceptance key, partition
// key): two edges sharing one key would collapse and one would be silently
// dropped.
//
// The retract the refresh owns is repo-wide by default and file-scoped on a
// delta generation, carried by [DeltaScope]. Which of those applies is a
// per-repository decision, never a scope-wide one; see
// [sharedintent.ApplyRepoRefreshDeltaScope] for the two readings that lose
// edges.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [factload], [payloadcore], [sharedintent], [schemadecode], internal/facts
// and pkg/log, and never the parent internal/reducer package. The dependency
// runs the other way -- the root's handler catalog constructs
// [MaterializationHandler] and its shared-projection runner names
// [EvidenceSource].
//
// # Observability
//
// This package registers no metric instrument of its own. The
// rationale_materialization domain runs as a standard reducer execution
// covered by eshu_dp_reducer_executions_total and
// eshu_dp_reducer_run_duration_seconds, under the reducer.run span. The
// domain is an attribute on those metrics rather than a span of its own, and
// the span carries no domain attribute either, so isolate this family through
// the domain-tagged metrics and the structured logs rather than by filtering
// traces. [MaterializationHandler.Handle] emits an "rationale materialization
// started"/"rationale materialization completed" structured log pair
// carrying edge_count, repo_count and intent_count.
package rationale
