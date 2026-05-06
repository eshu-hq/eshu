package parser

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	createFunctionHeaderPattern  = regexp.MustCompile(`(?is)\bCREATE(?:\s+OR\s+REPLACE)?\s+FUNCTION\s+(?P<name>` + sqlNamePattern + `)\s*\([^)]*\)\s+RETURNS\b`)
	createProcedureHeaderPattern = regexp.MustCompile(`(?is)\bCREATE(?:\s+OR\s+REPLACE)?\s+PROCEDURE\s+(?P<name>` + sqlNamePattern + `)\s*\([^)]*\)`)
	routineLanguagePattern       = regexp.MustCompile(`(?is)\bLANGUAGE\s+(?P<language>[A-Za-z_][\w$]*)\b`)
	routineBodyStartPattern      = regexp.MustCompile(`(?is)\bAS\s+(?P<tag>\$\$|\$[A-Za-z_][\w$]*\$)`)
)

func appendSQLRoutineFromHeader(
	payload map[string]any,
	source string,
	options Options,
	seenEntities map[string]map[string]struct{},
	seenRelationships map[string]struct{},
	match []int,
	indexes map[string]int,
	routineKind string,
) {
	name := normalizeSQLName(submatchValue(source, match, indexes["name"]))
	statementEnd := sqlStatementEndOutsideDollarQuotes(source, match[1])
	statement := source[match[0]:statementEnd]
	body, bodyOffset, _ := extractSQLRoutineBody(statement)
	language := extractSQLRoutineLanguage(statement)
	lineNumber := sqlLineNumberForOffset(source, match[0])

	item := map[string]any{
		"name":              name,
		"line_number":       lineNumber,
		"type":              "content_entity",
		"sql_entity_type":   "SqlFunction",
		"schema":            sqlSchema(name),
		"qualified_name":    name,
		"function_language": language,
	}
	if routineKind != "function" {
		item["routine_kind"] = routineKind
	}

	appendSQLEntity(payload, seenEntities, "sql_functions", name, item, source, match[0], statementEnd, options)
	if body == "" {
		return
	}

	for _, mention := range collectSQLTableMentions(body, true) {
		appendSQLRelationship(
			payload,
			seenRelationships,
			"READS_FROM",
			name,
			mention.name,
			sqlLineNumberForOffset(source, match[0]+bodyOffset+mention.offset),
		)
	}
}

func extractSQLRoutineLanguage(statement string) string {
	match := routineLanguagePattern.FindStringSubmatchIndex(statement)
	if match == nil {
		return ""
	}
	indexes := namedCaptureIndexes(routineLanguagePattern)
	return strings.TrimSpace(submatchValue(statement, match, indexes["language"]))
}

func extractSQLRoutineBody(statement string) (string, int, bool) {
	match := routineBodyStartPattern.FindStringSubmatchIndex(statement)
	if match == nil {
		return "", 0, false
	}
	indexes := namedCaptureIndexes(routineBodyStartPattern)
	tag := submatchValue(statement, match, indexes["tag"])
	bodyStart := match[indexes["tag"]*2+1]
	bodyEnd := strings.Index(statement[bodyStart:], tag)
	if bodyEnd < 0 {
		return "", 0, false
	}
	return statement[bodyStart : bodyStart+bodyEnd], bodyStart, true
}

func sqlStatementEndOutsideDollarQuotes(source string, start int) int {
	activeTag := ""
	for index := start; index < len(source); {
		if activeTag != "" {
			if strings.HasPrefix(source[index:], activeTag) {
				index += len(activeTag)
				activeTag = ""
				continue
			}
			index++
			continue
		}
		if tag := sqlDollarQuoteTagAt(source[index:]); tag != "" {
			activeTag = tag
			index += len(tag)
			continue
		}
		if source[index] == ';' {
			return index + 1
		}
		index++
	}
	return len(source)
}

func sqlDollarQuoteTagAt(source string) string {
	if !strings.HasPrefix(source, "$") {
		return ""
	}
	closing := strings.IndexByte(source[1:], '$')
	if closing < 0 {
		return ""
	}
	tag := source[:closing+2]
	if tag == "$$" {
		return tag
	}
	inner := tag[1 : len(tag)-1]
	for index, r := range inner {
		switch {
		case index == 0 && (unicode.IsLetter(r) || r == '_'):
		case index > 0 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'):
		default:
			return ""
		}
	}
	return tag
}
