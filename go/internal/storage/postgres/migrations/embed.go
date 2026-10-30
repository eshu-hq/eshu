// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package migrations owns the embedded Postgres bootstrap SQL files and the
// ordered Definition list root's postgres package applies. See doc.go#6693
// D2: this leaf holds the //go:embed so the migrations/ directory is the
// single source of truth for both the embedded bytes and the ordering logic.
package migrations

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

//go:embed *.sql
var embedded embed.FS

// Definition describes one ordered bootstrap SQL payload.
//
// Variant and FullChecksum support the deferred-content-search-index
// variant: root's BootstrapDefinitionsWithoutContentSearchIndexes rewrites a
// definition's SQL and sets these two fields so the migration tracker can
// record it as a distinct ("deferred") variant of the same Path, then
// recognize the eventual full-checksum migration as already satisfied. Most
// callers never touch these fields; BootstrapDefinitions leaves them zero.
type Definition struct {
	Name string
	Path string
	SQL  string

	// Variant distinguishes multiple tracked applications of the same Path
	// (e.g. "deferred" vs the default "full"). Empty means "full".
	Variant string
	// FullChecksum is the checksum_sha256 of the eventual full-variant SQL,
	// recorded so the tracker can detect the full migration as already
	// applied once a deferred variant completes.
	FullChecksum string
	// PublicOnly marks a database-global authority migration (#7033) that
	// refuses any non-public schema via its own SQL guard. The marked set
	// lives in publicOnlyMigrationPaths below, next to the files — never
	// in the SQL itself, because #7002 forbids editing a shipped
	// migration. Test binaries bootstrapping isolated schemas skip these
	// definitions explicitly (see postgres.ApplyBootstrapWithOptions);
	// production bootstraps always run public and install them. A rename
	// breaks the checksum manifest loudly, so the map cannot drift
	// silently from the file it names.
	PublicOnly bool
}

// publicOnlyMigrationPaths is the exact set of PublicOnly definitions, keyed
// by embedded path. It exists because the SQL files themselves are immutable
// (#7002): the marker cannot live in the migration body. A key naming a file
// that no longer exists never matches, so a renumber that moves a file
// without updating this map silently unmarks the migration;
// public_only_paths_exist_test.go pins every key to an embedded file.
// Authority-coupled scanner migrations (#9299) apply only where the
// database-global authority layout exists. Each file below touches an
// authority object at apply time, directly (%ROWTYPE on eshu_security
// or a 241a-owned table, ALTER/REFERENCES/CREATE TRIGGER on one,
// top-level DML into one, CREATE/REVOKE of an eshu_security function)
// or transitively (DDL against a table owned by an earlier file in this
// list, e.g. 258 ALTERs a 245a table), so an isolated-schema
// bootstrap that skips 241a must skip these too. 240a (own
// targets/sources/attempts tables plus a base-table trigger), 251 (own
// discovery-cursors table), and 262 (pure base-table function) stay
// applicable everywhere.
var publicOnlyMigrationPaths = map[string]bool{
	"go/internal/storage/postgres/migrations/241a_scanner_typed_source_and_results.sql":        true,
	"go/internal/storage/postgres/migrations/241b_scanner_authority_epoch_backfill.sql":        true,
	"go/internal/storage/postgres/migrations/242a_scanner_role_attestation.sql":                true,
	"go/internal/storage/postgres/migrations/243a_scanner_role_policy_install.sql":             true,
	"go/internal/storage/postgres/migrations/244a_scanner_protected_capability.sql":            true,
	"go/internal/storage/postgres/migrations/245a_scanner_scan_result_receipts.sql":            true,
	"go/internal/storage/postgres/migrations/246a_scanner_source_collection_start.sql":         true,
	"go/internal/storage/postgres/migrations/247a_scanner_source_stage_page.sql":               true,
	"go/internal/storage/postgres/migrations/248_scanner_source_publish.sql":                   true,
	"go/internal/storage/postgres/migrations/249_scanner_existing_static_findings.sql":         true,
	"go/internal/storage/postgres/migrations/250_scanner_attempt_authority.sql":                true,
	"go/internal/storage/postgres/migrations/252_scanner_discovery_cursor_api.sql":             true,
	"go/internal/storage/postgres/migrations/253_scanner_canonical_admission_authority.sql":    true,
	"go/internal/storage/postgres/migrations/254_scanner_public_advisory_policy.sql":           true,
	"go/internal/storage/postgres/migrations/255_scanner_public_advisory_stage.sql":            true,
	"go/internal/storage/postgres/migrations/256_scanner_public_advisory_complete.sql":         true,
	"go/internal/storage/postgres/migrations/257_scanner_public_advisory_publish.sql":          true,
	"go/internal/storage/postgres/migrations/258_scanner_scan_fact_integrity.sql":              true,
	"go/internal/storage/postgres/migrations/259_scanner_advisory_epoch_guards.sql":            true,
	"go/internal/storage/postgres/migrations/260_scanner_public_advisory_completion_guard.sql": true,
	"go/internal/storage/postgres/migrations/261_scanner_advisory_producer_ingestion_acl.sql":  true,
	"go/internal/storage/postgres/migrations/263_scanner_static_impact_public_authority.sql":   true,
	"go/internal/storage/postgres/migrations/264_scanner_static_impact_authorize.sql":          true,
	"go/internal/storage/postgres/migrations/265_scanner_admission_authority_deny_no_row.sql":  true,
	"go/internal/storage/postgres/migrations/268_scanner_impact_publication.sql":               true,
	"go/internal/storage/postgres/migrations/269_scanner_impact_publish.sql":                   true,
}

// BootstrapDefinitions returns the ordered Wave 2 bootstrap layout, sourced
// from embed.FS so this package's directory is the single source of truth.
func BootstrapDefinitions() []Definition {
	entries, err := embedded.ReadDir(".")
	if err != nil {
		panic("migrations: read embedded migrations dir: " + err.Error())
	}
	defs := make([]Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		data, err := embedded.ReadFile(name)
		if err != nil {
			panic("migrations: read embedded migration " + name + ": " + err.Error())
		}
		// Derive definition name: strip numeric prefix and .sql extension.
		// e.g. "001_ingestion_scopes.sql" → "ingestion_scopes".
		// Skip files without the expected NNN_ prefix (a human should rename them).
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		defName := strings.TrimSuffix(parts[1], ".sql")

		// Path reflects the real embed location for callers that read files
		// from disk (e.g. the bootstrap mirror test).
		fspath := path.Join("go", "internal", "storage", "postgres", "migrations", name)

		defs = append(defs, Definition{
			Name:       defName,
			Path:       fspath,
			SQL:        string(data),
			PublicOnly: publicOnlyMigrationPaths[fspath],
		})
	}
	sort.SliceStable(defs, func(i, j int) bool {
		return defs[i].Path < defs[j].Path
	})
	return defs
}

// Checksum returns the sha256 hex digest of a migration statement, the value
// recorded in the eshu_schema_migrations ledger's checksum_sha256 column.
func Checksum(statement string) string {
	sum := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(sum[:])
}
