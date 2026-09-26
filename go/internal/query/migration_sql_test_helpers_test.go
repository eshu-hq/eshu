// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// migrationSQLByName returns the embedded bootstrap migration whose definition
// name is name, so binding tests read the shipped DDL rather than a copy. SQL
// comments are stripped: a binding must be satisfied by the statement the
// database runs, never by prose that merely mentions the same text.
func migrationSQLByName(t *testing.T, name string) string {
	t.Helper()
	for _, def := range storagepostgres.BootstrapDefinitions() {
		if def.Name == name {
			return stripSQLComments(def.SQL)
		}
	}
	t.Fatalf("bootstrap definition %q not found", name)
	return ""
}

// normalizeSQLWhitespace collapses every whitespace run to one space so a
// containment check ignores formatting differences only.
func normalizeSQLWhitespace(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// stripSQLComments removes "--" line comments and nestable "/* */" block
// comments from sql, replacing each with one space so the tokens on either
// side stay separate. Single-quoted strings (a doubled quote escapes) and double-quoted
// identifiers are copied verbatim, so comment markers inside them survive.
// Dollar-quoted bodies are not recognized; the migrations these tests bind
// carry none.
func stripSQLComments(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))
	for i := 0; i < len(sql); {
		switch c := sql[i]; {
		case c == '\'' || c == '"':
			end := i + 1
			for end < len(sql) {
				if sql[end] == c {
					if end+1 < len(sql) && sql[end+1] == c {
						end += 2
						continue
					}
					break
				}
				end++
			}
			if end < len(sql) {
				end++
			}
			out.WriteString(sql[i:end])
			i = end
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			depth := 1
			i += 2
			for i < len(sql) && depth > 0 {
				switch {
				case strings.HasPrefix(sql[i:], "/*"):
					depth++
					i += 2
				case strings.HasPrefix(sql[i:], "*/"):
					depth--
					i += 2
				default:
					i++
				}
			}
			out.WriteByte(' ')
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}
