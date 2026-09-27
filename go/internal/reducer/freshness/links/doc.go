// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package links is the changed_since_link reducer domain (#7127 PR-3a). Its
// Runner turns generation activations into changed-since links. A later read
// path answers get_changed_since from those links in O(changes) instead of
// diffing two whole generations.
//
// Each Runner cycle does four things. It journals activations the Ack path
// has not recorded: a sweeper row with an unknown prior for every active
// generation, plus a bounded backfill of retained chains. It deletes the
// ledger rows of deleted scopes. It links each backlog scope, with at most
// Config.Workers scopes at once and at most Config.MaxLinksPerScope links per
// scope. Finally it samples the backlog, lag and state-table gauges.
//
// The storage package (storage/postgres/freshness/links) owns every
// statement, the per-scope cursor fence and the database-wide full-link
// slots. A retryable outcome (cursor_locked, generation_locked, slot_busy,
// statement_timeout) satisfies contract.RetryableError. It stops that scope
// for the cycle and leaves its cursor alone, so the next cycle retries the
// same activation. There is no work-item queue: the journal and the cursor are
// the durable queue, so there is nothing to dead-letter.
//
// The domain is dark. cmd/reducer builds a Runner only when
// ESHU_CHANGED_SINCE_LINK_ENABLED=true, and a nil Runner issues no SQL.
package links
