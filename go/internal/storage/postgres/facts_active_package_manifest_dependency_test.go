// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListActivePackageManifestDependencyFactsUsesActiveGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-dep-1",
					"repository:repo-1",
					"generation-1",
					"content_entity",
					"content_entity:dep-1",
					"1.0.0",
					"git",
					int64(0),
					"unknown",
					"git",
					"content_entity:dep-1",
					"file:///repo/path",
					"dep-1",
					time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"entity_type":"Variable","entity_name":"left-pad","entity_metadata":{"config_kind":"dependency","package_manager":"npm"}}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActivePackageManifestDependencyFacts(
		context.Background(),
		[]string{"npm"},
		[]string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActivePackageManifestDependencyFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "content_entity"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'content_entity'",
		"fact.source_system = 'git'",
		"fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])",
		`(LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = ANY($2::text[])`,
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $5",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

// TestFactStoreListActivePackageManifestDependencyFactsExcludesTombstones is the
// regression guard for issue #1927: a manifest-dependency content_entity fact
// tombstoned within a still-active generation must not be returned as live to
// reducer correlation consumers. The active-generation join keeps tombstoned
// facts visible (a tombstone supersedes within the same generation), so the
// read model must filter them out with an explicit predicate, matching every
// sibling active source-local reader. Two of the three consumers
// (packages/correlation/source_handler.go, securityalert/reconciliation_handler.go)
// do not filter IsTombstone themselves, so the guard belongs in the query.
func TestFactStoreListActivePackageManifestDependencyFactsExcludesTombstones(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	if _, err := store.ListActivePackageManifestDependencyFacts(
		context.Background(),
		[]string{"npm"},
		[]string{"left-pad"},
	); err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "fact.is_tombstone = FALSE") {
		t.Fatalf("query must exclude tombstoned manifest dependency facts via %q:\n%s",
			"fact.is_tombstone = FALSE", query)
	}
}

// manifestDependencyEntityNameFold is the #9568 SQL fold: it equals
// packageidentity.LowerASCII, the fold that wrote the $2 candidate set.
const manifestDependencyEntityNameFold = `(LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default")`

// manifestDependencyLocaleFoldPattern matches a LOWER(TRIM(entity_name))
// fold with no COLLATE inside it, that is a fold that follows the database
// collation and would map U+0130 to i and U+212A to k, unlike LowerASCII.
var manifestDependencyLocaleFoldPattern = regexp.MustCompile(`LOWER\(TRIM\(fact\.payload->>'entity_name'\)\)`)

// TestManifestDependencyQueryFoldsEntityNameASCIIOnly pins the #9568 SQL
// contract on both shipped queries: the entity_name prefilter and the sbt
// exclusion fold the stored name with LOWER under COLLATE "C", never a locale
// LOWER that maps U+0130 to i and U+212A to k.
func TestManifestDependencyQueryFoldsEntityNameASCIIOnly(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"direct": listActivePackageManifestDependencyFactsQuery,
		"sbt":    listActivePackageManifestDependencyFactsWithSBTQuery,
	} {
		if !strings.Contains(query, manifestDependencyEntityNameFold+" = ANY($2::text[])") {
			t.Fatalf("%s query does not fold entity_name with %s", name, manifestDependencyEntityNameFold)
		}
		if manifestDependencyLocaleFoldPattern.MatchString(query) {
			t.Fatalf("%s query keeps a locale LOWER fold; use LOWER(TRIM(x) COLLATE \"C\") COLLATE \"default\"", name)
		}
	}
	if !strings.Contains(listActivePackageManifestDependencyFactsWithSBTQuery, manifestDependencyEntityNameFold+" <> ALL($2::text[])") {
		t.Fatalf("sbt query does not fold the entity_name exclusion with %s", manifestDependencyEntityNameFold)
	}
	// Seeded-violation pair: the guard must fire on the locale fold the query
	// used before #9568 and stay quiet on the shipped COLLATE "C" form.
	if !manifestDependencyLocaleFoldPattern.MatchString("fact.payload->>'entity_name' = ANY($2) OR LOWER(TRIM(fact.payload->>'entity_name')) = ANY($2)") {
		t.Fatal("locale-fold guard does not fire on the pre-#9568 predicate")
	}
	if manifestDependencyLocaleFoldPattern.MatchString(listActivePackageManifestDependencyFactsQuery) {
		t.Fatal("locale-fold guard fires on the shipped fold")
	}
}

// TestManifestDependencyFoldedIndexStaysInBootstrapAndMigration pins migration
// 247 next to its bootstrap mirror: one concurrent build, the same partial
// predicate as the read, and no blocking index statement.
func TestManifestDependencyFoldedIndexStaysInBootstrapAndMigration(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("migrations/247_fact_records_manifest_dependency_folded_entity_index.sql")
	if err != nil {
		t.Fatalf("read folded entity index migration: %v", err)
	}
	migration := string(contents)
	for _, want := range []string{
		"fact_records_active_package_dependency_folded_entity_idx",
		`(LOWER(TRIM(fact_records.payload->>'entity_name') COLLATE "C")) COLLATE "default"`,
		"payload->'entity_metadata'->>'package_manager'",
		"payload->'entity_metadata'->>'config_kind' = 'dependency'",
	} {
		if !strings.Contains(migration, want) {
			t.Errorf("migration 247 missing %q", want)
		}
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Errorf("bootstrap schema missing %q for the folded entity index", want)
		}
	}
	if got := strings.Count(migration, "CREATE INDEX CONCURRENTLY IF NOT EXISTS"); got != 1 {
		t.Errorf("migration 247 must contain exactly one concurrent index build, got %d", got)
	}
}
