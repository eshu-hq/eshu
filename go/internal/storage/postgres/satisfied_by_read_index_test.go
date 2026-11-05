// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"
)

// crossplaneXRDIndexName is the partial index migration 280 adds (#10005).
const crossplaneXRDIndexName = "fact_records_crossplane_xrd_entity_idx"

// sqlWhitespace collapses runs of whitespace for text comparisons.
var sqlWhitespace = regexp.MustCompile(`\s+`)

// sqlLineComment matches a -- comment through end of line.
var sqlLineComment = regexp.MustCompile(`--[^\n]*`)

func normalizeSQL(text string) string {
	return strings.TrimSpace(sqlWhitespace.ReplaceAllString(sqlLineComment.ReplaceAllString(text, ""), " "))
}

// TestCrossplaneXRDIndexPredicateMatchesTheReader pins the partial index's
// predicate to the XRD reader text it serves: every predicate term appears
// in the reader as the same text on the fact alias, so the planner can prove
// the implication for the active and the derived base text. It also pins
// migration 280 to the Go DDL, modulo CONCURRENTLY.
func TestCrossplaneXRDIndexPredicateMatchesTheReader(t *testing.T) {
	t.Parallel()

	ddl := normalizeSQL(crossplaneXRDFactRecordReadIndexesSQL)
	where := strings.Index(ddl, " WHERE ")
	if where < 0 {
		t.Fatalf("Go DDL has no partial predicate: %s", ddl)
	}
	predicate := strings.TrimSuffix(ddl[where+len(" WHERE "):], ";")
	aliased := regexp.MustCompile(`\b(fact_kind|source_system|is_tombstone|payload)\b`).ReplaceAllString(predicate, "fact.$1")
	base, err := deriveDeltaOverlayBaseQuery(listActiveCrossplaneXRDFactsQuery, crossplaneXRDDeltaOverlayScopeParam)
	if err != nil {
		t.Fatal(err)
	}
	for name, reader := range map[string]string{"active": listActiveCrossplaneXRDFactsQuery, "base": base} {
		normalized := normalizeSQL(reader)
		for _, term := range strings.Split(aliased, " AND ") {
			if !strings.Contains(normalized, strings.TrimSpace(term)) {
				t.Fatalf("%s reader lacks index predicate term %q:\n%s", name, term, normalized)
			}
		}
	}
	if !strings.Contains(ddl, "ON fact_records (scope_id, generation_id, observed_at, fact_id)") {
		t.Fatalf("Go DDL key columns drifted from the readers' join prefix and keyset order: %s", ddl)
	}

	var migration string
	for _, def := range BootstrapDefinitions() {
		if def.Name == crossplaneXRDIndexName {
			migration = def.SQL
		}
	}
	if migration == "" {
		t.Fatalf("no bootstrap definition named %s", crossplaneXRDIndexName)
	}
	if got, want := strings.Replace(normalizeSQL(migration), "CREATE INDEX CONCURRENTLY IF NOT EXISTS", "CREATE INDEX IF NOT EXISTS", 1), ddl; got != want {
		t.Fatalf("migration 280 and the Go DDL differ:\nmigration: %s\nGo DDL:    %s", got, want)
	}
}
