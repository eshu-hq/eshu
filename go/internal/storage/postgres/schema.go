// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Definition describes one ordered bootstrap SQL payload.
type Definition struct {
	Name string
	Path string
	SQL  string
}

// Executor is the narrow adapter surface required to apply schema bootstrap
// statements against a SQL connection or transaction.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// BootstrapDefinitions returns the ordered Wave 2 bootstrap layout, sourced
// from embed.FS so the migrations/ directory is the single source of truth.
func BootstrapDefinitions() []Definition {
	entries, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		panic("postgres: read embedded migrations dir: " + err.Error())
	}
	defs := make([]Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		data, err := embeddedMigrations.ReadFile(path.Join("migrations", name))
		if err != nil {
			panic("postgres: read embedded migration " + name + ": " + err.Error())
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

// BootstrapDefinitionsWithoutContentSearchIndexes returns the bootstrap layout
// without the expensive content trigram indexes. It is intended for
// local-authoritative bulk-load flows that call EnsureContentSearchIndexes
// after the initial write-heavy drain completes.
func BootstrapDefinitionsWithoutContentSearchIndexes() []Definition {
	defs := BootstrapDefinitions()
	for i := range defs {
		if defs[i].Name == "content_store" {
			defs[i].SQL = contentStoreSchemaWithoutSearchIndexesSQL
			break
		}
	}
	return defs
}

// BootstrapStatements returns the ordered SQL payloads that make up the
// bootstrap layout.
func BootstrapStatements() []string {
	defs := BootstrapDefinitions()
	statements := make([]string, 0, len(defs))
	for _, def := range defs {
		statements = append(statements, def.SQL)
	}

	return statements
}

// ValidateDefinitions checks that a schema layout is complete enough to apply.
func ValidateDefinitions(defs []Definition) error {
	seen := make(map[string]struct{}, len(defs))
	for i, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			return fmt.Errorf("definition %d has an empty name", i)
		}
		if strings.TrimSpace(def.Path) == "" {
			return fmt.Errorf("definition %q has an empty path", def.Name)
		}
		if strings.TrimSpace(def.SQL) == "" {
			return fmt.Errorf("definition %q has empty SQL", def.Name)
		}
		if _, ok := seen[def.Name]; ok {
			return fmt.Errorf("duplicate definition name %q", def.Name)
		}
		seen[def.Name] = struct{}{}
	}

	return nil
}

// ApplyDefinitions executes one ordered schema layout against the executor.
func ApplyDefinitions(ctx context.Context, exec Executor, defs []Definition) error {
	if err := ValidateDefinitions(defs); err != nil {
		return err
	}
	if exec == nil {
		return fmt.Errorf("executor is required")
	}

	for _, def := range defs {
		if _, err := exec.ExecContext(ctx, def.SQL); err != nil {
			return fmt.Errorf("apply %s: %w", def.Name, err)
		}
	}

	return nil
}

// ApplyBootstrap applies the Wave 2 schema bootstrap layout.
func ApplyBootstrap(ctx context.Context, exec Executor) error {
	return ApplyDefinitions(ctx, exec, BootstrapDefinitions())
}

// ApplyBootstrapWithoutContentSearchIndexes applies the bootstrap layout while
// deferring content trigram indexes for a later bulk index build.
func ApplyBootstrapWithoutContentSearchIndexes(ctx context.Context, exec Executor) error {
	return ApplyDefinitions(ctx, exec, BootstrapDefinitionsWithoutContentSearchIndexes())
}

// EnsureContentSearchIndexes creates the trigram indexes that accelerate
// content file and entity source search.
func EnsureContentSearchIndexes(ctx context.Context, exec Executor) error {
	if exec == nil {
		return fmt.Errorf("executor is required")
	}
	if _, err := exec.ExecContext(ctx, contentStoreSearchIndexSchemaSQL); err != nil {
		return fmt.Errorf("ensure content search indexes: %w", err)
	}
	return nil
}

// MigrationSQL returns the SQL payload for the named migration definition.
// It panics if the name is not found in the embedded migrations.
func MigrationSQL(defName string) string {
	for _, def := range BootstrapDefinitions() {
		if def.Name == defName {
			return def.SQL
		}
	}
	panic("postgres: migration not found: " + defName)
}
