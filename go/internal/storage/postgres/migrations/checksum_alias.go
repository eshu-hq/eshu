// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

// checksumAliasKey identifies one tracked application of a migration: its
// Definition.Path plus its normalized variant ("full" for the default, or
// "deferred" for the content-search-index-deferred rewrite). The ledger keys
// applied migrations by (path, variant) -- see schemaMigrationKey in root's
// schema_bootstrap_lock.go -- and the alias table must be keyed at least as
// narrowly. A path-only key is variant-blind: deferred rewrites such as
// content_substring_index_state change only Variant/FullChecksum while leaving
// the SQL byte-identical, so a path-only alias for one of them would match
// both variants indistinguishably. Keying by (path, variant) keeps every alias
// scoped to the exact population that recorded it.
type checksumAliasKey struct {
	path    string
	variant string
}

// checksumAliasEntry pins the aliases accepted for one (path, variant) to the
// exact shipped bytes they were carved out for: an alias is only ever valid
// while the SQL on disk still checksums to shipped. If that SQL is edited
// again, shipped no longer matches and every alias for it stops working,
// instead of silently covering the new drift too.
type checksumAliasEntry struct {
	shipped string
	aliases map[string]bool
}

// supersededChecksums lists narrow checksum aliases accepted in place of a
// shipped migration's current checksum, keyed by the migration's
// (Definition.Path, normalized variant).
//
// #7002: 093_cross_scope_completion_queue.sql shipped at 9696f2d9db (checksum
// c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42) and was
// later edited in place twice -- #6785 (2228f477c6, checksum
// 6cdb3e58545d3cbf13649402549ee7c1e577f99666bc2a5e0e87c1b9d0735e50) and #6923
// (43958c106f, checksum
// 7f73153be9a782875b054085c6eafa9d7e6723507b343e2e5ec43b307168473c) -- before
// landing back on its original shipped bytes here. Editing a shipped
// migration is forbidden (see README.md); this table exists only because
// both edited copies were already applied to real Eshu deployments (QA
// among them) between 2026-09-20 and 2026-09-23, and the checksum guard in
// root's applyTrackedDefinitions must accept a ledger that recorded either
// edited checksum without treating that acceptance as a general bypass.
// 112_value_flow_refresh_producer_domains.sql and
// 120_value_flow_refresh_code_function_summary_producer.sql already converge
// those installs' schema idempotently once 093 is unblocked, so no other
// migration needs an alias.
//
// #9279: the deferred variant of 004_content_store.sql is a Go constant, not
// the shipped .sql file, and #8028 edited that constant in place --
// content_files_language_repo_idx went from (language, repo_id) to
// (language, repo_id) INCLUDE (indexed_at) -- moving the deferred checksum
// from 027b3563ab35a7824f5add728209998f530497a1ed3a7293844bd5e4ad53fdf4 to
// 3d8ef124d1e6b5641f5d84fe581ede18046fcde70f6aad5adb3934611562f5f3. The
// shipped 004_content_store.sql never changed (full checksum 236862da...,
// still pinned in migration_checksum_manifest_test.go), so the manifest
// stayed green and CI missed it. This entry accepts only the pre-#8028
// deferred checksum, only under the deferred variant, and only while the
// constant still checksums to the shipped 3d8ef124.... Migrations 171 to 173
// converge the index itself once 004 is unblocked, for full and deferred
// installs alike, so no other migration needs an alias.
//
// Do not add an entry here for a future edit: widen the shipped SQL through a
// new guarded migration instead. An alias is only ever warranted when a
// migration was, like these, already edited-in-place and applied to real
// databases before the mistake was caught.
var supersededChecksums = map[checksumAliasKey]checksumAliasEntry{
	{path: "go/internal/storage/postgres/migrations/093_cross_scope_completion_queue.sql", variant: "full"}: {
		shipped: "c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42",
		aliases: map[string]bool{
			"6cdb3e58545d3cbf13649402549ee7c1e577f99666bc2a5e0e87c1b9d0735e50": true, // #6785, applied 2026-09-20 to 2026-09-22
			"7f73153be9a782875b054085c6eafa9d7e6723507b343e2e5ec43b307168473c": true, // #6923, applied 2026-09-22 to the #7002 fix
		},
	},
	{path: "go/internal/storage/postgres/migrations/004_content_store.sql", variant: "deferred"}: {
		shipped: "3d8ef124d1e6b5641f5d84fe581ede18046fcde70f6aad5adb3934611562f5f3",
		aliases: map[string]bool{
			"027b3563ab35a7824f5add728209998f530497a1ed3a7293844bd5e4ad53fdf4": true, // #9279, pre-#8028 deferred installs (2026-10-08 to 2026-10-13)
		},
	},
}

// IsSupersededChecksum reports whether recorded is an accepted alias for the
// (path, variant) application of a migration: a database that already
// applied an edited-in-place copy before the edit was caught. variant is the
// normalized ledger variant ("full" for the default -- see migrationVariant
// in root's schema_bootstrap_lock.go, which maps "" to "full"). current is
// the definition's checksum as it exists right now; an alias only ever
// matches when current still equals the shipped checksum a table entry was
// pinned to -- if the definition were edited in place again, current would
// diverge from shipped and every alias for it would stop matching, so a new
// drift can never hide behind an old one. It is deliberately narrow -- only
// the exact (path, variant, checksum) triples listed in supersededChecksums
// match, so an alias for one migration or variant never masks a genuine
// checksum drift on another.
func IsSupersededChecksum(path, variant, recorded, current string) bool {
	entry, ok := supersededChecksums[checksumAliasKey{path: path, variant: variant}]
	if !ok {
		return false
	}
	if current != entry.shipped {
		return false
	}
	return entry.aliases[recorded]
}
