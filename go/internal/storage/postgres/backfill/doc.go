// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package backfill owns the deferred relationship backfill engine: the
// corpus-wide backward-evidence discovery pass, the bounded per-repository
// batch writes behind it, the per-partition fan-in that publishes readiness
// and the partition memo, the per-commit new-repository backfill, and the
// generation freshness guard the commit path consults. It was extracted from
// the postgres root (issue #7794) so the targeted-maintenance family (#7648)
// can nest behind the same seam.
//
// A pass discovers evidence with relationships.DiscoverEvidence over
// anchor-scoped facts, commits it in bounded per-repository-batch
// transactions that each hold only their own repositories' exclusive
// maintenance locks, then publishes backward-evidence readiness and the
// partition memo in a per-partition fan-in. No step holds a fleet-wide lock.
// The memo gate lets later passes skip partitions whose evidence already
// committed under the current catalog fingerprint, and the whole-pass fan-in
// then leaves those partitions unpublished when the pass inserted no evidence
// under them and their readiness row exists.
//
// Store carries the engine's durable handles: the database, the transaction
// beginner, the batch-size and worker-count overrides, the clock, the logger,
// and the instruments, plus the two seams to root-owned behavior the engine
// must not import: the catalog loader and the evidence-store constructor.
// The root IngestionStore builds a Store with all seams supplied; the
// exported entry points the rest of the program uses stay methods on
// IngestionStore in the parent package and delegate here.
//
// ListDeployableUnitReopenCandidatesQuery and its fragments gate the
// deployable_unit_correlation reopen on the input fingerprint the item's last
// success recorded (#8283); RecordDeployableUnitReopenFingerprintQuery is the
// claim-fenced write that records it.
//
// DecideSuccessor and DecideDeltaFileUnion hold the verdict of the #7926
// destructive-successor backstop (#8750). A full successor is compared with
// the prior generation's collector rows, never rows a reducer wrote back into
// it. A git delta successor is sized by the file-level union it will project
// over its anchor generation: a zero-query lower bound stores ordinary
// deltas, and only a delta that may remove most of the anchor's files is
// probed and refused on its exact union. The functions do no I/O; the root
// supplies the counts and records the decision.
//
// The package imports the db contracts and the leaf facts, relationships,
// reducer contract, scope, and telemetry packages, never the parent postgres
// package.
package backfill
