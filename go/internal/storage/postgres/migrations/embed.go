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
			Name: defName,
			Path: fspath,
			SQL:  string(data),
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
