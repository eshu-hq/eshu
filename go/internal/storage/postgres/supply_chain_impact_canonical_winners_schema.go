// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Maintained cross-scope canonical winner per supply-chain impact finding
// (#3389). See docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md.
//
// The impact-findings list endpoint deduplicates at read time with
// ROW_NUMBER() OVER (PARTITION BY canonical_key ...), which sorts the full
// filtered set and spills (~98MB at a broad page). This table moves that dedup
// off the read path: it holds exactly one row per currently-active canonical_key
// — the winner the read-time tiebreak would pick — denormalized with the
// filterable columns so the list read runs filter + keyset + LIMIT on this table
// alone (measured O(page), sub-ms) and joins fact_records by winner_fact_id only
// for the page payloads. fact_records stays the single home for payload truth.
