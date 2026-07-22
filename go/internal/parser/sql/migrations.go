// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	sqlMigrationLayouts = []struct {
		pattern *regexp.Regexp
		tool    string
	}{
		{pattern: regexp.MustCompile(`(?i)/prisma/migrations/.+/migration\.sql$`), tool: "prisma"},
		{pattern: regexp.MustCompile(`(?i)/liquibase/`), tool: "liquibase"},
		{pattern: regexp.MustCompile(`(?i)/changelog/`), tool: "liquibase"},
		{pattern: regexp.MustCompile(`(?i)/migrations/.+\.up\.sql$`), tool: "golang-migrate"},
		{pattern: regexp.MustCompile(`(?i)/migrations/`), tool: "generic"},
	}
	sqlFlywayFilename = regexp.MustCompile(`(?i)(^|/)V\d+__.+\.sql$`)
)

func detectSQLMigrationTool(path string) string {
	normalized := filepath.ToSlash(path)
	if sqlFlywayFilename.MatchString(normalized) {
		return "flyway"
	}
	for _, candidate := range sqlMigrationLayouts {
		if candidate.pattern.MatchString(normalized) {
			return candidate.tool
		}
	}
	return ""
}

// sqlMigrationTargetsCap bounds the number of migration_targets stamped onto a
// SqlMigration entity's metadata. Mirrors sqlSourceTablesCap (entities.go) so a
// migration file touching a pathological number of tables still produces a
// bounded, stable payload (#5346).
const sqlMigrationTargetsCap = sqlSourceTablesCap

// buildSQLMigrationEntries builds the single SqlMigration content-entity item
// for one SQL file recognized as a migration by detectSQLMigrationTool, or an
// empty slice when the file is not a migration.
//
// Each recognized migration file produces exactly ONE entity, not one row per
// touched target (the pre-#5346 shape): a per-target row carried no `name`
// field, so registering it as-is through the generic content-entity pipeline
// (materializeEntities -> content.CanonicalEntityID) minted a garbage
// empty-name uid, and nothing downstream ever consumed the bucket. The
// entity's migration_targets metadata carries every forward
// (create/alter/reference/DML-write/drop) target the reducer's MIGRATES derivation
// resolves against SqlTable/SqlView/SqlFunction/SqlTrigger/SqlIndex entities.
// A target mentioned ONLY via a SELECT read is deliberately excluded --
// write-not-read honesty, mirroring dropShadowedReads (#5345): a backfill
// migration's read-only table is not "migrated". DROP targets are recorded as
// operation="drop" metadata; that operation does not imply the target's
// head-state presence or absence.
func buildSQLMigrationEntries(
	path string,
	lineIndex sqlLineIndex,
	payload map[string]any,
	tableMentions []sqlMention,
) []map[string]any {
	tool := detectSQLMigrationTool(path)
	if tool == "" {
		return []map[string]any{}
	}

	targets := make([]map[string]any, 0)
	seenTargets := make(map[string]struct{})
	addTarget := func(kind string, name string, operation string, lineNumber int) {
		if strings.TrimSpace(name) == "" {
			return
		}
		key := kind + "|" + name
		if _, ok := seenTargets[key]; ok {
			return
		}
		seenTargets[key] = struct{}{}
		targets = append(targets, map[string]any{
			"kind":        kind,
			"name":        name,
			"operation":   operation,
			"line_number": lineNumber,
		})
	}

	// Entities this same migration file CREATEs (new tables/views/functions/
	// triggers/indexes) are forward migration targets in their own right.
	// Processed first so a table both created and later altered in the same
	// file keeps its "create" operation (seenTargets dedupes by kind+name).
	for _, bucket := range []struct {
		name string
		kind string
	}{
		{name: "sql_tables", kind: "SqlTable"},
		{name: "sql_views", kind: "SqlView"},
		{name: "sql_functions", kind: "SqlFunction"},
		{name: "sql_triggers", kind: "SqlTrigger"},
		{name: "sql_indexes", kind: "SqlIndex"},
	} {
		items, _ := payload[bucket.name].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			lineNumber, _ := item["line_number"].(int)
			addTarget(bucket.kind, name, "create", lineNumber)
		}
	}

	// Bounded table mentions record ALTER/INSERT/UPDATE/DELETE/REFERENCES/DROP
	// touches against an existing table. A "select" mention is read-only and
	// must never be recorded as a migration target (#5346).
	for _, mention := range tableMentions {
		if mention.operation == "select" {
			continue
		}
		addTarget("SqlTable", mention.name, mention.operation, lineIndex.lineForOffset(mention.offset))
	}

	sort.SliceStable(targets, func(i, j int) bool {
		leftLine, _ := targets[i]["line_number"].(int)
		rightLine, _ := targets[j]["line_number"].(int)
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		leftKind, _ := targets[i]["kind"].(string)
		rightKind, _ := targets[j]["kind"].(string)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		leftName, _ := targets[i]["name"].(string)
		rightName, _ := targets[j]["name"].(string)
		return leftName < rightName
	})
	if len(targets) > sqlMigrationTargetsCap {
		targets = targets[:sqlMigrationTargetsCap]
	}

	return []map[string]any{
		{
			"name":              sqlMigrationIdentifier(path, tool),
			"line_number":       1,
			"type":              "content_entity",
			"sql_entity_type":   "SqlMigration",
			"tool":              tool,
			"migration_targets": targets,
		},
	}
}

// sqlMigrationIdentifier returns the readable name stamped on the SqlMigration
// entity. Prisma names every migration file "migration.sql" (one per
// timestamped directory), so a basename-derived identifier would read
// "migration" for every prisma migration in a repo -- the entity's canonical
// uid stays unique regardless (relativePath is already part of
// content.CanonicalEntityID), but that display name would be worthless, so the
// migration's parent directory name is used instead. Every other supported
// tool names the migration file itself meaningfully (flyway "V42__x.sql",
// golang-migrate "*.up.sql", generic/liquibase "*.sql"), so the file's base
// name with its extension stripped is used.
func sqlMigrationIdentifier(path string, tool string) string {
	if tool == "prisma" {
		return filepath.Base(filepath.Dir(path))
	}
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".up.sql")
	base = strings.TrimSuffix(base, ".down.sql")
	base = strings.TrimSuffix(base, ".sql")
	return base
}
