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
          "as_of": {"type": "string", "format": "date-time", "description": "The time the active-work counts are true at: the stored row's as_of for source model, otherwise the snapshot clock; a read that shared another read's live statement reports the clock that statement ran at."},
          "age_seconds": {"type": "number", "description": "How old the served active-work data was at the read, from the database clock; 0 for a live read."},
          "stale": {"type": "boolean", "description": "true when the served active-work data is older than the configured limit. A stored row that is too old is never served: the live statement answers and reason says stale, so this is false on every route that reads through the stored-summary reader; it is reserved for the runtime /metrics scrape."}
        }
      },
`

// componentsTerraformStateSource documents the terraform_state_source object
// of the three routes that render the terraform_state section (#7009): the same
// five keys and closed value sets as ActiveWorkSource, with a Terraform-state
// description. TestOpenAPIDocumentsTheTerraformStateSource keeps the two
// components' enums equal.
const componentsTerraformStateSource = `      "TerraformStateSource": {
        "type": "object",
        "description": "Where the terraform_state section of a status report (the last observed serial per state locator and the recent warnings per locator) came from, and how old it is. A stored summary row holds the answer the writer computed just after its as_of, the database clock it read after taking its lock. Each of the two statements sees the rows committed when it started (READ COMMITTED), so a stored row can include rows committed between as_of and that statement, and the live read can differ from it by rows committed in that gap, bounded by one writer pass. On the API and MCP server the live read runs both statements in one REPEATABLE READ snapshot; a runtime that reads status on a plain connection runs each statement on its own snapshot. observed_at values are the collector's clock, not the database's, so an observed_at can be later than as_of. No value in the section is an age, so nothing is advanced at read. The live statements are true at the snapshot clock. Present on the pipeline, index, and index-status responses and on the runtime admin status JSON, even when the section is empty; absent on every route that skips Terraform-state evidence and when the status reader reports no source.",
        "properties": {
          "source": {"type": "string", "enum": ["model", "live", "live_fallback"], "description": "model: a stored summary row that passed every fence. live: the stored-summary reader is off, so the live statements ran. live_fallback: the stored row could not be served, so the live statements answered the whole section; reason says why."},
          "reason": {"type": "string", "enum": ["fresh", "flag_off", "missing", "not_installed", "version", "row_count", "stale", "decode"], "description": "fresh: served from the row. flag_off: the reader is off. missing: no row yet. not_installed: the table does not exist yet. version: the row was written by another schema or statement version. row_count: the stored row_count disagrees with its payload. stale: the row is older than the configured stale limit. decode: the row could not be decoded."},
          "as_of": {"type": "string", "format": "date-time", "description": "For source model, the database clock the writer read after taking its lock, just before it ran the two statements (a stored row can include rows committed between as_of and the statements); otherwise the snapshot clock."},
          "age_seconds": {"type": "number", "description": "How old the served terraform_state data was at the read, from the database clock; 0 for a live read."},
          "stale": {"type": "boolean", "description": "Always false: a stored row that is too old is never served. The live statements answer and reason says stale."}
        }
      },
`
