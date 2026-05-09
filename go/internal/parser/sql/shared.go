package sql

import (
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const sqlNamePattern = "(?:\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]|[A-Za-z_][\\w$]*)(?:\\s*\\.\\s*(?:\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]|[A-Za-z_][\\w$]*))*"

// Options configures one SQL parser execution.
type Options = shared.Options

var (
	sqlFromJoinPattern   = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(?P<name>` + sqlNamePattern + `)`)
	sqlUpdatePattern     = regexp.MustCompile(`(?i)\bUPDATE\s+(?P<name>` + sqlNamePattern + `)`)
	sqlInsertPattern     = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(?P<name>` + sqlNamePattern + `)`)
	sqlDeletePattern     = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(?P<name>` + sqlNamePattern + `)`)
	sqlReferencesPattern = regexp.MustCompile(`(?i)\bREFERENCES\s+(?P<name>` + sqlNamePattern + `)`)
	sqlAlterTablePattern = regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+(?P<name>` + sqlNamePattern + `)`)
)

func normalizeSQLName(raw string) string {
	parts := strings.Split(raw, ".")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := strings.TrimSpace(part)
		cleaned = strings.TrimPrefix(cleaned, `"`)
		cleaned = strings.TrimSuffix(cleaned, `"`)
		cleaned = strings.TrimPrefix(cleaned, "`")
		cleaned = strings.TrimSuffix(cleaned, "`")
		cleaned = strings.TrimPrefix(cleaned, "[")
		cleaned = strings.TrimSuffix(cleaned, "]")
		if cleaned != "" {
			normalized = append(normalized, cleaned)
		}
	}
	return strings.Join(normalized, ".")
}

func sqlLineNumberForOffset(source string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	return strings.Count(source[:offset], "\n") + 1
}

func collectSQLTableMentions(text string, includeReads bool) []sqlMention {
	patterns := []struct {
		operation string
		pattern   *regexp.Regexp
	}{
		{operation: "update", pattern: sqlUpdatePattern},
		{operation: "insert", pattern: sqlInsertPattern},
		{operation: "delete", pattern: sqlDeletePattern},
		{operation: "reference", pattern: sqlReferencesPattern},
		{operation: "alter", pattern: sqlAlterTablePattern},
	}
	if includeReads {
		patterns = append([]struct {
			operation string
			pattern   *regexp.Regexp
		}{{operation: "select", pattern: sqlFromJoinPattern}}, patterns...)
	}

	mentions := make([]sqlMention, 0)
	for _, candidate := range patterns {
		indexes := candidate.pattern.SubexpIndex("name")
		for _, match := range candidate.pattern.FindAllStringSubmatchIndex(text, -1) {
			if indexes < 0 {
				continue
			}
			start := match[indexes*2]
			end := match[indexes*2+1]
			mentions = append(mentions, sqlMention{
				name:      normalizeSQLName(text[start:end]),
				operation: candidate.operation,
				offset:    start,
			})
		}
	}
	sort.SliceStable(mentions, func(i, j int) bool {
		if mentions[i].offset != mentions[j].offset {
			return mentions[i].offset < mentions[j].offset
		}
		if mentions[i].operation != mentions[j].operation {
			return mentions[i].operation < mentions[j].operation
		}
		return mentions[i].name < mentions[j].name
	})
	return mentions
}

func isSQLTableConstraintLine(line string) bool {
	upper := strings.ToUpper(strings.TrimSpace(line))
	for _, prefix := range []string{
		"CONSTRAINT ",
		"PRIMARY KEY",
		"FOREIGN KEY",
		"UNIQUE ",
		"UNIQUE(",
		"CHECK ",
		"CHECK(",
		"EXCLUDE ",
	} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

type sqlMention struct {
	name      string
	operation string
	offset    int
}
