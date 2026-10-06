// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsActiveWorkSource documents the active_work_source object the
// status routes carry (#7009), included into components' "schemas" object as a
// separate constant fragment to keep components.go under the repository's
// file-size limit, like componentsSignInPolicy.
const componentsActiveWorkSource = `      "ActiveWorkSource": {
        "type": "object",
        "description": "Where the queue, stage, backlog, blockage, and latest-failure sections of a status report came from, and how old they are. A stored summary row is counted at its as_of and its ages are advanced to the read; the live statement is true at the snapshot clock. The other sections of the report are always live, so one report can mix both. Absent when the status reader reports no source.",
        "properties": {
          "source": {"type": "string", "enum": ["model", "live", "live_fallback"], "description": "model: a stored summary row that passed every fence. live: the stored-summary reader is off, so the live statement ran. live_fallback: the stored row could not be served, so the live statement answered the whole section; reason says why."},
          "reason": {"type": "string", "enum": ["fresh", "flag_off", "missing", "not_installed", "version", "row_count", "stale", "decode"], "description": "fresh: served from the row. flag_off: the reader is off. missing: no row yet. not_installed: the table does not exist yet. version: the row was written by another schema or statement version. row_count: the stored row_count disagrees with its payload. stale: the row is older than the configured stale limit. decode: the row could not be decoded."},
          "as_of": {"type": "string", "format": "date-time", "description": "The time the active-work counts are true at: the stored row's as_of for source model, the snapshot clock otherwise."},
          "age_seconds": {"type": "number", "description": "How old the served active-work data was at the read, from the database clock; 0 for a live read."},
          "stale": {"type": "boolean", "description": "true when a stored row existed but was older than the configured limit, so the live statement answered instead."}
        }
      },
`
