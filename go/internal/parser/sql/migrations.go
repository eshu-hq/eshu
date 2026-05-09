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

func buildSQLMigrationEntries(path string, source string, payload map[string]any) []map[string]any {
	tool := detectSQLMigrationTool(path)
	if tool == "" {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0)
	seenTargets := make(map[string]struct{})
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
			if strings.TrimSpace(name) == "" {
				continue
			}
			key := bucket.kind + "|" + name
			if _, ok := seenTargets[key]; ok {
				continue
			}
			seenTargets[key] = struct{}{}
			rows = append(rows, map[string]any{
				"tool":        tool,
				"target_kind": bucket.kind,
				"target_name": name,
				"line_number": lineNumber,
			})
		}
	}

	for _, mention := range collectSQLTableMentions(source, true) {
		if mention.operation != "select" &&
			mention.operation != "update" &&
			mention.operation != "insert" &&
			mention.operation != "delete" &&
			mention.operation != "alter" &&
			mention.operation != "reference" {
			continue
		}
		key := "SqlTable|" + mention.name
		if _, ok := seenTargets[key]; ok {
			continue
		}
		seenTargets[key] = struct{}{}
		rows = append(rows, map[string]any{
			"tool":        tool,
			"target_kind": "SqlTable",
			"target_name": mention.name,
			"line_number": sqlLineNumberForOffset(source, mention.offset),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i]
		right := rows[j]
		leftLine, _ := left["line_number"].(int)
		rightLine, _ := right["line_number"].(int)
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		leftKind, _ := left["target_kind"].(string)
		rightKind, _ := right["target_kind"].(string)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		leftName, _ := left["target_name"].(string)
		rightName, _ := right["target_name"].(string)
		return leftName < rightName
	})
	return rows
}
