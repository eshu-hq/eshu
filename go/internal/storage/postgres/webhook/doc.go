// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package webhookstore persists provider webhook intake decisions for later
// targeted repository refresh handoff.
//
// WebhookTriggerStore deduplicates refresh requests by refresh_key in the
// webhook_refresh_triggers table, moves a prior ignored row back to queued
// when a later accepted delivery carries the same refresh key (and moves
// a queued row back to ignored when a later ignored delivery does, so
// status tracks the latest decision), claims queued triggers with
// FOR UPDATE SKIP LOCKED in received_at order, and records handed-off or
// failed rows. Every claim bumps the row's fencing token; handoff
// completions match (trigger_id, token) so a holder whose lease expired
// and was reaped completes zero rows instead of the new owner's claim
// (#7661). MarkIgnoredDecisionTriggersFailed terminates same-tick
// non-accepted claims with the ignored_decision class under the same
// fence plus AND decision <> 'accepted' at UPDATE time, so rows flipped
// back to accepted under the live claim stay on the handoff path, and
// returns only the rows actually terminated for the exact operator
// counter (#7795). ReapExpiredTriggerClaims returns stale accepted claims
// to queued (or fails poison rows past the attempt cap), fails stale
// claims whose decision is not accepted with the ignored_decision class
// (#7795), and CountStaleClaims feeds the stuck-claim gauge. Webhook
// payloads remain trigger evidence only: storing a trigger never marks
// graph or repository truth fresh.
//
// The SQL text lives in trigger_store_sql.go and the DDL in
// trigger_store_schema_sql.go; both move byte-identically with the store.
// This package depends on the shared database contracts in the sibling db
// package, the facts package, and the webhook domain types. It must not
// import the parent postgres package: root integration tests and the
// cmd/ wiring construct the store from outside.
package webhookstore
