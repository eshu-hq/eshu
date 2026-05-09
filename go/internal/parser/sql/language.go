package sql

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	createTableHeaderPattern = regexp.MustCompile(`(?is)\bCREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+(?P<name>` + sqlNamePattern + `)\s*\(`)
	createViewPattern        = regexp.MustCompile(`(?is)\bCREATE(?:\s+OR\s+REPLACE)?\s+(?P<kind>MATERIALIZED\s+)?VIEW\s+(?P<name>` + sqlNamePattern + `)\s+AS\s+(?P<body>.*?)(?:;|$)`)
	createTrigPattern        = regexp.MustCompile(`(?is)\bCREATE\s+TRIGGER\s+(?P<trigger>` + sqlNamePattern + `)\b.*?\bON\s+(?P<table>` + sqlNamePattern + `)\b.*?\bEXECUTE\s+(?:FUNCTION|PROCEDURE)\s+(?P<function>` + sqlNamePattern + `)\s*\(`)
	createIndexPattern       = regexp.MustCompile(`(?is)\bCREATE(?:\s+UNIQUE)?\s+INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+NOT\s+EXISTS)?\s+(?P<name>` + sqlNamePattern + `)\s+\bON\s+(?P<table>` + sqlNamePattern + `)\b`)
	alterTablePattern        = regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+(?P<table>` + sqlNamePattern + `)\s+(?P<body>.*?)(?:;|$)`)
	addColumnClausePattern   = regexp.MustCompile(`(?is)\bADD\s+COLUMN(?:\s+IF\s+NOT\s+EXISTS)?\s+`)
	columnPattern            = regexp.MustCompile(`(?is)^\s*(?P<name>"[^"]+"|` + "`[^`]+`" + `|\[[^\]]+\]|[A-Za-z_][\w$]*)\s+(?P<type>[A-Za-z_][\w$]*(?:\s*\([^)]*\))?)`)
	nextCreatePattern        = regexp.MustCompile(`(?is)\bCREATE\s+(?:TABLE|(?:MATERIALIZED\s+)?VIEW|FUNCTION|PROCEDURE|TRIGGER|INDEX)\b`)
)

// Parse extracts SQL schema objects, relationships, and migration metadata from
// one source file.
func Parse(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	text := string(source)

	payload := map[string]any{
		"path":              path,
		"sql_tables":        []map[string]any{},
		"sql_columns":       []map[string]any{},
		"sql_views":         []map[string]any{},
		"sql_functions":     []map[string]any{},
		"sql_triggers":      []map[string]any{},
		"sql_indexes":       []map[string]any{},
		"sql_relationships": []map[string]any{},
		"sql_migrations":    []map[string]any{},
		"is_dependency":     isDependency,
		"lang":              "sql",
	}

	seenEntities := map[string]map[string]struct{}{
		"sql_tables":    {},
		"sql_columns":   {},
		"sql_views":     {},
		"sql_functions": {},
		"sql_triggers":  {},
		"sql_indexes":   {},
	}
	seenRelationships := make(map[string]struct{})

	parseSQLTables(payload, text, options, seenEntities, seenRelationships)
	parseSQLAlterTableColumns(payload, text, options, seenEntities, seenRelationships)
	parseSQLViews(payload, text, options, seenEntities, seenRelationships)
	parseSQLFunctions(payload, text, options, seenEntities, seenRelationships)
	parseSQLTriggers(payload, text, options, seenEntities, seenRelationships)
	parseSQLIndexes(payload, text, options, seenEntities, seenRelationships)
	payload["sql_migrations"] = buildSQLMigrationEntries(path, text, payload)

	for _, bucket := range []string{
		"sql_tables",
		"sql_columns",
		"sql_views",
		"sql_functions",
		"sql_triggers",
		"sql_indexes",
		"sql_relationships",
		"sql_migrations",
	} {
		sortSQLBucket(payload, bucket)
	}
	return payload, nil
}

func parseSQLTables(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
) {
	indexes := namedCaptureIndexes(createTableHeaderPattern)
	for _, match := range createTableHeaderPattern.FindAllStringSubmatchIndex(source, -1) {
		name := normalizeSQLName(submatchValue(source, match, indexes["name"]))
		bodyStart := match[1]
		bodyEnd := sqlSectionEnd(source, bodyStart)
		body := source[bodyStart:bodyEnd]
		lineNumber := sqlLineNumberForOffset(source, match[0])
		appendSQLEntity(payload, seenEntities, "sql_tables", name, map[string]any{
			"name":            name,
			"line_number":     lineNumber,
			"type":            "content_entity",
			"sql_entity_type": "SqlTable",
			"schema":          sqlSchema(name),
			"qualified_name":  name,
		}, source, match[0], match[1], options)

		consumedOffset := 0
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				consumedOffset += len(line) + 1
				continue
			}
			if candidate := columnPattern.FindStringSubmatchIndex(trimmed); candidate != nil &&
				!isSQLTableConstraintLine(trimmed) {
				columnName := normalizeSQLName(submatchValue(trimmed, candidate, namedCaptureIndexes(columnPattern)["name"]))
				columnType := strings.TrimSpace(submatchValue(trimmed, candidate, namedCaptureIndexes(columnPattern)["type"]))
				qualified := name + "." + columnName
				columnLine := sqlLineNumberForOffset(source, bodyStart+consumedOffset)
				appendSQLEntity(payload, seenEntities, "sql_columns", qualified, map[string]any{
					"name":            qualified,
					"line_number":     columnLine,
					"type":            "content_entity",
					"sql_entity_type": "SqlColumn",
					"table_name":      name,
					"column_name":     columnName,
					"data_type":       columnType,
				}, source, bodyStart+consumedOffset, bodyStart+consumedOffset+len(line), options)
				appendSQLRelationship(payload, seenRelationships, "HAS_COLUMN", name, qualified, columnLine)
			}

			for _, mention := range collectSQLTableMentions(trimmed, false) {
				appendSQLRelationship(
					payload,
					seenRelationships,
					"REFERENCES_TABLE",
					name,
					mention.name,
					sqlLineNumberForOffset(source, bodyStart+consumedOffset+mention.offset),
				)
			}
			consumedOffset += len(line) + 1
		}
	}
}

func parseSQLViews(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
) {
	indexes := namedCaptureIndexes(createViewPattern)
	for _, match := range createViewPattern.FindAllStringSubmatchIndex(source, -1) {
		name := normalizeSQLName(submatchValue(source, match, indexes["name"]))
		body := submatchValue(source, match, indexes["body"])
		viewKind := "view"
		if strings.TrimSpace(submatchValue(source, match, indexes["kind"])) != "" {
			viewKind = "materialized"
		}
		lineNumber := sqlLineNumberForOffset(source, match[0])
		item := map[string]any{
			"name":            name,
			"line_number":     lineNumber,
			"type":            "content_entity",
			"sql_entity_type": "SqlView",
			"schema":          sqlSchema(name),
			"qualified_name":  name,
		}
		if viewKind != "view" {
			item["view_kind"] = viewKind
		}
		appendSQLEntity(payload, seenEntities, "sql_views", name, item, source, match[0], match[1], options)
		for _, mention := range collectSQLTableMentions(body, true) {
			if mention.operation != "select" {
				continue
			}
			appendSQLRelationship(
				payload,
				seenRelationships,
				"READS_FROM",
				name,
				mention.name,
				sqlLineNumberForOffset(source, match[0]+mention.offset),
			)
		}
	}
}

func parseSQLAlterTableColumns(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
) {
	indexes := namedCaptureIndexes(alterTablePattern)
	columnIndexes := namedCaptureIndexes(columnPattern)
	for _, match := range alterTablePattern.FindAllStringSubmatchIndex(source, -1) {
		tableName := normalizeSQLName(submatchValue(source, match, indexes["table"]))
		body := submatchValue(source, match, indexes["body"])
		bodyStart := match[indexes["body"]*2]
		for _, clause := range addColumnClausePattern.FindAllStringIndex(body, -1) {
			fragmentStart := clause[1]
			fragment := body[fragmentStart:]
			fragment = fragment[:sqlClauseBoundary(fragment)]
			candidate := columnPattern.FindStringSubmatchIndex(strings.TrimSpace(fragment))
			if candidate == nil {
				continue
			}
			columnName := normalizeSQLName(submatchValue(strings.TrimSpace(fragment), candidate, columnIndexes["name"]))
			columnType := strings.TrimSpace(submatchValue(strings.TrimSpace(fragment), candidate, columnIndexes["type"]))
			qualified := tableName + "." + columnName
			lineNumber := sqlLineNumberForOffset(source, bodyStart+fragmentStart)
			appendSQLEntity(payload, seenEntities, "sql_columns", qualified, map[string]any{
				"name":            qualified,
				"line_number":     lineNumber,
				"type":            "content_entity",
				"sql_entity_type": "SqlColumn",
				"table_name":      tableName,
				"column_name":     columnName,
				"data_type":       columnType,
			}, source, bodyStart+fragmentStart, bodyStart+fragmentStart+len(fragment), options)
			appendSQLRelationship(payload, seenRelationships, "HAS_COLUMN", tableName, qualified, lineNumber)
		}
	}
}

func parseSQLFunctions(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
) {
	indexes := namedCaptureIndexes(createFunctionHeaderPattern)
	for _, match := range createFunctionHeaderPattern.FindAllStringSubmatchIndex(source, -1) {
		appendSQLRoutineFromHeader(payload, source, options, seenEntities, seenRelationships, match, indexes, "function")
	}

	indexes = namedCaptureIndexes(createProcedureHeaderPattern)
	for _, match := range createProcedureHeaderPattern.FindAllStringSubmatchIndex(source, -1) {
		appendSQLRoutineFromHeader(payload, source, options, seenEntities, seenRelationships, match, indexes, "procedure")
	}
}

func parseSQLTriggers(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
) {
	indexes := namedCaptureIndexes(createTrigPattern)
	for _, match := range createTrigPattern.FindAllStringSubmatchIndex(source, -1) {
		triggerName := normalizeSQLName(submatchValue(source, match, indexes["trigger"]))
		tableName := normalizeSQLName(submatchValue(source, match, indexes["table"]))
		functionName := normalizeSQLName(submatchValue(source, match, indexes["function"]))
		lineNumber := sqlLineNumberForOffset(source, match[0])
		appendSQLEntity(payload, seenEntities, "sql_triggers", triggerName, map[string]any{
			"name":            triggerName,
			"line_number":     lineNumber,
			"type":            "content_entity",
			"sql_entity_type": "SqlTrigger",
			"table_name":      tableName,
			"function_name":   functionName,
		}, source, match[0], match[1], options)
		appendSQLRelationship(payload, seenRelationships, "TRIGGERS_ON", triggerName, tableName, lineNumber)
		appendSQLRelationship(payload, seenRelationships, "EXECUTES", triggerName, functionName, lineNumber)
	}
}

func parseSQLIndexes(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
) {
	indexes := namedCaptureIndexes(createIndexPattern)
	for _, match := range createIndexPattern.FindAllStringSubmatchIndex(source, -1) {
		indexName := normalizeSQLName(submatchValue(source, match, indexes["name"]))
		tableName := normalizeSQLName(submatchValue(source, match, indexes["table"]))
		lineNumber := sqlLineNumberForOffset(source, match[0])
		appendSQLEntity(payload, seenEntities, "sql_indexes", indexName, map[string]any{
			"name":            indexName,
			"line_number":     lineNumber,
			"type":            "content_entity",
			"sql_entity_type": "SqlIndex",
			"table_name":      tableName,
		}, source, match[0], match[1], options)
		appendSQLRelationship(payload, seenRelationships, "INDEXES", indexName, tableName, lineNumber)
	}
}

func appendSQLEntity(
	payload map[string]any,
	seen map[string]map[string]struct{},
	bucket string,
	name string,
	item map[string]any,
	source string,
	start int,
	end int,
	options Options,
) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if _, ok := seen[bucket][name]; ok {
		return
	}
	seen[bucket][name] = struct{}{}
	if options.IndexSource {
		item["source"] = source[max(0, start):min(len(source), end)]
	}
	shared.AppendBucket(payload, bucket, item)
}

func appendSQLRelationship(
	payload map[string]any,
	seen map[string]struct{},
	relationshipType string,
	sourceName string,
	targetName string,
	lineNumber int,
) {
	if strings.TrimSpace(sourceName) == "" || strings.TrimSpace(targetName) == "" {
		return
	}
	key := relationshipType + "|" + sourceName + "|" + targetName
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	shared.AppendBucket(payload, "sql_relationships", map[string]any{
		"type":        relationshipType,
		"source_name": sourceName,
		"target_name": targetName,
		"line_number": lineNumber,
	})
}

func sortSQLBucket(payload map[string]any, key string) {
	items, _ := payload[key].([]map[string]any)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		leftLine := shared.IntValue(left["line_number"])
		rightLine := shared.IntValue(right["line_number"])
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		return fmt.Sprint(left["name"]) < fmt.Sprint(right["name"])
	})
	payload[key] = items
}

func sqlSchema(name string) string {
	if strings.Contains(name, ".") {
		return name[:strings.LastIndex(name, ".")]
	}
	return ""
}

func namedCaptureIndexes(pattern *regexp.Regexp) map[string]int {
	indexes := make(map[string]int)
	for index, name := range pattern.SubexpNames() {
		if name != "" {
			indexes[name] = index
		}
	}
	return indexes
}

func submatchValue(source string, match []int, index int) string {
	if index < 0 || index*2+1 >= len(match) {
		return ""
	}
	start := match[index*2]
	end := match[index*2+1]
	if start < 0 || end < 0 || start >= end {
		return ""
	}
	return source[start:end]
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func sqlSectionEnd(source string, start int) int {
	remaining := source[start:]
	closeIndex := strings.Index(remaining, ");")
	nextCreate := -1
	if loc := nextCreatePattern.FindStringIndex(remaining); loc != nil && loc[0] > 0 {
		nextCreate = loc[0]
	}
	switch {
	case closeIndex >= 0 && nextCreate >= 0:
		if closeIndex < nextCreate {
			return start + closeIndex
		}
		return start + nextCreate
	case closeIndex >= 0:
		return start + closeIndex
	case nextCreate >= 0:
		return start + nextCreate
	default:
		return len(source)
	}
}

func sqlClauseBoundary(fragment string) int {
	depth := 0
	for index, r := range fragment {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				return index
			}
		}
	}
	return len(fragment)
}
