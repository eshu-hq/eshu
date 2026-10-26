// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package eol joins repository runtime declarations against a vendored
// endoflife.date snapshot, so a read path can answer whether a declared
// runtime is still supported without any network access (issue #9163,
// research in docs/internal/evidence/8820-eol-snapshot-research.md).
//
// The snapshot directory holds one endoflife.date per-product JSON file for
// each mapped slug plus a snapshot_date file with the UTC day of the fetch.
// The join is a pure function of the snapshot, the declared runtime and
// version, and the clock: Resolve reports supported, eol, support-ending or
// unknown, and every answer carries the snapshot date, its age in days, and
// whether the snapshot is past its refresh window. Missing data is never
// read as supported: an unmapped runtime, a missing product file, a missing
// cycle, or an unresolvable version all yield unknown.
package eol
