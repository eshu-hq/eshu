// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package inventory maintains and reads infra_resource_entities, the Postgres
// read model behind the infra resource aggregate routes (#6793).
//
// The table holds one narrow row per entity-derived infrastructure node, keyed
// by entity_id. Rows are copied from content_entities for the closed label set
// in Labels, with every dimension the aggregate readers filter or group on
// promoted into its own column. The canonical node writer writes the same
// labels from the same content rows with the same trimmed metadata values, so
// the table's per-label and per-dimension counts equal the graph's for those
// labels. Labels outside Labels stay graph-only; see Labels for why.
//
// Mirror is called by the content writer after it commits a Write. It first
// drops the rows of every tombstoned entity id (an entity can have moved away
// from the path its tombstone names), then calls MirrorPaths, which
// re-derives the given paths of one repository in one transaction per chunk:
// take the per-repository advisory lock, delete the chunk's rows, and insert
// them again from content_entities. The lock comes first so the insert reads a
// snapshot that postdates any other deriver's commit for that repository.
// MirrorRepo does the same for a whole repository and is what the backfill
// runs. Both are idempotent: replaying a call converges on the same rows.
// Both return an error when the database cannot begin transactions rather than
// writing without the lock.
//
// Generation retention calls LockRepositoriesForGenerations before it prunes
// content_entities and DeleteOrphanedRows after, inside its own transaction,
// so the table never keeps a row whose content row was pruned.
//
// Backfiller.Run derives every existing repository and then records
// BackfillMarker. BackfillComplete reports whether the marker exists. Readers
// must not serve table counts before it does, because until then the table
// may cover only part of the corpus. Reader, CountBuckets, and
// DimensionBuckets are the aggregate reads. Their filters and "unknown"
// buckets mirror the graph aggregate readers clause for clause for the labels
// in Labels.
//
// ReconcileCycle is the reducer's drift repair. A content writer that does
// not derive (for example an older binary during a rolling upgrade) leaves
// rows the backfill marker cannot detect. Once the marker exists, each cycle
// re-checks the previous cycle's suspects, then walks up to the rest of a
// budget of repositories claimed from a shared persisted cursor (ClaimPage),
// so replicas take disjoint pages and a restarted process resumes the walk.
// ReconcileRepo compares (row count, sum of a per-row hash) of a
// repository's infra content rows against its table rows without a lock. On
// the walk a mismatch is only ReconcileSuspect, because a Write's content rows
// commit before its derive; a suspect that still differs on the next cycle is
// re-checked under the repository's derive lock and re-derived in that
// transaction with MirrorRepo's delete and insert.
//
// Migration 109's content_entities triggers fence rolling upgrades: an
// infra-typed write from a connection without WriterSessionSQL (an older
// binary, or manual SQL) marks its repository dirty in the same statement.
// ReadModelReady keeps readers on the graph while any mark exists, and each
// ReconcileCycle repairs marked repositories first (ReconcileFenced).
package inventory
