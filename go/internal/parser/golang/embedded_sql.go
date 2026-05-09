package golang

import (
	"regexp"
	"strings"
)

var (
	goFunctionPattern  = regexp.MustCompile(`func\s+(?:\([^)]*\)\s*)?(?P<name>[A-Za-z_]\w*)\s*\([^)]*\)\s*(?:\([^)]*\)|[^{\n]+)?\{`)
	goSQLCallPattern   = regexp.MustCompile(`\.\s*(?P<call>ExecContext|Exec|QueryContext|QueryRowContext|QueryRow|QueryxContext|Queryx|GetContext|Get|SelectContext|Select)\s*\(`)
	goSQLTablePatterns = []struct {
		operation string
		pattern   *regexp.Regexp
	}{
		{
			operation: "select",
			pattern: regexp.MustCompile(
				`\b(?:FROM|JOIN)\s+(?P<name>[A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)*)`,
			),
		},
		{
			operation: "update",
			pattern: regexp.MustCompile(
				`\bUPDATE\s+(?P<name>[A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)*)`,
			),
		},
		{
			operation: "insert",
			pattern: regexp.MustCompile(
				`\bINSERT\s+INTO\s+(?P<name>[A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)*)`,
			),
		},
		{
			operation: "delete",
			pattern: regexp.MustCompile(
				`\bDELETE\s+FROM\s+(?P<name>[A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)*)`,
			),
		},
	}
)

// EmbeddedSQLQuery records a SQL table reference found in a Go string literal
// passed to a database/sql or sqlx query method.
type EmbeddedSQLQuery struct {
	FunctionName       string
	FunctionLineNumber int
	TableName          string
	Operation          string
	LineNumber         int
	API                string
}

type goStringLiteral struct {
	body   string
	offset int
}

type goFunctionBody struct {
	name        string
	body        string
	startOffset int
	lineNumber  int
}

// EmbeddedSQLQueries extracts bounded table references from SQL literals passed
// to common Go database APIs. It is intentionally conservative: unrecognized
// call sites and SQL without an obvious table name produce no query row.
func EmbeddedSQLQueries(source string) []EmbeddedSQLQuery {
	queries := make([]EmbeddedSQLQuery, 0)
	for _, function := range iterGoFunctionBodies(source) {
		for _, literal := range iterGoStringLiterals(function.body) {
			api := detectGoSQLAPIForOffset(function.body, literal.offset)
			if api == "" {
				continue
			}
			for _, candidate := range goSQLTablePatterns {
				matches := candidate.pattern.FindStringSubmatchIndex(literal.body)
				if matches == nil {
					continue
				}
				nameIndex := candidate.pattern.SubexpIndex("name")
				if nameIndex < 0 {
					continue
				}
				start := matches[2*nameIndex]
				end := matches[2*nameIndex+1]
				if start < 0 || end < 0 {
					continue
				}
				queries = append(queries, EmbeddedSQLQuery{
					FunctionName:       function.name,
					FunctionLineNumber: function.lineNumber,
					TableName:          literal.body[start:end],
					Operation:          candidate.operation,
					LineNumber: lineNumberForOffset(
						source,
						function.startOffset+literal.offset+start,
					),
					API: api,
				})
				break
			}
		}
	}
	return queries
}

func iterGoFunctionBodies(source string) []goFunctionBody {
	matches := goFunctionPattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return nil
	}

	bodies := make([]goFunctionBody, 0, len(matches))
	nameIndex := goFunctionPattern.SubexpIndex("name")
	for _, match := range matches {
		openBrace := strings.IndexByte(source[match[0]:], '{')
		if openBrace < 0 {
			continue
		}
		openIndex := match[0] + openBrace
		closeIndex := matchingBraceIndex(source, openIndex)
		if closeIndex < 0 {
			continue
		}
		bodyStart := openIndex + 1
		bodies = append(bodies, goFunctionBody{
			name:        source[match[2*nameIndex]:match[2*nameIndex+1]],
			body:        source[bodyStart:closeIndex],
			startOffset: bodyStart,
			lineNumber:  lineNumberForOffset(source, match[0]),
		})
	}
	return bodies
}

func matchingBraceIndex(source string, openIndex int) int {
	depth := 0
	for index := openIndex; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func iterGoStringLiterals(source string) []goStringLiteral {
	literals := make([]goStringLiteral, 0)
	for index := 0; index < len(source); {
		switch source[index] {
		case '`':
			end := strings.IndexByte(source[index+1:], '`')
			if end < 0 {
				return literals
			}
			bodyStart := index + 1
			bodyEnd := bodyStart + end
			literals = append(literals, goStringLiteral{
				body:   source[bodyStart:bodyEnd],
				offset: bodyStart,
			})
			index = bodyEnd + 1
		case '"':
			bodyStart := index + 1
			index++
			builder := strings.Builder{}
			for index < len(source) {
				current := source[index]
				if current == '\\' && index+1 < len(source) {
					builder.WriteByte(current)
					builder.WriteByte(source[index+1])
					index += 2
					continue
				}
				if current == '"' {
					literals = append(literals, goStringLiteral{
						body:   builder.String(),
						offset: bodyStart,
					})
					index++
					break
				}
				builder.WriteByte(current)
				index++
			}
		default:
			index++
		}
	}
	return literals
}

func detectGoSQLAPIForOffset(functionBody string, literalOffset int) string {
	matches := goSQLCallPattern.FindAllStringSubmatchIndex(functionBody[:literalOffset], -1)
	if len(matches) == 0 {
		return ""
	}
	callIndex := goSQLCallPattern.SubexpIndex("call")
	last := matches[len(matches)-1]
	return sqlAPIForCallName(functionBody[last[2*callIndex]:last[2*callIndex+1]])
}

func sqlAPIForCallName(callName string) string {
	switch callName {
	case "QueryxContext", "Queryx", "GetContext", "Get", "SelectContext", "Select":
		return "sqlx"
	default:
		return "database/sql"
	}
}

func lineNumberForOffset(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}
