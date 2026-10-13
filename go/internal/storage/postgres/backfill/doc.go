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
// committed under the current catalog fingerprint.
//
// Store carries the engine's durable handles: the database, the transaction
// beginner, the batch-size and worker-count overrides, the clock, the logger,
// and the instruments, plus the two seams to root-owned behavior the engine
// must not import: the catalog loader and the evidence-store constructor.
// The root IngestionStore builds a Store with all seams supplied; the
// exported entry points the rest of the program uses stay methods on
// IngestionStore in the parent package and delegate here.
//
// The package imports the db contracts and the leaf facts, relationships,
// scope, telemetry, and workflow packages, never the parent postgres package.
package backfill
