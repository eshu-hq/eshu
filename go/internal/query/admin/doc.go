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
// never the reverse. The OpenAPI fragments documenting the admin routes stay
// in the query root (openapi_paths_auth_admin_*.go), where
// scripts/verify-openapi.sh requires every family's fragments to live. Thin
// aliases and forwarders in the root admin_alias.go keep every other caller
// compiling unchanged.
package admin
