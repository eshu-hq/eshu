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
// MirrorPaths is called by the content writer after it commits a Write. It
// re-derives the given paths of one repository in one transaction per chunk:
// take the per-repository advisory lock, delete the chunk's rows, and insert
// them again from content_entities. The lock comes first so the insert reads a
// snapshot that postdates any other deriver's commit for that repository.
// MirrorRepo does the same for a whole repository and is what the backfill
// runs. Both are idempotent: replaying a call converges on the same rows.
// Both return an error when the database cannot begin transactions rather than
// writing without the lock.
package inventory
