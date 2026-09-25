// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// migrationSQLByName returns the embedded bootstrap migration whose definition
// name is name, so binding tests read the shipped DDL rather than a copy.
func migrationSQLByName(t *testing.T, name string) string {
	t.Helper()
	for _, def := range storagepostgres.BootstrapDefinitions() {
		if def.Name == name {
			return def.SQL
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
