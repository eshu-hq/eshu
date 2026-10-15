// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admin holds the admin-handler family (Issue #6060, lane B): the
// Handler HTTP surface (recovery, work-item inspection, dead-lettering,
// replay, backfill, replay events, decisions, input-invalid facts) plus every
// file that declares one of its methods, the shared work-item/decision row
// and filter models, the Store port, and the replay-safety set behind the
// replay guard.
//
// The tenant identity reads/mutations live in identity/, the provider-config
// reads/mutations in provider/config/, the Postgres store in store/, and the
// shared audit/permission glue in audit/. Those leaves import this package,
// never the reverse. The /api/v0/admin/* routes are documented in
// openapi/paths/status/admin.go and the identity and provider-config routes
// in openapi/paths/auth/; scripts/verify-openapi.sh scans the nested
// fragment tree recursively. Thin aliases and forwarders in the root
// admin_alias.go keep every other caller compiling unchanged.
//
// A failed route step answers a fixed per-step message, never the backend
// error text (#7674): inspection reads give a stale PostgreSQL reader the
// shared retryable 503 first, bounded reads then answer their route-timeout
// 504, and every other failure answers 500 through tracing.WriteServerFailure,
// or 499 on a client cancel. A 499 on a mutation says only that the client
// left, not that nothing was written.
package admin
