// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

// supersededChecksums lists narrow checksum aliases accepted in place of a
// shipped migration's current checksum, keyed by the migration's
// Definition.Path.
//
// #7002: 093_cross_scope_completion_queue.sql shipped at 9696f2d9db (checksum
// c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42) and was
// later edited in place twice -- #6785 (2228f477c6, checksum
// 6cdb3e58545d3cbf13649402549ee7c1e577f99666bc2a5e0e87c1b9d0735e50) and #6923
// (43958c106f, checksum
// 7f73153be9a782875b054085c6eafa9d7e6723507b343e2e5ec43b307168473c) -- before
// landing back on its original shipped bytes here. Editing a shipped
// migration is forbidden (see README.md); this table exists only because
// both edited copies were already applied to real Eshu deployments (ops-qa
// among them) between 2026-09-20 and 2026-09-23, and the checksum guard in
// root's applyTrackedDefinitions must accept a ledger that recorded either
// edited checksum without treating that acceptance as a general bypass.
// 112_value_flow_refresh_producer_domains.sql and
// 120_value_flow_refresh_code_function_summary_producer.sql already converge
// those installs' schema idempotently once 093 is unblocked, so no other
// migration needs an alias.
//
// Do not add an entry here for a future edit: widen the shipped SQL through a
// new guarded migration instead. An alias is only ever warranted when a
// migration was, like this one, already edited-in-place and applied to real
// databases before the mistake was caught.
var supersededChecksums = map[string]map[string]bool{
	"go/internal/storage/postgres/migrations/093_cross_scope_completion_queue.sql": {
		"6cdb3e58545d3cbf13649402549ee7c1e577f99666bc2a5e0e87c1b9d0735e50": true, // #6785, applied 2026-09-20 to 2026-09-22
		"7f73153be9a782875b054085c6eafa9d7e6723507b343e2e5ec43b307168473c": true, // #6923, applied 2026-09-22 to the #7002 fix
	},
}

// IsSupersededChecksum reports whether recorded is an accepted alias for
// path's current checksum: a database that already applied an
// edited-in-place copy of a shipped migration before the edit was reverted.
// It is deliberately narrow -- only the exact (path, checksum) pairs listed
// in supersededChecksums match, so an alias for one migration never masks a
// genuine checksum drift on another.
func IsSupersededChecksum(path, recorded string) bool {
	aliases, ok := supersededChecksums[path]
	if !ok {
		return false
	}
	return aliases[recorded]
}
